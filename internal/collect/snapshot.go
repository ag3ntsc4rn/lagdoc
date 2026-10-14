// Package collect gathers a point-in-time Snapshot of the machine.
// It holds no judgement: deciding what is a problem is the rules package's job.
package collect

import "time"

// Snapshot is everything the rules look at. Fields are plain data so tests
// can build fake snapshots by hand, and later milestones can store them.
type Snapshot struct {
	TakenAt time.Time `json:"taken_at"`
	Host    Host      `json:"host"`
	Memory  Memory    `json:"memory"`
	CPU     CPU       `json:"cpu"`
	Disks   []Disk    `json:"disks"`
	Network Network   `json:"network"`
	// Processes holds every process we could read, with memory and CPU use.
	Processes []Process `json:"processes"`
	// Errors lists collectors that failed; their section is left zero.
	Errors map[string]string `json:"errors,omitempty"`
}

type Host struct {
	Hostname string        `json:"hostname"`
	OS       string        `json:"os"`      // runtime.GOOS: darwin, linux, windows
	OSName   string        `json:"os_name"` // human name, e.g. "macOS 15.7"
	Uptime   time.Duration `json:"uptime"`
	CPUs     int           `json:"cpus"` // logical CPUs
}

type Memory struct {
	Total     uint64 `json:"total"`
	Available uint64 `json:"available"` // what the OS says can be handed out without swapping
	SwapTotal uint64 `json:"swap_total"`
	SwapUsed  uint64 `json:"swap_used"`
	// Compressed is RAM occupied by the memory compressor (macOS); 0 elsewhere.
	Compressed uint64 `json:"compressed"`
	// Pressure is the OS's own memory pressure reading, if it has one (macOS).
	Pressure *Pressure `json:"pressure,omitempty"`
}

// Pressure mirrors what macOS reports through memory_pressure(1).
type Pressure struct {
	// FreePercent is kern.memorystatus_level: "System-wide memory free percentage".
	FreePercent float64 `json:"free_percent"`
	// Level is the kernel's pressure state: normal, warn or critical.
	Level string `json:"level"`
}

type CPU struct {
	Percent float64       `json:"percent"` // whole machine, 0-100, over Sample
	Sample  time.Duration `json:"sample"`
}

type Disk struct {
	Mount  string `json:"mount"`
	FSType string `json:"fstype"`
	Total  uint64 `json:"total"`
	Free   uint64 `json:"free"` // available to ordinary users
}

type Network struct {
	DNSHost  string        `json:"dns_host"`
	DNS      time.Duration `json:"dns"`
	DNSErr   string        `json:"dns_error,omitempty"`
	DialAddr string        `json:"dial_addr"`
	Dial     time.Duration `json:"dial"`
	DialErr  string        `json:"dial_error,omitempty"`
}

type Process struct {
	PID  int32  `json:"pid"`
	Name string `json:"name"`
	Exe  string `json:"exe,omitempty"`
	RSS  uint64 `json:"rss"`
	// CPUPercent is percent of one core over the sample (200 = two cores busy).
	CPUPercent float64 `json:"cpu_percent"`
}
