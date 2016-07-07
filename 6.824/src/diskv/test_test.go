package diskv

import "testing"
import "shardmaster"
import "runtime"
import "strconv"
import "strings"
import "os"
import "os/exec"
import "time"
import "fmt"
import "sync"
import "io/ioutil"
import "log"
import "math/rand"
import crand "crypto/rand"
import "encoding/base64"
import "path/filepath"
import "sync/atomic"

type tServer struct {
	p       *os.Process
	port    string // this replica's port name
	dir     string // directory for persistent data
	started bool   // has started at least once already
}

// information about the servers of one replica group.
type tGroup struct {
	gid     int64
	servers []*tServer
}

// information about all the servers of a k/v cluster.
type tCluster struct {
	t           *testing.T
	dir         string
	unreliable  bool
	masters     []*shardmaster.ShardMaster
	mck         *shardmaster.Clerk
	masterports []string
	groups      []*tGroup
}

func randstring(n int) string {
	b := make([]byte, 2*n)
	crand.Read(b)
	s := base64.URLEncoding.EncodeToString(b)
	return s[0:n]
}

func (tc *tCluster) newport() string {
	return tc.dir + randstring(12)
}

//
// start a k/v replica server process.
// use separate process, rather than thread, so we
// can kill a replica unexpectedly.
// ../main/diskvd
//
func (tc *tCluster) start1(gi int, si int) {
	args := []string{"../main/diskvd"}

	attr := &os.ProcAttr{}
	in, err := os.Open("/dev/null")
	attr.Files = make([]*os.File, 3)
	attr.Files[0] = in
	attr.Files[1] = os.Stdout
	attr.Files[2] = os.Stderr

	g := tc.groups[gi]
	s := g.servers[si]

	args = append(args, "-g")
	args = append(args, strconv.FormatInt(g.gid, 10))
	for _, m := range tc.masterports {
		args = append(args, "-m")
		args = append(args, m)
	}
	for _, sx := range g.servers {
		args = append(args, "-s")
		args = append(args, sx.port)
	}
	args = append(args, "-i")
	args = append(args, strconv.Itoa(si))
	args = append(args, "-u")
	args = append(args, strconv.FormatBool(tc.unreliable))
	args = append(args, "-d")
	args = append(args, s.dir)
	args = append(args, "-r")
	args = append(args, strconv.FormatBool(s.started)) // re-start?

	p, err := os.StartProcess(args[0], args, attr)
	if err != nil {
		tc.t.Fatalf("StartProcess(%v): %v\n", args[0], err)
	}

	s.p = p
	s.started = true
}

func (tc *tCluster) kill1(gi int, si int, deletefiles bool) {
	g := tc.groups[gi]
	s := g.servers[si]
	if s.p != nil {
		s.p.Kill()
		s.p.Wait()
		s.p = nil
	}
	if deletefiles {
		if err := os.RemoveAll(s.dir); err != nil {
			tc.t.Fatalf("RemoveAll")
		}
		os.Mkdir(s.dir, 0777)
	}
}

func (tc *tCluster) cleanup() {
	for gi := 0; gi < len(tc.groups); gi++ {
		g := tc.groups[gi]
		for si := 0; si < len(g.servers); si++ {
			tc.kill1(gi, si, false)
		}
	}

	for i := 0; i < len(tc.masters); i++ {
		if tc.masters[i] != nil {
			tc.masters[i].Kill()
		}
	}

	// this RemoveAll, along with the directory naming
	// policy in setup(), means that you can't run
	// concurrent tests. the reason is to avoid accumulating
	// lots of stuff in /var/tmp on Athena.
	os.RemoveAll(tc.dir)
}

func (tc *tCluster) shardclerk() *shardmaster.Clerk {
	return shardmaster.MakeClerk(tc.masterports)
}

func (tc *tCluster) clerk() *Clerk {
	return MakeClerk(tc.masterports)
}

func (tc *tCluster) join(gi int) {
	ports := []string{}
	for _, s := range tc.groups[gi].servers {
		ports = append(ports, s.port)
	}
	tc.mck.Join(tc.groups[gi].gid, ports)
}

func (tc *tCluster) leave(gi int) {
	tc.mck.Leave(tc.groups[gi].gid)
}

