package raft

//
// support for Raft tester.
//
// we will use the original config.go to test your code for grading.
// so, while you can modify this code to help you debug, please
// test with the original before submitting.
//
// 我们使用最初的config.go文件分级测试你们的代码,所以你们可与修改这边的代码帮助你们调试，
// 请在提交之前测试。

import "labrpc"
import "log"
import "sync"
import "testing"
import "runtime"
import crand "crypto/rand"
import "encoding/base64"
import "sync/atomic"
import "time"
import "fmt"

// 产生随机字符串
func randstring(n int) string {
	b := make([]byte, 2*n)  // 为什么申请了两倍空间？
	crand.Read(b)
	s := base64.URLEncoding.EncodeToString(b)
	return s[0:n]
}

// 构建测试环境对象
type config struct {
	mu        sync.Mutex
	t         *testing.T
	net       *labrpc.Network 	// 模拟网络
	n         int			// 服务器的数量
	done      int32 		// tell internal threads to die
	// 下面slice的长度都为n
	rafts     []*Raft		// 管理主机实例,数量为n
	applyErr  []string 		// from apply channel readers
	connected []bool   		// whether each server is on the net
	saved     []*Persister		// 数据持久化：Raft状态值和快照数据
	endnames  [][]string   		// the port file names each sends to
	logs      []map[int]int 	// copy of each server's committed entries,每台服务器的日志副本
}

// 构建测试环境
func make_config(t *testing.T, n int, unreliable bool) *config {
	runtime.GOMAXPROCS(4)
	cfg := &config{}
	cfg.t = t
	cfg.net = labrpc.MakeNetwork()		// 构建测试网络
	cfg.n = n
	cfg.applyErr = make([]string, cfg.n)
	cfg.rafts = make([]*Raft, cfg.n)	//  构建cfg.n个Raft实例
	cfg.connected = make([]bool, cfg.n)
	cfg.saved = make([]*Persister, cfg.n)
	cfg.endnames = make([][]string, cfg.n)
	cfg.logs = make([]map[int]int, cfg.n)

	cfg.setunreliable(unreliable)	// 设置网络为不可靠网络

	cfg.net.LongDelays(true)	// 存在长延时的网络

	// create a full set of Rafts.
	// 创建raft集合(cfg.n × cfg.n个端点，因为需要互相连接),cfg.n个Raft实例
	for i := 0; i < cfg.n; i++ {
		cfg.logs[i] = map[int]int{}
		cfg.start1(i)	// 构建节点之间的相互连接关系（创建Raft实例）
	}

	// connect everyone
	// 连接每一个raft实例
	for i := 0; i < cfg.n; i++ {
		cfg.connect(i)
	}

	return cfg
}

// shut down a Raft server but save its persistent state.
func (cfg *config) crash1(i int) {
	cfg.disconnect(i)
	cfg.net.DeleteServer(i) // disable client connections to the server.

	cfg.mu.Lock()
	defer cfg.mu.Unlock()

	// a fresh persister, in case old instance
	// continues to update the Persister.
	// but copy old persister's content so that we always
	// pass Make() the last persisted state.
	if cfg.saved[i] != nil {
		cfg.saved[i] = cfg.saved[i].Copy()
	}

	rf := cfg.rafts[i]
	if rf != nil {
		cfg.mu.Unlock()
		rf.Kill()
		cfg.mu.Lock()
		cfg.rafts[i] = nil
	}

	if cfg.saved[i] != nil {
		raftlog := cfg.saved[i].ReadRaftState()
		cfg.saved[i] = &Persister{}
		cfg.saved[i].SaveRaftState(raftlog)
	}
}

