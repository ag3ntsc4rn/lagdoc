package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/fix"
	"github.com/ag3ntsc4rn/lagdoc/internal/rules"
)

func sampleFixes() FixReport {
	return FixReport{
		TakenAt: time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC),
		Host:    collect.Host{Hostname: "studio.local", OS: "darwin", OSName: "macOS 15.7.9"},
		Findings: []rules.Finding{
			{Check: "memory", Severity: rules.Crit, Summary: "under heavy pressure"},
			{Check: "disk", Severity: rules.Warn, Summary: "getting full"},
			{Check: "cpu", Severity: rules.OK, Summary: "mostly idle"},
		},
		Fixes: []fix.Fix{
			{ID: "quit:Google Chrome", Check: "memory", Action: "Quit Google Chrome",
				Title: "Quit Google Chrome (23 processes), using 4.1 GB", Note: "Asks it to quit.",
				Quit: &fix.Quit{App: "Google Chrome", BundleID: "com.google.Chrome", PIDs: []int32{1}}},
			{ID: "keep:iTerm", Check: "memory", Manual: true, Title: "iTerm is running this terminal, so lagdoc won't quit it"},
			{ID: "cache:go", Check: "disk", Action: "Clear Go's build cache", Title: "Clear Go's build cache, about 6.3 GB",
				Command: []string{"go", "clean", "-cache"}, Frees: 6.3e9, Mount: "/"},
			{ID: "trash", Check: "disk", Manual: true, Title: "Empty the Trash, which holds 3.4 GB", Frees: 3.4e9, Mount: "/"},
			{ID: "downloads", Check: "disk", Manual: true, Title: "Look through Downloads, which holds 12 GB",
				Steps: []string{"Xcode.xip 7.9 GB"}, Mount: "/"},
		},
	}
}

func TestFixPreviewPlain(t *testing.T) {
	var buf bytes.Buffer
	if err := FixPreview(&buf, sampleFixes(), false); err != nil {
		t.Fatal(err)
	}
	want := `
  lagdoc fix · studio

  2 fixes lagdoc can run · 3 for you to do

  ✖ memory · under heavy pressure
    1 Quit Google Chrome (23 processes), using 4.1 GB
      Asks it to quit.
    · iTerm is running this terminal, so lagdoc won't quit it

  ▲ disk · getting full
    2 Clear Go's build cache, about 6.3 GB
      runs go clean -cache
    · Empty the Trash, which holds 3.4 GB
    · Look through Downloads, which holds 12 GB
      └ Xcode.xip 7.9 GB

`
	if got := buf.String(); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFixPreviewNothingToDo(t *testing.T) {
	r := sampleFixes()
	r.Fixes = nil
	var buf bytes.Buffer
	FixPreview(&buf, r, false)
	if !strings.Contains(buf.String(), "No safe fix for these problems") {
		t.Errorf("got:\n%s", buf.String())
	}
	r.Findings = []rules.Finding{{Check: "cpu", Severity: rules.OK}}
	buf.Reset()
	FixPreview(&buf, r, false)
	if !strings.Contains(buf.String(), "OK  nothing to fix") {
		t.Errorf("got:\n%s", buf.String())
	}
}

func TestFixPromptAndResult(t *testing.T) {
	f := sampleFixes().Fixes[0]
	if got := FixPrompt(1, f); got != "  Run 1, quit Google Chrome? [y/N/q] " {
		t.Errorf("prompt: %q", got)
	}
	var buf bytes.Buffer
	FixResult(&buf, f, fix.Result{Status: fix.Pending, Message: "still open"}, false)
	if got := buf.String(); got != "  ▲ Quit Google Chrome: still open\n" {
		t.Errorf("result: %q", got)
	}
}

func TestFixJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := FixJSON(&buf, sampleFixes()); err != nil {
		t.Fatal(err)
	}
	var got struct {
		SchemaVersion int    `json:"schema_version"`
		Verdict       string `json:"verdict"`
		Fixes         []struct {
			ID      string   `json:"id"`
			Manual  bool     `json:"manual"`
			Command string   `json:"command"`
			Steps   []string `json:"steps"`
			Frees   uint64   `json:"frees_bytes"`
		} `json:"fixes"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != FixSchemaVersion || got.Verdict != "slow" || len(got.Fixes) != 5 {
		t.Fatalf("got %+v", got)
	}
	if f := got.Fixes[2]; f.ID != "cache:go" || f.Command != "go clean -cache" || f.Frees != 6.3e9 || f.Steps == nil {
		t.Errorf("cache fix: %+v", f)
	}
	if !got.Fixes[1].Manual {
		t.Error("manual flag lost")
	}
	if strings.Contains(buf.String(), "null") {
		t.Errorf("lists must be arrays:\n%s", buf.String())
	}
}