// how many total bytes of file space in use?
func (tc *tCluster) space() int64 {
	var bytes int64 = 0
	ff := func(_ string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsDir() == false {
			bytes += info.Size()
		}
		return nil
	}
	filepath.Walk(tc.dir, ff)
	return bytes
}

func setup(t *testing.T, tag string, ngroups int, nreplicas int, unreliable bool) *tCluster {
	runtime.GOMAXPROCS(4)

	// compile ../main/diskvd.go
	// cmd := exec.Command("go", "build", "-race", "diskvd.go")
	cmd := exec.Command("go", "build", "diskvd.go")
	cmd.Dir = "../main"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("could not compile ../main/diskvd.go: %v", err)
	}

	const nmasters = 3

	tc := &tCluster{}
	tc.t = t
	tc.unreliable = unreliable

	tc.dir = "/var/tmp/824-"
	tc.dir += strconv.Itoa(os.Getuid()) + "/"
	os.Mkdir(tc.dir, 0777)
	tc.dir += "lab5-" + tag + "/"
	os.RemoveAll(tc.dir)
	os.Mkdir(tc.dir, 0777)

	tc.masters = make([]*shardmaster.ShardMaster, nmasters)
	tc.masterports = make([]string, nmasters)
	for i := 0; i < nmasters; i++ {
		tc.masterports[i] = tc.newport()
	}
	log.SetOutput(ioutil.Discard) // suppress method errors &c
	for i := 0; i < nmasters; i++ {
		tc.masters[i] = shardmaster.StartServer(tc.masterports, i)
	}
	log.SetOutput(os.Stdout) // re-enable error output.
	tc.mck = tc.shardclerk()

	tc.groups = make([]*tGroup, ngroups)

	for i := 0; i < ngroups; i++ {
		g := &tGroup{}
		tc.groups[i] = g
		g.gid = int64(i + 100)
		g.servers = make([]*tServer, nreplicas)
		for j := 0; j < nreplicas; j++ {
			g.servers[j] = &tServer{}
			g.servers[j].port = tc.newport()
			g.servers[j].dir = tc.dir + randstring(12)
			if err := os.Mkdir(g.servers[j].dir, 0777); err != nil {
				t.Fatalf("Mkdir(%v): %v", g.servers[j].dir, err)
			}
		}
		for j := 0; j < nreplicas; j++ {
			tc.start1(i, j)
		}
	}

	// return smh, gids, ha, sa, clean
	return tc
}

//
// these tests are the same as in Lab 4.
//

func Test4Basic(t *testing.T) {
	tc := setup(t, "basic", 3, 3, false)
	defer tc.cleanup()

	fmt.Printf("Test: Basic Join/Leave (lab4) ...\n")

	tc.join(0)

	ck := tc.clerk()

	ck.Put("a", "x")
	ck.Append("a", "b")
	if ck.Get("a") != "xb" {
		t.Fatalf("wrong value")
	}

	keys := make([]string, 10)
	vals := make([]string, len(keys))
	for i := 0; i < len(keys); i++ {
		keys[i] = strconv.Itoa(rand.Int())
		vals[i] = strconv.Itoa(rand.Int())
		ck.Put(keys[i], vals[i])
	}

	// are keys still there after joins?
	for gi := 1; gi < len(tc.groups); gi++ {
		tc.join(gi)
		time.Sleep(1 * time.Second)
		for i := 0; i < len(keys); i++ {
			v := ck.Get(keys[i])
			if v != vals[i] {
				t.Fatalf("joining; wrong value; g=%v k=%v wanted=%v got=%v",
					gi, keys[i], vals[i], v)
			}
			vals[i] = strconv.Itoa(rand.Int())
			ck.Put(keys[i], vals[i])
		}
	}

	// are keys still there after leaves?
	for gi := 0; gi < len(tc.groups)-1; gi++ {
		tc.leave(gi)
		time.Sleep(1 * time.Second)
		for i := 0; i < len(keys); i++ {
			v := ck.Get(keys[i])
			if v != vals[i] {
				t.Fatalf("leaving; wrong value; g=%v k=%v wanted=%v got=%v",
					gi, keys[i], vals[i], v)
			}
			vals[i] = strconv.Itoa(rand.Int())
			ck.Put(keys[i], vals[i])
		}
	}

	fmt.Printf("  ... Passed\n")
}

