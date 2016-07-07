package pbservice

import "viewservice"
import "fmt"
import "io"
import "net"
import "testing"
import "time"
import "log"
import "runtime"
import "math/rand"
import "os"
import "sync"
import "strconv"
import "strings"
import "sync/atomic"

func check(ck *Clerk, key string, value string) {
	v := ck.Get(key)
	if v != value {
		log.Fatalf("Get(%v) -> %v, expected %v", key, v, value)
	}
}

func port(tag string, host int) string {
	s := "/var/tmp/824-"
	s += strconv.Itoa(os.Getuid()) + "/"
	os.Mkdir(s, 0777)
	s += "pb-"
	s += strconv.Itoa(os.Getpid()) + "-"
	s += tag + "-"
	s += strconv.Itoa(host)
	return s
}

func TestBasicFail(t *testing.T) {
	runtime.GOMAXPROCS(4)

	tag := "basic"
	vshost := port(tag+"v", 1)
	vs := viewservice.StartServer(vshost)
	time.Sleep(time.Second)
	vck := viewservice.MakeClerk("", vshost)

	ck := MakeClerk(vshost, "")

	fmt.Printf("Test: Single primary, no backup ...\n")

	s1 := StartServer(vshost, port(tag, 1))

	deadtime := viewservice.PingInterval * viewservice.DeadPings
	time.Sleep(deadtime * 2)
	if vck.Primary() != s1.me {
		t.Fatal("first primary never formed view")
	}

	ck.Put("111", "v1")
	check(ck, "111", "v1")

	ck.Put("2", "v2")
	check(ck, "2", "v2")

	ck.Put("1", "v1a")
	check(ck, "1", "v1a")

	ck.Append("ak", "hello")
	check(ck, "ak", "hello")
	ck.Put("ak", "xx")
	ck.Append("ak", "yy")
	check(ck, "ak", "xxyy")

	fmt.Printf("  ... Passed\n")

	// add a backup

	fmt.Printf("Test: Add a backup ...\n")

	s2 := StartServer(vshost, port(tag, 2))
	for i := 0; i < viewservice.DeadPings*2; i++ {
		v, _ := vck.Get()
		if v.Backup == s2.me {
			break
		}
		time.Sleep(viewservice.PingInterval)
	}
	v, _ := vck.Get()
	if v.Backup != s2.me {
		t.Fatal("backup never came up")
	}

	ck.Put("3", "33")
	check(ck, "3", "33")

	// give the backup time to initialize
	time.Sleep(3 * viewservice.PingInterval)

	ck.Put("4", "44")
	check(ck, "4", "44")

	fmt.Printf("  ... Passed\n")

	fmt.Printf("Test: Count RPCs to viewserver ...\n")

	// verify that the client or server doesn't contact the
	// viewserver for every request -- i.e. that both client
	// and servers cache the current view and only refresh
	// it when something seems to be wrong. this test allows
	// each server to Ping() the viewserver 10 times / second.

	count1 := int(vs.GetRPCCount())
	t1 := time.Now()
	for i := 0; i < 100; i++ {
		ck.Put("xk"+strconv.Itoa(i), strconv.Itoa(i))
	}
	count2 := int(vs.GetRPCCount())
	t2 := time.Now()
	dt := t2.Sub(t1)
	allowed := 2 * (dt / (100 * time.Millisecond)) // two servers tick()ing 10/second
	if (count2 - count1) > int(allowed)+20 {
		t.Fatal("too many viewserver RPCs")
	}

	fmt.Printf("  ... Passed\n")

	// kill the primary

	fmt.Printf("Test: Primary failure ...\n")

	s1.kill()
	for i := 0; i < viewservice.DeadPings*2; i++ {
		v, _ := vck.Get()
		if v.Primary == s2.me {
			break
		}
		time.Sleep(viewservice.PingInterval)
	}
	v, _ = vck.Get()
	if v.Primary != s2.me {
		t.Fatal("backup never switched to primary")
	}

	check(ck, "1", "v1a")
	check(ck, "3", "33")
	check(ck, "4", "44")

	fmt.Printf("  ... Passed\n")

	// kill solo server, start new server, check that
	// it does not start serving as primary

	fmt.Printf("Test: Kill last server, new one should not be active ...\n")

	s2.kill()
	s3 := StartServer(vshost, port(tag, 3))
	time.Sleep(1 * time.Second)
	get_done := make(chan bool)
	go func() {
		ck.Get("1")
		get_done <- true
	}()

	select {
	case <-get_done:
		t.Fatalf("ck.Get() returned even though no initialized primary")
	case <-time.After(2 * time.Second):
	}

	fmt.Printf("  ... Passed\n")

	s1.kill()
	s2.kill()
	s3.kill()
	time.Sleep(time.Second)
	vs.Kill()
	time.Sleep(time.Second)
}

