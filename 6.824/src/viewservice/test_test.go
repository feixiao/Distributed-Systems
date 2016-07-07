package viewservice

import "testing"
import "runtime"
import "time"
import "fmt"
import "os"
import "strconv"

func check(t *testing.T, ck *Clerk, p string, b string, n uint) {
	view, _ := ck.Get()
	if view.Primary != p {
		t.Fatalf("wanted primary %v, got %v", p, view.Primary)
	}
	if view.Backup != b {
		t.Fatalf("wanted backup %v, got %v", b, view.Backup)
	}
	if n != 0 && n != view.Viewnum {
		t.Fatalf("wanted viewnum %v, got %v", n, view.Viewnum)
	}
	if ck.Primary() != p {
		t.Fatalf("wanted primary %v, got %v", p, ck.Primary())
	}
}

func port(suffix string) string {
	s := "/var/tmp/824-"
	s += strconv.Itoa(os.Getuid()) + "/"
	os.Mkdir(s, 0777)
	s += "viewserver-"
	s += strconv.Itoa(os.Getpid()) + "-"
	s += suffix
	return s
}

func Test1(t *testing.T) {
	runtime.GOMAXPROCS(4)

	vshost := port("v")
	vs := StartServer(vshost)

	ck1 := MakeClerk(port("1"), vshost)
	ck2 := MakeClerk(port("2"), vshost)
	ck3 := MakeClerk(port("3"), vshost)

	//

	if ck1.Primary() != "" {
		t.Fatalf("there was a primary too soon")
	}

	// very first primary
	fmt.Printf("Test: First primary ...\n")

	for i := 0; i < DeadPings*2; i++ {
		view, _ := ck1.Ping(0)
		if view.Primary == ck1.me {
			break
		}
		time.Sleep(PingInterval)
	}
	check(t, ck1, ck1.me, "", 1)
	fmt.Printf("  ... Passed\n")

	// very first backup
	fmt.Printf("Test: First backup ...\n")

	{
		vx, _ := ck1.Get()
		for i := 0; i < DeadPings*2; i++ {
			ck1.Ping(1)
			view, _ := ck2.Ping(0)
			if view.Backup == ck2.me {
				break
			}
			time.Sleep(PingInterval)
		}
		check(t, ck1, ck1.me, ck2.me, vx.Viewnum+1)
	}
	fmt.Printf("  ... Passed\n")

	// primary dies, backup should take over
	fmt.Printf("Test: Backup takes over if primary fails ...\n")

	{
		ck1.Ping(2)
		vx, _ := ck2.Ping(2)
		for i := 0; i < DeadPings*2; i++ {
			v, _ := ck2.Ping(vx.Viewnum)
			if v.Primary == ck2.me && v.Backup == "" {
				break
			}
			time.Sleep(PingInterval)
		}
		check(t, ck2, ck2.me, "", vx.Viewnum+1)
	}
	fmt.Printf("  ... Passed\n")

	// revive ck1, should become backup
	fmt.Printf("Test: Restarted server becomes backup ...\n")

	{
		vx, _ := ck2.Get()
		ck2.Ping(vx.Viewnum)
		for i := 0; i < DeadPings*2; i++ {
			ck1.Ping(0)
			v, _ := ck2.Ping(vx.Viewnum)
			if v.Primary == ck2.me && v.Backup == ck1.me {
				break
			}
			time.Sleep(PingInterval)
		}
		check(t, ck2, ck2.me, ck1.me, vx.Viewnum+1)
	}
	fmt.Printf("  ... Passed\n")

	// start ck3, kill the primary (ck2), the previous backup (ck1)
	// should become the server, and ck3 the backup.
	// this should happen in a single view change, without
	// any period in which there's no backup.
	fmt.Printf("Test: Idle third server becomes backup if primary fails ...\n")

	{
		vx, _ := ck2.Get()
		ck2.Ping(vx.Viewnum)
		for i := 0; i < DeadPings*2; i++ {
			ck3.Ping(0)
			v, _ := ck1.Ping(vx.Viewnum)
			if v.Primary == ck1.me && v.Backup == ck3.me {
				break
			}
			vx = v
			time.Sleep(PingInterval)
		}
		check(t, ck1, ck1.me, ck3.me, vx.Viewnum+1)
	}
	fmt.Printf("  ... Passed\n")

	// kill and immediately restart the primary -- does viewservice
	// conclude primary is down even though it's pinging?
	fmt.Printf("Test: Restarted primary treated as dead ...\n")

	{
		vx, _ := ck1.Get()
		ck1.Ping(vx.Viewnum)
		for i := 0; i < DeadPings*2; i++ {
			ck1.Ping(0)
			ck3.Ping(vx.Viewnum)
			v, _ := ck3.Get()
			if v.Primary != ck1.me {
				break
			}
			time.Sleep(PingInterval)
		}
		vy, _ := ck3.Get()
		if vy.Primary != ck3.me {
			t.Fatalf("expected primary=%v, got %v\n", ck3.me, vy.Primary)
		}
	}
	fmt.Printf("  ... Passed\n")

	fmt.Printf("Test: Dead backup is removed from view ...\n")

	// set up a view with just 3 as primary,
	// to prepare for the next test.
	{
		for i := 0; i < DeadPings*3; i++ {
			vx, _ := ck3.Get()
			ck3.Ping(vx.Viewnum)
			time.Sleep(PingInterval)
		}
		v, _ := ck3.Get()
		if v.Primary != ck3.me || v.Backup != "" {
			t.Fatalf("wrong primary or backup")
		}
	}
	fmt.Printf("  ... Passed\n")

	// does viewserver wait for ack of previous view before
	// starting the next one?
	fmt.Printf("Test: Viewserver waits for primary to ack view ...\n")

	{
		// set up p=ck3 b=ck1, but
		// but do not ack
		vx, _ := ck1.Get()
		for i := 0; i < DeadPings*3; i++ {
			ck1.Ping(0)
			ck3.Ping(vx.Viewnum)
			v, _ := ck1.Get()
			if v.Viewnum > vx.Viewnum {
				break
			}
			time.Sleep(PingInterval)
		}
		check(t, ck1, ck3.me, ck1.me, vx.Viewnum+1)
		vy, _ := ck1.Get()
		// ck3 is the primary, but it never acked.
		// let ck3 die. check that ck1 is not promoted.
		for i := 0; i < DeadPings*3; i++ {
			v, _ := ck1.Ping(vy.Viewnum)
			if v.Viewnum > vy.Viewnum {
				break
			}
			time.Sleep(PingInterval)
		}
		check(t, ck2, ck3.me, ck1.me, vy.Viewnum)
	}
	fmt.Printf("  ... Passed\n")

	// if old servers die, check that a new (uninitialized) server
	// cannot take over.
	fmt.Printf("Test: Uninitialized server can't become primary ...\n")

	{
		for i := 0; i < DeadPings*2; i++ {
			v, _ := ck1.Get()
			ck1.Ping(v.Viewnum)
			ck2.Ping(0)
			ck3.Ping(v.Viewnum)
			time.Sleep(PingInterval)
		}
		for i := 0; i < DeadPings*2; i++ {
			ck2.Ping(0)
			time.Sleep(PingInterval)
		}
		vz, _ := ck2.Get()
		if vz.Primary == ck2.me {
			t.Fatalf("uninitialized backup promoted to primary")
		}
	}
	fmt.Printf("  ... Passed\n")

	vs.Kill()
}