func Test4Move(t *testing.T) {
	tc := setup(t, "move", 3, 3, false)
	defer tc.cleanup()

	fmt.Printf("Test: Shards really move (lab4) ...\n")

	tc.join(0)

	ck := tc.clerk()

	// insert one key per shard
	for i := 0; i < shardmaster.NShards; i++ {
		ck.Put(string('0'+i), string('0'+i))
	}

	// add group 1.
	tc.join(1)
	time.Sleep(5 * time.Second)

	// check that keys are still there.
	for i := 0; i < shardmaster.NShards; i++ {
		if ck.Get(string('0'+i)) != string('0'+i) {
			t.Fatalf("missing key/value")
		}
	}

	// remove sockets from group 0.
	for _, s := range tc.groups[0].servers {
		os.Remove(s.port)
	}

	count := int32(0)
	var mu sync.Mutex
	for i := 0; i < shardmaster.NShards; i++ {
		go func(me int) {
			myck := tc.clerk()
			v := myck.Get(string('0' + me))
			if v == string('0'+me) {
				mu.Lock()
				atomic.AddInt32(&count, 1)
				mu.Unlock()
			} else {
				t.Fatalf("Get(%v) yielded %v\n", me, v)
			}
		}(i)
	}

	time.Sleep(10 * time.Second)

	ccc := atomic.LoadInt32(&count)
	if ccc > shardmaster.NShards/3 && ccc < 2*(shardmaster.NShards/3) {
		fmt.Printf("  ... Passed\n")
	} else {
		t.Fatalf("%v keys worked after killing 1/2 of groups; wanted %v",
			ccc, shardmaster.NShards/2)
	}
}

func Test4Limp(t *testing.T) {
	tc := setup(t, "limp", 3, 3, false)
	defer tc.cleanup()

	fmt.Printf("Test: Reconfiguration with some dead replicas (lab4) ...\n")

	tc.join(0)

	ck := tc.clerk()

	ck.Put("a", "b")
	if ck.Get("a") != "b" {
		t.Fatalf("got wrong value")
	}

	// kill one server from each replica group.
	for gi := 0; gi < len(tc.groups); gi++ {
		sa := tc.groups[gi].servers
		tc.kill1(gi, rand.Int()%len(sa), false)
	}

	keys := make([]string, 10)
	vals := make([]string, len(keys))
	for i := 0; i < len(keys); i++ {
		keys[i] = strconv.Itoa(rand.Int())
		vals[i] = strconv.Itoa(rand.Int())
		ck.Put(keys[i], vals[i])
	}

	// are keys still there after joins?
	for gi := 1; gi < len(tc.groups); gi++ {
		tc.join(gi)
		time.Sleep(1 * time.Second)
		for i := 0; i < len(keys); i++ {
			v := ck.Get(keys[i])
			if v != vals[i] {
				t.Fatalf("joining; wrong value; g=%v k=%v wanted=%v got=%v",
					gi, keys[i], vals[i], v)
			}
			vals[i] = strconv.Itoa(rand.Int())
			ck.Put(keys[i], vals[i])
		}
	}

	// are keys still there after leaves?
	for gi := 0; gi < len(tc.groups)-1; gi++ {
		tc.leave(gi)
		time.Sleep(2 * time.Second)
		g := tc.groups[gi]
		for i := 0; i < len(g.servers); i++ {
			tc.kill1(gi, i, false)
		}
		for i := 0; i < len(keys); i++ {
			v := ck.Get(keys[i])
			if v != vals[i] {
				t.Fatalf("leaving; wrong value; g=%v k=%v wanted=%v got=%v",
					g, keys[i], vals[i], v)
			}
			vals[i] = strconv.Itoa(rand.Int())
			ck.Put(keys[i], vals[i])
		}
	}

	fmt.Printf("  ... Passed\n")
}