//
// start or re-start a Raft.
// if one already exists, "kill" it first.
// allocate new outgoing port file names, and a new
// state persister, to isolate previous instance of
// this server. since we cannot really kill it.
//
// 启动或者重新启动一个Raft实例,如果实例已经存在那么我们下“杀死”（译注：应该只是隔离，重新启动一个新的raft实例）。
// 重新分配一个新的输出端口的文件名(???)和一个新的状态持久化对象，为了隔离之前本机的服务器实例。
// 因为我们不能杀死它。
func (cfg *config) start1(i int) {
	cfg.crash1(i)

	// a fresh set of outgoing ClientEnd names.
	// so that old crashed instance's ClientEnds can't send.
	// 一套新鲜的对外ClientEnd名字，所以旧的崩溃实例ClientEnds不能发送。
	cfg.endnames[i] = make([]string, cfg.n)
	for j := 0; j < cfg.n; j++ {
		cfg.endnames[i][j] = randstring(20) // 随机产生的名字,这边产生 cfg.n个名字(需要连接到其他的服务器，创建不同的端点)
	}

	// a fresh set of ClientEnds.
	// 新鲜的ClientEnds集合
	ends := make([]*labrpc.ClientEnd, cfg.n)
	for j := 0; j < cfg.n; j++ {
		ends[j] = cfg.net.MakeEnd(cfg.endnames[i][j]) 	// 创建名为cfg.endnames[i][j]的ClientEnd
		cfg.net.Connect(cfg.endnames[i][j], j)		// cfg.n个节点连接到j（在这边服务器是0,1,2）
		// cfg.endnames[i][j]：索引j表示连接到的服务器，索引i为自身
		// 0 : 0->0 0->1 0->2	00 01 02
		// 1 : 1->0 1->1 1->2	10 11 12
		// 2 : 2->0 2->1 2->2	20 21 22
	}

	cfg.mu.Lock()

	// a fresh persister, so old instance doesn't overwrite
	// new instance's persisted state.
	// but copy old persister's content so that we always
	// pass Make() the last persisted state.
	// 一个新的持久化对象，
	if cfg.saved[i] != nil {
		cfg.saved[i] = cfg.saved[i].Copy()
	} else {
		cfg.saved[i] = MakePersister()
	}

	cfg.mu.Unlock()

	// listen to messages from Raft indicating newly committed messages.
	// 创建channel处理消息
	applyCh := make(chan ApplyMsg)
	go func() {
		for m := range applyCh {
			err_msg := ""
			if m.UseSnapshot {
				// ignore the snapshot
				// 忽略快照
			} else if v, ok := (m.Command).(int); ok {
				cfg.mu.Lock()
				// 日志处理

				// 遍历全部的日志系列
				for j := 0; j < len(cfg.logs); j++ {
					if old, oldok := cfg.logs[j][m.Index]; oldok && old != v {
						// some server has already committed a different value for this entry!
						err_msg = fmt.Sprintf("commit index=%v server=%v %v != server=%v %v",
							m.Index, i, m.Command, j, old)
					}
				}

				// 添加日志
				_, prevok := cfg.logs[i][m.Index-1] // map中是否存在key：m.Index-1
				cfg.logs[i][m.Index] = v
				cfg.mu.Unlock()

				// 索引肯定是从开始，排除是一个情况
				// 如果之前的不存在，那么这台服务上面的日志已经乱序
				if m.Index > 1 && prevok == false {
					err_msg = fmt.Sprintf("server %v apply out of order %v", i, m.Index)
				}
			} else {
				err_msg = fmt.Sprintf("committed command %v is not an int", m.Command)
			}

			if err_msg != "" {
				log.Fatalf("apply error: %v\n", err_msg)
				cfg.applyErr[i] = err_msg
				// keep reading after error so that Raft doesn't block
				// holding locks...
			}
		}
	}()

	// 创建Raft实例,启动心跳准备超时进行选举
	rf := Make(ends, i, cfg.saved[i], applyCh)

	// 记录自己的位置
	cfg.mu.Lock()
	cfg.rafts[i] = rf
	cfg.mu.Unlock()

	// 创建提供Raft相关方法的实例，添加到本网络
	svc := labrpc.MakeService(rf)
	srv := labrpc.MakeServer()
	srv.AddService(svc)
	cfg.net.AddServer(i, srv)
}

func (cfg *config) cleanup() {
	for i := 0; i < len(cfg.rafts); i++ {
		if cfg.rafts[i] != nil {
			cfg.rafts[i].Kill()
		}
	}
	atomic.StoreInt32(&cfg.done, 1)
}

// attach server i to the net.
// 使服务器在此网络上线
func (cfg *config) connect(i int) {
	// fmt.Printf("connect(%d)\n", i)

	cfg.connected[i] = true

	// func (cfg *config) start1(i int)构建的连接关系
	// cfg.endnames[i][j]：索引j表示连接到的服务器，索引i为自身
	// 0 : 0->0 0->1 0->2	00 01 02
	// 1 : 1->0 1->1 1->2	10 11 12
	// 2 : 2->0 2->1 2->2	20 21 22

	// outgoing ClientEnds
	// cfg.endnames[i][j] 是主动连接上来的，所以是outgoing ClientEnds
	for j := 0; j < cfg.n; j++ {
		if cfg.connected[j] {
			endname := cfg.endnames[i][j]
			cfg.net.Enable(endname, true)	// 使连接到这个服务器的客户端上线
		}
	}

	// incoming ClientEnds
	// cfg.endnames[j][i] 是被动连接， 所以是incoming ClientEnds
	for j := 0; j < cfg.n; j++ {
		if cfg.connected[j] {
			endname := cfg.endnames[j][i]
			cfg.net.Enable(endname, true)
		}
	}
}

// detach server i from the net.
func (cfg *config) disconnect(i int) {
	// fmt.Printf("disconnect(%d)\n", i)

	cfg.connected[i] = false

	// outgoing ClientEnds
	for j := 0; j < cfg.n; j++ {
		if cfg.endnames[i] != nil {
			endname := cfg.endnames[i][j]
			cfg.net.Enable(endname, false)
		}
	}

	// incoming ClientEnds
	for j := 0; j < cfg.n; j++ {
		if cfg.endnames[j] != nil {
			endname := cfg.endnames[j][i]
			cfg.net.Enable(endname, false)
		}
	}
}

func (cfg *config) rpcCount(server int) int {
	return cfg.net.GetCount(server)
}

func (cfg *config) setunreliable(unrel bool) {
	cfg.net.Reliable(!unrel)
}

