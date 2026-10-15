package live

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/render"
	"github.com/ag3ntsc4rn/lagdoc/internal/rules"
)

// fakeSampler returns the snapshots in order, then repeats the last.
func fakeSampler(snaps ...collect.Snapshot) (SampleFunc, *int) {
	calls := 0
	return func(context.Context) collect.Snapshot {
		s := snaps[min(calls, len(snaps)-1)]
		calls++
		return s
	}, &calls
}

func snap(at time.Time, freeDisk uint64, procs ...collect.Process) collect.Snapshot {
	return collect.Snapshot{
		TakenAt:   at,
		Host:      collect.Host{Hostname: "studio"},
		Memory:    collect.Memory{Total: 16 << 30, Available: 8 << 30},
		CPU:       collect.CPU{Percent: 10, Sample: time.Second},
		Disks:     []collect.Disk{{Mount: "/", Total: 250e9, Free: freeDisk}},
		Processes: procs,
	}
}

func key(k string) tea.KeyMsg {
	switch k {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func TestTopSortsAndScrolls(t *testing.T) {
	procs := []collect.Process{
		{PID: 1, Name: "big", RSS: 3 << 30, CPUPercent: 1},
		{PID: 2, Name: "busy", RSS: 1 << 30, CPUPercent: 90},
		{PID: 3, Name: "small", RSS: 1 << 20},
	}
	sample, _ := fakeSampler(snap(time.Now(), 100e9, procs...))
	var m tea.Model = NewTop(context.Background(), sample, render.NewScreen(&bytes.Buffer{}, false), time.Second, render.SortMemory)
	if !strings.Contains(m.View(), "measuring") {
		t.Error("before the first sample it says it's measuring")
	}
	m, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 8}) // two rows
	if cmd != nil {
		t.Error("resizing needs no command")
	}
	m, cmd = m.Update(cmd0(m.Init()))
	if cmd == nil {
		t.Fatal("a sample schedules the next tick")
	}
	top := m.(Top)
	if top.apps[0].Name != "big" {
		t.Errorf("memory first: %v", top.apps)
	}
	m, _ = m.Update(key("c"))
	if m.(Top).apps[0].Name != "busy" || m.(Top).sortBy != render.SortCPU {
		t.Errorf("c sorts by CPU: %v", m.(Top).apps)
	}
	m, _ = m.Update(key("down"))
	m, _ = m.Update(key("down"))
	m, _ = m.Update(key("down"))
	if o := m.(Top).offset; o != 1 {
		t.Errorf("scrolling stops at the last screenful: offset %d", o)
	}
	m, _ = m.Update(key("up"))
	m, _ = m.Update(key("up"))
	if o := m.(Top).offset; o != 0 {
		t.Errorf("and at the top: offset %d", o)
	}
	for _, k := range []string{"q", "ctrl+c"} {
		if _, cmd := m.Update(key(k)); cmd == nil || cmd() != tea.Quit() {
			t.Errorf("%s should quit", k)
		}
	}
}

// cmd0 runs a command and returns its message.
func cmd0(c tea.Cmd) tea.Msg { return c() }

func TestTopTickTakesASample(t *testing.T) {
	sample, calls := fakeSampler(snap(time.Now(), 100e9))
	var m tea.Model = NewTop(context.Background(), sample, render.NewScreen(&bytes.Buffer{}, false), time.Second, render.SortMemory)
	_, cmd := m.Update(tickMsg{})
	if _, ok := cmd().(snapMsg); !ok || *calls != 1 {
		t.Errorf("a tick samples once: %d calls", *calls)
	}
}

func TestWatchRecordsTransitions(t *testing.T) {
	t0 := time.Date(2026, 10, 14, 14, 0, 0, 0, time.UTC)
	sample, _ := fakeSampler(
		snap(t0, 100e9),
		snap(t0.Add(5*time.Second), 5e9), // disk fills up
		snap(t0.Add(10*time.Second), 5e9),
	)
	var m tea.Model = NewWatch(context.Background(), sample, render.NewScreen(&bytes.Buffer{}, false), 5*time.Second, t0, 0)
	for range 3 {
		m, _ = m.Update(snapMsg(sample(context.Background())))
	}
	w := m.(Watch)
	var changes []rules.Transition
	for _, tr := range w.transitions {
		if !tr.Initial {
			changes = append(changes, tr)
		}
	}
	if len(changes) != 1 || changes[0].Check != rules.CheckDisk || changes[0].To != rules.Crit {
		t.Errorf("got %+v", changes)
	}
	if !strings.Contains(m.View(), "14:00:05  ✖ disk     ok → crit") {
		t.Errorf("view:\n%s", m.View())
	}
}

func TestWatchLog(t *testing.T) {
	t0 := time.Date(2026, 10, 14, 14, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	snaps := []collect.Snapshot{snap(t0, 100e9), snap(t0.Add(time.Second), 100e9), snap(t0.Add(2*time.Second), 5e9)}
	calls := 0
	sample := func(context.Context) collect.Snapshot {
		s := snaps[min(calls, len(snaps)-1)]
		calls++
		if calls == len(snaps) {
			cancel() // stop after the last one is written
		}
		return s
	}
	var buf bytes.Buffer
	if err := WatchLog(ctx, &buf, sample, time.Millisecond, 0, false); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	// Four checks at the start (network unknown), then nothing until the disk changes.
	if len(lines) != 4 || !strings.Contains(lines[0], "memory") {
		t.Errorf("got:\n%s", buf.String())
	}
}

func TestWatchCount(t *testing.T) {
	calls := 0
	sample := func(context.Context) collect.Snapshot { calls++; return snap(time.Now(), 100e9) }
	var buf bytes.Buffer
	if err := WatchLog(context.Background(), &buf, sample, time.Millisecond, 3, true); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Errorf("--count 3 checked %d times", calls)
	}
	var m tea.Model = NewWatch(context.Background(), sample, render.NewScreen(&bytes.Buffer{}, false), time.Second, time.Now(), 1)
	if _, cmd := m.Update(snapMsg(sample(context.Background()))); cmd == nil || cmd() != tea.Quit() {
		t.Error("the full-screen view stops after --count samples too")
	}
}

func TestZeroWindowSizeIsIgnored(t *testing.T) {
	sample, _ := fakeSampler(snap(time.Now(), 100e9))
	var m tea.Model = NewTop(context.Background(), sample, render.NewScreen(&bytes.Buffer{}, false), time.Second, render.SortMemory)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 0, Height: 0})
	if top := m.(Top); top.width != 80 || top.height != 24 {
		t.Errorf("got %dx%d", top.width, top.height)
	}
	if strings.Contains(m.View(), "unknown host") {
		t.Errorf("before the first sample:\n%s", m.View())
	}
}
