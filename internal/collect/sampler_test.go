package collect

import (
	"context"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
)

func TestCPUPercent(t *testing.T) {
	a := cpu.TimesStat{User: 10, System: 5, Idle: 85}
	b := cpu.TimesStat{User: 30, System: 15, Idle: 155} // 30 busy of 100
	if p, ok := cpuPercent(a, b); !ok || p < 29.99 || p > 30.01 {
		t.Errorf("got %v %v", p, ok)
	}
	if _, ok := cpuPercent(b, b); ok {
		t.Error("no time passed: no reading")
	}
}

func TestSamplerOnlyWaitsOnce(t *testing.T) {
	sm := &Sampler{}
	start := time.Now()
	first := sm.Next(context.Background())
	if time.Since(start) < SampleWindow {
		t.Error("the first sample measures over SampleWindow")
	}
	if first.Network.DialAddr != "" {
		t.Error("NetworkEvery 0 must not probe the network")
	}
	time.Sleep(50 * time.Millisecond)
	start = time.Now()
	second := sm.Next(context.Background())
	if d := time.Since(start); d > SampleWindow/2 {
		t.Errorf("later samples shouldn't wait: took %v", d)
	}
	if second.CPU.Sample <= 0 || second.CPU.Sample > SampleWindow {
		t.Errorf("sample window is the time since the last call: %v", second.CPU.Sample)
	}
	if len(second.Processes) == 0 {
		t.Error("no processes")
	}
}

func TestSamplerReusesNetworkBetweenProbes(t *testing.T) {
	sm := &Sampler{NetworkEvery: time.Hour}
	sm.net = Network{DialAddr: "cached"}
	sm.netAt = time.Now()
	sm.procs = map[int32]procSample{} // skip the first wait
	sm.at = time.Now()
	if s := sm.Next(context.Background()); s.Network.DialAddr != "cached" {
		t.Errorf("got %+v", s.Network)
	}
}

func TestSamplerKeepsLastCPUWhenCountersStall(t *testing.T) {
	sm := &Sampler{}
	sm.procs = map[int32]procSample{} // skip the first wait
	sm.at = time.Now()
	sm.cpu = cpu.TimesStat{User: 1e15} // "later" than now: no usable delta
	sm.lastCPU = CPU{Percent: 42, Sample: time.Second}
	s := sm.Next(context.Background())
	if s.CPU.Percent != 42 || s.Errors["cpu"] != "" {
		t.Errorf("got %+v, errors %v", s.CPU, s.Errors)
	}
	// With no earlier reading, it's reported as unmeasured.
	sm.cpu = cpu.TimesStat{User: 1e15}
	sm.lastCPU = CPU{}
	if s := sm.Next(context.Background()); s.Errors["cpu"] == "" {
		t.Errorf("want a cpu error, got %+v", s.CPU)
	}
}
