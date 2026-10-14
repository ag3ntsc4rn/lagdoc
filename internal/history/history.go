// Package history keeps compact records of past runs on this machine, so
// `lagdoc changes` can say what's different now. Records never leave the
// machine.
package history

import (
	"cmp"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/rules"
)

// RecordVersion changes when a field is removed or changes meaning. Records
// from a newer lagdoc are ignored rather than misread.
const RecordVersion = 1

// Record is the part of a run worth comparing later: about 5 KB gzipped.
type Record struct {
	Version   int               `json:"v"`
	TakenAt   time.Time         `json:"taken_at"`
	Hostname  string            `json:"hostname"`
	Disks     []collect.Disk    `json:"disks"`
	Running   []AppUsage        `json:"running"`
	Inventory collect.Inventory `json:"inventory"`
	Errors    map[string]string `json:"errors,omitempty"` // collectors that failed in this run
}

// AppUsage is one running app's processes added together.
type AppUsage struct {
	Name       string  `json:"name"`
	Processes  int     `json:"processes"`
	RSS        uint64  `json:"rss"`
	CPUPercent float64 `json:"cpu_percent"`
}

func NewRecord(s collect.Snapshot, inv collect.Inventory) Record {
	r := Record{
		Version:   RecordVersion,
		TakenAt:   s.TakenAt,
		Hostname:  s.Host.Hostname,
		Disks:     s.Disks,
		Inventory: inv,
		Errors:    s.Errors,
	}
	for _, g := range rules.GroupByApp(s.Processes) {
		r.Running = append(r.Running, AppUsage{
			Name: g.Name, Processes: g.Count, RSS: g.RSS,
			CPUPercent: math.Round(g.CPUPercent*10) / 10,
		})
	}
	slices.SortFunc(r.Running, func(a, b AppUsage) int { return cmp.Compare(a.Name, b.Name) })
	return r
}

// Dir is where records are kept: ~/Library/Application Support/lagdoc/history
// on macOS, $XDG_STATE_HOME/lagdoc/history (~/.local/state) on Linux and
// other Unixes, %LocalAppData%\lagdoc\history on Windows.
func Dir() (string, error) {
	var base string
	var err error
	switch runtime.GOOS {
	case "darwin":
		base, err = os.UserConfigDir() // ~/Library/Application Support
	case "windows":
		base, err = os.UserCacheDir() // %LocalAppData%
	default:
		base = os.Getenv("XDG_STATE_HOME")
		if !filepath.IsAbs(base) { // the spec says relative paths are invalid
			var home string
			home, err = os.UserHomeDir()
			base = filepath.Join(home, ".local", "state")
		}
	}
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "lagdoc", "history"), nil
}

// Store is a directory of records, one gzipped JSON file per run, named by
// the time it was taken (UTC).
type Store struct {
	Dir string
}

const (
	fileLayout = "20060102T150405Z"
	fileSuffix = ".json.gz"
)

func fileName(t time.Time) string { return t.UTC().Format(fileLayout) + fileSuffix }

// Save writes r and then thins out old records (see Prune).
func (s Store) Save(r Record) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Dir, ".record-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op once renamed
	zw := gzip.NewWriter(f)
	err = json.NewEncoder(zw).Encode(r)
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(s.Dir, fileName(r.TakenAt))); err != nil {
		return err
	}
	return s.Prune(r.TakenAt)
}

// Times lists when the stored records were taken, oldest first. Files that
// aren't records are ignored.
func (s Store) Times() ([]time.Time, error) {
	es, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var ts []time.Time
	for _, e := range es {
		name, ok := strings.CutSuffix(e.Name(), fileSuffix)
		if !ok {
			continue
		}
		if t, err := time.Parse(fileLayout, name); err == nil {
			ts = append(ts, t)
		}
	}
	slices.SortFunc(ts, time.Time.Compare)
	return ts, nil
}

// maxRecordSize guards against a corrupt or planted file inflating to
// something huge.
const maxRecordSize = 16 << 20

func (s Store) Load(t time.Time) (Record, error) {
	f, err := os.Open(filepath.Join(s.Dir, fileName(t)))
	if err != nil {
		return Record{}, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return Record{}, err
	}
	var r Record
	if err := json.NewDecoder(io.LimitReader(zr, maxRecordSize)).Decode(&r); err != nil {
		return Record{}, err
	}
	if r.Version != RecordVersion {
		return Record{}, fmt.Errorf("record version %d, this lagdoc reads %d", r.Version, RecordVersion)
	}
	return r, nil
}

// Baseline picks the record to compare with at now: the newest one at least
// `since` old or, if every record is younger, the oldest. Records that
// can't be read are passed over. ok is false when there's nothing to use.
func (s Store) Baseline(now time.Time, since time.Duration) (r Record, ok bool, err error) {
	ts, err := s.Times()
	if err != nil {
		return Record{}, false, err
	}
	for _, t := range baselineOrder(ts, now, since) {
		if r, err := s.Load(t); err == nil {
			return r, true, nil
		}
	}
	return Record{}, false, nil
}

// baselineOrder is the order in which records are tried as a baseline.
// Records stamped in the future (a clock change) come last.
func baselineOrder(ts []time.Time, now time.Time, since time.Duration) []time.Time {
	cutoff := now.Add(-since)
	var old, young, future []time.Time
	for _, t := range ts {
		switch {
		case !t.After(cutoff):
			old = append(old, t)
		case !t.After(now):
			young = append(young, t)
		default:
			future = append(future, t)
		}
	}
	slices.Reverse(old) // newest qualifying first
	return slices.Concat(old, young, future)
}

// Retention: every record for the last hour, then the newest record per
// hour for two days, then the newest per day, for 90 days. That's at most
// about 140 files.
const (
	keepAll    = time.Hour
	keepHourly = 48 * time.Hour
	keepFor    = 90 * 24 * time.Hour
)

// Prune deletes the records Thin says to drop.
func (s Store) Prune(now time.Time) error {
	ts, err := s.Times()
	if err != nil {
		return err
	}
	var errs []error
	for _, t := range Thin(ts, now) {
		if err := os.Remove(filepath.Join(s.Dir, fileName(t))); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Thin returns the times to drop under the retention policy.
func Thin(ts []time.Time, now time.Time) []time.Time {
	ts = slices.Clone(ts)
	slices.SortFunc(ts, func(a, b time.Time) int { return b.Compare(a) }) // newest first
	type bucket struct {
		start time.Time
		size  time.Duration
	}
	kept := map[bucket]bool{}
	var drop []time.Time
	for _, t := range ts {
		age := now.Sub(t)
		var b bucket
		switch {
		case age < keepAll:
			continue
		case age < keepHourly:
			b = bucket{t.UTC().Truncate(time.Hour), time.Hour}
		case age < keepFor:
			b = bucket{t.UTC().Truncate(24 * time.Hour), 24 * time.Hour}
		default:
			drop = append(drop, t)
			continue
		}
		if kept[b] {
			drop = append(drop, t)
		} else {
			kept[b] = true
		}
	}
	return drop
}
