package shardmaster

//
// Shardmaster clerk.
// Please don't change this file.
//

import "net/rpc"
import "time"
import "fmt"

type Clerk struct {
	servers []string // shardmaster replicas
}

func MakeClerk(servers []string) *Clerk {
	ck := new(Clerk)
	ck.servers = servers
	return ck
}

//
// call() sends an RPC to the rpcname handler on server srv
// with arguments args, waits for the reply, and leaves the
// reply in reply. the reply argument should be a pointer
// to a reply structure.
//
// the return value is true if the server responded, and false
// if call() was not able to contact the server. in particular,
// the reply's contents are only valid if call() returned true.
//
// you should assume that call() will return an
// error after a while if the server is dead.
// don't provide your own time-out mechanism.
//
// please use call() to send all RPCs, in client.go and server.go.
// please don't change this function.
//
func call(srv string, rpcname string,
	args interface{}, reply interface{}) bool {
	c, errx := rpc.Dial("unix", srv)
	if errx != nil {
		return false
	}
	defer c.Close()

	err := c.Call(rpcname, args, reply)
	if err == nil {
		return true
	}

	fmt.Println(err)
	return false
}

func (ck *Clerk) Query(num int) Config {
	for {
		// try each known server.
		for _, srv := range ck.servers {
			args := &QueryArgs{}
			args.Num = num
			var reply QueryReply
			ok := call(srv, "ShardMaster.Query", args, &reply)
			if ok {
				return reply.Config
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (ck *Clerk) Join(gid int64, servers []string) {
	for {
		// try each known server.
		for _, srv := range ck.servers {
			args := &JoinArgs{}
			args.GID = gid
			args.Servers = servers
			var reply JoinReply
			ok := call(srv, "ShardMaster.Join", args, &reply)
			if ok {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (ck *Clerk) Leave(gid int64) {
	for {
		// try each known server.
		for _, srv := range ck.servers {
			args := &LeaveArgs{}
			args.GID = gid
			var reply LeaveReply
			ok := call(srv, "ShardMaster.Leave", args, &reply)
			if ok {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (ck *Clerk) Move(shard int, gid int64) {
	for {
		// try each known server.
		for _, srv := range ck.servers {
			args := &MoveArgs{}
			args.Shard = shard
			args.GID = gid
			var reply MoveReply
			ok := call(srv, "ShardMaster.Move", args, &reply)
			if ok {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}