func doConcurrent(t *testing.T, unreliable bool) {
	tc := setup(t, "concurrent-"+strconv.FormatBool(unreliable), 3, 3, unreliable)
	defer tc.cleanup()

	for i := 0; i < len(tc.groups); i++ {
		tc.join(i)
	}

	const npara = 11
	var ca [npara]chan bool
	for i := 0; i < npara; i++ {
		ca[i] = make(chan bool)
		go func(me int) {
			ok := true
			defer func() { ca[me] <- ok }()
			ck := tc.clerk()
			mymck := tc.shardclerk()
			key := strconv.Itoa(me)
			last := ""
			for iters := 0; iters < 3; iters++ {
				nv := strconv.Itoa(rand.Int())
				ck.Append(key, nv)
				last = last + nv
				v := ck.Get(key)
				if v != last {
					ok = false
					t.Fatalf("Get(%v) expected %v got %v\n", key, last, v)
				}

				gi := rand.Int() % len(tc.groups)
				gid := tc.groups[gi].gid
				mymck.Move(rand.Int()%shardmaster.NShards, gid)

				time.Sleep(time.Duration(rand.Int()%30) * time.Millisecond)
			}
		}(i)
	}

	for i := 0; i < npara; i++ {
		x := <-ca[i]
		if x == false {
			t.Fatalf("something is wrong")
		}
	}
}

func Test4Concurrent(t *testing.T) {
	fmt.Printf("Test: Concurrent Put/Get/Move (lab4) ...\n")
	doConcurrent(t, false)
	fmt.Printf("  ... Passed\n")
}

func Test4ConcurrentUnreliable(t *testing.T) {
	fmt.Printf("Test: Concurrent Put/Get/Move (unreliable) (lab4) ...\n")
	doConcurrent(t, true)
	fmt.Printf("  ... Passed\n")
}

//
// the rest of the tests are lab5-specific.
//

//
// do the servers write k/v pairs to disk, so that they
// are still available after kill+restart?
//
func Test5BasicPersistence(t *testing.T) {
	tc := setup(t, "basicpersistence", 1, 3, false)
	defer tc.cleanup()

	fmt.Printf("Test: Basic Persistence ...\n")

	tc.join(0)

	ck := tc.clerk()

	ck.Append("a", "x")
	ck.Append("a", "y")
	if ck.Get("a") != "xy" {
		t.Fatalf("wrong value")
	}

	// kill all servers in all groups.
	for gi, g := range tc.groups {
		for si, _ := range g.servers {
			tc.kill1(gi, si, false)
		}
	}

	// check that requests are not executed.
	ch := make(chan string)
	go func() {
		ck1 := tc.clerk()
		v := ck1.Get("a")
		ch <- v
	}()

	select {
	case <-ch:
		t.Fatalf("Get should not have succeeded after killing all servers.")
	case <-time.After(3 * time.Second):
		// this is what we hope for.
	}

	// restart all servers, check that they recover the data.
	for gi, g := range tc.groups {
		for si, _ := range g.servers {
			tc.start1(gi, si)
		}
	}
	time.Sleep(2 * time.Second)
	ck.Append("a", "z")
	v := ck.Get("a")
	if v != "xyz" {
		t.Fatalf("wrong value %v after restart", v)
	}

	fmt.Printf("  ... Passed\n")
}

//
// if server S1 is dead for a bit, and others accept operations,
// do they bring S1 up to date correctly after it restarts?
//
func Test5OneRestart(t *testing.T) {
	tc := setup(t, "onerestart", 1, 3, false)
	defer tc.cleanup()

	fmt.Printf("Test: One server restarts ...\n")

	tc.join(0)
	ck := tc.clerk()
	g0 := tc.groups[0]

	k1 := randstring(10)
	k1v := randstring(10)
	ck.Append(k1, k1v)

	k2 := randstring(10)
	k2v := randstring(10)
	ck.Put(k2, k2v)

	for i := 0; i < len(g0.servers); i++ {
		k1x := ck.Get(k1)
		if k1x != k1v {
			t.Fatalf("wrong value for k1, i=%v, wanted=%v, got=%v", i, k1v, k1x)
		}
		k2x := ck.Get(k2)
		if k2x != k2v {
			t.Fatalf("wrong value for k2")
		}

		tc.kill1(0, i, false)
		time.Sleep(1 * time.Second)

		z := randstring(10)
		k1v += z
		ck.Append(k1, z)

		k2v = randstring(10)
		ck.Put(k2, k2v)

		tc.start1(0, i)
		time.Sleep(2 * time.Second)
	}

	if ck.Get(k1) != k1v {
		t.Fatalf("wrong value for k1")
	}
	if ck.Get(k2) != k2v {
		t.Fatalf("wrong value for k2")
	}

	fmt.Printf("  ... Passed\n")
}

