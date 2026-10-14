package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/rules"
)

func p(v float64) *float64 { return &v }

func sampleReport() Report {
	return Report{
		TakenAt: time.Date(2026, 10, 14, 20, 0, 0, 500, time.UTC),
		Host: collect.Host{
			Hostname: "studio.local", OS: "darwin", OSName: "macOS 15.7.9",
			Uptime: 3*24*time.Hour + 4*time.Hour + 5*time.Minute, CPUs: 8,
		},
		Findings: []rules.Finding{
			{Check: "memory", Severity: rules.Crit, Percent: p(92.04), Summary: "under heavy pressure",
				Details: []string{"Google Chrome (23 processes) 4.1 GB", "swap in use 6.2 GB"}},
			{Check: "disk", Severity: rules.Warn, Percent: p(94), Summary: "getting full",
				Details: []string{"/ 14.0 GB free of 250 GB"}},
			{Check: "cpu", Severity: rules.OK, Percent: p(23), Summary: "mostly idle",
				Details: []string{"should not be printed"}},
			{Check: "network", Severity: rules.OK, Summary: "dns 12 ms"},
		},
	}
}

func TestTextPlain(t *testing.T) {
	var buf bytes.Buffer
	if err := Text(&buf, sampleReport(), false); err != nil {
		t.Fatal(err)
	}
	want := `
  lagdoc · studio · macOS 15.7.9 · up 3d 4h

  SLOW  2 causes found

  ✖ memory    █████████░  92%  under heavy pressure
    └ Google Chrome (23 processes) 4.1 GB
    └ swap in use 6.2 GB
  ▲ disk      █████████░  94%  getting full
    └ / 14.0 GB free of 250 GB
  ✓ cpu       ██░░░░░░░░  23%  mostly idle
  ✓ network   dns 12 ms

  → run lagdoc fix to see what can be done

`
	if got := buf.String(); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTextNoEscapeCodesWithoutColor(t *testing.T) {
	var buf bytes.Buffer
	_ = Text(&buf, sampleReport(), false)
	if strings.Contains(buf.String(), "\x1b") {
		t.Error("colour off must not write escape codes")
	}
}

func TestTextVerdicts(t *testing.T) {
	cases := []struct {
		sevs []rules.Severity
		want string
		fix  bool
	}{
		{[]rules.Severity{rules.OK, rules.OK}, "OK  no problems found", false},
		{[]rules.Severity{rules.Warn, rules.OK}, "WARN  1 thing to watch", true},
		{[]rules.Severity{rules.Unknown}, "UNKNOWN  nothing could be measured", false},
	}
	for _, c := range cases {
		r := sampleReport()
		r.Findings = nil
		for _, s := range c.sevs {
			r.Findings = append(r.Findings, rules.Finding{Check: "cpu", Severity: s})
		}
		var buf bytes.Buffer
		_ = Text(&buf, r, false)
		out := buf.String()
		if !strings.Contains(out, c.want) {
			t.Errorf("want %q in:\n%s", c.want, out)
		}
		if strings.Contains(out, "lagdoc fix") != c.fix {
			t.Errorf("fix hint shown = %v, want %v", !c.fix, c.fix)
		}
	}
}

func TestBarAndUptime(t *testing.T) {
	if got := bar(0); got != "░░░░░░░░░░" {
		t.Errorf("bar(0) = %q", got)
	}
	if got := bar(150); got != "██████████" {
		t.Errorf("bar(150) = %q", got)
	}
	if got := bar(-5); got != "░░░░░░░░░░" {
		t.Errorf("bar(-5) = %q", got)
	}
	for d, want := range map[time.Duration]string{
		5 * time.Minute:               "5m",
		2*time.Hour + 7*time.Minute:   "2h 7m",
		50*time.Hour + 30*time.Minute: "2d 2h",
	} {
		if got := uptime(d); got != want {
			t.Errorf("uptime(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, sampleReport()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "\x1b") {
		t.Error("JSON must have no styling")
	}
	var got struct {
		SchemaVersion int    `json:"schema_version"`
		TakenAt       string `json:"taken_at"`
		Verdict       string `json:"verdict"`
		Causes        int    `json:"causes"`
		Host          struct {
			Hostname      string `json:"hostname"`
			UptimeSeconds int64  `json:"uptime_seconds"`
		} `json:"host"`
		Findings []map[string]any `json:"findings"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 1 || got.Verdict != "slow" || got.Causes != 2 {
		t.Errorf("header = %+v", got)
	}
	if got.TakenAt != "2026-10-14T20:00:00Z" || got.Host.UptimeSeconds != 273900 {
		t.Errorf("taken_at %q uptime %d", got.TakenAt, got.Host.UptimeSeconds)
	}
	mem, net := got.Findings[0], got.Findings[3]
	if mem["check"] != "memory" || mem["severity"] != "crit" || mem["percent"] != 92.0 {
		t.Errorf("memory finding = %v", mem)
	}
	if _, ok := net["percent"]; ok {
		t.Error("network has no percent, so the key is omitted")
	}
	if d, ok := net["details"].([]any); !ok || len(d) != 0 {
		t.Errorf("details must be an empty array, got %#v", net["details"])
	}
	// Passing checks keep their details in JSON; only the text view hides them.
	if d := got.Findings[2]["details"].([]any); len(d) != 1 {
		t.Errorf("cpu details = %v", d)
	}
}
