package fix

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/rules"
)

const gib = 1 << 30

type fakeEnv struct {
	os, home, cacheDir string
	env                map[string]string
	tools              []string
	sizes              map[string]uint64
	largest            []File
	others             map[int32]bool // PIDs owned by someone else
	ancestors          map[int32]bool
	bundles            map[string]string
	docker             uint64
}

func (e fakeEnv) OS() string             { return e.os }
func (e fakeEnv) Home() string           { return e.home }
func (e fakeEnv) CacheDir() string       { return e.cacheDir }
func (e fakeEnv) Getenv(k string) string { return e.env[k] }
func (e fakeEnv) LookPath(t string) bool { return slices.Contains(e.tools, t) }
func (e fakeEnv) SystemTool(n string) string {
	if e.os == "windows" {
		return `C:\Windows\System32\` + n + ".exe"
	}
	return n
}
func (e fakeEnv) DirSize(_ context.Context, d string) (uint64, bool) {
	s, ok := e.sizes[d]
	return s, ok
}
func (e fakeEnv) LargestFiles(string, int) []File { return e.largest }
func (e fakeEnv) Mine(pid int32) bool             { return !e.others[pid] }
func (e fakeEnv) Ancestors() map[int32]bool       { return e.ancestors }
func (e fakeEnv) BundleID(app string) string      { return e.bundles[app] }
func (e fakeEnv) DockerBuildCache(context.Context) (uint64, bool) {
	return e.docker, e.docker > 0
}

func mac() fakeEnv {
	return fakeEnv{
		os: "darwin", home: "/Users/me", cacheDir: "/Users/me/Library/Caches",
		sizes: map[string]uint64{}, others: map[int32]bool{}, ancestors: map[int32]bool{},
		bundles: map[string]string{
			"/Applications/Google Chrome.app": "com.google.Chrome",
			"/Applications/Slack.app":         "com.tinyspeck.slackmacgap",
			"/Applications/iTerm.app":         "com.googlecode.iterm2",
		},
	}
}

func ids(fs []Fix) (out []string) {
	for _, f := range fs {
		out = append(out, f.ID)
	}
	return out
}

func finding(check string, sev rules.Severity) rules.Finding {
	return rules.Finding{Check: check, Severity: sev}
}

func TestNothingToFixWhenAllOK(t *testing.T) {
	fs := []rules.Finding{finding(rules.CheckMemory, rules.OK), finding(rules.CheckDisk, rules.OK)}
	s := collect.Snapshot{Processes: []collect.Process{{PID: 1, Exe: "/Applications/Slack.app/Contents/MacOS/Slack", RSS: 4 * gib}}}
	if got := Plan(context.Background(), s, fs, mac()); len(got) != 0 {
		t.Errorf("got %v", ids(got))
	}
}

func TestQuitOnMac(t *testing.T) {
	s := collect.Snapshot{Processes: []collect.Process{
		{PID: 10, Exe: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", RSS: 2 * gib},
		{PID: 11, Exe: "/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Helper (Renderer).app/Contents/MacOS/Google Chrome Helper (Renderer)", RSS: 2 * gib},
		{PID: 20, Exe: "/System/Library/PrivateFrameworks/SkyLight.framework/Resources/WindowServer.app/Contents/MacOS/WindowServer", RSS: 3 * gib},
		{PID: 30, Exe: "/Applications/iTerm.app/Contents/MacOS/iTerm2", RSS: 1 * gib},
		{PID: 40, Exe: "/usr/local/bin/node", Name: "node", RSS: 1 * gib},
	}}
	env := mac()
	env.ancestors[30] = true // lagdoc runs inside iTerm
	got := Plan(context.Background(), s, []rules.Finding{finding(rules.CheckMemory, rules.Crit)}, env)
	if !slices.Equal(ids(got), []string{"quit:Google Chrome", "keep:iTerm"}) {
		t.Fatalf("got %v", ids(got))
	}
	q := got[0].Quit
	if q.BundleID != "com.google.Chrome" || !slices.Equal(q.PIDs, []int32{10, 11}) {
		t.Errorf("quit: %+v", q)
	}
	if got[0].Title != "Quit Google Chrome (2 processes), using 4.0 GB" || got[0].Action != "Quit Google Chrome" {
		t.Errorf("title: %q", got[0].Title)
	}
	if !got[1].Manual || got[1].Quit != nil {
		t.Errorf("the terminal's app must only be mentioned: %+v", got[1])
	}
}

func TestQuitSkipsOtherUsersAndUnknownBundles(t *testing.T) {
	s := collect.Snapshot{Processes: []collect.Process{
		{PID: 10, Exe: "/Applications/Slack.app/Contents/MacOS/Slack", RSS: 2 * gib},
		{PID: 50, Exe: "/Applications/Mystery.app/Contents/MacOS/Mystery", RSS: 2 * gib}, // no bundle ID
	}}
	env := mac()
	env.others[10] = true
	if got := Plan(context.Background(), s, []rules.Finding{finding(rules.CheckMemory, rules.Warn)}, env); len(got) != 0 {
		t.Errorf("got %v", ids(got))
	}
}

func TestQuitOnLinuxAndWindows(t *testing.T) {
	ps := []collect.Process{
		{PID: 1, Name: "gnome-shell", RSS: 2 * gib},
		{PID: 2, Name: "firefox", Exe: "/usr/lib/firefox/firefox", RSS: 3 * gib},
		{PID: 3, Name: "explorer.exe", RSS: 2 * gib},
	}
	linux := fakeEnv{os: "linux", others: map[int32]bool{}, ancestors: map[int32]bool{}}
	got := Plan(context.Background(), collect.Snapshot{Processes: ps}, []rules.Finding{finding(rules.CheckMemory, rules.Crit)}, linux)
	// App names drop ".exe" everywhere; only Windows keeps Explorer.
	if !slices.Equal(ids(got), []string{"quit:firefox", "quit:explorer"}) {
		t.Errorf("linux: %v", ids(got))
	}
	// On Windows the name comes without .exe, and Explorer is the desktop.
	ps[1] = collect.Process{PID: 2, Name: "firefox.exe", RSS: 3 * gib}
	win := linux
	win.os = "windows"
	got = Plan(context.Background(), collect.Snapshot{Processes: ps}, []rules.Finding{finding(rules.CheckMemory, rules.Crit)}, win)
	if !slices.Equal(ids(got), []string{"quit:firefox", "quit:gnome-shell"}) {
		t.Errorf("windows: %v", ids(got))
	}
}

func TestCPUQuitsAreNotRepeated(t *testing.T) {
	s := collect.Snapshot{Processes: []collect.Process{
		{PID: 10, Exe: "/Applications/Slack.app/Contents/MacOS/Slack", RSS: 2 * gib, CPUPercent: 150},
	}}
	fs := []rules.Finding{finding(rules.CheckMemory, rules.Crit), finding(rules.CheckCPU, rules.Warn)}
	got := Plan(context.Background(), s, fs, mac())
	if !slices.Equal(ids(got), []string{"quit:Slack"}) || got[0].Check != rules.CheckMemory {
		t.Errorf("got %v", got)
	}
	fs = []rules.Finding{finding(rules.CheckCPU, rules.Warn)}
	if got := Plan(context.Background(), s, fs, mac()); got[0].Title != "Quit Slack, using 150% CPU" {
		t.Errorf("cpu title: %q", got[0].Title)
	}
}

// full is a nearly full 250 GB volume; spare is a roomy one.
var (
	full  = collect.Disk{Mount: "/", Total: 250e9, Free: 10e9}
	spare = collect.Disk{Mount: "/Volumes/Data", Total: 2e12, Free: 1e12}
)

// skipOnWindowsHost skips tests that use macOS or Linux paths: Plan joins
// paths with the host's separator, so they only line up on a Unix host.
func skipOnWindowsHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix paths")
	}
}

func TestDiskCaches(t *testing.T) {
	skipOnWindowsHost(t)
	env := mac()
	env.tools = []string{"go", "npm", "brew", "pip3", "docker"}
	env.sizes = map[string]uint64{
		"/Users/me/Library/Caches/go-build": 6e9,
		"/Users/me/.npm/_cacache":           100e6, // too small to bother
		"/Users/me/Library/Caches/Homebrew": 2e9,
		"/Volumes/Data/pip":                 9e9, // on a volume with room
		"/Users/me/.Trash":                  3e9,
		"/Users/me/Downloads":               12e9,
	}
	env.env = map[string]string{"PIP_CACHE_DIR": "/Volumes/Data/pip"}
	env.docker = 4e9
	env.largest = []File{{"Xcode.xip", 7.9e9}, {"movie.mp4", 2e9}}
	s := collect.Snapshot{Disks: []collect.Disk{full, spare}}
	got := Plan(context.Background(), s, []rules.Finding{finding(rules.CheckDisk, rules.Crit)}, env)
	// Runnable first, by size; then manual ones (Downloads has no estimate:
	// not everything in it can go).
	want := []string{"cache:go", "cache:docker", "cache:brew", "trash", "downloads"}
	if !slices.Equal(ids(got), want) {
		t.Fatalf("got %v, want %v", ids(got), want)
	}
	if g := got[0]; g.Title != "Clear Go's build cache, about 6.0 GB" || !slices.Equal(g.Command, []string{"go", "clean", "-cache"}) || g.Mount != "/" || g.Frees != 6e9 {
		t.Errorf("go: %+v", g)
	}
	if d := got[4]; !d.Manual || !slices.Equal(d.Steps, []string{"Xcode.xip 7.9 GB", "movie.mp4 2.0 GB"}) {
		t.Errorf("downloads: %+v", d)
	}
	for _, f := range got[3:] {
		if !f.Manual || len(f.Command) > 0 {
			t.Errorf("%s touches the person's files and must be manual", f.ID)
		}
	}
}

func TestDiskCachesNeedTheTool(t *testing.T) {
	skipOnWindowsHost(t)
	env := mac()
	env.sizes = map[string]uint64{"/Users/me/Library/Caches/go-build": 6e9}
	s := collect.Snapshot{Disks: []collect.Disk{full}}
	if got := Plan(context.Background(), s, []rules.Finding{finding(rules.CheckDisk, rules.Crit)}, env); len(got) != 0 {
		t.Errorf("go isn't installed: %v", ids(got))
	}
	env.tools = []string{"go"}
	env.env = map[string]string{"GOCACHE": "off"}
	if got := Plan(context.Background(), s, []rules.Finding{finding(rules.CheckDisk, rules.Crit)}, env); len(got) != 0 {
		t.Errorf("GOCACHE=off: %v", ids(got))
	}
}

func TestDiskCachesOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("uses Windows paths")
	}
	env := fakeEnv{
		os: "windows", home: `C:\Users\me`, cacheDir: `C:\Users\me\AppData\Local`,
		tools: []string{"go", "npm", "brew"},
		sizes: map[string]uint64{
			`C:\Users\me\AppData\Local\npm-cache\_cacache`: 2e9,
			`C:\Users\me\AppData\Local\go-build`:           3e9,
			`C:\Users\me\AppData\Local\Homebrew`:           9e9, // no Homebrew on Windows
		},
	}
	s := collect.Snapshot{Disks: []collect.Disk{{Mount: "C:", Total: 250e9, Free: 10e9}}}
	got := Plan(context.Background(), s, []rules.Finding{finding(rules.CheckDisk, rules.Crit)}, env)
	if !slices.Equal(ids(got), []string{"cache:go", "cache:npm", "windows-storage"}) {
		t.Errorf("got %v", ids(got))
	}
}

func TestLinuxSystemCachesAreManual(t *testing.T) {
	env := fakeEnv{
		os: "linux", home: "/home/me", cacheDir: "/home/me/.cache",
		tools: []string{"apt-get", "journalctl"},
		sizes: map[string]uint64{"/var/cache/apt/archives": 2e9, "/var/log/journal": 3e9},
	}
	s := collect.Snapshot{Disks: []collect.Disk{full}}
	got := Plan(context.Background(), s, []rules.Finding{finding(rules.CheckDisk, rules.Warn)}, env)
	if !slices.Equal(ids(got), []string{"journal", "apt"}) {
		t.Fatalf("got %v", ids(got))
	}
	for _, f := range got {
		if !f.Manual || !strings.HasPrefix(f.Steps[0], "sudo ") {
			t.Errorf("%s needs root and must be manual: %+v", f.ID, f)
		}
	}
}

func TestNetworkFixes(t *testing.T) {
	slow := collect.Snapshot{Network: collect.Network{DNS: 2 * time.Second, Dial: 20 * time.Millisecond}}
	net := []rules.Finding{finding(rules.CheckNetwork, rules.Warn)}

	win := fakeEnv{os: "windows"}
	got := Plan(context.Background(), slow, net, win)
	if len(got) != 1 || got[0].Manual || got[0].Command[0] != `C:\Windows\System32\ipconfig.exe` {
		t.Errorf("windows flushes DNS itself: %+v", got)
	}
	if got := Plan(context.Background(), slow, net, mac()); len(got) != 1 || !got[0].Manual || got[0].Steps[0] != "sudo dscacheutil -flushcache" {
		t.Errorf("macOS needs sudo, so it's manual: %+v", got)
	}
	if got := Plan(context.Background(), slow, net, fakeEnv{os: "linux"}); len(got) != 0 {
		t.Errorf("no resolvectl, nothing to suggest: %+v", got)
	}

	offline := collect.Snapshot{Network: collect.Network{DNSErr: "x", DialErr: "y"}}
	if got := Plan(context.Background(), offline, []rules.Finding{finding(rules.CheckNetwork, rules.Crit)}, win); !slices.Equal(ids(got), []string{"network:check"}) {
		t.Errorf("offline: %v", ids(got))
	}
}

func TestMountFor(t *testing.T) {
	disks := []collect.Disk{{Mount: "/"}, {Mount: "/home"}, {Mount: "/home/me/big"}}
	cases := map[string]string{
		"/home/me/.cache": "/home", "/home": "/home", "/homework": "/", "/home/me/big/x": "/home/me/big", "/var": "/",
	}
	for in, want := range cases {
		if got := mountFor(in, disks, false); got != want {
			t.Errorf("%s: got %q, want %q", in, got, want)
		}
	}
	win := []collect.Disk{{Mount: "C:"}, {Mount: "D:"}}
	if got := mountFor(`c:\Users\me\AppData\Local`, win, true); got != "C:" {
		t.Errorf("windows: %q", got)
	}
	if got := mountFor(`E:\x`, win, true); got != "" {
		t.Errorf("unknown drive: %q", got)
	}
}

func TestParseDocker(t *testing.T) {
	out := []byte(`{"Active":"3","Reclaimable":"2.1GB (45%)","Size":"4.6GB","TotalCount":"12","Type":"Images"}
{"Active":"0","Reclaimable":"1.234GB","Size":"1.234GB","TotalCount":"40","Type":"Build Cache"}`)
	if n, ok := parseDockerDF(out); !ok || n != 1.234e9 {
		t.Errorf("got %d %v", n, ok)
	}
	for in, want := range map[string]uint64{"0B": 0, "512kB": 512e3, "3.5MB": 3.5e6, "1TB": 1e12} {
		if got, ok := parseDockerSize(in); !ok || got != want {
			t.Errorf("%s: %d %v", in, got, ok)
		}
	}
	if _, ok := parseDockerSize("lots"); ok {
		t.Error("garbage parsed")
	}
}

type fakeRunner struct {
	execs   [][]string
	execErr error
	output  string
	alive   map[int32]int // PID -> how many more Alive checks it survives
	parents map[int32]int32
	termed  []int32
	free    []uint64
}

func (r *fakeRunner) Exec(_ context.Context, name string, args ...string) ([]byte, error) {
	r.execs = append(r.execs, append([]string{name}, args...))
	return []byte(r.output), r.execErr
}

func (r *fakeRunner) Alive(pids []int32) []int32 {
	var out []int32
	for _, p := range pids {
		if n, ok := r.alive[p]; ok && n > 0 {
			r.alive[p]--
			out = append(out, p)
		}
	}
	return out
}

func (r *fakeRunner) Parent(pid int32) (int32, error) {
	if p, ok := r.parents[pid]; ok {
		return p, nil
	}
	return 0, errors.New("gone")
}
func (r *fakeRunner) Terminate(pid int32) error { r.termed = append(r.termed, pid); return nil }
func (r *fakeRunner) Free(string) (uint64, bool) {
	if len(r.free) == 0 {
		return 0, false
	}
	v := r.free[0]
	r.free = r.free[1:]
	return v, true
}
func (r *fakeRunner) Sleep(time.Duration) {}

func TestRunQuitMac(t *testing.T) {
	r := &fakeRunner{alive: map[int32]int{10: 3, 11: 3}}
	res := runQuit(context.Background(), &Quit{App: "Chrome", BundleID: `com.google.Chrome" to do shell script "x`, PIDs: []int32{10, 11}}, r, "darwin")
	if res.Status != Done {
		t.Errorf("got %+v", res)
	}
	e := r.execs[0]
	if e[0] != "/usr/bin/osascript" || e[len(e)-1] != `com.google.Chrome" to do shell script "x` {
		t.Errorf("the bundle ID must be an argument, not script text: %q", e)
	}
	for _, a := range e[1 : len(e)-1] {
		if strings.Contains(a, "com.google") {
			t.Errorf("bundle ID leaked into the script: %q", a)
		}
	}
}

func TestRunQuitStillOpen(t *testing.T) {
	r := &fakeRunner{alive: map[int32]int{10: 1 << 20}}
	res := runQuit(context.Background(), &Quit{App: "Editor", BundleID: "x", PIDs: []int32{10}}, r, "darwin")
	if res.Status != Pending || !strings.Contains(res.Message, "save") {
		t.Errorf("got %+v", res)
	}
}

func TestRunQuitSignalsOnlyRoots(t *testing.T) {
	// 10 is the app, 11 and 12 its helpers; 13 was started by something else.
	r := &fakeRunner{
		alive:   map[int32]int{10: 2, 11: 2, 12: 2, 13: 2},
		parents: map[int32]int32{10: 1, 11: 10, 12: 10, 13: 99},
	}
	res := runQuit(context.Background(), &Quit{App: "firefox", PIDs: []int32{10, 11, 12, 13}}, r, "linux")
	if res.Status != Done || !slices.Equal(r.termed, []int32{10, 13}) {
		t.Errorf("got %+v, signalled %v", res, r.termed)
	}

	r = &fakeRunner{alive: map[int32]int{10: 2, 11: 2}, parents: map[int32]int32{10: 1, 11: 10}}
	runQuit(context.Background(), &Quit{App: "firefox", PIDs: []int32{10, 11}}, r, "windows")
	if got := r.execs[0][1:]; !slices.Equal(got, []string{"/PID", "10"}) || slices.Contains(r.execs[0], "/F") {
		t.Errorf("taskkill must close windows politely: %q", r.execs[0])
	}
}

func TestRunCommand(t *testing.T) {
	f := Fix{Command: []string{"go", "clean", "-cache"}, Mount: "/"}
	r := &fakeRunner{free: []uint64{10e9, 16.3e9}}
	if res := Run(context.Background(), f, r); res.Status != Done || res.Message != "freed 6.3 GB" {
		t.Errorf("got %+v", res)
	}
	r = &fakeRunner{execErr: errors.New("exit status 1"), output: "working...\nError: permission denied\n"}
	if res := Run(context.Background(), f, r); res.Status != Failed || res.Message != "Error: permission denied" {
		t.Errorf("got %+v", res)
	}
}

func TestManualFixesNeverRun(t *testing.T) {
	r := &fakeRunner{}
	res := Run(context.Background(), Fix{Manual: true, Command: []string{"rm", "-rf", "/"}}, r)
	if res.Status != Failed || len(r.execs) != 0 {
		t.Errorf("got %+v, ran %v", res, r.execs)
	}
}

func TestCommandLine(t *testing.T) {
	if got := CommandLine([]string{`C:\Windows\System32\ipconfig.exe`, "/flushdns"}); got != "ipconfig /flushdns" {
		t.Errorf("got %q", got)
	}
}
