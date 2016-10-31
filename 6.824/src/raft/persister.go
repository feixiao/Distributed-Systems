package raft

//
// support for Raft and kvraft to save persistent
// Raft state (log &c) and k/v server snapshots.
//
// we will use the original persister.go to test your code for grading.
// so, while you can modify this code to help you debug, please
// test with the original before submitting.
//

import "sync"

// 持久化对象
type Persister struct {
	mu        sync.Mutex	// 锁保护
	raftstate []byte 	// Raft状态值
	snapshot  []byte	// 快照数据
}

// 创建
func MakePersister() *Persister {
	return &Persister{}
}

// 拷贝持久化对象
func (ps *Persister) Copy() *Persister {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	np := MakePersister()
	// 居然是浅拷贝,数据变化相互影响
	np.raftstate = ps.raftstate
	np.snapshot = ps.snapshot
	return np
}

// 保存数据到持久化对象
func (ps *Persister) SaveRaftState(data []byte) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.raftstate = data
}

// 获取持久化数据
func (ps *Persister) ReadRaftState() []byte {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.raftstate
}

// 获取Raft状态数据的大小
func (ps *Persister) RaftStateSize() int {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return len(ps.raftstate)
}

// 保存快照数据
func (ps *Persister) SaveSnapshot(snapshot []byte) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.snapshot = snapshot
}

// 获取快照数据
func (ps *Persister) ReadSnapshot() []byte {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.snapshot
}
