// Package cmd wires lagdoc's commands together.
package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/history"
	"github.com/ag3ntsc4rn/lagdoc/internal/render"
)

// Set by goreleaser through -ldflags.
var (
	version = "dev"
	commit  = "none"
)

func newRootCmd() *cobra.Command {
	var asJSON, noColor, noSave bool
	root := &cobra.Command{
		Use:   "lagdoc",
		Short: "Why is my machine slow right now?",
		Long: "lagdoc checks memory, disk, CPU and network, and tells you in plain\n" +
			"words what is slowing this machine down, worst cause first.",
		Version:       versionString(),
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: false,
		RunE: func(cmd *cobra.Command, _ []string) error {
			snap, inv := collectAll(cmd.Context())
			if !noSave {
				dir, err := history.Dir()
				if err == nil {
					save(cmd, history.Store{Dir: dir}, history.NewRecord(snap, inv))
				}
			}
			r := render.NewReport(snap)
			out := cmd.OutOrStdout()
			if asJSON {
				return render.JSON(out, r)
			}
			return render.Text(out, r, useColor(out, noColor))
		},
	}
	root.Flags().BoolVar(&asJSON, "json", false, "print findings as JSON")
	root.Flags().BoolVar(&noColor, "no-color", false, "don't use colour (also off when NO_COLOR is set or output isn't a terminal)")
	root.PersistentFlags().BoolVar(&noSave, "no-save", false, "don't keep a record of this run for lagdoc changes")
	root.AddCommand(newChangesCmd(&noSave), newFixCmd())
	return root
}

// collectAll takes a snapshot and reads the inventory alongside the
// snapshot's one-second CPU sample, so it costs no extra time.
func collectAll(ctx context.Context) (collect.Snapshot, collect.Inventory) {
	ch := make(chan collect.Inventory, 1)
	go func() { ch <- collect.ReadInventory() }()
	snap := collect.Collect(ctx)
	return snap, <-ch
}

// save records a run for lagdoc changes. A failure is reported on stderr
// but never fails the command: history is a bonus, not the point.
func save(cmd *cobra.Command, s history.Store, r history.Record) bool {
	if err := s.Save(r); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "lagdoc: couldn't save this run to %s: %v\n", s.Dir, err)
		return false
	}
	return true
}

// useColor follows https://no-color.org: any non-empty NO_COLOR turns it off.
func useColor(w io.Writer, noColor bool) bool {
	if noColor || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return isTerminal(w)
}

func Execute() int {
	if err := newRootCmd().ExecuteContext(context.Background()); err != nil {
		return 1
	}
	return 0
}