//
// check that the persistent state isn't too big.
//
func Test5DiskUse(t *testing.T) {
	tc := setup(t, "diskuse", 1, 3, false)
	defer tc.cleanup()

	fmt.Printf("Test: Servers don't use too much disk space ...\n")

	tc.join(0)
	ck := tc.clerk()
	g0 := tc.groups[0]

	k1 := randstring(10)
	k1v := randstring(10)
	ck.Append(k1, k1v)

	k2 := randstring(10)
	k2v := randstring(10)
	ck.Put(k2, k2v)

	k3 := randstring(10)
	k3v := randstring(10)
	ck.Put(k3, k3v)

	k4 := randstring(10)
	k4v := randstring(10)
	ck.Append(k4, k4v)

	n := 100 + (rand.Int() % 20)
	for i := 0; i < n; i++ {
		k2v = randstring(1000)
		ck.Put(k2, k2v)
		x := randstring(1)
		ck.Append(k3, x)
		k3v += x
		ck.Get(k4)
	}

	time.Sleep(100 * time.Millisecond)
	k2v = randstring(1000)
	ck.Put(k2, k2v)
	time.Sleep(100 * time.Millisecond)
	x := randstring(1)
	ck.Append(k3, x)
	k3v += x
	time.Sleep(100 * time.Millisecond)
	ck.Get(k4)

	// let all the replicas tick().
	time.Sleep(2100 * time.Millisecond)

	max := int64(20 * 1000)

	{
		nb := tc.space()
		if nb > max {
			t.Fatalf("using too many bytes on disk (%v)", nb)
		}
	}

	for i := 0; i < len(g0.servers); i++ {
		tc.kill1(0, i, false)
	}

	{
		nb := tc.space()
		if nb > max {
			t.Fatalf("using too many bytes on disk (%v > %v)", nb, max)
		}
	}

	for i := 0; i < len(g0.servers); i++ {
		tc.start1(0, i)
	}
	time.Sleep(time.Second)

	if ck.Get(k1) != k1v {
		t.Fatalf("wrong value for k1")
	}
	if ck.Get(k2) != k2v {
		t.Fatalf("wrong value for k2")
	}
	if ck.Get(k3) != k3v {
		t.Fatalf("wrong value for k3")
	}

	{
		nb := tc.space()
		if nb > max {
			t.Fatalf("using too many bytes on disk (%v > %v)", nb, max)
		}
	}

	fmt.Printf("  ... Passed\n")
}

//
// check that the persistent state isn't too big for Appends.
//
func Test5AppendUse(t *testing.T) {
	tc := setup(t, "appenduse", 1, 3, false)
	defer tc.cleanup()

	fmt.Printf("Test: Servers don't use too much disk space for Appends ...\n")

	tc.join(0)
	ck := tc.clerk()
	g0 := tc.groups[0]

	k1 := randstring(10)
	k1v := randstring(10)
	ck.Append(k1, k1v)

	k2 := randstring(10)
	k2v := randstring(10)
	ck.Put(k2, k2v)

	k3 := randstring(10)
	k3v := randstring(10)
	ck.Put(k3, k3v)

	k4 := randstring(10)
	k4v := randstring(10)
	ck.Append(k4, k4v)

	n := 100 + (rand.Int() % 20)
	for i := 0; i < n; i++ {
		k2v = randstring(1000)
		ck.Put(k2, k2v)
		x := randstring(1000)
		ck.Append(k3, x)
		k3v += x
		ck.Get(k4)
	}

	time.Sleep(100 * time.Millisecond)
	k2v = randstring(1000)
	ck.Put(k2, k2v)
	time.Sleep(100 * time.Millisecond)
	x := randstring(1)
	ck.Append(k3, x)
	k3v += x
	time.Sleep(100 * time.Millisecond)
	ck.Get(k4)

	// let all the replicas tick().
	time.Sleep(2100 * time.Millisecond)

	max := int64(3*n*1000) + 20000

	{
		nb := tc.space()
		if nb > max {
			t.Fatalf("using too many bytes on disk (%v > %v)", nb, max)
		}
	}

	for i := 0; i < len(g0.servers); i++ {
		tc.kill1(0, i, false)
	}

	{
		nb := tc.space()
		if nb > max {
			t.Fatalf("using too many bytes on disk (%v > %v)", nb, max)
		}
	}

	for i := 0; i < len(g0.servers); i++ {
		tc.start1(0, i)
	}
	time.Sleep(time.Second)

	if ck.Get(k3) != k3v {
		t.Fatalf("wrong value for k3")
	}
	time.Sleep(100 * time.Millisecond)
	if ck.Get(k2) != k2v {
		t.Fatalf("wrong value for k2")
	}
	time.Sleep(1100 * time.Millisecond)
	if ck.Get(k1) != k1v {
		t.Fatalf("wrong value for k1")
	}

	{
		nb := tc.space()
		if nb > max {
			t.Fatalf("using too many bytes on disk (%v > %v)", nb, max)
		}
	}

	fmt.Printf("  ... Passed\n")
}

