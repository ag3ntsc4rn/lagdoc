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

// Collect takes one Snapshot. It takes about SampleWindow; the network
// probe runs alongside the CPU sample. A collector that fails is recorded
// in Snapshot.Errors and its section left empty, so one broken source never
// hides the others.
func Collect(ctx context.Context) Snapshot {
	return (&Sampler{NetworkEvery: time.Nanosecond}).Next(ctx)
}

// Sampler takes snapshots one after another, for live views. CPU use is
// measured between calls, so only the first call waits SampleWindow; later
// ones return as soon as they've read the machine. Not safe for concurrent
// use.
type Sampler struct {
	// NetworkEvery is how often the network is probed (it sends a DNS
	// query and opens a connection). Between probes the last result is
	// reused. Zero means never: Snapshot.Network stays empty.
	NetworkEvery time.Duration

	host     Host
	procs    map[int32]procSample
	cpu      cpu.TimesStat
	at       time.Time
	names    map[int32]procName
	lastCPU  CPU // the last good reading, reused if the next one is unusable
	net      Network
	netAt    time.Time
	hostRead bool
}

func (sm *Sampler) Next(ctx context.Context) Snapshot {
	s := Snapshot{TakenAt: time.Now(), Errors: map[string]string{}}
	var mu sync.Mutex
	fail := func(name string, err error) {
		mu.Lock()
		s.Errors[name] = err.Error()
		mu.Unlock()
	}

	var wg sync.WaitGroup
	if sm.NetworkEvery > 0 && (sm.netAt.IsZero() || time.Since(sm.netAt) >= sm.NetworkEvery) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sm.net = probeNetwork(ctx)
			sm.netAt = time.Now()
		}()
	}

	if !sm.hostRead {
		sm.host = collectHost(ctx)
		sm.hostRead = true
	} else {
		sm.host.Uptime += time.Since(sm.at)
	}
	s.Host = sm.host
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
	// machine-wide figure, so the two agree.
	if sm.names == nil {
		sm.names = map[int32]procName{}
	}
	if sm.procs == nil {
		sm.procs = readProcesses(ctx, sm.names)
		sm.cpu, _ = cpuTimes(ctx)
		sm.at = time.Now()
		select {
		case <-ctx.Done():
		case <-time.After(SampleWindow):
		}
	}
	procs := readProcesses(ctx, sm.names)
	times, err := cpuTimes(ctx)
	now := time.Now()
	elapsed := now.Sub(sm.at)
	// The counters occasionally fail to advance between close readings.
	// A live view then shows the last good figure rather than flapping to
	// "not measured".
	if pct, ok := cpuPercent(sm.cpu, times); err == nil && ok {
		s.CPU = CPU{Percent: pct, Sample: elapsed}
		sm.lastCPU = s.CPU
	} else if sm.lastCPU.Sample > 0 {
		s.CPU = sm.lastCPU
	} else if err != nil {
		fail("cpu", err)
	} else {
		fail("cpu", errNoData)
	}
	s.Processes = mergeProcessSamples(sm.procs, procs, elapsed)
	sm.procs, sm.cpu, sm.at = procs, times, now

	wg.Wait()
	s.Network = sm.net
	if len(s.Errors) == 0 {
		s.Errors = nil
	}
	return s
}

func cpuTimes(ctx context.Context) (cpu.TimesStat, error) {
	ts, err := cpu.TimesWithContext(ctx, false)
	if err != nil {
		return cpu.TimesStat{}, err
	}
	if len(ts) == 0 {
		return cpu.TimesStat{}, errNoData
	}
	return ts[0], nil
}

// cpuPercent is the share of time the machine was busy between two
// readings, as gopsutil's cpu.Percent works it out.
func cpuPercent(a, b cpu.TimesStat) (float64, bool) {
	busy := func(t cpu.TimesStat) float64 { return t.Total() - t.Idle - t.Iowait }
	total := b.Total() - a.Total()
	if total <= 0 {
		return 0, false
	}
	return min(100, max(0, (busy(b)-busy(a))/total*100)), true
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
