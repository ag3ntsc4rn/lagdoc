package rules

import (
	"fmt"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
)

func CPU(s collect.Snapshot) Finding {
	if s.CPU.Sample == 0 {
		return unknown(CheckCPU, s, "cpu")
	}
	p := s.CPU.Percent
	f := Finding{Check: CheckCPU, Severity: OK, Percent: pct(p), Summary: "mostly idle"}
	switch {
	case p >= CPUCritPercent:
		f.Severity, f.Summary = Crit, "maxed out"
	case p >= CPUWarnPercent:
		f.Severity, f.Summary = Warn, "busy"
	case p >= CPUSomeLoadPercent:
		f.Summary = "some load"
	}
	for _, g := range topBy(GroupByApp(s.Processes), func(g AppGroup) float64 { return g.CPUPercent }, CPUProcNotable, TopN) {
		f.Details = append(f.Details, fmt.Sprintf("%s %s", g.label(), cores(g.CPUPercent)))
	}
	return f
}

// cores phrases per-process CPU (100 = one core) the way people think of it.
func cores(p float64) string {
	if p < 95 {
		return fmt.Sprintf("%.0f%% of a core", p)
	}
	return fmt.Sprintf("%.1f cores", p/100)
}
