package rules

import (
	"fmt"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
)

// MemoryPercent is how much of RAM is genuinely spoken for. On macOS it is
// pressure (100 - the kernel's free percentage), not "used": macOS keeps RAM
// full of cache and compressed pages on purpose, so "used" is always high.
func MemoryPercent(m collect.Memory) float64 {
	if m.Pressure != nil {
		return 100 - m.Pressure.FreePercent
	}
	if m.Total == 0 {
		return 0
	}
	avail := min(m.Available, m.Total)
	return float64(m.Total-avail) / float64(m.Total) * 100
}

func Memory(s collect.Snapshot) Finding {
	m := s.Memory
	if m.Total == 0 {
		return unknown(CheckMemory, s, "memory")
	}
	p := MemoryPercent(m)
	f := Finding{Check: CheckMemory, Severity: OK, Percent: pct(p), Summary: "plenty free"}

	switch {
	case p >= MemCritPercent:
		f.Severity = Crit
	case p >= MemWarnPercent:
		f.Severity = Warn
	}
	if m.Pressure != nil {
		switch m.Pressure.Level {
		case "critical":
			f.Severity = Crit
		case "warn":
			f.Severity = max(f.Severity, Warn)
		}
	}
	heavySwap := m.SwapUsed >= SwapNotable && float64(m.SwapUsed) >= SwapWarnShareOfRAM*float64(m.Total)
	if heavySwap {
		f.Severity = max(f.Severity, Warn)
	}

	switch f.Severity {
	case Crit:
		f.Summary = "under heavy pressure"
	case Warn:
		f.Summary = "running low"
		if heavySwap && p < MemWarnPercent {
			f.Summary = "heavy swapping"
		}
	}

	for _, g := range topBy(GroupByApp(s.Processes), func(g AppGroup) uint64 { return g.RSS }, MemProcNotable, TopN) {
		f.Details = append(f.Details, fmt.Sprintf("%s %s", g.label(), MemSize(g.RSS)))
	}
	if m.SwapUsed >= SwapNotable {
		f.Details = append(f.Details, fmt.Sprintf("swap in use %s", MemSize(m.SwapUsed)))
	}
	if m.Compressed >= SwapNotable {
		f.Details = append(f.Details, fmt.Sprintf("compressed %s", MemSize(m.Compressed)))
	}
	return f
}
