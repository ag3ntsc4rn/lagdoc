package rules

import "time"

// Every threshold lagdoc judges by lives here.

const (
	gib = 1 << 30
	gb  = 1e9
)

// Memory: percent of RAM in use, after counting cache the OS can drop.
// On macOS the figure is 100 - kern.memorystatus_level, and the kernel's own
// pressure level (normal/warn/critical) also sets a floor.
const (
	MemWarnPercent = 80.0
	MemCritPercent = 90.0
	// Swap above this share of RAM means the machine has been short of memory
	// for a while, even if pressure has eased.
	SwapWarnShareOfRAM = 0.5
	// Swap below this isn't worth mentioning.
	SwapNotable = 512 << 20
	// Apps using less than this aren't named as a cause.
	MemProcNotable = 300 << 20
)

// Disk: a volume is judged by free space, with a percentage guard so a big
// data drive at 90 % with hundreds of GB free isn't called full, and a
// small one (a 1 GB /boot) isn't called full just for having under 2 GB.
const (
	DiskWarnPercent  = 90.0
	DiskWarnFreeUpTo = 50 * gb // ≥ DiskWarnPercent only warns below this much free
	DiskWarnFree     = 10 * gb // warns below this much free once the volume is ≥ DiskLowFreeFrom full
	DiskCritPercent  = 95.0
	DiskCritFreeUpTo = 20 * gb
	DiskCritFree     = 2 * gb // crit below this once the volume is ≥ DiskWarnPercent full
	DiskLowFreeFrom  = 80.0
)

// CPU: whole-machine use over the ~1 s sample.
const (
	CPUWarnPercent = 75.0
	CPUCritPercent = 90.0
	// Below warn, from here the summary says "some load" instead of "mostly idle".
	CPUSomeLoadPercent = 30.0
	// Processes using less than this share of one core aren't named.
	CPUProcNotable = 10.0
)

// Network.
const (
	DNSWarn  = 500 * time.Millisecond
	DialWarn = 500 * time.Millisecond
)

// TopN is how many processes or mounts a finding names at most.
const TopN = 3
