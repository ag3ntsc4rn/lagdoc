package render

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ag3ntsc4rn/lagdoc/internal/rules"
)

// Screen draws the live views (lagdoc top and lagdoc watch) as whole
// frames. It uses the same palette and paint as the other output.
type Screen struct{ t styles }

func NewScreen(w io.Writer, color bool) Screen { return Screen{textStyles(w, color)} }

// Sort orders for lagdoc top.
const (
	SortMemory = "memory"
	SortCPU    = "cpu"
)

type TopState struct {
	Report    Report
	Apps      []rules.AppGroup // already sorted
	SortBy    string
	Offset    int // first app row shown
	Interval  time.Duration
	Width     int
	Height    int // 0: no limit (a one-off print)
	Measuring bool
}

const (
	colProcs  = 5
	colMemory = 8
	colCPU    = 6
	colGap    = 2
	minName   = 12
	maxName   = 40 // wider pushes the numbers away from the names
)

// TopRows is how many app rows fit on a screen this tall.
func TopRows(height int) int { return max(1, height-6) }

// Top draws lagdoc top: a summary line, then apps by memory or CPU.
func (s Screen) Top(st TopState) string {
	t := s.t
	var b strings.Builder
	b.WriteString(t.paint(t.brand, "lagdoc top"))
	if st.Report.Host.Hostname != "" {
		b.WriteString(" · " + shortHost(st.Report.Host.Hostname))
	}
	if st.Height > 0 {
		fmt.Fprintf(&b, " · every %s", st.Interval)
	}
	b.WriteString("\n")
	if st.Measuring {
		b.WriteString("\nmeasuring…\n")
		return frame(b.String())
	}
	b.WriteString(s.summary(st.Report.Findings) + "\n\n")

	width := st.Width
	if width <= 0 {
		width = 80
	}
	name := min(maxName, max(minName, width-2-colProcs-colMemory-colCPU-3*colGap))
	mark := func(col, label string) string {
		if st.SortBy == col {
			return label + "↓"
		}
		return label
	}
	header := pad("APP", name) + "  " + padLeft("PROCS", colProcs) + "  " +
		padLeft(mark(SortMemory, "MEMORY"), colMemory) + "  " + padLeft(mark(SortCPU, "CPU"), colCPU)
	b.WriteString(t.paint(t.plain.Bold(true), header) + "\n")

	rows := st.Apps[min(st.Offset, len(st.Apps)):]
	if st.Height > 0 {
		rows = rows[:min(len(rows), TopRows(st.Height))]
	}
	for _, g := range rows {
		fmt.Fprintf(&b, "%s  %*d  %*s  %*s\n", pad(truncate(g.Name, name), name),
			colProcs, g.Count, colMemory, rules.MemSize(g.RSS), colCPU, fmt.Sprintf("%.0f%%", g.CPUPercent))
	}
	if st.Height > 0 {
		for range TopRows(st.Height) - len(rows) {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "\n%s  %s\n", t.paint(t.command, "q quit · m memory · c cpu · ↑↓ scroll"), "CPU 100% = one core")
	}
	return frame(b.String())
}

// summary is one line of checks: "✓ memory 42%  ▲ disk 93%  ✓ cpu 9%".
// Checks that weren't measured are left out.
func (s Screen) summary(fs []rules.Finding) string {
	t := s.t
	var parts []string
	for _, f := range fs {
		if f.Severity == rules.Unknown {
			continue
		}
		p := f.Check
		if f.Percent != nil {
			p += fmt.Sprintf(" %.0f%%", *f.Percent)
		} else {
			p += " " + f.Summary
		}
		parts = append(parts, t.paint(t.sev(f.Severity), symbols[f.Severity])+" "+p)
	}
	return strings.Join(parts, "   ")
}

type WatchState struct {
	Report      Report
	Transitions []rules.Transition // newest first; Initial ones aren't shown
	Started     time.Time
	Interval    time.Duration
	Height      int
	Measuring   bool
}

