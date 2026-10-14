package collect

import (
	"context"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
)

// SampleWindow is how long CPU use is measured over.
const SampleWindow = time.Second

// Collect takes a Snapshot. It takes about SampleWindow; the network probe
// runs alongside the CPU sample. A collector that fails is recorded in
// Snapshot.Errors and its section left empty, so one broken source never
// hides the others.
func Collect(ctx context.Context) Snapshot {
	s := Snapshot{TakenAt: time.Now(), Errors: map[string]string{}}
	var mu sync.Mutex
	fail := func(name string, err error) {
		mu.Lock()
		s.Errors[name] = err.Error()
		mu.Unlock()
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.Network = probeNetwork(ctx)
	}()

	s.Host = collectHost(ctx)
	if m, err := collectMemory(ctx); err != nil {
		fail("memory", err)
	} else {
		s.Memory = m
	}
	if d, err := collectDisks(ctx); err != nil {
		fail("disk", err)
	} else {
		s.Disks = d
	}

	// Process CPU is the change in CPU time across the same window as the
	// machine-wide sample, so the two agree.
	before := readProcesses(ctx)
	start := time.Now()
	pct, err := cpu.PercentWithContext(ctx, SampleWindow, false)
	elapsed := time.Since(start)
	after := readProcesses(ctx)
	if err != nil || len(pct) == 0 {
		if err == nil {
			err = errNoData
		}
		fail("cpu", err)
	} else {
		s.CPU = CPU{Percent: pct[0], Sample: elapsed}
	}
	s.Processes = mergeProcessSamples(before, after, elapsed)

	wg.Wait()
	if len(s.Errors) == 0 {
		s.Errors = nil
	}
	return s
}

func collectHost(ctx context.Context) Host {
	h := Host{OS: runtime.GOOS, CPUs: runtime.NumCPU()}
	h.Hostname, _ = os.Hostname()
	if info, err := host.InfoWithContext(ctx); err == nil {
		h.Uptime = time.Duration(info.Uptime) * time.Second
		h.OSName = osName(info.Platform, info.PlatformVersion)
		if h.Hostname == "" {
			h.Hostname = info.Hostname
		}
	}
	if h.OSName == "" {
		h.OSName = runtime.GOOS
	}
	return h
}