func TestAtMostOnce(t *testing.T) {
	runtime.GOMAXPROCS(4)

	tag := "tamo"
	vshost := port(tag+"v", 1)
	vs := viewservice.StartServer(vshost)
	time.Sleep(time.Second)
	vck := viewservice.MakeClerk("", vshost)

	fmt.Printf("Test: at-most-once Append; unreliable ...\n")

	const nservers = 1
	var sa [nservers]*PBServer
	for i := 0; i < nservers; i++ {
		sa[i] = StartServer(vshost, port(tag, i+1))
		sa[i].setunreliable(true)
	}

	for iters := 0; iters < viewservice.DeadPings*2; iters++ {
		view, _ := vck.Get()
		if view.Primary != "" && view.Backup != "" {
			break
		}
		time.Sleep(viewservice.PingInterval)
	}

	// give p+b time to ack, initialize
	time.Sleep(viewservice.PingInterval * viewservice.DeadPings)

	ck := MakeClerk(vshost, "")
	k := "counter"
	val := ""
	for i := 0; i < 100; i++ {
		v := strconv.Itoa(i)
		ck.Append(k, v)
		val = val + v
	}

	v := ck.Get(k)
	if v != val {
		t.Fatalf("ck.Get() returned %v but expected %v\n", v, val)
	}

	fmt.Printf("  ... Passed\n")

	for i := 0; i < nservers; i++ {
		sa[i].kill()
	}
	time.Sleep(time.Second)
	vs.Kill()
	time.Sleep(time.Second)
}

// Put right after a backup dies.
func TestFailPut(t *testing.T) {
	runtime.GOMAXPROCS(4)

	tag := "failput"
	vshost := port(tag+"v", 1)
	vs := viewservice.StartServer(vshost)
	time.Sleep(time.Second)
	vck := viewservice.MakeClerk("", vshost)

	s1 := StartServer(vshost, port(tag, 1))
	time.Sleep(time.Second)
	s2 := StartServer(vshost, port(tag, 2))
	time.Sleep(time.Second)
	s3 := StartServer(vshost, port(tag, 3))

	for i := 0; i < viewservice.DeadPings*3; i++ {
		v, _ := vck.Get()
		if v.Primary != "" && v.Backup != "" {
			break
		}
		time.Sleep(viewservice.PingInterval)
	}
	time.Sleep(time.Second) // wait for backup initializion
	v1, _ := vck.Get()
	if v1.Primary != s1.me || v1.Backup != s2.me {
		t.Fatalf("wrong primary or backup")
	}

	ck := MakeClerk(vshost, "")

	ck.Put("a", "aa")
	ck.Put("b", "bb")
	ck.Put("c", "cc")
	check(ck, "a", "aa")
	check(ck, "b", "bb")
	check(ck, "c", "cc")

	// kill backup, then immediate Put
	fmt.Printf("Test: Put() immediately after backup failure ...\n")
	s2.kill()
	ck.Put("a", "aaa")
	check(ck, "a", "aaa")

	for i := 0; i < viewservice.DeadPings*3; i++ {
		v, _ := vck.Get()
		if v.Viewnum > v1.Viewnum && v.Primary != "" && v.Backup != "" {
			break
		}
		time.Sleep(viewservice.PingInterval)
	}
	time.Sleep(time.Second) // wait for backup initialization
	v2, _ := vck.Get()
	if v2.Primary != s1.me || v2.Backup != s3.me {
		t.Fatal("wrong primary or backup")
	}

	check(ck, "a", "aaa")
	fmt.Printf("  ... Passed\n")

	// kill primary, then immediate Put
	fmt.Printf("Test: Put() immediately after primary failure ...\n")
	s1.kill()
	ck.Put("b", "bbb")
	check(ck, "b", "bbb")

	for i := 0; i < viewservice.DeadPings*3; i++ {
		v, _ := vck.Get()
		if v.Viewnum > v2.Viewnum && v.Primary != "" {
			break
		}
		time.Sleep(viewservice.PingInterval)
	}
	time.Sleep(time.Second)

	check(ck, "a", "aaa")
	check(ck, "b", "bbb")
	check(ck, "c", "cc")
	fmt.Printf("  ... Passed\n")

	s1.kill()
	s2.kill()
	s3.kill()
	time.Sleep(viewservice.PingInterval * 2)
	vs.Kill()
}

