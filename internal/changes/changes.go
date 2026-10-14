// Package changes compares two history records: what was added, removed or
// grew between an earlier run and now. Like rules, it's a pure function of
// its inputs, tested with hand-built records.
package changes

import (
	"cmp"
	"slices"
	"strings"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/history"
	"github.com/ag3ntsc4rn/lagdoc/internal/rules"
)

// Section names, used in Skipped.
const (
	SectionStartup = "startup items"
	SectionApps    = "apps"
	SectionDisk    = "disk"
	SectionHeavy   = "heavy apps"
)

// Changes is the comparison. The JSON contract is render's, not this type's.
type Changes struct {
	StartupAdded   []collect.StartupItem
	StartupRemoved []collect.StartupItem
	AppsAdded      []collect.App
	AppsRemoved    []collect.App
	AppsUpdated    []AppUpdate
	Disks          []DiskChange
	Heavy          []HeavyApp
	// Unread lists sources (directories, registry keys) that couldn't be
	// read in either run; their items weren't compared.
	Unread []string
	// Skipped lists sections with no data in one of the runs.
	Skipped []string
}

type AppUpdate struct {
	Name string
	From string
	To   string
}

type DiskChange struct {
	Mount  string
	Before collect.Disk
	After  collect.Disk
}

// UsedChange is how much more space is used now (negative when freed).
func (d DiskChange) UsedChange() int64 {
	return int64(d.After.Total-d.After.Free) - int64(d.Before.Total-d.Before.Free)
}

// HeavyApp is an app that is heavy now and wasn't before. Before is nil if
// it wasn't running. Mem and CPU say which use made it count.
type HeavyApp struct {
	Now      history.AppUsage
	Before   *history.AppUsage
	Mem, CPU bool
}

// Count is the number of changes listed.
func (c Changes) Count() int {
	return len(c.StartupAdded) + len(c.StartupRemoved) + len(c.AppsAdded) +
		len(c.AppsRemoved) + len(c.AppsUpdated) + len(c.Disks) + len(c.Heavy)
}

// Diff compares an earlier record with a later one.
func Diff(before, after history.Record) Changes {
	var c Changes
	unreadBefore := setOf(before.Inventory.Failed)
	unreadAfter := setOf(after.Inventory.Failed)
	c.Unread = sortedKeys(unreadBefore, unreadAfter)

	// An item can only be "added" if its source was read before, and only
	// "removed" if its source was read now.
	key := func(s collect.StartupItem) string { return s.Source + "\x00" + s.Name }
	bs := index(before.Inventory.StartupItems, key)
	as := index(after.Inventory.StartupItems, key)
	for _, s := range after.Inventory.StartupItems {
		if _, ok := bs[key(s)]; !ok && !unreadBefore[s.Source] {
			c.StartupAdded = append(c.StartupAdded, s)
		}
	}
	for _, s := range before.Inventory.StartupItems {
		if _, ok := as[key(s)]; !ok && !unreadAfter[s.Source] {
			c.StartupRemoved = append(c.StartupRemoved, s)
		}
	}

	// Apps are matched by name, so moving one between folders isn't a change.
	name := func(a collect.App) string { return a.Name }
	ba := index(before.Inventory.Apps, name)
	aa := index(after.Inventory.Apps, name)
	for _, a := range after.Inventory.Apps {
		b, ok := ba[a.Name]
		switch {
		case !ok && !unreadBefore[a.Source]:
			c.AppsAdded = append(c.AppsAdded, a)
		case ok && a.Version != "" && b.Version != "" && a.Version != b.Version:
			c.AppsUpdated = append(c.AppsUpdated, AppUpdate{Name: a.Name, From: b.Version, To: a.Version})
		}
	}
	for _, b := range before.Inventory.Apps {
		if _, ok := aa[b.Name]; !ok && !unreadAfter[b.Source] {
			c.AppsRemoved = append(c.AppsRemoved, b)
		}
	}

	if hasDisks(before) && hasDisks(after) {
		c.Disks = diffDisks(before.Disks, after.Disks)
	} else {
		c.Skipped = append(c.Skipped, SectionDisk)
	}
	if len(before.Running) > 0 && len(after.Running) > 0 {
		c.Heavy = diffHeavy(before.Running, after.Running)
	} else {
		c.Skipped = append(c.Skipped, SectionHeavy)
	}

	byName := func(a, b collect.StartupItem) int { return compareFold(a.Name, b.Name) }
	slices.SortFunc(c.StartupAdded, byName)
	slices.SortFunc(c.StartupRemoved, byName)
	appByName := func(a, b collect.App) int { return compareFold(a.Name, b.Name) }
	slices.SortFunc(c.AppsAdded, appByName)
	slices.SortFunc(c.AppsRemoved, appByName)
	slices.SortFunc(c.AppsUpdated, func(a, b AppUpdate) int { return compareFold(a.Name, b.Name) })
	return c
}

func hasDisks(r history.Record) bool {
	_, failed := r.Errors["disk"]
	return !failed && len(r.Disks) > 0
}

// diffDisks lists volumes in both runs whose used space moved by at least
// DiskChangeNotable, in the order they're listed now.
func diffDisks(before, after []collect.Disk) []DiskChange {
	bm := index(before, func(d collect.Disk) string { return d.Mount })
	var out []DiskChange
	for _, a := range after {
		b, ok := bm[a.Mount]
		if !ok {
			continue
		}
		d := DiskChange{Mount: a.Mount, Before: b, After: a}
		if u := d.UsedChange(); u >= rules.DiskChangeNotable || -u >= rules.DiskChangeNotable {
			out = append(out, d)
		}
	}
	return out
}

// diffHeavy lists apps that are heavy now and at least doubled (or grew by
// HeavyAppGrowth) since before, biggest first.
func diffHeavy(before, after []history.AppUsage) []HeavyApp {
	bm := index(before, func(a history.AppUsage) string { return a.Name })
	var out []HeavyApp
	for _, a := range after {
		h := HeavyApp{Now: a}
		b, ok := bm[a.Name] // zero if it wasn't running
		if ok {
			h.Before = &b
		}
		h.Mem = a.RSS >= rules.HeavyAppMem &&
			(a.RSS >= 2*b.RSS || (a.RSS > b.RSS && a.RSS-b.RSS >= rules.HeavyAppGrowth))
		h.CPU = a.CPUPercent >= rules.HeavyAppCPU && a.CPUPercent >= 2*b.CPUPercent
		if h.Mem || h.CPU {
			out = append(out, h)
		}
	}
	slices.SortFunc(out, func(x, y HeavyApp) int {
		if c := cmp.Compare(y.Now.RSS, x.Now.RSS); c != 0 {
			return c
		}
		return cmp.Compare(x.Now.Name, y.Now.Name)
	})
	if len(out) > rules.ChangesTopN {
		out = out[:rules.ChangesTopN]
	}
	return out
}

func index[T any](xs []T, key func(T) string) map[string]T {
	m := make(map[string]T, len(xs))
	for _, x := range xs {
		if _, dup := m[key(x)]; !dup {
			m[key(x)] = x
		}
	}
	return m
}

func setOf(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func sortedKeys(ms ...map[string]bool) []string {
	var out []string
	for _, m := range ms {
		for k := range m {
			if !slices.Contains(out, k) {
				out = append(out, k)
			}
		}
	}
	slices.Sort(out)
	return out
}

func compareFold(a, b string) int {
	if c := strings.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}