// Watch draws lagdoc watch: the verdict as lagdoc prints it, then every
// time a check's severity changed.
func (s Screen) Watch(st WatchState) string {
	t := s.t
	var b strings.Builder
	b.WriteString(t.paint(t.brand, "lagdoc watch"))
	if st.Report.Host.Hostname != "" {
		b.WriteString(" · " + shortHost(st.Report.Host.Hostname))
	}
	fmt.Fprintf(&b, " · every %s · since %s\n\n", st.Interval, st.Started.Format("15:04"))
	if st.Measuring {
		b.WriteString("measuring…\n")
		return frame(b.String())
	}
	t.verdict(&b, st.Report.Findings)

	b.WriteString("\n" + t.paint(t.plain.Bold(true), "changes") + "\n")
	room := len(st.Transitions)
	if st.Height > 0 {
		used := strings.Count(b.String(), "\n") + 2 // and a blank line and the footer
		room = max(1, st.Height-used)
	}
	shown := 0
	for _, tr := range st.Transitions {
		if tr.Initial {
			continue
		}
		if shown == room {
			break
		}
		b.WriteString(s.transition(tr, "15:04:05") + "\n")
		shown++
	}
	if shown == 0 {
		fmt.Fprintf(&b, "none since %s\n", st.Started.Format("15:04"))
	}
	if st.Height > 0 {
		for range room - max(shown, 1) {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "\n%s\n", t.paint(t.command, "q quit"))
	}
	return frame(b.String())
}

func (s Screen) transition(tr rules.Transition, layout string) string {
	t := s.t
	sym := t.paint(t.sev(tr.To), symbols[tr.To])
	state := tr.To.String()
	if !tr.Initial {
		state = tr.From.String() + " → " + state
	}
	line := fmt.Sprintf("%s  %s %-8s %s", tr.At.Format(layout), sym, tr.Check, state)
	if tr.Summary != "" {
		line += " · " + tr.Summary
	}
	return line
}

// WatchLog writes transitions as plain lines with the date, for when
// lagdoc watch isn't in a terminal (a log file, a pipe).
func WatchLog(w io.Writer, trs []rules.Transition) error {
	s := NewScreen(w, false)
	for _, tr := range trs {
		if _, err := fmt.Fprintln(w, s.transition(tr, "2006-01-02 15:04:05")); err != nil {
			return err
		}
	}
	return nil
}

// WatchSchemaVersion is the contract for `lagdoc watch --json` lines.
const WatchSchemaVersion = 1

type jsonTransition struct {
	SchemaVersion int       `json:"schema_version"`
	Time          time.Time `json:"time"`
	Check         string    `json:"check"`
	From          *string   `json:"from"` // null on the first reading
	To            string    `json:"to"`
	Summary       string    `json:"summary"`
	Details       []string  `json:"details"`
}

// WatchJSON writes one JSON object per line per transition.
func WatchJSON(w io.Writer, trs []rules.Transition) error {
	enc := json.NewEncoder(w)
	for _, tr := range trs {
		j := jsonTransition{
			SchemaVersion: WatchSchemaVersion,
			Time:          tr.At.UTC().Truncate(time.Second),
			Check:         tr.Check, To: tr.To.String(), Summary: tr.Summary,
			Details: orEmpty(tr.Details),
		}
		if !tr.Initial {
			from := tr.From.String()
			j.From = &from
		}
		if err := enc.Encode(j); err != nil {
			return err
		}
	}
	return nil
}

// frame indents a screen by one column. Unlike indent it adds no blank
// lines, so a frame is exactly as tall as its content.
func frame(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = " " + l
		}
	}
	return strings.Join(lines, "\n")
}

// truncate shortens s to at most n columns, ending in "…" if cut.
func truncate(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > n {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// padLeft right-aligns s in n columns.
func padLeft(s string, n int) string {
	return strings.Repeat(" ", max(0, n-lipgloss.Width(s))) + s
}

// pad fills s with spaces to n columns (fmt's %-*s counts bytes, not columns).
func pad(s string, n int) string {
	return s + strings.Repeat(" ", max(0, n-lipgloss.Width(s)))
}

// TopSchemaVersion is the contract for the apps list (lagdoc mcp's
// top_apps tool).
const TopSchemaVersion = 1

type jsonTop struct {
	SchemaVersion int       `json:"schema_version"`
	TakenAt       time.Time `json:"taken_at"`
	Host          jsonHost  `json:"host"`
	Sort          string    `json:"sort"` // memory | cpu
	Apps          []jsonApp `json:"apps"`
}

type jsonApp struct {
	Name       string  `json:"name"`
	Processes  int     `json:"processes"`
	RSS        uint64  `json:"rss_bytes"`
	CPUPercent float64 `json:"cpu_percent"` // of one core: 200 = two cores busy
}

// TopDoc is apps (already sorted and cut to length) as JSON.
func TopDoc(r Report, apps []rules.AppGroup, sortBy string) any {
	out := jsonTop{
		SchemaVersion: TopSchemaVersion, TakenAt: r.TakenAt.UTC().Truncate(time.Second),
		Host: hostJSON(r.Host), Sort: sortBy, Apps: []jsonApp{},
	}
	for _, g := range apps {
		out.Apps = append(out.Apps, jsonApp{g.Name, g.Count, g.RSS, math.Round(g.CPUPercent*10) / 10})
	}
	return out
}
