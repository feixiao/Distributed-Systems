package viewservice

import "net/rpc"
import "fmt"

//
// the viewservice Clerk lives in the client
// and maintains a little state.
//
type Clerk struct {
	me     string // client's name (host:port)
	server string // viewservice's host:port
}

func MakeClerk(me string, server string) *Clerk {
	ck := new(Clerk)
	ck.me = me
	ck.server = server
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

func (ck *Clerk) Ping(viewnum uint) (View, error) {
	// prepare the arguments.
	args := &PingArgs{}
	args.Me = ck.me
	args.Viewnum = viewnum
	var reply PingReply

	// send an RPC request, wait for the reply.
	ok := call(ck.server, "ViewServer.Ping", args, &reply)
	if ok == false {
		return View{}, fmt.Errorf("Ping(%v) failed", viewnum)
	}

	return reply.View, nil
}

func (ck *Clerk) Get() (View, bool) {
	args := &GetArgs{}
	var reply GetReply
	ok := call(ck.server, "ViewServer.Get", args, &reply)
	if ok == false {
		return View{}, false
	}
	return reply.View, true
}

func (ck *Clerk) Primary() string {
	v, ok := ck.Get()
	if ok {
		return v.Primary
	}
	return ""
}
