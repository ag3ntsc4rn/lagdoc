package render

import (
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ag3ntsc4rn/lagdoc/internal/rules"
)

// Palette: severity colours only, plus purple for the brand and blue for
// commands. Every status also has a symbol, so nothing depends on colour.
var (
	colorCrit    = lipgloss.Color("#FF5F5F")
	colorWarn    = lipgloss.Color("#FFAF00")
	colorOK      = lipgloss.Color("#5FD787")
	colorBrand   = lipgloss.Color("#AF87FF")
	colorCommand = lipgloss.Color("#5FAFFF")
)

var symbols = map[rules.Severity]string{
	rules.Crit: "✖", rules.Warn: "▲", rules.OK: "✓", rules.Unknown: "?",
}

const barWidth = 10

// Text writes the verdict for a person. With color false it writes plain
// text and no escape codes at all.
func Text(w io.Writer, r Report, color bool) error {
	t := textStyles(w, color)
	var b strings.Builder

	fmt.Fprintf(&b, "%s · %s · %s · up %s\n\n",
		t.paint(t.brand, "lagdoc"), shortHost(r.Host.Hostname), r.Host.OSName, uptime(r.Host.Uptime))

	causes := rules.Causes(r.Findings)
	switch rules.Verdict(r.Findings) {
	case "slow":
		fmt.Fprintf(&b, "%s  %s found\n\n", t.paint(t.sev(rules.Crit).Bold(true), "SLOW"), plural(causes, "cause"))
	case "warn":
		fmt.Fprintf(&b, "%s  %s to watch\n\n", t.paint(t.sev(rules.Warn).Bold(true), "WARN"), plural(causes, "thing"))
	case "ok":
		fmt.Fprintf(&b, "%s  no problems found\n\n", t.paint(t.sev(rules.OK).Bold(true), "OK"))
	default:
		fmt.Fprintf(&b, "%s  nothing could be measured\n\n", t.paint(t.plain.Bold(true), "UNKNOWN"))
	}

	for _, f := range r.Findings {
		st := t.sev(f.Severity)
		line := fmt.Sprintf("%s %-9s ", t.paint(st, symbols[f.Severity]), f.Check)
		if f.Percent != nil {
			p := *f.Percent
			line += fmt.Sprintf("%s %3.0f%%  ", t.paint(st, bar(p)), p)
		}
		b.WriteString(strings.TrimRight(line+f.Summary, " ") + "\n")
		// Details explain problems. For passing checks they'd be noise.
		if f.Severity != rules.OK {
			for _, d := range f.Details {
				fmt.Fprintf(&b, "  └ %s\n", d)
			}
		}
	}

	if causes > 0 {
		fmt.Fprintf(&b, "\n→ run %s to preview %s\n", t.paint(t.command, "lagdoc fix"), plural(causes, "fix"))
	}

	_, err := io.WriteString(w, indent(b.String(), "  "))
	return err
}

type styles struct {
	color                 bool
	plain, brand, command lipgloss.Style
	crit, warn, ok        lipgloss.Style
}

func textStyles(w io.Writer, color bool) styles {
	s := styles{color: color}
	if !color {
		return s
	}
	r := lipgloss.NewRenderer(w)
	s.plain = r.NewStyle()
	s.brand = r.NewStyle().Foreground(colorBrand).Bold(true)
	s.command = r.NewStyle().Foreground(colorCommand)
	s.crit = r.NewStyle().Foreground(colorCrit)
	s.warn = r.NewStyle().Foreground(colorWarn)
	s.ok = r.NewStyle().Foreground(colorOK)
	return s
}

// paint is the only place text gets styled, so colour off means no escape
// codes anywhere.
func (s styles) paint(st lipgloss.Style, text string) string {
	if !s.color {
		return text
	}
	return st.Render(text)
}

func (s styles) sev(v rules.Severity) lipgloss.Style {
	switch v {
	case rules.Crit:
		return s.crit
	case rules.Warn:
		return s.warn
	case rules.OK:
		return s.ok
	}
	return s.plain
}

func bar(p float64) string {
	n := int(math.Round(math.Max(0, math.Min(100, p)) / 100 * barWidth))
	return strings.Repeat("█", n) + strings.Repeat("░", barWidth-n)
}

func uptime(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

// shortHost drops the ".local" macOS adds to Bonjour names.
func shortHost(h string) string {
	if h == "" {
		return "unknown host"
	}
	return strings.TrimSuffix(h, ".local")
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	if strings.HasSuffix(word, "x") {
		return fmt.Sprintf("%d %ses", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func indent(s, pad string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = pad + l
		}
	}
	return "\n" + strings.Join(lines, "\n") + "\n\n"
}
