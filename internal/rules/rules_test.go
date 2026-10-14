package rules

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
)

const (
	mib = 1 << 20
)

// healthy is a machine with nothing wrong; tests change one thing at a time.
func healthy() collect.Snapshot {
	return collect.Snapshot{
		Memory: collect.Memory{Total: 16 * gib, Available: 10 * gib},
		CPU:    collect.CPU{Percent: 12, Sample: time.Second},
		Disks:  []collect.Disk{{Mount: "/", FSType: "apfs", Total: 500 * gb, Free: 300 * gb}},
		Network: collect.Network{
			DNSHost: "cloudflare.com", DNS: 12 * time.Millisecond,
			DialAddr: "1.1.1.1:443", Dial: 15 * time.Millisecond,
		},
	}
}

func hasDetail(f Finding, sub string) bool {
	return slices.ContainsFunc(f.Details, func(d string) bool { return strings.Contains(d, sub) })
}

func TestMemoryLinuxUsesAvailable(t *testing.T) {
	cases := []struct {
		avail uint64
		want  Severity
	}{
		{10 * gib, OK},
		{3 * gib, Warn}, // 81 %
		{1 * gib, Crit}, // 94 %
		{20 * gib, OK},  // nonsense Available > Total is clamped
	}
	for _, c := range cases {
		s := healthy()
		s.Memory.Available = c.avail
		if got := Memory(s).Severity; got != c.want {
			t.Errorf("available %d GiB: got %v, want %v", c.avail/gib, got, c.want)
		}
	}
}

func TestMemoryMacUsesPressureNotUsed(t *testing.T) {
	// macOS reports little "available" because it keeps RAM full of cache
	// and compressed pages. With the kernel saying 65 % free, that's fine.
	s := healthy()
	s.Memory = collect.Memory{
		Total: 16 * gib, Available: 1 * gib, Compressed: 2 * gib,
		Pressure: &collect.Pressure{FreePercent: 65, Level: "normal"},
	}
	f := Memory(s)
	if f.Severity != OK || *f.Percent != 35 {
		t.Fatalf("got %v at %v%%, want ok at 35%%", f.Severity, *f.Percent)
	}
	if !hasDetail(f, "compressed 2.0 GB") {
		t.Errorf("details %q should mention compressed memory", f.Details)
	}
}

func TestMemoryMacKernelLevelSetsFloor(t *testing.T) {
	for level, want := range map[string]Severity{"normal": OK, "warn": Warn, "critical": Crit} {
		s := healthy()
		s.Memory.Pressure = &collect.Pressure{FreePercent: 50, Level: level}
		if got := Memory(s).Severity; got != want {
			t.Errorf("level %s: got %v, want %v", level, got, want)
		}
	}
	s := healthy()
	s.Memory.Pressure = &collect.Pressure{FreePercent: 5, Level: "normal"}
	if got := Memory(s).Severity; got != Crit {
		t.Errorf("5%% free at level normal: got %v, want crit", got)
	}
}

func TestMemoryHeavySwapWarns(t *testing.T) {
	s := healthy()
	s.Memory.SwapTotal, s.Memory.SwapUsed = 10*gib, 9*gib
	f := Memory(s)
	if f.Severity != Warn || f.Summary != "heavy swapping" {
		t.Fatalf("got %v %q, want warn heavy swapping", f.Severity, f.Summary)
	}
	if !hasDetail(f, "swap in use 9.0 GB") {
		t.Errorf("details %q should mention swap", f.Details)
	}

	// Some swap on a healthy machine is normal and only noted.
	s.Memory.SwapUsed = 2 * gib
	if f := Memory(s); f.Severity != OK || !hasDetail(f, "swap in use 2.0 GB") {
		t.Errorf("2 GiB swap: got %v %q", f.Severity, f.Details)
	}
	s.Memory.SwapUsed = 100 * mib
	if f := Memory(s); hasDetail(f, "swap") {
		t.Errorf("tiny swap shouldn't be mentioned: %q", f.Details)
	}
}

