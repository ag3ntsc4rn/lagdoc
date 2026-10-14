//go:build darwin

package collect

import "syscall"

// addPlatformMemory reads macOS's own pressure figures, the same ones
// memory_pressure(1) prints. On macOS "used" memory is nearly always high
// because the kernel fills spare RAM with cache and compresses idle pages,
// so pressure is the honest measure.
func addPlatformMemory(m *Memory) {
	if free, err := syscall.SysctlUint32("kern.memorystatus_level"); err == nil {
		m.Pressure = &Pressure{FreePercent: float64(free), Level: "normal"}
		if lvl, err := syscall.SysctlUint32("kern.memorystatus_vm_pressure_level"); err == nil {
			m.Pressure.Level = pressureLevel(lvl)
		}
	}
	if b, err := sysctlUint64("vm.compressor_bytes_used"); err == nil {
		m.Compressed = b
	}
}

// pressureLevel maps the kernel's dispatch-source pressure bits
// (DISPATCH_MEMORYPRESSURE_NORMAL/WARN/CRITICAL = 1/2/4).
func pressureLevel(v uint32) string {
	switch {
	case v&4 != 0:
		return "critical"
	case v&2 != 0:
		return "warn"
	default:
		return "normal"
	}
}

func sysctlUint64(name string) (uint64, error) {
	s, err := syscall.Sysctl(name)
	if err != nil {
		return 0, err
	}
	// syscall.Sysctl returns the raw bytes as a string and drops a trailing
	// NUL, so a little-endian uint64 can come back shorter than 8 bytes.
	b := []byte(s)
	var v uint64
	for i := len(b) - 1; i >= 0; i-- {
		v = v<<8 | uint64(b[i])
	}
	return v, nil
}