//
// recovery if a single replica loses disk content.
//
func Test5OneLostDisk(t *testing.T) {
	tc := setup(t, "onelostdisk", 1, 3, false)
	defer tc.cleanup()

	fmt.Printf("Test: One server loses disk and restarts ...\n")

	tc.join(0)
	ck := tc.clerk()
	g0 := tc.groups[0]

	k1 := randstring(10)
	k1v := ""
	k2 := randstring(10)
	k2v := ""

	for i := 0; i < 7+(rand.Int()%7); i++ {
		x := randstring(10)
		ck.Append(k1, x)
		k1v += x
		k2v = randstring(10)
		ck.Put(k2, k2v)
	}

	time.Sleep(300 * time.Millisecond)
	ck.Get(k1)
	time.Sleep(300 * time.Millisecond)
	ck.Get(k2)

	for i := 0; i < len(g0.servers); i++ {
		k1x := ck.Get(k1)
		if k1x != k1v {
			t.Fatalf("wrong value for k1, i=%v, wanted=%v, got=%v", i, k1v, k1x)
		}
		k2x := ck.Get(k2)
		if k2x != k2v {
			t.Fatalf("wrong value for k2")
		}

		tc.kill1(0, i, true)
		time.Sleep(1 * time.Second)

		{
			z := randstring(10)
			k1v += z
			ck.Append(k1, z)

			k2v = randstring(10)
			ck.Put(k2, k2v)
		}

		tc.start1(0, i)

		{
			z := randstring(10)
			k1v += z
			ck.Append(k1, z)

			time.Sleep(10 * time.Millisecond)
			z = randstring(10)
			k1v += z
			ck.Append(k1, z)
		}

		time.Sleep(2 * time.Second)
	}

	if ck.Get(k1) != k1v {
		t.Fatalf("wrong value for k1")
	}
	if ck.Get(k2) != k2v {
		t.Fatalf("wrong value for k2")
	}

	fmt.Printf("  ... Passed\n")
}

//
// one disk lost while another replica is merely down.
//
func Test5OneLostOneDown(t *testing.T) {
	tc := setup(t, "onelostonedown", 1, 5, false)
	defer tc.cleanup()

	fmt.Printf("Test: One server down, another loses disk ...\n")

	tc.join(0)
	ck := tc.clerk()
	g0 := tc.groups[0]

	k1 := randstring(10)
	k1v := ""
	k2 := randstring(10)
	k2v := ""

	for i := 0; i < 7+(rand.Int()%7); i++ {
		x := randstring(10)
		ck.Append(k1, x)
		k1v += x
		k2v = randstring(10)
		ck.Put(k2, k2v)
	}

	time.Sleep(300 * time.Millisecond)
	ck.Get(k1)
	time.Sleep(300 * time.Millisecond)
	ck.Get(k2)

	tc.kill1(0, 0, false)

	for i := 1; i < len(g0.servers); i++ {
		k1x := ck.Get(k1)
		if k1x != k1v {
			t.Fatalf("wrong value for k1, i=%v, wanted=%v, got=%v", i, k1v, k1x)
		}
		k2x := ck.Get(k2)
		if k2x != k2v {
			t.Fatalf("wrong value for k2")
		}

		tc.kill1(0, i, true)
		time.Sleep(1 * time.Second)

		{
			z := randstring(10)
			k1v += z
			ck.Append(k1, z)

			k2v = randstring(10)
			ck.Put(k2, k2v)
		}

		tc.start1(0, i)

		{
			z := randstring(10)
			k1v += z
			ck.Append(k1, z)

			time.Sleep(10 * time.Millisecond)
			z = randstring(10)
			k1v += z
			ck.Append(k1, z)
		}

		time.Sleep(2 * time.Second)
	}

	if ck.Get(k1) != k1v {
		t.Fatalf("wrong value for k1")
	}
	if ck.Get(k2) != k2v {
		t.Fatalf("wrong value for k2")
	}

	tc.start1(0, 0)
	ck.Put("a", "b")
	time.Sleep(1 * time.Second)
	ck.Put("a", "c")
	if ck.Get(k1) != k1v {
		t.Fatalf("wrong value for k1")
	}
	if ck.Get(k2) != k2v {
		t.Fatalf("wrong value for k2")
	}

	fmt.Printf("  ... Passed\n")
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
				t.Fatalf("missing element %v %v in Append result", i, j)
			}
			off1 := strings.LastIndex(v, wanted)
			if off1 != off {
				t.Fatalf("duplicate element %v %v in Append result", i, j)
			}
			if off <= lastoff {
				t.Fatalf("wrong order for element in Append result")
			}
			lastoff = off
		}
	}
}

