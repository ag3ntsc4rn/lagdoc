package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ag3ntsc4rn/lagdoc/internal/changes"
	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/history"
)

func sampleChanges() ChangesReport {
	now := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
	base := time.Date(2026, 10, 12, 14, 3, 0, 0, time.UTC)
	return ChangesReport{
		TakenAt:  now,
		Host:     collect.Host{Hostname: "studio.local", OS: "darwin", OSName: "macOS 15.7.9"},
		Since:    24 * time.Hour,
		Baseline: &base,
		Saved:    true,
		Changes: changes.Changes{
			StartupAdded:   []collect.StartupItem{{Name: "com.docker.vmnetd", Source: "/Library/LaunchDaemons"}},
			StartupRemoved: []collect.StartupItem{{Name: "us.zoom.updater", Source: "/Library/LaunchAgents"}},
			AppsAdded:      []collect.App{{Name: "Docker", Version: "4.90.0", Source: "/Applications"}, {Name: "Arc"}},
			AppsRemoved:    []collect.App{{Name: "zoom.us", Version: "6.1"}},
			AppsUpdated:    []changes.AppUpdate{{Name: "Firefox", From: "156.0", To: "157.0.1"}},
			Disks: []changes.DiskChange{
				{Mount: "/", Before: collect.Disk{Total: 250e9, Free: 35.6e9}, After: collect.Disk{Total: 250e9, Free: 17.4e9}},
				{Mount: "/data", Before: collect.Disk{Total: 2e12, Free: 1e12}, After: collect.Disk{Total: 2e12, Free: 1.012e12}},
			},
			Heavy: []changes.HeavyApp{
				{Now: history.AppUsage{Name: "Docker", Processes: 4, RSS: 3 << 30}, Mem: true},
				{Now: history.AppUsage{Name: "Firefox", Processes: 19, RSS: 3482 << 20, CPUPercent: 185.6},
					Before: &history.AppUsage{Name: "Firefox", Processes: 18, RSS: 1126 << 20, CPUPercent: 2.8},
					Mem:    true, CPU: true},
			},
			Unread:  []string{"/Library/LaunchDaemons"},
			Skipped: []string{"heavy apps"},
		},
	}
}

func TestChangesTextPlain(t *testing.T) {
	var buf bytes.Buffer
	if err := ChangesText(&buf, sampleChanges(), false); err != nil {
		t.Fatal(err)
	}
	want := `
  lagdoc changes · studio · since Mon 12 Oct 14:03 (2 days ago)

  10 changes

  startup items
    + com.docker.vmnetd (/Library/LaunchDaemons)
    − us.zoom.updater (/Library/LaunchAgents)

  apps
    + Docker 4.90.0
    + Arc
    ↑ Firefox 156.0 → 157.0.1
    − zoom.us

  disk
    ↑ / 18.2 GB more used, 17.4 GB free of 250 GB
    ↓ /data 12.0 GB freed, 1.0 TB free of 2.0 TB

  heavy apps
    + Docker (4 processes) 3.0 GB, not running then
    ↑ Firefox (19 processes) 3.4 GB and 186% CPU, was 1.1 GB and 3% CPU

  ? couldn't read /Library/LaunchDaemons
  ? heavy apps: no data to compare

`
	if got := buf.String(); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(buf.String(), "\x1b[") {
		t.Error("plain output must not contain escape codes")
	}
}

func TestChangesTextFallbackAndNone(t *testing.T) {
	r := sampleChanges()
	base := r.TakenAt.Add(-3 * time.Hour)
	r.Baseline = &base
	r.Changes = changes.Changes{}
	var buf bytes.Buffer
	if err := ChangesText(&buf, r, false); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"since today 17:00 (3h ago, the oldest record)", "No changes found"} {
		if !strings.Contains(buf.String(), s) {
			t.Errorf("missing %q in:\n%s", s, buf.String())
		}
	}
}

func TestChangesTextFirstRun(t *testing.T) {
	r := sampleChanges()
	r.Baseline = nil
	r.Changes = changes.Changes{}
	var buf bytes.Buffer
	if err := ChangesText(&buf, r, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Nothing to compare with yet. This run has been saved") {
		t.Errorf("got:\n%s", buf.String())
	}
	r.Saved = false
	buf.Reset()
	ChangesText(&buf, r, false)
	if !strings.Contains(buf.String(), "--no-save") {
		t.Errorf("unsaved first run should say why: \n%s", buf.String())
	}
}

func TestWhenAndAgo(t *testing.T) {
	now := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
	cases := map[time.Time]string{
		now.Add(-2 * time.Hour):  "today 18:00",
		now.Add(-22 * time.Hour): "yesterday 22:00",
		now.AddDate(0, 0, -3):    "Sun 11 Oct 20:00",
		now.AddDate(-1, 0, 0):    "Tue 14 Oct 2025 20:00",
	}
	for in, want := range cases {
		if got := when(in, now); got != want {
			t.Errorf("when(%v) = %q, want %q", in, got, want)
		}
	}
	for d, want := range map[time.Duration]string{
		10 * time.Second: "just now", 5 * time.Minute: "5m ago",
		30 * time.Hour: "30h ago", 50 * time.Hour: "2 days ago",
	} {
		if got := ago(d); got != want {
			t.Errorf("ago(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestChangesJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := ChangesJSON(&buf, sampleChanges()); err != nil {
		t.Fatal(err)
	}
	var got struct {
		SchemaVersion int     `json:"schema_version"`
		Since         *string `json:"since"`
		Changes       int     `json:"changes"`
		StartupItems  struct {
			Added []map[string]string `json:"added"`
		} `json:"startup_items"`
		Disks []struct {
			Mount string `json:"mount"`
			Used  int64  `json:"used_change_bytes"`
		} `json:"disks"`
		HeavyApps []struct {
			Name    string         `json:"name"`
			Before  map[string]any `json:"before"`
			Reasons []string       `json:"reasons"`
		} `json:"heavy_apps"`
		Saved bool `json:"saved"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != ChangesSchemaVersion || got.Since == nil || *got.Since != "2026-10-12T14:03:00Z" || got.Changes != 10 || !got.Saved {
		t.Errorf("header: %+v", got)
	}
	if got.StartupItems.Added[0]["source"] != "/Library/LaunchDaemons" {
		t.Errorf("startup: %+v", got.StartupItems)
	}
	if got.Disks[0].Used != 18.2e9 || got.Disks[1].Used != -12e9 {
		t.Errorf("disks: %+v", got.Disks)
	}
	if got.HeavyApps[0].Before != nil || len(got.HeavyApps[1].Reasons) != 2 {
		t.Errorf("heavy: %+v", got.HeavyApps)
	}
}

func TestChangesJSONEmptyListsAreArrays(t *testing.T) {
	r := sampleChanges()
	r.Baseline = nil
	r.Changes = changes.Changes{}
	var buf bytes.Buffer
	if err := ChangesJSON(&buf, r); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.Contains(s, `"since": null`) {
		t.Errorf("no baseline means since is null:\n%s", s)
	}
	if strings.Count(s, "null") != 1 {
		t.Errorf("lists must be [] not null:\n%s", s)
	}
}
