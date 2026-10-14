package collect

import (
	"slices"
	"testing"
	"time"
)

func TestSkipMount(t *testing.T) {
	skip := []struct{ mount, fs string }{
		{"/proc", "proc"}, {"/dev/shm", "tmpfs"}, {"/mnt/share", "nfs4"},
		{"/Volumes/NAS", "smbfs"}, {"/System/Volumes/VM", "apfs"},
		{"/System/Volumes/Preboot", "apfs"}, {"/snap/core/123", "squashfs"},
		{"/boot/efi", "vfat"}, {"/dev", "devfs"},
	}
	for _, c := range skip {
		if !skipMount(c.mount, c.fs) {
			t.Errorf("%s (%s) should be skipped", c.mount, c.fs)
		}
	}
	keep := []struct{ mount, fs string }{
		{"/", "apfs"}, {"/System/Volumes/Data", "apfs"}, {"/home", "ext4"},
		{"/run/media/me/USB", "exfat"}, {"/Volumes/Backup", "apfs"},
		{"C:", "NTFS"}, {"/development", "ext4"}, {"/snapshots", "btrfs"},
	}
	for _, c := range keep {
		if skipMount(c.mount, c.fs) {
			t.Errorf("%s (%s) should be kept", c.mount, c.fs)
		}
	}
}

func TestDedupeMounts(t *testing.T) {
	ms := []mount{
		// macOS: sealed system snapshot and Data share one APFS container.
		{Disk: Disk{Mount: "/", Total: 250, Free: 20}, ReadOnly: true},
		{Disk: Disk{Mount: "/System/Volumes/Data", Total: 250, Free: 20}},
		// A mounted disk image: read-only and always full.
		{Disk: Disk{Mount: "/Volumes/Installer", Total: 5, Free: 0}, ReadOnly: true},
		{Disk: Disk{Mount: "/Volumes/Backup", Total: 2000, Free: 900}},
	}
	got := dedupeMounts(ms)
	var mounts []string
	for _, d := range got {
		mounts = append(mounts, d.Mount)
	}
	if want := []string{"/", "/Volumes/Backup"}; !slices.Equal(mounts, want) {
		t.Errorf("mounts = %v, want %v", mounts, want)
	}
}

func TestMergeProcessSamples(t *testing.T) {
	before := map[int32]procSample{
		1: {Process: Process{PID: 1, Name: "busy"}, cpuSeconds: 10, haveCPU: true},
		2: {Process: Process{PID: 2, Name: "old"}, cpuSeconds: 5, haveCPU: true},
	}
	after := map[int32]procSample{
		1: {Process: Process{PID: 1, Name: "busy", RSS: 100}, cpuSeconds: 11.5, haveCPU: true},
		2: {Process: Process{PID: 2, Name: "reused"}, cpuSeconds: 9, haveCPU: true},
		3: {Process: Process{PID: 3, Name: "new"}, cpuSeconds: 1, haveCPU: true},
	}
	got := map[int32]Process{}
	for _, p := range mergeProcessSamples(before, after, time.Second) {
		got[p.PID] = p
	}
	if p := got[1]; p.CPUPercent != 150 || p.RSS != 100 {
		t.Errorf("busy = %+v, want 150%% and RSS from the later sample", p)
	}
	if got[2].CPUPercent != 0 {
		t.Error("a reused PID must not get CPU from the old process")
	}
	if got[3].CPUPercent != 0 {
		t.Error("a process seen only once has no CPU figure")
	}
}

func TestOSName(t *testing.T) {
	cases := map[[2]string]string{
		{"darwin", "15.7.9"}:                       "macOS 15.7.9",
		{"ubuntu", "24.04"}:                        "Ubuntu 24.04",
		{"Microsoft Windows 11 Pro", "10.0.26100"}: "Microsoft Windows 11 Pro",
		{"arch", ""}:                               "Arch",
		{"", ""}:                                   "",
	}
	for in, want := range cases {
		if got := osName(in[0], in[1]); got != want {
			t.Errorf("osName(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}