func doConcurrentCrash(t *testing.T, unreliable bool) {
	tc := setup(t, "concurrentcrash", 1, 3, unreliable)
	defer tc.cleanup()

	tc.join(0)
	ck := tc.clerk()

	k1 := randstring(10)
	ck.Put(k1, "")

	stop := int32(0)

	ff := func(me int, ch chan int) {
		ret := -1
		defer func() { ch <- ret }()
		myck := tc.clerk()
		n := 0
		for atomic.LoadInt32(&stop) == 0 || n < 5 {
			myck.Append(k1, "x "+strconv.Itoa(me)+" "+strconv.Itoa(n)+" y")
			n++
			time.Sleep(200 * time.Millisecond)
		}
		ret = n
	}

	ncli := 5
	cha := []chan int{}
	for i := 0; i < ncli; i++ {
		cha = append(cha, make(chan int))
		go ff(i, cha[i])
	}

	for i := 0; i < 3; i++ {
		tc.kill1(0, i%3, false)
		time.Sleep(1000 * time.Millisecond)
		ck.Get(k1)
		tc.start1(0, i%3)
		time.Sleep(3000 * time.Millisecond)
		if unreliable {
			time.Sleep(5000 * time.Millisecond)
		}
		ck.Get(k1)
	}

	for i := 0; i < 3; i++ {
		tc.kill1(0, i%3, true)
		time.Sleep(1000 * time.Millisecond)
		ck.Get(k1)
		tc.start1(0, i%3)
		time.Sleep(3000 * time.Millisecond)
		if unreliable {
			time.Sleep(5000 * time.Millisecond)
		}
		ck.Get(k1)
	}

	time.Sleep(2 * time.Second)
	atomic.StoreInt32(&stop, 1)

	counts := []int{}
	for i := 0; i < ncli; i++ {
		n := <-cha[i]
		if n < 0 {
			t.Fatal("client failed")
		}
		counts = append(counts, n)
	}

	vx := ck.Get(k1)
	checkAppends(t, vx, counts)

	for i := 0; i < 3; i++ {
		tc.kill1(0, i, false)
		if ck.Get(k1) != vx {
			t.Fatalf("mismatch")
		}
		tc.start1(0, i)
		if ck.Get(k1) != vx {
			t.Fatalf("mismatch")
		}
		time.Sleep(3000 * time.Millisecond)
		if unreliable {
			time.Sleep(5000 * time.Millisecond)
		}
		if ck.Get(k1) != vx {
			t.Fatalf("mismatch")
		}
	}
}

func Test5ConcurrentCrashReliable(t *testing.T) {
	fmt.Printf("Test: Concurrent Append and Crash ...\n")
	doConcurrentCrash(t, false)
	fmt.Printf("  ... Passed\n")
}

//
// Append() at same time as crash.
//
func Test5Simultaneous(t *testing.T) {
	tc := setup(t, "simultaneous", 1, 3, true)
	defer tc.cleanup()

	fmt.Printf("Test: Simultaneous Append and Crash ...\n")

	tc.join(0)
	ck := tc.clerk()

	k1 := randstring(10)
	ck.Put(k1, "")

	ch := make(chan int)

	ff := func(x int) {
		ret := -1
		defer func() { ch <- ret }()
		myck := tc.clerk()
		myck.Append(k1, "x "+strconv.Itoa(0)+" "+strconv.Itoa(x)+" y")
		ret = 1
	}

	counts := []int{0}

	for i := 0; i < 50; i++ {
		go ff(i)

		time.Sleep(time.Duration(rand.Int()%200) * time.Millisecond)
		if (rand.Int() % 1000) < 500 {
			tc.kill1(0, i%3, false)
		} else {
			tc.kill1(0, i%3, true)
		}
		time.Sleep(1000 * time.Millisecond)
		vx := ck.Get(k1)
		checkAppends(t, vx, counts)
		tc.start1(0, i%3)
		time.Sleep(2200 * time.Millisecond)

		z := <-ch
		if z != 1 {
			t.Fatalf("Append thread failed")
		}
		counts[0] += z
	}

	fmt.Printf("  ... Passed\n")
}

