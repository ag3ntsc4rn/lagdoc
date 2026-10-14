package collect

import (
	"context"
	"slices"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
)

// skipFSTypes are pseudo, virtual and network filesystems. Network mounts are
// skipped before statfs, because statfs on a stale NFS or SMB mount can hang.
var skipFSTypes = map[string]bool{
	"autofs": true, "binfmt_misc": true, "bpf": true, "cgroup": true, "cgroup2": true,
	"configfs": true, "debugfs": true, "devfs": true, "devpts": true, "devtmpfs": true,
	"efivarfs": true, "fusectl": true, "hugetlbfs": true, "mqueue": true, "nsfs": true,
	"overlay": true, "proc": true, "pstore": true, "ramfs": true, "rpc_pipefs": true,
	"securityfs": true, "squashfs": true, "sysfs": true, "tmpfs": true, "tracefs": true,
	"nullfs": true, "lifs": true, "iso9660": true, "udf": true, "cd9660": true,
	"nfs": true, "nfs4": true, "cifs": true, "smbfs": true, "smb3": true, "afpfs": true,
	"webdav": true, "9p": true, "fuse.sshfs": true, "fuse.gvfsd-fuse": true, "fuse.portal": true,
}

// skipMountPrefixes are system mounts that duplicate or never matter for the
// user: macOS's sealed system volumes (they share the Data volume's
// container), Linux pseudo trees and snap images. /run is left alone:
// removable drives are mounted under /run/media.
var skipMountPrefixes = []string{
	"/System/Volumes/VM", "/System/Volumes/Preboot", "/System/Volumes/Update",
	"/System/Volumes/xarts", "/System/Volumes/iSCPreboot", "/System/Volumes/Hardware",
	"/Library/Developer/CoreSimulator/Volumes",
	"/proc", "/sys", "/dev", "/snap", "/boot/efi",
}

func collectDisks(ctx context.Context) ([]Disk, error) {
	parts, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return nil, err
	}
	var out []mount
	for _, p := range parts {
		if skipMount(p.Mountpoint, p.Fstype) {
			continue
		}
		u, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil || u.Total == 0 {
			continue
		}
		out = append(out, mount{
			Disk:     Disk{Mount: p.Mountpoint, FSType: p.Fstype, Total: u.Total, Free: u.Free},
			ReadOnly: slices.Contains(p.Opts, "ro"),
		})
	}
	return dedupeMounts(out), nil
}

func skipMount(mountpoint, fstype string) bool {
	if skipFSTypes[fstype] {
		return true
	}
	for _, p := range skipMountPrefixes {
		if mountpoint == p || strings.HasPrefix(mountpoint, p+"/") {
			return true
		}
	}
	return false
}

type mount struct {
	Disk
	ReadOnly bool
}

// dedupeMounts merges mounts that are one pool of space: APFS volumes in one
// container (macOS "/" and "/System/Volumes/Data"), btrfs subvolumes, bind
// mounts. They report identical total and free bytes. Each group is shown
// once, under its shortest mount path. Groups that are entirely read-only
// (mounted disk images, installer media) are dropped: they are always full
// by design and can't slow anything down.
func dedupeMounts(ms []mount) []Disk {
	type key struct{ total, free uint64 }
	idx := map[key]int{}
	var groups [][]mount
	for _, m := range ms {
		k := key{m.Total, m.Free}
		i, ok := idx[k]
		if !ok {
			i = len(groups)
			idx[k] = i
			groups = append(groups, nil)
		}
		groups[i] = append(groups[i], m)
	}
	var out []Disk
	for _, g := range groups {
		if !slices.ContainsFunc(g, func(m mount) bool { return !m.ReadOnly }) {
			continue
		}
		best := g[0]
		for _, m := range g[1:] {
			if len(m.Mount) < len(best.Mount) {
				best = m
			}
		}
		out = append(out, best.Disk)
	}
	return out
}
