package changes

import (
	"slices"
	"testing"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/history"
)

const (
	mib = 1 << 20
	gib = 1 << 30
)

func record() history.Record {
	return history.Record{
		Disks:   []collect.Disk{{Mount: "/", Total: 250e9, Free: 50e9}},
		Running: []history.AppUsage{{Name: "Finder", Processes: 1, RSS: 100 * mib}},
		Inventory: collect.Inventory{
			StartupItems: []collect.StartupItem{
				{Name: "com.example.updater", Source: "/Library/LaunchDaemons"},
				{Name: "com.old.agent", Source: "~/Library/LaunchAgents"},
			},
			Apps: []collect.App{
				{Name: "Editor", Version: "1.0", Source: "/Applications"},
				{Name: "Zoom", Version: "6.1", Source: "/Applications"},
				{Name: "NoVersion", Source: "/Applications"},
			},
		},
	}
}

func TestNoChanges(t *testing.T) {
	c := Diff(record(), record())
	if c.Count() != 0 || len(c.Skipped) != 0 || len(c.Unread) != 0 {
		t.Errorf("identical records: %+v", c)
	}
}

func TestInventoryChanges(t *testing.T) {
	before, after := record(), record()
	after.Inventory.StartupItems = []collect.StartupItem{
		{Name: "com.example.updater", Source: "/Library/LaunchDaemons"},
		{Name: "com.docker.vmnetd", Source: "/Library/LaunchDaemons"},
		// The same name in another place is a different item.
		{Name: "com.example.updater", Source: "/Library/LaunchAgents"},
	}
	after.Inventory.Apps = []collect.App{
		{Name: "Editor", Version: "1.1", Source: "/Applications"},
		{Name: "docker", Version: "4.9", Source: "/Applications"},
		{Name: "Arc", Source: "~/Applications"},
		{Name: "NoVersion", Version: "3.0", Source: "/Applications"}, // gained a version: not an update
	}
	c := Diff(before, after)

	names := func(xs []collect.StartupItem) (out []string) {
		for _, x := range xs {
			out = append(out, x.Source+"/"+x.Name)
		}
		return out
	}
	if got := names(c.StartupAdded); !slices.Equal(got, []string{"/Library/LaunchDaemons/com.docker.vmnetd", "/Library/LaunchAgents/com.example.updater"}) {
		t.Errorf("startup added: %v", got)
	}
	if got := names(c.StartupRemoved); !slices.Equal(got, []string{"~/Library/LaunchAgents/com.old.agent"}) {
		t.Errorf("startup removed: %v", got)
	}
	appNames := func(xs []collect.App) (out []string) {
		for _, x := range xs {
			out = append(out, x.Name)
		}
		return out
	}
	if got := appNames(c.AppsAdded); !slices.Equal(got, []string{"Arc", "docker"}) {
		t.Errorf("apps added (sorted ignoring case): %v", got)
	}
	if got := appNames(c.AppsRemoved); !slices.Equal(got, []string{"Zoom"}) {
		t.Errorf("apps removed: %v", got)
	}
	if !slices.Equal(c.AppsUpdated, []AppUpdate{{Name: "Editor", From: "1.0", To: "1.1"}}) {
		t.Errorf("apps updated: %v", c.AppsUpdated)
	}
	if c.Count() != 7 {
		t.Errorf("count %d", c.Count())
	}
}

func TestAppMovedBetweenFoldersIsNotAChange(t *testing.T) {
	before, after := record(), record()
	after.Inventory.Apps[0].Source = "~/Applications"
	if c := Diff(before, after); c.Count() != 0 {
		t.Errorf("got %+v", c)
	}
}

func TestUnreadSourcesAreNotChanges(t *testing.T) {
	// Now: /Library/LaunchDaemons and /Applications couldn't be read, so
	// their items are missing; that isn't a removal.
	before, after := record(), record()
	after.Inventory.StartupItems = after.Inventory.StartupItems[1:]
	after.Inventory.Apps = nil
	after.Inventory.Failed = []string{"/Library/LaunchDaemons", "/Applications"}
	c := Diff(before, after)
	if c.Count() != 0 {
		t.Errorf("unreadable now: %+v", c)
	}
	if !slices.Equal(c.Unread, []string{"/Applications", "/Library/LaunchDaemons"}) {
		t.Errorf("unread: %v", c.Unread)
	}
	// And the other way round: unreadable before, readable now isn't an addition.
	c = Diff(after, before)
	if c.Count() != 0 {
		t.Errorf("unreadable before: %+v", c)
	}
}