//
// recovery with mixture of lost disks and simple reboot.
// does a replica that loses its disk wait for majority?
//
func Test5RejoinMix1(t *testing.T) {
	tc := setup(t, "rejoinmix1", 1, 5, false)
	defer tc.cleanup()

	fmt.Printf("Test: replica waits correctly after disk loss ...\n")

	tc.join(0)
	ck := tc.clerk()

	k1 := randstring(10)
	k1v := ""

	for i := 0; i < 7+(rand.Int()%7); i++ {
		x := randstring(10)
		ck.Append(k1, x)
		k1v += x
	}

	time.Sleep(300 * time.Millisecond)
	ck.Get(k1)

	tc.kill1(0, 0, false)

	for i := 0; i < 2; i++ {
		x := randstring(10)
		ck.Append(k1, x)
		k1v += x
	}

	time.Sleep(300 * time.Millisecond)
	ck.Get(k1)
	time.Sleep(300 * time.Millisecond)

	tc.kill1(0, 1, true)
	tc.kill1(0, 2, true)

	tc.kill1(0, 3, false)
	tc.kill1(0, 4, false)

	tc.start1(0, 0)
	tc.start1(0, 1)
	tc.start1(0, 2)
	time.Sleep(300 * time.Millisecond)

	// check that requests are not executed.
	ch := make(chan string)
	go func() {
		ck1 := tc.clerk()
		v := ck1.Get(k1)
		ch <- v
	}()

	select {
	case <-ch:
		t.Fatalf("Get should not have succeeded.")
	case <-time.After(3 * time.Second):
		// this is what we hope for.
	}

	tc.start1(0, 3)
	tc.start1(0, 4)

	{
		x := randstring(10)
		ck.Append(k1, x)
		k1v += x
	}

	v := ck.Get(k1)
	if v != k1v {
		t.Fatalf("Get returned wrong value")
	}

	fmt.Printf("  ... Passed\n")
}

//
// does a replica that loses its state avoid
// changing its mind about Paxos agreements?
//
func Test5RejoinMix3(t *testing.T) {
	tc := setup(t, "rejoinmix3", 1, 5, false)
	defer tc.cleanup()

	fmt.Printf("Test: replica Paxos resumes correctly after disk loss ...\n")

	tc.join(0)
	ck := tc.clerk()

	k1 := randstring(10)
	k1v := ""

	for i := 0; i < 7+(rand.Int()%7); i++ {
		x := randstring(10)
		ck.Append(k1, x)
		k1v += x
	}

	time.Sleep(300 * time.Millisecond)
	ck.Get(k1)

	// kill R1, R2.
	tc.kill1(0, 1, false)
	tc.kill1(0, 2, false)

	// R0, R3, and R4 are up.
	for i := 0; i < 100+(rand.Int()%7); i++ {
		x := randstring(10)
		ck.Append(k1, x)
		k1v += x
	}

	// kill R0, lose disk.
	tc.kill1(0, 0, true)

	time.Sleep(50 * time.Millisecond)

	// restart R1, R2, R0.
	tc.start1(0, 1)
	tc.start1(0, 2)
	time.Sleep(1 * time.Millisecond)
	tc.start1(0, 0)

	chx := make(chan bool)
	x1 := randstring(10)
	x2 := randstring(10)
	go func() { ck.Append(k1, x1); chx <- true }()
	time.Sleep(10 * time.Millisecond)
	go func() { ck.Append(k1, x2); chx <- true }()

	<-chx
	<-chx

	xv := ck.Get(k1)
	if xv == k1v+x1+x2 || xv == k1v+x2+x1 {
		// ok
	} else {
		t.Fatalf("wrong value")
	}

	fmt.Printf("  ... Passed\n")
}
