// Package live runs lagdoc's live views: top (apps by memory or CPU) and
// watch (the verdict, and when each check changed). The models are plain
// bubbletea models with the sampler injected, so they're tested without a
// terminal; render draws every frame.
package live

import (
	"cmp"
	"context"
	"errors"
	"io"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/render"
	"github.com/ag3ntsc4rn/lagdoc/internal/rules"
)

// SampleFunc takes the next snapshot (collect.Sampler.Next).
type SampleFunc func(context.Context) collect.Snapshot

type snapMsg collect.Snapshot

type tickMsg struct{}

// maxTransitions is how many changes watch remembers.
const maxTransitions = 200

func sampleCmd(ctx context.Context, sample SampleFunc) tea.Cmd {
	return func() tea.Msg { return snapMsg(sample(ctx)) }
}

func tick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return tickMsg{} })
}

func quitKey(k string) bool { return k == "q" || k == "ctrl+c" || k == "esc" }

// Top is lagdoc top's model.
type Top struct {
	ctx      context.Context
	sample   SampleFunc
	screen   render.Screen
	interval time.Duration

	report   render.Report
	apps     []rules.AppGroup
	sortBy   string
	offset   int
	width    int
	height   int
	measured bool
}

func NewTop(ctx context.Context, sample SampleFunc, screen render.Screen, interval time.Duration, sortBy string) Top {
	return Top{ctx: ctx, sample: sample, screen: screen, interval: interval, sortBy: sortBy, width: 80, height: 24}
}

func (m Top) Init() tea.Cmd { return sampleCmd(m.ctx, m.sample) }

func (m Top) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case snapMsg:
		m.report = render.NewReport(collect.Snapshot(msg))
		m.apps = SortApps(rules.GroupByApp(msg.Processes), m.sortBy)
		m.measured = true
		m.clamp()
		return m, tick(m.interval)
	case tickMsg:
		return m, sampleCmd(m.ctx, m.sample)
	case tea.WindowSizeMsg:
		if msg.Width > 0 && msg.Height > 0 { // some ptys report 0×0
			m.width, m.height = msg.Width, msg.Height
		}
		m.clamp()
	case tea.KeyMsg:
		rows := render.TopRows(m.height)
		switch k := msg.String(); {
		case quitKey(k):
			return m, tea.Quit
		case k == "m":
			m.sortBy, m.offset = render.SortMemory, 0
			m.apps = SortApps(m.apps, m.sortBy)
		case k == "c":
			m.sortBy, m.offset = render.SortCPU, 0
			m.apps = SortApps(m.apps, m.sortBy)
		case k == "up" || k == "k":
			m.offset--
		case k == "down" || k == "j":
			m.offset++
		case k == "pgup":
			m.offset -= rows
		case k == "pgdown" || k == " ":
			m.offset += rows
		case k == "home" || k == "g":
			m.offset = 0
		case k == "end" || k == "G":
			m.offset = len(m.apps)
		}
		m.clamp()
	}
	return m, nil
}

// clamp keeps the scroll offset within the list.
func (m *Top) clamp() {
	m.offset = max(0, min(m.offset, len(m.apps)-render.TopRows(m.height)))
}

func (m Top) View() string {
	return m.screen.Top(render.TopState{
		Report: m.report, Apps: m.apps, SortBy: m.sortBy, Offset: m.offset,
		Interval: m.interval, Width: m.width, Height: m.height, Measuring: !m.measured,
	})
}

// SortApps orders apps biggest first by memory or by CPU, with ties
// broken so rows don't jump around between refreshes.
func SortApps(apps []rules.AppGroup, by string) []rules.AppGroup {
	apps = slices.Clone(apps)
	slices.SortFunc(apps, func(a, b rules.AppGroup) int {
		if by == render.SortCPU {
			if c := cmp.Compare(b.CPUPercent, a.CPUPercent); c != 0 {
				return c
			}
		}
		if c := cmp.Compare(b.RSS, a.RSS); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return apps
}

// Watch is lagdoc watch's model.
type Watch struct {
	ctx      context.Context
	sample   SampleFunc
	screen   render.Screen
	interval time.Duration
	started  time.Time

	report      render.Report
	prev        []rules.Finding
	transitions []rules.Transition // newest first
	height      int
	count       int // stop after this many samples; 0: no limit
	samples     int
}

func NewWatch(ctx context.Context, sample SampleFunc, screen render.Screen, interval time.Duration, started time.Time, count int) Watch {
	return Watch{ctx: ctx, sample: sample, screen: screen, interval: interval, started: started, height: 24, count: count}
}

func (m Watch) Init() tea.Cmd { return sampleCmd(m.ctx, m.sample) }

func (m Watch) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case snapMsg:
		s := collect.Snapshot(msg)
		m.report = render.NewReport(s)
		trs := rules.Transitions(m.prev, m.report.Findings, s.TakenAt)
		slices.Reverse(trs)
		m.transitions = append(trs, m.transitions...)[:min(maxTransitions, len(trs)+len(m.transitions))]
		m.prev = m.report.Findings
		if m.samples++; m.samples == m.count {
			return m, tea.Quit
		}
		return m, tick(m.interval)
	case tickMsg:
		return m, sampleCmd(m.ctx, m.sample)
	case tea.WindowSizeMsg:
		if msg.Height > 0 {
			m.height = msg.Height
		}
	case tea.KeyMsg:
		if quitKey(msg.String()) {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Watch) View() string {
	return m.screen.Watch(render.WatchState{
		Report: m.report, Transitions: m.transitions, Started: m.started,
		Interval: m.interval, Height: m.height, Measuring: m.prev == nil,
	})
}

// Run shows a model full-screen until q, Ctrl-C or ctx is cancelled.
func Run(ctx context.Context, m tea.Model, in io.Reader, out io.Writer) error {
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(in), tea.WithOutput(out), tea.WithAltScreen())
	_, err := p.Run()
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	return err
}

// WatchLog is lagdoc watch without a terminal: it writes the state of each
// check once, then a line each time one changes, until ctx is cancelled or
// it has checked count times (0: no limit). With asJSON each line is a
// JSON object.
func WatchLog(ctx context.Context, w io.Writer, sample SampleFunc, interval time.Duration, count int, asJSON bool) error {
	var prev []rules.Finding
	for n := 1; ; n++ {
		s := sample(ctx)
		if ctx.Err() != nil {
			return nil
		}
		fs := rules.Evaluate(s)
		trs := rules.Transitions(prev, fs, s.TakenAt)
		prev = fs
		write := render.WatchLog
		if asJSON {
			write = render.WatchJSON
		}
		if err := write(w, trs); err != nil {
			return err
		}
		if n == count {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
}
