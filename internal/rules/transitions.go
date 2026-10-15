package rules

import "time"

// Transition is a check whose severity changed between two runs, as
// `lagdoc watch` reports it. Initial marks the first reading, when there
// was nothing to change from.
type Transition struct {
	At       time.Time
	Check    string
	From, To Severity
	Initial  bool
	Summary  string
	Details  []string
}

// Transitions compares two sets of findings. With no previous findings,
// every check is an Initial transition, so a log starts with the state.
// Only severity changes count: "dns 12 ms" becoming "dns 15 ms" isn't one.
func Transitions(prev, next []Finding, at time.Time) []Transition {
	var out []Transition
	for _, n := range next {
		t := Transition{At: at, Check: n.Check, To: n.Severity, Summary: n.Summary, Details: n.Details}
		if prev == nil {
			t.Initial = true
			out = append(out, t)
			continue
		}
		for _, p := range prev {
			if p.Check == n.Check && p.Severity != n.Severity {
				t.From = p.Severity
				out = append(out, t)
			}
		}
	}
	return out
}
