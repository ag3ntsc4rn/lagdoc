// Package rules turns a Snapshot into ranked Findings. Everything here is a
// pure function of the snapshot, so it is tested with hand-built snapshots.
package rules

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
)

type Severity int

const (
	Unknown Severity = iota // the data couldn't be collected
	OK
	Warn
	Crit
)

var severityNames = [...]string{"unknown", "ok", "warn", "crit"}

func (s Severity) String() string { return severityNames[s] }

func (s Severity) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

func (s *Severity) UnmarshalJSON(b []byte) error {
	var name string
	if err := json.Unmarshal(b, &name); err != nil {
		return err
	}
	i := slices.Index(severityNames[:], name)
	if i < 0 {
		return fmt.Errorf("unknown severity %q", name)
	}
	*s = Severity(i)
	return nil
}

type Finding struct {
	Check    string   `json:"check"`
	Severity Severity `json:"severity"`
	// Summary is a few plain words: "under heavy pressure", "dns 12 ms".
	Summary string `json:"summary"`
	// Percent drives the bar; nil when a check has no single percentage.
	Percent *float64 `json:"percent,omitempty"`
	// Details name the causes: apps, mounts, errors.
	Details []string `json:"details"`
}

// Check names, in the order they're listed when severities tie.
const (
	CheckMemory  = "memory"
	CheckDisk    = "disk"
	CheckCPU     = "cpu"
	CheckNetwork = "network"
)

var checkOrder = []string{CheckMemory, CheckDisk, CheckCPU, CheckNetwork}

// Evaluate runs every rule and ranks the findings, worst first.
func Evaluate(s collect.Snapshot) []Finding {
	fs := []Finding{Memory(s), Disk(s), CPU(s), Network(s)}
	slices.SortStableFunc(fs, func(a, b Finding) int {
		if a.Severity != b.Severity {
			return int(b.Severity) - int(a.Severity)
		}
		return slices.Index(checkOrder, a.Check) - slices.Index(checkOrder, b.Check)
	})
	return fs
}

// Causes counts the findings that are problems (warn or crit).
func Causes(fs []Finding) int {
	n := 0
	for _, f := range fs {
		if f.Severity >= Warn {
			n++
		}
	}
	return n
}

// Worst returns the highest severity among the findings.
func Worst(fs []Finding) Severity {
	w := Unknown
	for _, f := range fs {
		w = max(w, f.Severity)
	}
	return w
}

func pct(v float64) *float64 { return &v }

func unknown(check string, s collect.Snapshot, section string) Finding {
	why := "couldn't read " + section
	if e := s.Errors[section]; e != "" {
		why += ": " + e
	}
	return Finding{Check: check, Severity: Unknown, Summary: "not measured", Details: []string{why}}
}

// Verdict is the one-word answer: "slow" if anything is critical, "warn" if
// something only needs watching, "ok" if every check passed, and "unknown"
// if nothing could be measured.
func Verdict(fs []Finding) string {
	switch Worst(fs) {
	case Crit:
		return "slow"
	case Warn:
		return "warn"
	case OK:
		return "ok"
	}
	return "unknown"
}
