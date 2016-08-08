package mapreduce

import (
	"fmt"
	"log"
	"net"
	"net/rpc"
	"os"
)

// Shutdown is an RPC method that shuts down the Master's RPC server.
// shotdown是一个RPC方法，用于关闭master rpc服务器。
func (mr *Master) Shutdown(_, _ *struct{}) error {
	debug("Shutdown: registration server\n")
	close(mr.shutdown)
	mr.l.Close() // causes the Accept to fail
	return nil
}

// startRPCServer starts the Master's RPC server. It continues accepting RPC
// calls (Register in particular) for as long as the worker is alive.
// startServer是开启master rpc服务。不断的接受来自worker的rpc调用。
func (mr *Master) startRPCServer() {
	rpcs := rpc.NewServer()
	rpcs.Register(mr) // 注册自己的方法
	os.Remove(mr.address) // only needed for "unix"
	l, e := net.Listen("unix", mr.address)  // unix套接字，套接字和文件绑定
	if e != nil {
		log.Fatal("RegstrationServer", mr.address, " error: ", e)
	}
	mr.l = l

	// now that we are listening on the master address, can fork off
	// accepting connections to another thread.
	go func() {
	loop:
		for {
			select {
			case <-mr.shutdown: // close的时候退出
				break loop
			default:
			}
			conn, err := mr.l.Accept()
			if err == nil {
				go func() {
					rpcs.ServeConn(conn)
					conn.Close()
				}()
			} else {
				debug("RegistrationServer: accept error", err)
				break
			}
		}
		debug("RegistrationServer: done\n")
	}()
}

// stopRPCServer stops the master RPC server.
// This must be done through an RPC to avoid race conditions between the RPC
// server thread and the current thread.
// 关闭rpc 服务的方法。
func (mr *Master) stopRPCServer() {
	var reply ShutdownReply
	ok := call(mr.address, "Master.Shutdown", new(struct{}), &reply)
	if ok == false {
		fmt.Printf("Cleanup: RPC %s error\n", mr.address)
	}
	debug("cleanupRegistration: done\n")
}