// do a bunch of concurrent Put()s on the same key,
// then check that primary and backup have identical values.
// i.e. that they processed the Put()s in the same order.
func TestConcurrentSame(t *testing.T) {
	runtime.GOMAXPROCS(4)

	tag := "cs"
	vshost := port(tag+"v", 1)
	vs := viewservice.StartServer(vshost)
	time.Sleep(time.Second)
	vck := viewservice.MakeClerk("", vshost)

	fmt.Printf("Test: Concurrent Put()s to the same key ...\n")

	const nservers = 2
	var sa [nservers]*PBServer
	for i := 0; i < nservers; i++ {
		sa[i] = StartServer(vshost, port(tag, i+1))
	}

	for iters := 0; iters < viewservice.DeadPings*2; iters++ {
		view, _ := vck.Get()
		if view.Primary != "" && view.Backup != "" {
			break
		}
		time.Sleep(viewservice.PingInterval)
	}

	// give p+b time to ack, initialize
	time.Sleep(viewservice.PingInterval * viewservice.DeadPings)

	done := int32(0)

	view1, _ := vck.Get()
	const nclients = 3
	const nkeys = 2
	for xi := 0; xi < nclients; xi++ {
		go func(i int) {
			ck := MakeClerk(vshost, "")
			rr := rand.New(rand.NewSource(int64(os.Getpid() + i)))
			for atomic.LoadInt32(&done) == 0 {
				k := strconv.Itoa(rr.Int() % nkeys)
				v := strconv.Itoa(rr.Int())
				ck.Put(k, v)
			}
		}(xi)
	}

	time.Sleep(5 * time.Second)
	atomic.StoreInt32(&done, 1)
	time.Sleep(time.Second)

	// read from primary
	ck := MakeClerk(vshost, "")
	var vals [nkeys]string
	for i := 0; i < nkeys; i++ {
		vals[i] = ck.Get(strconv.Itoa(i))
		if vals[i] == "" {
			t.Fatalf("Get(%v) failed from primary", i)
		}
	}

	// kill the primary
	for i := 0; i < nservers; i++ {
		if view1.Primary == sa[i].me {
			sa[i].kill()
			break
		}
	}
	for iters := 0; iters < viewservice.DeadPings*2; iters++ {
		view, _ := vck.Get()
		if view.Primary == view1.Backup {
			break
		}
		time.Sleep(viewservice.PingInterval)
	}
	view2, _ := vck.Get()
	if view2.Primary != view1.Backup {
		t.Fatal("wrong Primary")
	}

	// read from old backup
	for i := 0; i < nkeys; i++ {
		z := ck.Get(strconv.Itoa(i))
		if z != vals[i] {
			t.Fatalf("Get(%v) from backup; wanted %v, got %v", i, vals[i], z)
		}
	}

	fmt.Printf("  ... Passed\n")

	for i := 0; i < nservers; i++ {
		sa[i].kill()
	}
	time.Sleep(time.Second)
	vs.Kill()
	time.Sleep(time.Second)
}

