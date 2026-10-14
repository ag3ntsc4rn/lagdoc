// Package cmd wires lagdoc's commands together.
package cmd

import (
	"context"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/render"
)

// Set by goreleaser through -ldflags.
var (
	version = "dev"
	commit  = "none"
)

func newRootCmd() *cobra.Command {
	var asJSON, noColor bool
	root := &cobra.Command{
		Use:   "lagdoc",
		Short: "Why is my machine slow right now?",
		Long: "lagdoc checks memory, disk, CPU and network, and tells you in plain\n" +
			"words what is slowing this machine down, worst cause first.",
		Version:       version + " (" + commit + ")",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: false,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r := render.NewReport(collect.Collect(cmd.Context()))
			out := cmd.OutOrStdout()
			if asJSON {
				return render.JSON(out, r)
			}
			return render.Text(out, r, useColor(out, noColor))
		},
	}
	root.Flags().BoolVar(&asJSON, "json", false, "print findings as JSON")
	root.Flags().BoolVar(&noColor, "no-color", false, "don't use colour (also off when NO_COLOR is set or output isn't a terminal)")
	root.AddCommand(newFixCmd())
	return root
}

// useColor follows https://no-color.org: any non-empty NO_COLOR turns it off.
func useColor(w io.Writer, noColor bool) bool {
	if noColor || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func Execute() int {
	if err := newRootCmd().ExecuteContext(context.Background()); err != nil {
		return 1
	}
	return 0
}
