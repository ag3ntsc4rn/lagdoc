package collect

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

type procSample struct {
	Process
	cpuSeconds float64
	haveCPU    bool
}

// readProcesses reads every process it is allowed to. Processes owned by
// other users may be unreadable without root; those are skipped, which is
// fine for a "what's slowing me down" view.
func readProcesses(ctx context.Context) map[int32]procSample {
	pids, err := process.PidsWithContext(ctx)
	if err != nil {
		return nil
	}
	self := int32(os.Getpid())
	out := make(map[int32]procSample, len(pids))
	for _, pid := range pids {
		if pid == self {
			continue // lagdoc's own sampling shouldn't show up as a cause
		}
		p := &process.Process{Pid: pid}
		s := procSample{Process: Process{PID: pid}}
		if exe, err := p.ExeWithContext(ctx); err == nil && exe != "" {
			s.Exe = exe
			s.Name = filepath.Base(exe)
		} else if name, err := p.NameWithContext(ctx); err == nil {
			s.Name = name
		} else {
			continue
		}
		if mi, err := p.MemoryInfoWithContext(ctx); err == nil {
			s.RSS = mi.RSS
		}
		if t, err := p.TimesWithContext(ctx); err == nil {
			s.cpuSeconds = t.User + t.System
			s.haveCPU = true
		}
		out[pid] = s
	}
	return out
}

// mergeProcessSamples turns two readings into per-process CPU use over the
// window. Memory comes from the later reading. A PID whose name changed
// between readings was reused, so it gets no CPU figure.
func mergeProcessSamples(before, after map[int32]procSample, elapsed time.Duration) []Process {
	out := make([]Process, 0, len(after))
	secs := elapsed.Seconds()
	for pid, a := range after {
		p := a.Process
		if b, ok := before[pid]; ok && secs > 0 && b.haveCPU && a.haveCPU && b.Name == a.Name {
			if d := a.cpuSeconds - b.cpuSeconds; d > 0 {
				p.CPUPercent = d / secs * 100
			}
		}
		out = append(out, p)
	}
	return out
}