// check that all known appends are present in a value,
// and are in order for each concurrent client.
func checkAppends(t *testing.T, v string, counts []int) {
	nclients := len(counts)
	for i := 0; i < nclients; i++ {
		lastoff := -1
		for j := 0; j < counts[i]; j++ {
			wanted := "x " + strconv.Itoa(i) + " " + strconv.Itoa(j) + " y"
			off := strings.Index(v, wanted)
			if off < 0 {
				t.Fatalf("missing element in Append result")
			}
			off1 := strings.LastIndex(v, wanted)
			if off1 != off {
				t.Fatalf("duplicate element in Append result")
			}
			if off <= lastoff {
				t.Fatalf("wrong order for element in Append result")
			}
			lastoff = off
		}
	}
}

// do a bunch of concurrent Append()s on the same key,
// then check that primary and backup have identical values.
// i.e. that they processed the Append()s in the same order.
func TestConcurrentSameAppend(t *testing.T) {
	runtime.GOMAXPROCS(4)

	tag := "csa"
	vshost := port(tag+"v", 1)
	vs := viewservice.StartServer(vshost)
	time.Sleep(time.Second)
	vck := viewservice.MakeClerk("", vshost)

	fmt.Printf("Test: Concurrent Append()s to the same key ...\n")

	const nservers = 2
	var sa [nservers]*PBServer
	for i := 0; i < nservers; i++ {
		sa[i] = StartServer(vshost, port(tag, i+1))
	}

	for iters := 0; iters < viewservice.DeadPings*2; iters++ {
		view, _ := vck.Get()
		if view.Primary != "" && view.Backup != "" {
			break
		}
		time.Sleep(viewservice.PingInterval)
	}

	// give p+b time to ack, initialize
	time.Sleep(viewservice.PingInterval * viewservice.DeadPings)

	view1, _ := vck.Get()

	// code for i'th concurrent client thread.
	ff := func(i int, ch chan int) {
		ret := -1
		defer func() { ch <- ret }()
		ck := MakeClerk(vshost, "")
		n := 0
		for n < 50 {
			v := "x " + strconv.Itoa(i) + " " + strconv.Itoa(n) + " y"
			ck.Append("k", v)
			n += 1
		}
		ret = n
	}

	// start the concurrent clients
	const nclients = 3
	chans := []chan int{}
	for i := 0; i < nclients; i++ {
		chans = append(chans, make(chan int))
		go ff(i, chans[i])
	}

	// wait for the clients, accumulate Append counts.
	counts := []int{}
	for i := 0; i < nclients; i++ {
		n := <-chans[i]
		if n < 0 {
			t.Fatalf("child failed")
		}
		counts = append(counts, n)
	}

	ck := MakeClerk(vshost, "")

	// check that primary's copy of the value has all
	// the Append()s.
	primaryv := ck.Get("k")
	checkAppends(t, primaryv, counts)

	// kill the primary so we can check the backup
	for i := 0; i < nservers; i++ {
		if view1.Primary == sa[i].me {
			sa[i].kill()
			break
		}
	}
	for iters := 0; iters < viewservice.DeadPings*2; iters++ {
		view, _ := vck.Get()
		if view.Primary == view1.Backup {
			break
		}
		time.Sleep(viewservice.PingInterval)
	}
	view2, _ := vck.Get()
	if view2.Primary != view1.Backup {
		t.Fatal("wrong Primary")
	}

	// check that backup's copy of the value has all
	// the Append()s.
	backupv := ck.Get("k")
	checkAppends(t, backupv, counts)

	if backupv != primaryv {
		t.Fatal("primary and backup had different values")
	}

	fmt.Printf("  ... Passed\n")

	for i := 0; i < nservers; i++ {
		sa[i].kill()
	}
	time.Sleep(time.Second)
	vs.Kill()
	time.Sleep(time.Second)
}