func (cfg *config) setlongreordering(longrel bool) {
	cfg.net.LongReordering(longrel)
}

// check that there's exactly one leader.
// try a few times in case re-elections are needed.
// 检测是否存在领导者， 尝试几次，以便需要重新选举。
func (cfg *config) checkOneLeader() int {
	for iters := 0; iters < 10; iters++ {
		time.Sleep(500 * time.Millisecond)

		leaders := make(map[int][]int)	// key:currentTerm, value: 切片类型(Raft实例的索引)
		for i := 0; i < cfg.n; i++ {
			if cfg.connected[i] {
				if t, leader := cfg.rafts[i].GetState(); leader {
					leaders[t] = append(leaders[t], i)   // 获取领导者个数
				}
			}
		}

		lastTermWithLeader := -1
		for t, lds := range leaders {
			if len(lds) > 1 {
				cfg.t.Fatalf("term %d has %d (>1) leaders", t, len(lds))
			}
			if t > lastTermWithLeader {
				lastTermWithLeader = t
			}
		}

		if len(leaders) != 0 {
			return leaders[lastTermWithLeader][0]  // 找到领导者，并返回领导者索引
		}
	}
	cfg.t.Fatalf("expected one leader, got none")
	return -1
}

// check that everyone agrees on the term.
func (cfg *config) checkTerms() int {
	term := -1
	for i := 0; i < cfg.n; i++ {
		if cfg.connected[i] {
			xterm, _ := cfg.rafts[i].GetState()
			if term == -1 {
				term = xterm
			} else if term != xterm {
				cfg.t.Fatalf("servers disagree on term")
			}
		}
	}
	return term
}

// check that there's no leader
func (cfg *config) checkNoLeader() {
	for i := 0; i < cfg.n; i++ {
		if cfg.connected[i] {
			_, is_leader := cfg.rafts[i].GetState()
			if is_leader {
				cfg.t.Fatalf("expected no leader, but %v claims to be leader", i)
			}
		}
	}
}

// how many servers think a log entry is committed?
func (cfg *config) nCommitted(index int) (int, interface{}) {
	count := 0
	cmd := -1
	for i := 0; i < len(cfg.rafts); i++ {
		if cfg.applyErr[i] != "" {
			cfg.t.Fatal(cfg.applyErr[i])
		}

		cfg.mu.Lock()
		cmd1, ok := cfg.logs[i][index]
		cfg.mu.Unlock()

		if ok {
			if count > 0 && cmd != cmd1 {
				cfg.t.Fatalf("committed values do not match: index %v, %v, %v\n",
					index, cmd, cmd1)
			}
			count += 1
			cmd = cmd1
		}
	}
	return count, cmd
}

// wait for at least n servers to commit.
// but don't wait forever.
func (cfg *config) wait(index int, n int, startTerm int) interface{} {
	to := 10 * time.Millisecond
	for iters := 0; iters < 30; iters++ {
		nd, _ := cfg.nCommitted(index)
		if nd >= n {
			break
		}
		time.Sleep(to)
		if to < time.Second {
			to *= 2
		}
		if startTerm > -1 {
			for _, r := range cfg.rafts {
				if t, _ := r.GetState(); t > startTerm {
					// someone has moved on
					// can no longer guarantee that we'll "win"
					return -1
				}
			}
		}
	}
	nd, cmd := cfg.nCommitted(index)
	if nd < n {
		cfg.t.Fatalf("only %d decided for index %d; wanted %d\n",
			nd, index, n)
	}
	return cmd
}

// do a complete agreement.
// it might choose the wrong leader initially,
// and have to re-submit after giving up.
// entirely gives up after about 10 seconds.
// indirectly checks that the servers agree on the
// same value, since nCommitted() checks this,
// as do the threads that read from applyCh.
// returns index.
func (cfg *config) one(cmd int, expectedServers int) int {
	t0 := time.Now()
	starts := 0
	for time.Since(t0).Seconds() < 10 {
		// try all the servers, maybe one is the leader.
		index := -1
		for si := 0; si < cfg.n; si++ {
			starts = (starts + 1) % cfg.n
			var rf *Raft
			cfg.mu.Lock()
			if cfg.connected[starts] {
				rf = cfg.rafts[starts]
			}
			cfg.mu.Unlock()
			if rf != nil {
				index1, _, ok := rf.Start(cmd)
				if ok {
					index = index1
					break
				}
			}
		}

		if index != -1 {
			// somebody claimed to be the leader and to have
			// submitted our command; wait a while for agreement.
			t1 := time.Now()
			for time.Since(t1).Seconds() < 2 {
				nd, cmd1 := cfg.nCommitted(index)
				if nd > 0 && nd >= expectedServers {
					// committed
					if cmd2, ok := cmd1.(int); ok && cmd2 == cmd {
						// and it was the command we submitted.
						return index
					}
				}
				time.Sleep(20 * time.Millisecond)
			}
		} else {
			time.Sleep(50 * time.Millisecond)
		}
	}
	cfg.t.Fatalf("one(%v) failed to reach agreement", cmd)
	return -1
}
