package render

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ag3ntsc4rn/lagdoc/internal/changes"
	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/history"
	"github.com/ag3ntsc4rn/lagdoc/internal/rules"
)

// ChangesSchemaVersion is `lagdoc changes --json`'s own contract, with the
// same rule as SchemaVersion.
const ChangesSchemaVersion = 1

// ChangesReport is what `lagdoc changes` prints.
type ChangesReport struct {
	TakenAt time.Time
	Host    collect.Host
	// Since is how old the baseline was asked to be.
	Since time.Duration
	// Baseline is when the record compared with was taken; nil if there
	// was none yet, in which case Changes is empty.
	Baseline *time.Time
	Changes  changes.Changes
	// Saved says this run was recorded for next time.
	Saved bool
}

// ChangesText writes the comparison for a person. Added startup items,
// newly heavy apps and disks filling up are amber: they're the changes
// most likely to explain a slowdown.
func ChangesText(w io.Writer, r ChangesReport, color bool) error {
	t := textStyles(w, color)
	var b strings.Builder
	c := r.Changes

	fmt.Fprintf(&b, "%s · %s", t.paint(t.brand, "lagdoc changes"), shortHost(r.Host.Hostname))
	if r.Baseline == nil {
		b.WriteString("\n\nNothing to compare with yet.")
		if r.Saved {
			b.WriteString(" This run has been saved; run lagdoc changes\nagain later to see what's different.\n")
		} else {
			b.WriteString(" lagdoc keeps a record each time it runs\n(unless --no-save); run it again later.\n")
		}
		_, err := io.WriteString(w, indent(b.String(), "  "))
		return err
	}
	base := r.Baseline.In(r.TakenAt.Location())
	age := r.TakenAt.Sub(base)
	fmt.Fprintf(&b, " · since %s (%s", when(base, r.TakenAt), ago(age))
	if age < r.Since {
		b.WriteString(", the oldest record")
	}
	b.WriteString(")\n\n")

	if n := c.Count(); n == 0 {
		b.WriteString(t.paint(t.plain.Bold(true), "No changes found") + "\n")
	} else {
		b.WriteString(t.paint(t.plain.Bold(true), plural(n, "change")) + "\n")
	}

	warn := t.paint(t.warn, "+")
	section := func(title string, lines []string) {
		if len(lines) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s\n", t.paint(t.plain.Bold(true), title))
		for _, l := range lines {
			fmt.Fprintf(&b, "  %s\n", l)
		}
	}

	var lines []string
	for _, s := range c.StartupAdded {
		lines = append(lines, fmt.Sprintf("%s %s (%s)", warn, s.Name, s.Source))
	}
	for _, s := range c.StartupRemoved {
		lines = append(lines, fmt.Sprintf("− %s (%s)", s.Name, s.Source))
	}
	section(changes.SectionStartup, lines)

	lines = nil
	for _, a := range c.AppsAdded {
		lines = append(lines, strings.TrimSpace("+ "+a.Name+" "+a.Version))
	}
	for _, u := range c.AppsUpdated {
		lines = append(lines, fmt.Sprintf("↑ %s %s → %s", u.Name, u.From, u.To))
	}
	for _, a := range c.AppsRemoved {
		lines = append(lines, "− "+a.Name)
	}
	section(changes.SectionApps, lines)

	lines = nil
	for _, d := range c.Disks {
		u := d.UsedChange()
		free := fmt.Sprintf("%s free of %s", rules.DiskSize(d.After.Free), rules.DiskSize(d.After.Total))
		if u > 0 {
			lines = append(lines, fmt.Sprintf("%s %s %s more used, %s", t.paint(t.warn, "↑"), d.Mount, rules.DiskSize(uint64(u)), free))
		} else {
			lines = append(lines, fmt.Sprintf("↓ %s %s freed, %s", d.Mount, rules.DiskSize(uint64(-u)), free))
		}
	}
	section(changes.SectionDisk, lines)

	lines = nil
	for _, h := range c.Heavy {
		label := h.Now.Name
		if h.Now.Processes > 1 {
			label = fmt.Sprintf("%s (%d processes)", h.Now.Name, h.Now.Processes)
		}
		now := usage(h.Now, h.Mem, h.CPU)
		if h.Before == nil {
			lines = append(lines, fmt.Sprintf("%s %s %s, not running then", warn, label, now))
		} else {
			lines = append(lines, fmt.Sprintf("%s %s %s, was %s", t.paint(t.warn, "↑"), label, now, usage(*h.Before, h.Mem, h.CPU)))
		}
	}
	section(changes.SectionHeavy, lines)

	if len(c.Unread) > 0 || len(c.Skipped) > 0 {
		b.WriteString("\n")
	}
	for _, u := range c.Unread {
		fmt.Fprintf(&b, "%s couldn't read %s\n", symbols[rules.Unknown], u)
	}
	for _, s := range c.Skipped {
		fmt.Fprintf(&b, "%s %s: no data to compare\n", symbols[rules.Unknown], s)
	}

	_, err := io.WriteString(w, indent(b.String(), "  "))
	return err
}