func TestConcurrentSameUnreliable(t *testing.T) {
	runtime.GOMAXPROCS(4)

	tag := "csu"
	vshost := port(tag+"v", 1)
	vs := viewservice.StartServer(vshost)
	time.Sleep(time.Second)
	vck := viewservice.MakeClerk("", vshost)

	fmt.Printf("Test: Concurrent Put()s to the same key; unreliable ...\n")

	const nservers = 2
	var sa [nservers]*PBServer
	for i := 0; i < nservers; i++ {
		sa[i] = StartServer(vshost, port(tag, i+1))
		sa[i].setunreliable(true)
	}

	for iters := 0; iters < viewservice.DeadPings*2; iters++ {
		view, _ := vck.Get()
		if view.Primary != "" && view.Backup != "" {
			break
		}
		time.Sleep(viewservice.PingInterval)
	}

	// give p+b time to ack, initialize
	time.Sleep(viewservice.PingInterval * viewservice.DeadPings)

	{
		ck := MakeClerk(vshost, "")
		ck.Put("0", "x")
		ck.Put("1", "x")
	}

	done := int32(0)

	view1, _ := vck.Get()
	const nclients = 3
	const nkeys = 2
	cha := []chan bool{}
	for xi := 0; xi < nclients; xi++ {
		cha = append(cha, make(chan bool))
		go func(i int, ch chan bool) {
			ok := false
			defer func() { ch <- ok }()
			ck := MakeClerk(vshost, "")
			rr := rand.New(rand.NewSource(int64(os.Getpid() + i)))
			for atomic.LoadInt32(&done) == 0 {
				k := strconv.Itoa(rr.Int() % nkeys)
				v := strconv.Itoa(rr.Int())
				ck.Put(k, v)
			}
			ok = true
		}(xi, cha[xi])
	}

	time.Sleep(5 * time.Second)
	atomic.StoreInt32(&done, 1)

	for i := 0; i < len(cha); i++ {
		ok := <-cha[i]
		if ok == false {
			t.Fatalf("child failed")
		}
	}

	// read from primary
	ck := MakeClerk(vshost, "")
	var vals [nkeys]string
	for i := 0; i < nkeys; i++ {
		vals[i] = ck.Get(strconv.Itoa(i))
		if vals[i] == "" {
			t.Fatalf("Get(%v) failed from primary", i)
		}
	}

	// kill the primary
	for i := 0; i < nservers; i++ {
		if view1.Primary == sa[i].me {
			sa[i].kill()
			break
		}
	}
	for iters := 0; iters < viewservice.DeadPings*2; iters++ {
		view, _ := vck.Get()
		if view.Primary == view1.Backup {
			break
		}
		time.Sleep(viewservice.PingInterval)
	}
	view2, _ := vck.Get()
	if view2.Primary != view1.Backup {
		t.Fatal("wrong Primary")
	}

	// read from old backup
	for i := 0; i < nkeys; i++ {
		z := ck.Get(strconv.Itoa(i))
		if z != vals[i] {
			t.Fatalf("Get(%v) from backup; wanted %v, got %v", i, vals[i], z)
		}
	}

	fmt.Printf("  ... Passed\n")

	for i := 0; i < nservers; i++ {
		sa[i].kill()
	}
	time.Sleep(time.Second)
	vs.Kill()
	time.Sleep(time.Second)
}

