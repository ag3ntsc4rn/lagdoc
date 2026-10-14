package history

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
)

var t0 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func rec(at time.Time) Record {
	return Record{Version: RecordVersion, TakenAt: at, Hostname: "studio"}
}

func TestNewRecordGroupsApps(t *testing.T) {
	s := collect.Snapshot{
		TakenAt: t0,
		Processes: []collect.Process{
			{Name: "Slack Helper (GPU)", RSS: 100, CPUPercent: 1.26},
			{Name: "Slack", RSS: 200, CPUPercent: 2},
			{Name: "bash", RSS: 5},
		},
	}
	r := NewRecord(s, collect.Inventory{})
	want := []AppUsage{{Name: "Slack", Processes: 2, RSS: 300, CPUPercent: 3.3}, {Name: "bash", Processes: 1, RSS: 5}}
	if r.Version != RecordVersion || !slices.Equal(r.Running, want) {
		t.Errorf("got %+v", r.Running)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := Store{Dir: filepath.Join(t.TempDir(), "history")} // doesn't exist yet
	r := rec(t0)
	r.Inventory.Apps = []collect.App{{Name: "Editor", Version: "2.0", Source: "/Applications"}}
	if err := s.Save(r); err != nil {
		t.Fatal(err)
	}
	ts, err := s.Times()
	if err != nil || len(ts) != 1 || !ts[0].Equal(t0) {
		t.Fatalf("times: %v %v", ts, err)
	}
	got, err := s.Load(ts[0])
	if err != nil {
		t.Fatal(err)
	}
	if !got.TakenAt.Equal(t0) || got.Inventory.Apps[0] != r.Inventory.Apps[0] {
		t.Errorf("got %+v", got)
	}
	if fi, err := os.Stat(s.Dir); err != nil || runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("history dir should be private: %v %v", fi.Mode(), err)
	}
	// No temp files are left behind.
	es, _ := os.ReadDir(s.Dir)
	if len(es) != 1 {
		t.Errorf("want one file, got %d", len(es))
	}
}

func TestTimesIgnoresOtherFiles(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for _, n := range []string{"notes.txt", "garbage.json.gz", ".record-123", fileName(t0)} {
		if err := os.WriteFile(filepath.Join(s.Dir, n), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ts, err := s.Times()
	if err != nil || len(ts) != 1 {
		t.Errorf("got %v %v", ts, err)
	}
	if ts, err := (Store{Dir: filepath.Join(s.Dir, "nope")}).Times(); err != nil || ts != nil {
		t.Errorf("a missing store is empty: %v %v", ts, err)
	}
}

func writeRaw(t *testing.T, s Store, at time.Time, v any) {
	t.Helper()
	f, err := os.Create(filepath.Join(s.Dir, fileName(at)))
	if err != nil {
		t.Fatal(err)
	}
	zw := gzip.NewWriter(f)
	if err := json.NewEncoder(zw).Encode(v); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	f.Close()
}

func TestBaseline(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, ok, err := s.Baseline(t0, 24*time.Hour); ok || err != nil {
		t.Fatalf("empty store: ok=%v err=%v", ok, err)
	}
	for _, h := range []int{-72, -30, -2} {
		writeRaw(t, s, t0.Add(time.Duration(h)*time.Hour), rec(t0.Add(time.Duration(h)*time.Hour)))
	}
	pick := func(since time.Duration) time.Time {
		r, ok, err := s.Baseline(t0, since)
		if !ok || err != nil {
			t.Fatalf("since %v: ok=%v err=%v", since, ok, err)
		}
		return r.TakenAt
	}
	if got := pick(24 * time.Hour); !got.Equal(t0.Add(-30 * time.Hour)) {
		t.Errorf("24h: newest at least a day old, got %v", got)
	}
	if got := pick(0); !got.Equal(t0.Add(-2 * time.Hour)) {
		t.Errorf("0: the newest, got %v", got)
	}
	if got := pick(30 * 24 * time.Hour); !got.Equal(t0.Add(-72 * time.Hour)) {
		t.Errorf("older than all: the oldest, got %v", got)
	}

	// A corrupt record and one from a newer lagdoc are passed over.
	if err := os.WriteFile(filepath.Join(s.Dir, fileName(t0.Add(-26*time.Hour))), []byte("not gzip"), 0o600); err != nil {
		t.Fatal(err)
	}
	future := rec(t0.Add(-25 * time.Hour))
	future.Version = RecordVersion + 1
	writeRaw(t, s, future.TakenAt, future)
	if got := pick(24 * time.Hour); !got.Equal(t0.Add(-30 * time.Hour)) {
		t.Errorf("unreadable records must be skipped, got %v", got)
	}
}

func TestBaselineOrderPutsFutureLast(t *testing.T) {
	ts := []time.Time{t0.Add(-48 * time.Hour), t0.Add(-time.Hour), t0.Add(time.Hour)}
	got := baselineOrder(ts, t0, 24*time.Hour)
	want := []time.Time{ts[0], ts[1], ts[2]}
	if !slices.Equal(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestThin(t *testing.T) {
	now := t0
	at := func(d time.Duration) time.Time { return now.Add(-d) }
	ts := []time.Time{
		at(10 * time.Minute), at(20 * time.Minute), // last hour: all kept
		at(2*time.Hour + 10*time.Minute), at(2*time.Hour + 40*time.Minute), // same hour: newer kept
		at(5 * 24 * time.Hour), at(5*24*time.Hour + time.Hour), // same day: newer kept
		at(6 * 24 * time.Hour),
		at(91 * 24 * time.Hour), // too old
	}
	got := Thin(ts, now)
	want := []time.Time{at(2*time.Hour + 40*time.Minute), at(5*24*time.Hour + time.Hour), at(91 * 24 * time.Hour)}
	slices.SortFunc(got, time.Time.Compare)
	slices.SortFunc(want, time.Time.Compare)
	if !slices.Equal(got, want) {
		t.Errorf("drop %v, want %v", got, want)
	}
}

func TestThinKeepsHourlyAndDailyApart(t *testing.T) {
	// Just under and just over 48h ago, both in the midnight hour of one
	// UTC day: the hourly bucket and the daily one start at the same time.
	// Both records must survive.
	now := time.Date(2026, 10, 14, 0, 45, 0, 0, time.UTC)
	ts := []time.Time{now.Add(-47*time.Hour - 55*time.Minute), now.Add(-48*time.Hour - 25*time.Minute)}
	if got := Thin(ts, now); len(got) != 0 {
		t.Errorf("dropped %v", got)
	}
}

func TestSavePrunes(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	old := t0.Add(-100 * 24 * time.Hour)
	writeRaw(t, s, old, rec(old))
	if err := s.Save(rec(t0)); err != nil {
		t.Fatal(err)
	}
	ts, _ := s.Times()
	if len(ts) != 1 || !ts[0].Equal(t0) {
		t.Errorf("old record should be pruned: %v", ts)
	}
}