func usage(u history.AppUsage, mem, cpu bool) string {
	var parts []string
	if mem {
		parts = append(parts, rules.MemSize(u.RSS))
	}
	if cpu {
		parts = append(parts, fmt.Sprintf("%.0f%% CPU", u.CPUPercent))
	}
	return strings.Join(parts, " and ")
}

// when is a short local time: "today 14:03", "yesterday 09:12" or
// "Mon 12 Oct 14:03".
func when(t, now time.Time) string {
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	days := int(time.Date(y2, m2, d2, 0, 0, 0, 0, time.UTC).Sub(time.Date(y1, m1, d1, 0, 0, 0, 0, time.UTC)).Hours() / 24)
	switch days {
	case 0:
		return "today " + t.Format("15:04")
	case 1:
		return "yesterday " + t.Format("15:04")
	}
	if y1 != y2 {
		return t.Format("Mon 2 Jan 2006 15:04")
	}
	return t.Format("Mon 2 Jan 15:04")
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%d days ago", int(d.Hours()/24))
}

type jsonChanges struct {
	SchemaVersion int        `json:"schema_version"`
	TakenAt       time.Time  `json:"taken_at"`
	Since         *time.Time `json:"since"` // when the baseline was taken; null if there was none
	Host          jsonHost   `json:"host"`
	Changes       int        `json:"changes"`
	StartupItems  struct {
		Added   []collect.StartupItem `json:"added"`
		Removed []collect.StartupItem `json:"removed"`
	} `json:"startup_items"`
	Apps struct {
		Added   []collect.App       `json:"added"`
		Removed []collect.App       `json:"removed"`
		Updated []changes.AppUpdate `json:"updated"`
	} `json:"apps"`
	Disks     []jsonDiskChange `json:"disks"`
	HeavyApps []jsonHeavyApp   `json:"heavy_apps"`
	Unread    []string         `json:"unread"`
	Skipped   []string         `json:"skipped"`
	Saved     bool             `json:"saved"`
}

type jsonDiskChange struct {
	Mount     string `json:"mount"`
	UsedDelta int64  `json:"used_change_bytes"` // negative when space was freed
	Free      uint64 `json:"free_bytes"`
	Total     uint64 `json:"total_bytes"`
}

type jsonUsage struct {
	RSS        uint64  `json:"rss_bytes"`
	CPUPercent float64 `json:"cpu_percent"`
}

type jsonHeavyApp struct {
	Name      string     `json:"name"`
	Processes int        `json:"processes"`
	Now       jsonUsage  `json:"now"`
	Before    *jsonUsage `json:"before"`  // null if it wasn't running
	Reasons   []string   `json:"reasons"` // "memory", "cpu"
}

// ChangesJSON writes the comparison as JSON. Every list is an array, never
// null, and lists keep the text output's order.
func ChangesJSON(w io.Writer, r ChangesReport) error {
	c := r.Changes
	out := jsonChanges{
		SchemaVersion: ChangesSchemaVersion,
		TakenAt:       r.TakenAt.UTC().Truncate(time.Second),
		Host:          hostJSON(r.Host),
		Changes:       c.Count(),
		Disks:         []jsonDiskChange{},
		HeavyApps:     []jsonHeavyApp{},
		Unread:        orEmpty(c.Unread),
		Skipped:       orEmpty(c.Skipped),
		Saved:         r.Saved,
	}
	if r.Baseline != nil {
		b := r.Baseline.UTC().Truncate(time.Second)
		out.Since = &b
	}
	out.StartupItems.Added = orEmpty(c.StartupAdded)
	out.StartupItems.Removed = orEmpty(c.StartupRemoved)
	out.Apps.Added = orEmpty(c.AppsAdded)
	out.Apps.Removed = orEmpty(c.AppsRemoved)
	out.Apps.Updated = orEmpty(c.AppsUpdated)
	for _, d := range c.Disks {
		out.Disks = append(out.Disks, jsonDiskChange{
			Mount: d.Mount, UsedDelta: d.UsedChange(), Free: d.After.Free, Total: d.After.Total,
		})
	}
	for _, h := range c.Heavy {
		j := jsonHeavyApp{
			Name: h.Now.Name, Processes: h.Now.Processes,
			Now:     jsonUsage{h.Now.RSS, h.Now.CPUPercent},
			Reasons: []string{},
		}
		if h.Before != nil {
			j.Before = &jsonUsage{h.Before.RSS, h.Before.CPUPercent}
		}
		if h.Mem {
			j.Reasons = append(j.Reasons, "memory")
		}
		if h.CPU {
			j.Reasons = append(j.Reasons, "cpu")
		}
		out.HeavyApps = append(out.HeavyApps, j)
	}
	return writeJSON(w, out)
}

func orEmpty[T any](xs []T) []T {
	if xs == nil {
		return []T{}
	}
	return xs
}