// constant put/get while crashing and restarting servers
func TestRepeatedCrash(t *testing.T) {
	runtime.GOMAXPROCS(4)

	tag := "rc"
	vshost := port(tag+"v", 1)
	vs := viewservice.StartServer(vshost)
	time.Sleep(time.Second)
	vck := viewservice.MakeClerk("", vshost)

	fmt.Printf("Test: Repeated failures/restarts ...\n")

	const nservers = 3
	var sa [nservers]*PBServer
	samu := sync.Mutex{}
	for i := 0; i < nservers; i++ {
		sa[i] = StartServer(vshost, port(tag, i+1))
	}

	for i := 0; i < viewservice.DeadPings; i++ {
		v, _ := vck.Get()
		if v.Primary != "" && v.Backup != "" {
			break
		}
		time.Sleep(viewservice.PingInterval)
	}

	// wait a bit for primary to initialize backup
	time.Sleep(viewservice.DeadPings * viewservice.PingInterval)

	done := int32(0)

	go func() {
		// kill and restart servers
		rr := rand.New(rand.NewSource(int64(os.Getpid())))
		for atomic.LoadInt32(&done) == 0 {
			i := rr.Int() % nservers
			// fmt.Printf("%v killing %v\n", ts(), 5001+i)
			sa[i].kill()

			// wait long enough for new view to form, backup to be initialized
			time.Sleep(2 * viewservice.PingInterval * viewservice.DeadPings)

			sss := StartServer(vshost, port(tag, i+1))
			samu.Lock()
			sa[i] = sss
			samu.Unlock()

			// wait long enough for new view to form, backup to be initialized
			time.Sleep(2 * viewservice.PingInterval * viewservice.DeadPings)
		}
	}()

	const nth = 2
	var cha [nth]chan bool
	for xi := 0; xi < nth; xi++ {
		cha[xi] = make(chan bool)
		go func(i int) {
			ok := false
			defer func() { cha[i] <- ok }()
			ck := MakeClerk(vshost, "")
			data := map[string]string{}
			rr := rand.New(rand.NewSource(int64(os.Getpid() + i)))
			for atomic.LoadInt32(&done) == 0 {
				k := strconv.Itoa((i * 1000000) + (rr.Int() % 10))
				wanted, ok := data[k]
				if ok {
					v := ck.Get(k)
					if v != wanted {
						t.Fatalf("key=%v wanted=%v got=%v", k, wanted, v)
					}
				}
				nv := strconv.Itoa(rr.Int())
				ck.Put(k, nv)
				data[k] = nv
				// if no sleep here, then server tick() threads do not get
				// enough time to Ping the viewserver.
				time.Sleep(10 * time.Millisecond)
			}
			ok = true
		}(xi)
	}

	time.Sleep(20 * time.Second)
	atomic.StoreInt32(&done, 1)

	fmt.Printf("  ... Put/Gets done ... \n")

	for i := 0; i < nth; i++ {
		ok := <-cha[i]
		if ok == false {
			t.Fatal("child failed")
		}
	}

	ck := MakeClerk(vshost, "")
	ck.Put("aaa", "bbb")
	if v := ck.Get("aaa"); v != "bbb" {
		t.Fatalf("final Put/Get failed")
	}

	fmt.Printf("  ... Passed\n")

	for i := 0; i < nservers; i++ {
		samu.Lock()
		sa[i].kill()
		samu.Unlock()
	}
	time.Sleep(time.Second)
	vs.Kill()
	time.Sleep(time.Second)
}

func TestRepeatedCrashUnreliable(t *testing.T) {
	runtime.GOMAXPROCS(4)

	tag := "rcu"
	vshost := port(tag+"v", 1)
	vs := viewservice.StartServer(vshost)
	time.Sleep(time.Second)
	vck := viewservice.MakeClerk("", vshost)

	fmt.Printf("Test: Repeated failures/restarts with concurrent updates to same key; unreliable ...\n")

	const nservers = 3
	var sa [nservers]*PBServer
	samu := sync.Mutex{}
	for i := 0; i < nservers; i++ {
		sa[i] = StartServer(vshost, port(tag, i+1))
		sa[i].setunreliable(true)
	}

	for i := 0; i < viewservice.DeadPings; i++ {
		v, _ := vck.Get()
		if v.Primary != "" && v.Backup != "" {
			break
		}
		time.Sleep(viewservice.PingInterval)
	}

	// wait a bit for primary to initialize backup
	time.Sleep(viewservice.DeadPings * viewservice.PingInterval)

	done := int32(0)

	go func() {
		// kill and restart servers
		rr := rand.New(rand.NewSource(int64(os.Getpid())))
		for atomic.LoadInt32(&done) == 0 {
			i := rr.Int() % nservers
			// fmt.Printf("%v killing %v\n", ts(), 5001+i)
			sa[i].kill()

			// wait long enough for new view to form, backup to be initialized
			time.Sleep(2 * viewservice.PingInterval * viewservice.DeadPings)

			sss := StartServer(vshost, port(tag, i+1))
			samu.Lock()
			sa[i] = sss
			samu.Unlock()

			// wait long enough for new view to form, backup to be initialized
			time.Sleep(2 * viewservice.PingInterval * viewservice.DeadPings)
		}
	}()

	// concurrent client thread.
	ff := func(i int, ch chan int) {
		ret := -1
		defer func() { ch <- ret }()
		ck := MakeClerk(vshost, "")
		n := 0
		for atomic.LoadInt32(&done) == 0 {
			v := "x " + strconv.Itoa(i) + " " + strconv.Itoa(n) + " y"
			ck.Append("0", v)
			// if no sleep here, then server tick() threads do not get
			// enough time to Ping the viewserver.
			time.Sleep(10 * time.Millisecond)
			n++
		}
		ret = n
	}

	const nth = 2
	var cha [nth]chan int
	for i := 0; i < nth; i++ {
		cha[i] = make(chan int)
		go ff(i, cha[i])
	}

	time.Sleep(20 * time.Second)
	atomic.StoreInt32(&done, 1)

	fmt.Printf("  ... Appends done ... \n")

	counts := []int{}
	for i := 0; i < nth; i++ {
		n := <-cha[i]
		if n < 0 {
			t.Fatal("child failed")
		}
		counts = append(counts, n)
	}

	ck := MakeClerk(vshost, "")

	checkAppends(t, ck.Get("0"), counts)

	ck.Put("aaa", "bbb")
	if v := ck.Get("aaa"); v != "bbb" {
		t.Fatalf("final Put/Get failed")
	}

	fmt.Printf("  ... Passed\n")

	for i := 0; i < nservers; i++ {
		samu.Lock()
		sa[i].kill()
		samu.Unlock()
	}
	time.Sleep(time.Second)
	vs.Kill()
	time.Sleep(time.Second)
}

