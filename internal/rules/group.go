package rules

import (
	"cmp"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
)

// AppGroup is one app's processes added together.
type AppGroup struct {
	Name       string
	Count      int
	RSS        uint64
	CPUPercent float64
}

// helperSuffix matches Chromium/Electron helper names:
// "Google Chrome Helper (Renderer)", "Slack Helper (GPU)", "Code Helper".
var helperSuffix = regexp.MustCompile(` Helper( \([^)]*\))?$`)

// AppName is the app a process belongs to. On macOS that's the outermost
// .app bundle in its path, so "Google Chrome.app/…/Google Chrome Helper
// (Renderer).app/…" counts as Google Chrome. Elsewhere it's the executable's
// name without ".exe" or a helper suffix.
func AppName(p collect.Process) string {
	if i := strings.Index(p.Exe, ".app/"); i >= 0 {
		return filepath.Base(p.Exe[:i])
	}
	name := p.Name
	if strings.EqualFold(filepath.Ext(name), ".exe") {
		name = name[:len(name)-4]
	}
	if n := helperSuffix.ReplaceAllString(name, ""); n != "" {
		name = n
	}
	return name
}

// GroupByApp sums processes per app. RSS counts shared pages once per
// process, so a many-process app reads a little high (as in top or ps).
func GroupByApp(ps []collect.Process) []AppGroup {
	idx := map[string]int{}
	var gs []AppGroup
	for _, p := range ps {
		name := AppName(p)
		i, ok := idx[name]
		if !ok {
			i = len(gs)
			idx[name] = i
			gs = append(gs, AppGroup{Name: name})
		}
		gs[i].Count++
		gs[i].RSS += p.RSS
		gs[i].CPUPercent += p.CPUPercent
	}
	return gs
}

// topBy returns up to n groups whose metric is at least min, largest first.
// Ties break by name so output is stable.
func topBy[T cmp.Ordered](gs []AppGroup, metric func(AppGroup) T, min T, n int) []AppGroup {
	gs = slices.Clone(gs)
	slices.SortFunc(gs, func(a, b AppGroup) int {
		if c := cmp.Compare(metric(b), metric(a)); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
	var out []AppGroup
	for _, g := range gs {
		if len(out) == n || metric(g) < min {
			break
		}
		out = append(out, g)
	}
	return out
}

// Label is the name as findings show it: "Google Chrome (23 processes)".
func (g AppGroup) Label() string {
	if g.Count > 1 {
		return fmt.Sprintf("%s (%d processes)", g.Name, g.Count)
	}
	return g.Name
}

// MemSize formats RAM in binary units, labelled GB/MB as Activity Monitor,
// Task Manager and free -h all do.
func MemSize(b uint64) string {
	if b >= gib {
		return fmt.Sprintf("%.1f GB", float64(b)/gib)
	}
	return fmt.Sprintf("%d MB", b>>20)
}

// DiskSize formats storage in decimal units, as Finder, Explorer and drive
// labels do.
func DiskSize(b uint64) string {
	f := float64(b)
	switch {
	case f >= 1e12:
		return fmt.Sprintf("%.1f TB", f/1e12)
	case f >= 100*gb:
		return fmt.Sprintf("%.0f GB", f/gb)
	case f >= gb:
		return fmt.Sprintf("%.1f GB", f/gb)
	default:
		return fmt.Sprintf("%.0f MB", f/1e6)
	}
}