func TestDiskChanges(t *testing.T) {
	before, after := record(), record()
	before.Disks = append(before.Disks,
		collect.Disk{Mount: "/data", Total: 2e12, Free: 1e12},
		collect.Disk{Mount: "/Volumes/USB", Total: 64e9, Free: 60e9})
	after.Disks = []collect.Disk{
		{Mount: "/", Total: 250e9, Free: 31.8e9},        // 18.2 GB more used
		{Mount: "/data", Total: 2e12, Free: 1.004e12},   // 4 GB freed: below the bar
		{Mount: "/Volumes/New", Total: 1e12, Free: 1e9}, // wasn't there before
	}
	c := Diff(before, after)
	if len(c.Disks) != 1 || c.Disks[0].Mount != "/" || c.Disks[0].UsedChange() != 18.2e9 {
		t.Fatalf("got %+v", c.Disks)
	}
	after.Disks[1].Free = 1.006e12 // 6 GB freed
	c = Diff(before, after)
	if len(c.Disks) != 2 || c.Disks[1].UsedChange() != -6e9 {
		t.Errorf("freed space counts too: %+v", c.Disks)
	}
}

func TestDiskSkippedWhenUnreadable(t *testing.T) {
	before, after := record(), record()
	after.Disks = nil
	after.Errors = map[string]string{"disk": "boom"}
	c := Diff(before, after)
	if !slices.Contains(c.Skipped, SectionDisk) || len(c.Disks) != 0 {
		t.Errorf("got %+v", c)
	}
}

func TestHeavyApps(t *testing.T) {
	before, after := record(), record()
	before.Running = []history.AppUsage{
		{Name: "Chrome", Processes: 20, RSS: 2 * gib},
		{Name: "Slack", Processes: 5, RSS: 400 * mib},
		{Name: "Code", Processes: 8, RSS: 3 * gib},
		{Name: "ffmpeg", Processes: 1, RSS: 50 * mib, CPUPercent: 4},
		{Name: "Spotify", Processes: 3, RSS: 900 * mib},
		{Name: "Fan", Processes: 1, CPUPercent: 40},
	}
	after.Running = []history.AppUsage{
		{Name: "Chrome", Processes: 23, RSS: 3*gib + 100*mib}, // +1.1 GiB, not double: counts
		{Name: "Slack", Processes: 5, RSS: 450 * mib},         // below heavy
		{Name: "Code", Processes: 8, RSS: 2 * gib},            // shrank
		{Name: "ffmpeg", Processes: 1, RSS: 50 * mib, CPUPercent: 180},
		{Name: "Docker", Processes: 4, RSS: 3 * gib},     // not running before
		{Name: "Spotify", Processes: 3, RSS: 1200 * mib}, // +300 MiB, not double: doesn't count
		{Name: "Fan", Processes: 1, CPUPercent: 60},      // heavy, but not double
	}
	c := Diff(before, after)
	var got []string
	for _, h := range c.Heavy {
		got = append(got, h.Now.Name)
	}
	if !slices.Equal(got, []string{"Chrome", "Docker", "ffmpeg"}) {
		t.Fatalf("heavy: %v", got)
	}
	if h := c.Heavy[1]; h.Before != nil || !h.Mem || h.CPU {
		t.Errorf("Docker is new and heavy by memory: %+v", h)
	}
	if h := c.Heavy[2]; h.Before == nil || h.Mem || !h.CPU {
		t.Errorf("ffmpeg is heavy by CPU: %+v", h)
	}
}

func TestHeavyAppsCapped(t *testing.T) {
	before, after := record(), record()
	after.Running = nil
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		after.Running = append(after.Running, history.AppUsage{Name: n, Processes: 1, RSS: gib})
	}
	if c := Diff(before, after); len(c.Heavy) != 5 {
		t.Errorf("got %d", len(c.Heavy))
	}
}

func TestHeavySkippedWithoutProcesses(t *testing.T) {
	before, after := record(), record()
	before.Running = nil
	c := Diff(before, after)
	if !slices.Contains(c.Skipped, SectionHeavy) {
		t.Errorf("got %+v", c.Skipped)
	}
}
