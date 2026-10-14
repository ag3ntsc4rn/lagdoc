package render

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/fix"
	"github.com/ag3ntsc4rn/lagdoc/internal/rules"
)

// FixSchemaVersion is `lagdoc fix --json`'s contract, with the same rule
// as SchemaVersion.
const FixSchemaVersion = 1

type FixReport struct {
	TakenAt  time.Time
	Host     collect.Host
	Findings []rules.Finding
	Fixes    []fix.Fix
}

// Runnable lists the fixes lagdoc can run, in the order they're numbered.
func (r FixReport) Runnable() []fix.Fix {
	var out []fix.Fix
	for _, f := range r.Fixes {
		if !f.Manual {
			out = append(out, f)
		}
	}
	return out
}

// FixPreview lists every fix under the problem it addresses. Fixes lagdoc
// can run are numbered; the ones for the person to do are bulleted.
func FixPreview(w io.Writer, r FixReport, color bool) error {
	t := textStyles(w, color)
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s\n\n", t.paint(t.brand, "lagdoc fix"), shortHost(r.Host.Hostname))

	runnable := len(r.Runnable())
	manual := len(r.Fixes) - runnable
	switch {
	case rules.Causes(r.Findings) == 0:
		b.WriteString(t.paint(t.sev(rules.OK).Bold(true), "OK") + "  nothing to fix, no problems found\n")
		_, err := io.WriteString(w, indent(b.String(), "  "))
		return err
	case len(r.Fixes) == 0:
		b.WriteString("No safe fix for these problems. lagdoc only quits apps and clears\ntool caches, and none of those would help here.\n")
	default:
		var parts []string
		if runnable > 0 {
			parts = append(parts, plural(runnable, "fix")+" lagdoc can run")
		}
		if manual > 0 {
			parts = append(parts, fmt.Sprintf("%d for you to do", manual))
		}
		b.WriteString(t.paint(t.plain.Bold(true), strings.Join(parts, " · ")) + "\n")
	}

	n := 0
	for _, f := range r.Findings {
		if f.Severity < rules.Warn {
			continue
		}
		st := t.sev(f.Severity)
		fmt.Fprintf(&b, "\n%s %s · %s\n", t.paint(st, symbols[f.Severity]), f.Check, f.Summary)
		for _, x := range r.Fixes {
			if x.Check != f.Check {
				continue
			}
			if x.Manual {
				fmt.Fprintf(&b, "  · %s\n", x.Title)
			} else {
				n++
				fmt.Fprintf(&b, "  %s %s\n", t.paint(t.command, fmt.Sprintf("%d", n)), x.Title)
			}
			if len(x.Command) > 0 {
				fmt.Fprintf(&b, "    runs %s\n", t.paint(t.command, fix.CommandLine(x.Command)))
			}
			for _, s := range x.Steps {
				fmt.Fprintf(&b, "    └ %s\n", s)
			}
			if x.Note != "" {
				fmt.Fprintf(&b, "    %s\n", x.Note)
			}
		}
	}
	_, err := io.WriteString(w, indent(b.String(), "  "))
	return err
}

// FixPrompt is the question asked before running the n-th fix.
func FixPrompt(n int, f fix.Fix) string {
	return fmt.Sprintf("  Run %d, %s? [y/N/q] ", n, firstLower(action(f)))
}

// FixResult is one line saying how a fix went.
func FixResult(w io.Writer, f fix.Fix, res fix.Result, color bool) error {
	t := textStyles(w, color)
	sev := map[fix.Status]rules.Severity{fix.Done: rules.OK, fix.Pending: rules.Warn, fix.Failed: rules.Crit}[res.Status]
	_, err := fmt.Fprintf(w, "  %s %s: %s\n", t.paint(t.sev(sev), symbols[sev]), action(f), res.Message)
	return err
}

func action(f fix.Fix) string {
	if f.Action != "" {
		return f.Action
	}
	return f.Title
}

func firstLower(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

type jsonFixReport struct {
	SchemaVersion int       `json:"schema_version"`
	TakenAt       time.Time `json:"taken_at"`
	Host          jsonHost  `json:"host"`
	Verdict       string    `json:"verdict"`
	Fixes         []jsonFix `json:"fixes"`
}

type jsonFix struct {
	ID      string   `json:"id"`
	Check   string   `json:"check"`
	Title   string   `json:"title"`
	Note    string   `json:"note"`
	Manual  bool     `json:"manual"`  // true: shown for the person to do, never run by lagdoc
	Command string   `json:"command"` // what lagdoc would run; "" for quits and manual fixes
	Steps   []string `json:"steps"`
	Frees   uint64   `json:"frees_bytes"` // estimate; 0 if unknown or not a disk fix
	Mount   string   `json:"mount"`
}

// FixJSON lists the fixes. It never runs anything.
func FixJSON(w io.Writer, r FixReport) error {
	out := jsonFixReport{
		SchemaVersion: FixSchemaVersion,
		TakenAt:       r.TakenAt.UTC().Truncate(time.Second),
		Host:          hostJSON(r.Host),
		Verdict:       rules.Verdict(r.Findings),
		Fixes:         []jsonFix{},
	}
	for _, f := range r.Fixes {
		out.Fixes = append(out.Fixes, jsonFix{
			ID: f.ID, Check: f.Check, Title: f.Title, Note: f.Note, Manual: f.Manual,
			Command: fix.CommandLine(f.Command), Steps: orEmpty(f.Steps), Frees: f.Frees, Mount: f.Mount,
		})
	}
	return writeJSON(w, out)
}