func proxy(t *testing.T, port string, delay *int32) {
	portx := port + "x"
	os.Remove(portx)
	if os.Rename(port, portx) != nil {
		t.Fatalf("proxy rename failed")
	}
	l, err := net.Listen("unix", port)
	if err != nil {
		t.Fatalf("proxy listen failed: %v", err)
	}
	go func() {
		defer l.Close()
		defer os.Remove(portx)
		defer os.Remove(port)
		for {
			c1, err := l.Accept()
			if err != nil {
				t.Fatalf("proxy accept failed: %v\n", err)
			}
			time.Sleep(time.Duration(atomic.LoadInt32(delay)) * time.Second)
			c2, err := net.Dial("unix", portx)
			if err != nil {
				t.Fatalf("proxy dial failed: %v\n", err)
			}

			go func() {
				for {
					buf := make([]byte, 1000)
					n, _ := c2.Read(buf)
					if n == 0 {
						break
					}
					n1, _ := c1.Write(buf[0:n])
					if n1 != n {
						break
					}
				}
			}()
			for {
				buf := make([]byte, 1000)
				n, err := c1.Read(buf)
				if err != nil && err != io.EOF {
					t.Fatalf("proxy c1.Read: %v\n", err)
				}
				if n == 0 {
					break
				}
				n1, err1 := c2.Write(buf[0:n])
				if err1 != nil || n1 != n {
					t.Fatalf("proxy c2.Write: %v\n", err1)
				}
			}

			c1.Close()
			c2.Close()
		}
	}()
}

func TestPartition1(t *testing.T) {
	runtime.GOMAXPROCS(4)

	tag := "part1"
	vshost := port(tag+"v", 1)
	vs := viewservice.StartServer(vshost)
	time.Sleep(time.Second)
	vck := viewservice.MakeClerk("", vshost)

	ck1 := MakeClerk(vshost, "")

	fmt.Printf("Test: Old primary does not serve Gets ...\n")

	vshosta := vshost + "a"
	os.Link(vshost, vshosta)

	s1 := StartServer(vshosta, port(tag, 1))
	delay := int32(0)
	proxy(t, port(tag, 1), &delay)

	deadtime := viewservice.PingInterval * viewservice.DeadPings
	time.Sleep(deadtime * 2)
	if vck.Primary() != s1.me {
		t.Fatal("primary never formed initial view")
	}

	s2 := StartServer(vshost, port(tag, 2))
	time.Sleep(deadtime * 2)
	v1, _ := vck.Get()
	if v1.Primary != s1.me || v1.Backup != s2.me {
		t.Fatal("backup did not join view")
	}

	ck1.Put("a", "1")
	check(ck1, "a", "1")

	os.Remove(vshosta)

	// start a client Get(), but use proxy to delay it long
	// enough that it won't reach s1 until after s1 is no
	// longer the primary.
	atomic.StoreInt32(&delay, 4)
	stale_get := make(chan bool)
	go func() {
		local_stale := false
		defer func() { stale_get <- local_stale }()
		x := ck1.Get("a")
		if x == "1" {
			local_stale = true
		}
	}()

	// now s1 cannot talk to viewserver, so view will change,
	// and s1 won't immediately realize.

	for iter := 0; iter < viewservice.DeadPings*3; iter++ {
		if vck.Primary() == s2.me {
			break
		}
		time.Sleep(viewservice.PingInterval)
	}
	if vck.Primary() != s2.me {
		t.Fatalf("primary never changed")
	}

	// wait long enough that s2 is guaranteed to have Pinged
	// the viewservice, and thus that s2 must know about
	// the new view.
	time.Sleep(2 * viewservice.PingInterval)

	// change the value (on s2) so it's no longer "1".
	ck2 := MakeClerk(vshost, "")
	ck2.Put("a", "111")
	check(ck2, "a", "111")

	// wait for the background Get to s1 to be delivered.
	select {
	case x := <-stale_get:
		if x {
			t.Fatalf("Get to old primary succeeded and produced stale value")
		}
	case <-time.After(5 * time.Second):
	}

	check(ck2, "a", "111")

	fmt.Printf("  ... Passed\n")

	s1.kill()
	s2.kill()
	vs.Kill()
}

