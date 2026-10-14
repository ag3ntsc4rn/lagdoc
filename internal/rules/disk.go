package rules

import (
	"fmt"
	"slices"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
)

// DiskPercent counts space reserved for root as used, because ordinary
// programs can't write to it either.
func DiskPercent(d collect.Disk) float64 {
	if d.Total == 0 {
		return 0
	}
	free := min(d.Free, d.Total)
	return float64(d.Total-free) / float64(d.Total) * 100
}

func diskSeverity(d collect.Disk) Severity {
	p, free := DiskPercent(d), float64(d.Free)
	switch {
	case (p >= DiskWarnPercent && free < DiskCritFree) || (p >= DiskCritPercent && free < DiskCritFreeUpTo):
		return Crit
	case (p >= DiskLowFreeFrom && free < DiskWarnFree) || (p >= DiskWarnPercent && free < DiskWarnFreeUpTo):
		return Warn
	}
	return OK
}

// Disk reports the fullest volume in the bar and names the volumes at fault.
// When all are fine it names each one, so the JSON still lists them.
func Disk(s collect.Snapshot) Finding {
	if len(s.Disks) == 0 {
		return unknown(CheckDisk, s, "disk")
	}
	type judged struct {
		collect.Disk
		sev Severity
		pct float64
	}
	var js []judged
	for _, d := range s.Disks {
		js = append(js, judged{d, diskSeverity(d), DiskPercent(d)})
	}
	slices.SortStableFunc(js, func(a, b judged) int {
		if a.sev != b.sev {
			return int(b.sev) - int(a.sev)
		}
		switch {
		case a.pct > b.pct:
			return -1
		case a.pct < b.pct:
			return 1
		}
		return 0
	})

	worst := js[0]
	f := Finding{Check: CheckDisk, Severity: worst.sev, Percent: pct(worst.pct), Summary: "space to spare"}
	switch worst.sev {
	case Crit:
		f.Summary = "almost full"
	case Warn:
		f.Summary = "getting full"
	}
	for _, j := range js {
		if len(f.Details) == TopN || (worst.sev > OK && j.sev == OK) {
			break
		}
		f.Details = append(f.Details, fmt.Sprintf("%s %s free of %s", j.Mount, DiskSize(j.Free), DiskSize(j.Total)))
	}
	return f
}
