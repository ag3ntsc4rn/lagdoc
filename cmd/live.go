package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/live"
	"github.com/ag3ntsc4rn/lagdoc/internal/render"
	"github.com/ag3ntsc4rn/lagdoc/internal/rules"
)

const minInterval = time.Second

// topPrintRows is how many apps lagdoc top prints when it isn't in a terminal.
const topPrintRows = 25

func newTopCmd() *cobra.Command {
	var noColor bool
	var interval time.Duration
	var sortBy string
	c := &cobra.Command{
		Use:   "top",
		Short: "Live view of the apps using the most memory or CPU",
		Long: "lagdoc top lists apps, with each app's helper processes counted together,\n" +
			"by memory or CPU, refreshing as you watch. Keys: m and c sort, ↑ ↓ scroll,\n" +
			"q quits. Without a terminal it prints the list once. It doesn't use the\n" +
			"network.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if interval < minInterval {
				return fmt.Errorf("--interval must be at least %s", minInterval)
			}
			if sortBy != render.SortMemory && sortBy != render.SortCPU {
				return fmt.Errorf("--sort must be %s or %s", render.SortMemory, render.SortCPU)
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			sampler := &collect.Sampler{} // no network probe
			out := cmd.OutOrStdout()
			color := useColor(out, noColor)
			if !isTerminal(cmd.InOrStdin()) || !isTerminal(out) {
				s := sampler.Next(ctx)
				apps := live.SortApps(rules.GroupByApp(s.Processes), sortBy)
				frame := render.NewScreen(out, color).Top(render.TopState{
					Report: render.NewReport(s), Apps: apps[:min(len(apps), topPrintRows)],
					SortBy: sortBy, Interval: interval,
				})
				_, err := fmt.Fprintln(out, frame)
				return err
			}
			m := live.NewTop(ctx, sampler.Next, render.NewScreen(out, color), interval, sortBy)
			return live.Run(ctx, m, cmd.InOrStdin(), out)
		},
	}
	c.Flags().DurationVar(&interval, "interval", 2*time.Second, "how often to refresh")
	c.Flags().StringVar(&sortBy, "sort", render.SortMemory, "sort by memory or cpu")
	c.Flags().BoolVar(&noColor, "no-color", false, "don't use colour")
	return c
}

// watchNetworkEvery is how often lagdoc watch probes the network: often
// enough to catch an outage, rarely enough not to add traffic of its own.
const watchNetworkEvery = 30 * time.Second

func newWatchCmd() *cobra.Command {
	var asJSON, noColor bool
	var interval time.Duration
	var count int
	c := &cobra.Command{
		Use:   "watch",
		Short: "Keep checking, and show when each check changed",
		Long: "lagdoc watch re-runs the checks every few seconds and keeps a list of\n" +
			"when each one changed (\"14:05 disk ok → warn\"), so you can see when the\n" +
			"machine got slow. q quits. Without a terminal it writes one line per\n" +
			"change, for a log file; --json makes each line a JSON object. The\n" +
			"network is probed every 30 seconds.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if interval < minInterval {
				return fmt.Errorf("--interval must be at least %s", minInterval)
			}
			if count < 0 {
				return fmt.Errorf("--count can't be negative")
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			sampler := &collect.Sampler{NetworkEvery: watchNetworkEvery}
			out := cmd.OutOrStdout()
			if asJSON || !isTerminal(cmd.InOrStdin()) || !isTerminal(out) {
				return live.WatchLog(ctx, out, sampler.Next, interval, count, asJSON)
			}
			m := live.NewWatch(ctx, sampler.Next, render.NewScreen(out, useColor(out, noColor)), interval, time.Now(), count)
			return live.Run(ctx, m, cmd.InOrStdin(), out)
		},
	}
	c.Flags().DurationVar(&interval, "interval", 5*time.Second, "how often to check")
	c.Flags().IntVar(&count, "count", 0, "stop after this many checks (0: until stopped)")
	c.Flags().BoolVar(&asJSON, "json", false, "write each change as a line of JSON")
	c.Flags().BoolVar(&noColor, "no-color", false, "don't use colour")
	return c
}