func TestPartition2(t *testing.T) {
	runtime.GOMAXPROCS(4)

	tag := "part2"
	vshost := port(tag+"v", 1)
	vs := viewservice.StartServer(vshost)
	time.Sleep(time.Second)
	vck := viewservice.MakeClerk("", vshost)

	ck1 := MakeClerk(vshost, "")

	vshosta := vshost + "a"
	os.Link(vshost, vshosta)

	s1 := StartServer(vshosta, port(tag, 1))
	delay := int32(0)
	proxy(t, port(tag, 1), &delay)

	fmt.Printf("Test: Partitioned old primary does not complete Gets ...\n")

	deadtime := viewservice.PingInterval * viewservice.DeadPings
	time.Sleep(deadtime * 2)
	if vck.Primary() != s1.me {
		t.Fatal("primary never formed initial view")
	}

	s2 := StartServer(vshost, port(tag, 2))
	time.Sleep(deadtime * 2)
	v1, _ := vck.Get()
	if v1.Primary != s1.me || v1.Backup != s2.me {
		t.Fatal("backup did not join view")
	}

	ck1.Put("a", "1")
	check(ck1, "a", "1")

	os.Remove(vshosta)

	// start a client Get(), but use proxy to delay it long
	// enough that it won't reach s1 until after s1 is no
	// longer the primary.
	atomic.StoreInt32(&delay, 5)
	stale_get := make(chan bool)
	go func() {
		local_stale := false
		defer func() { stale_get <- local_stale }()
		x := ck1.Get("a")
		if x == "1" {
			local_stale = true
		}
	}()

	// now s1 cannot talk to viewserver, so view will change.

	for iter := 0; iter < viewservice.DeadPings*3; iter++ {
		if vck.Primary() == s2.me {
			break
		}
		time.Sleep(viewservice.PingInterval)
	}
	if vck.Primary() != s2.me {
		t.Fatalf("primary never changed")
	}

	s3 := StartServer(vshost, port(tag, 3))
	for iter := 0; iter < viewservice.DeadPings*3; iter++ {
		v, _ := vck.Get()
		if v.Backup == s3.me && v.Primary == s2.me {
			break
		}
		time.Sleep(viewservice.PingInterval)
	}
	v2, _ := vck.Get()
	if v2.Primary != s2.me || v2.Backup != s3.me {
		t.Fatalf("new backup never joined")
	}
	time.Sleep(2 * time.Second)

	ck2 := MakeClerk(vshost, "")
	ck2.Put("a", "2")
	check(ck2, "a", "2")

	s2.kill()

	// wait for delayed get to s1 to complete.
	select {
	case x := <-stale_get:
		if x {
			t.Fatalf("partitioned primary replied to a Get with a stale value")
		}
	case <-time.After(6 * time.Second):
	}

	check(ck2, "a", "2")

	fmt.Printf("  ... Passed\n")

	s1.kill()
	s2.kill()
	s3.kill()
	vs.Kill()
}
