package render

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/rules"
)

func topState() TopState {
	return TopState{
		Report: Report{
			Host: collect.Host{Hostname: "studio.local"},
			Findings: []rules.Finding{
				{Check: "disk", Severity: rules.Warn, Percent: p(93)},
				{Check: "memory", Severity: rules.OK, Percent: p(42)},
				{Check: "network", Severity: rules.Unknown},
			},
		},
		Apps: []rules.AppGroup{
			{Name: "Firefox", Count: 19, RSS: 1536 << 20, CPUPercent: 3},
			{Name: "A very long application name that will not fit at all", Count: 1, RSS: 100 << 20},
			{Name: "Docker", Count: 10, RSS: 392 << 20, CPUPercent: 152},
		},
		SortBy: SortMemory, Interval: 2 * time.Second, Width: 60, Height: 10,
	}
}

func TestTopFrame(t *testing.T) {
	got := NewScreen(&bytes.Buffer{}, false).Top(topState())
	want := " lagdoc top · studio · every 2s\n" +
		" ▲ disk 93%   ✓ memory 42%\n" +
		"\n" +
		" APP                                PROCS   MEMORY↓     CPU\n" +
		" Firefox                               19    1.5 GB      3%\n" +
		" A very long application name tha…      1    100 MB      0%\n" +
		" Docker                                10    392 MB    152%\n" +
		"\n" +
		"\n" +
		" q quit · m memory · c cpu · ↑↓ scroll  CPU 100% = one core"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if n := strings.Count(got, "\n") + 1; n != 10 {
		t.Errorf("a frame must be exactly the screen's height: %d lines", n)
	}
}

func TestTopFrameScrollsAndFits(t *testing.T) {
	st := topState()
	st.Height = 7 // one row
	st.Offset = 2
	got := NewScreen(&bytes.Buffer{}, false).Top(st)
	if !strings.Contains(got, "Docker") || strings.Contains(got, "Firefox") {
		t.Errorf("got:\n%s", got)
	}
	if n := strings.Count(got, "\n") + 1; n != 7 {
		t.Errorf("%d lines", n)
	}
	st.Height = 0 // a one-off print: no footer, no "every"
	got = NewScreen(&bytes.Buffer{}, false).Top(st)
	if strings.Contains(got, "q quit") || strings.Contains(got, "every") {
		t.Errorf("got:\n%s", got)
	}
}

func TestWatchFrame(t *testing.T) {
	at := time.Date(2026, 10, 14, 14, 5, 10, 0, time.UTC)
	st := WatchState{
		Report: Report{Host: collect.Host{Hostname: "studio"}, Findings: []rules.Finding{
			{Check: "disk", Severity: rules.Warn, Summary: "getting full"},
			{Check: "memory", Severity: rules.OK, Summary: "plenty free"},
		}},
		Transitions: []rules.Transition{
			{At: at, Check: "disk", From: rules.OK, To: rules.Warn, Summary: "getting full"},
			{At: at.Add(-time.Hour), Check: "memory", From: rules.Crit, To: rules.OK, Summary: "plenty free"},
			{At: at.Add(-2 * time.Hour), Check: "disk", To: rules.OK, Initial: true},
		},
		Started: at.Add(-3 * time.Hour), Interval: 5 * time.Second, Height: 14,
	}
	got := NewScreen(&bytes.Buffer{}, false).Watch(st)
	want := " lagdoc watch · studio · every 5s · since 11:05\n" +
		"\n" +
		" WARN  1 thing to watch\n" +
		"\n" +
		" ▲ disk      getting full\n" +
		" ✓ memory    plenty free\n" +
		"\n" +
		" changes\n" +
		" 14:05:10  ▲ disk     ok → warn · getting full\n" +
		" 13:05:10  ✓ memory   crit → ok · plenty free\n" +
		"\n" +
		"\n" +
		"\n" +
		" q quit"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	st.Height = 11 // room for one change
	got = NewScreen(&bytes.Buffer{}, false).Watch(st)
	if !strings.Contains(got, "14:05:10") || strings.Contains(got, "13:05:10") {
		t.Errorf("newest first, as many as fit:\n%s", got)
	}
	if n := strings.Count(got, "\n") + 1; n != 11 {
		t.Errorf("%d lines", n)
	}
}

func TestWatchLogAndJSON(t *testing.T) {
	at := time.Date(2026, 10, 14, 14, 5, 10, 0, time.UTC)
	trs := []rules.Transition{
		{At: at, Check: "memory", To: rules.OK, Initial: true, Summary: "plenty free"},
		{At: at, Check: "disk", From: rules.OK, To: rules.Warn, Summary: "getting full", Details: []string{"/ 9 GB free"}},
	}
	var buf bytes.Buffer
	if err := WatchLog(&buf, trs); err != nil {
		t.Fatal(err)
	}
	want := "2026-10-14 14:05:10  ✓ memory   ok · plenty free\n" +
		"2026-10-14 14:05:10  ▲ disk     ok → warn · getting full\n"
	if buf.String() != want {
		t.Errorf("got:\n%s", buf.String())
	}
	buf.Reset()
	if err := WatchJSON(&buf, trs); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 ||
		lines[0] != `{"schema_version":1,"time":"2026-10-14T14:05:10Z","check":"memory","from":null,"to":"ok","summary":"plenty free","details":[]}` ||
		!strings.Contains(lines[1], `"from":"ok","to":"warn"`) {
		t.Errorf("got:\n%s", buf.String())
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("Firefox", 10); got != "Firefox" {
		t.Error(got)
	}
	if got := truncate("Google Chrome Helper", 10); got != "Google Ch…" {
		t.Error(got)
	}
	if got := pad("日本", 6); got != "日本  " {
		t.Errorf("%q", got)
	}
}