func TestMemoryNamesBiggestAppsGrouped(t *testing.T) {
	s := healthy()
	s.Memory.Available = 1 * gib
	chrome := "/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/Helpers/Google Chrome Helper (Renderer).app/Contents/MacOS/Google Chrome Helper (Renderer)"
	s.Processes = []collect.Process{
		{PID: 1, Name: "Google Chrome Helper (Renderer)", Exe: chrome, RSS: 2 * gib},
		{PID: 2, Name: "Google Chrome Helper (Renderer)", Exe: chrome, RSS: 1536 * mib},
		{PID: 3, Name: "Google Chrome", Exe: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", RSS: 512 * mib},
		{PID: 4, Name: "Slack", Exe: "/Applications/Slack.app/Contents/MacOS/Slack", RSS: 800 * mib},
		{PID: 5, Name: "tiny", RSS: 50 * mib},
	}
	f := Memory(s)
	want := []string{"Google Chrome (3 processes) 4.0 GB", "Slack 800 MB"}
	if !slices.Equal(f.Details, want) {
		t.Errorf("details = %q, want %q", f.Details, want)
	}
}

func TestMemoryUnknownWhenNotCollected(t *testing.T) {
	s := healthy()
	s.Memory = collect.Memory{}
	s.Errors = map[string]string{"memory": "boom"}
	f := Memory(s)
	if f.Severity != Unknown || !hasDetail(f, "boom") {
		t.Errorf("got %v %q", f.Severity, f.Details)
	}
}

func TestDiskThresholds(t *testing.T) {
	cases := []struct {
		name        string
		total, free uint64
		want        Severity
	}{
		{"plenty", 500 * gb, 300 * gb, OK},
		{"big drive 92% but 320 GB free", 4000 * gb, 320 * gb, OK},
		{"this Mac: 92% with 20 GB free", 250 * gb, 20 * gb, Warn},
		{"under 10 GB free", 60 * gb, 8 * gb, Warn},
		{"96% with 10 GB", 250 * gb, 10 * gb, Crit},
		{"under 2 GB free", 4000 * gb, 1 * gb, Crit},
		{"free > total is clamped", 100 * gb, 200 * gb, OK},
		// Small volumes: under 2 GB free is normal for a 1 GB /boot.
		{"small /boot, mostly empty", 923e6, 792e6, OK},
		{"small /boot, 95% full", 923e6, 40e6, Crit},
		{"32 GB card half empty", 32 * gb, 16 * gb, OK},
	}
	for _, c := range cases {
		s := healthy()
		s.Disks = []collect.Disk{{Mount: "/", Total: c.total, Free: c.free}}
		if got := Disk(s).Severity; got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDiskReportsWorstMountAndOnlyNamesProblems(t *testing.T) {
	s := healthy()
	s.Disks = []collect.Disk{
		{Mount: "/", Total: 500 * gb, Free: 300 * gb},
		{Mount: "/home", Total: 250 * gb, Free: 15 * gb},
		{Mount: "/data", Total: 1000 * gb, Free: 900 * gb},
	}
	f := Disk(s)
	if f.Severity != Warn || f.Summary != "getting full" {
		t.Fatalf("got %v %q", f.Severity, f.Summary)
	}
	if *f.Percent != 94 {
		t.Errorf("percent = %v, want 94 (the /home volume)", *f.Percent)
	}
	if want := []string{"/home 15.0 GB free of 250 GB"}; !slices.Equal(f.Details, want) {
		t.Errorf("details = %q, want %q", f.Details, want)
	}
}

func TestDiskAllFineListsMounts(t *testing.T) {
	s := healthy()
	s.Disks = append(s.Disks, collect.Disk{Mount: "D:", Total: 2000 * gb, Free: 1500 * gb})
	f := Disk(s)
	if f.Severity != OK || len(f.Details) != 2 || !strings.HasPrefix(f.Details[0], "/ ") {
		t.Errorf("got %v %q", f.Severity, f.Details)
	}
}

func TestDiskUnknownWithNoMounts(t *testing.T) {
	s := healthy()
	s.Disks = nil
	if got := Disk(s).Severity; got != Unknown {
		t.Errorf("got %v", got)
	}
}

func TestCPUThresholdsAndTopApps(t *testing.T) {
	cases := map[float64]Severity{10: OK, 50: OK, 80: Warn, 97: Crit}
	for p, want := range cases {
		s := healthy()
		s.CPU.Percent = p
		if got := CPU(s).Severity; got != want {
			t.Errorf("%v%%: got %v, want %v", p, got, want)
		}
	}

	s := healthy()
	s.CPU.Percent = 92
	s.Processes = []collect.Process{
		{PID: 1, Name: "Xcode", Exe: "/Applications/Xcode.app/Contents/MacOS/Xcode", CPUPercent: 350},
		{PID: 2, Name: "kernel_task", CPUPercent: 40},
		{PID: 3, Name: "idle", CPUPercent: 2},
	}
	f := CPU(s)
	if want := []string{"Xcode 3.5 cores", "kernel_task 40% of a core"}; !slices.Equal(f.Details, want) {
		t.Errorf("details = %q, want %q", f.Details, want)
	}
}

func TestCPUUnknownWithoutSample(t *testing.T) {
	s := healthy()
	s.CPU = collect.CPU{}
	if got := CPU(s).Severity; got != Unknown {
		t.Errorf("got %v", got)
	}
}

func TestNetwork(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*collect.Network)
		want    Severity
		summary string
	}{
		{"fine", func(*collect.Network) {}, OK, "dns 12 ms"},
		{"slow dns", func(n *collect.Network) { n.DNS = 900 * time.Millisecond }, Warn, "slow dns 900 ms"},
		{"slow connect", func(n *collect.Network) { n.Dial = 700 * time.Millisecond }, Warn, "slow connect 700 ms"},
		{"dns down", func(n *collect.Network) { n.DNSErr = "no such host" }, Crit, "dns not answering"},
		{"no route", func(n *collect.Network) { n.DialErr = "timeout" }, Crit, "can't reach the internet"},
		{"offline", func(n *collect.Network) { n.DNSErr, n.DialErr = "x", "y" }, Crit, "offline"},
	}
	for _, c := range cases {
		s := healthy()
		c.mutate(&s.Network)
		f := Network(s)
		if f.Severity != c.want || f.Summary != c.summary {
			t.Errorf("%s: got %v %q, want %v %q", c.name, f.Severity, f.Summary, c.want, c.summary)
		}
		if f.Percent != nil {
			t.Errorf("%s: network has no bar", c.name)
		}
	}
}

func TestEvaluateRanksWorstFirst(t *testing.T) {
	s := healthy()
	s.Disks[0].Free = 20 * gb      // warn
	s.Memory.Available = 512 * mib // crit
	fs := Evaluate(s)
	var order []string
	for _, f := range fs {
		order = append(order, f.Check)
	}
	if want := []string{"memory", "disk", "cpu", "network"}; !slices.Equal(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}
	if Causes(fs) != 2 || Worst(fs) != Crit {
		t.Errorf("causes %d worst %v", Causes(fs), Worst(fs))
	}

	s = healthy()
	s.Network.DialErr = "down"
	if fs := Evaluate(s); fs[0].Check != "network" {
		t.Errorf("a failing network check should rank first, got %s", fs[0].Check)
	}
	if fs := Evaluate(healthy()); Causes(fs) != 0 || Worst(fs) != OK {
		t.Errorf("healthy machine: causes %d worst %v", Causes(fs), Worst(fs))
	}
}

func TestSeverityJSON(t *testing.T) {
	b, _ := json.Marshal([]Severity{Unknown, OK, Warn, Crit})
	if string(b) != `["unknown","ok","warn","crit"]` {
		t.Errorf("got %s", b)
	}
	var s Severity
	if err := json.Unmarshal([]byte(`"warn"`), &s); err != nil || s != Warn {
		t.Errorf("unmarshal: %v %v", s, err)
	}
	if err := json.Unmarshal([]byte(`"bad"`), &s); err == nil {
		t.Error("unknown severity name should fail")
	}
}

func TestAppName(t *testing.T) {
	cases := []struct {
		p    collect.Process
		want string
	}{
		{collect.Process{Name: "Code Helper (Renderer)", Exe: "/Applications/Visual Studio Code.app/Contents/Frameworks/Code Helper (Renderer).app/Contents/MacOS/Code Helper (Renderer)"}, "Visual Studio Code"},
		{collect.Process{Name: "plugin-container", Exe: "/Applications/Firefox.app/Contents/MacOS/plugin-container.app/Contents/MacOS/plugin-container"}, "Firefox"},
		{collect.Process{Name: "chrome.exe", Exe: `C:\Program Files\Google\Chrome\Application\chrome.exe`}, "chrome"},
		{collect.Process{Name: "Slack Helper (GPU)"}, "Slack"},
		{collect.Process{Name: "Code Helper"}, "Code"},
		{collect.Process{Name: "firefox", Exe: "/usr/lib/firefox/firefox"}, "firefox"},
		{collect.Process{Name: "kernel_task"}, "kernel_task"},
	}
	for _, c := range cases {
		if got := AppName(c.p); got != c.want {
			t.Errorf("AppName(%q) = %q, want %q", c.p.Name, got, c.want)
		}
	}
}

func TestVerdict(t *testing.T) {
	cases := []struct {
		sevs []Severity
		want string
	}{
		{[]Severity{OK, OK}, "ok"},
		{[]Severity{OK, Unknown}, "ok"},
		{[]Severity{Warn, OK}, "warn"},
		{[]Severity{Warn, Crit}, "slow"},
		{[]Severity{Unknown}, "unknown"},
		{nil, "unknown"},
	}
	for _, c := range cases {
		var fs []Finding
		for _, s := range c.sevs {
			fs = append(fs, Finding{Severity: s})
		}
		if got := Verdict(fs); got != c.want {
			t.Errorf("Verdict(%v) = %q, want %q", c.sevs, got, c.want)
		}
	}
}
