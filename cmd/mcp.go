package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/fix"
	"github.com/ag3ntsc4rn/lagdoc/internal/history"
	"github.com/ag3ntsc4rn/lagdoc/internal/mcp"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve lagdoc's checks to AI agents (Model Context Protocol over stdio)",
		Long: "lagdoc mcp is an MCP server for AI agents and editors. Configure your MCP\n" +
			"client to run `lagdoc mcp`; it reads requests on stdin and answers on stdout.\n" +
			"Tools: check, top_apps, changes and fixes. They only read: nothing is changed\n" +
			"or saved, and fixes are only listed (apply them with lagdoc fix).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := history.Dir()
			if err != nil {
				return fmt.Errorf("can't find lagdoc's history: %w", err)
			}
			store := history.Store{Dir: dir}
			v, _ := resolveVersion()
			s := &mcp.Server{
				Name: "lagdoc", Version: v, Instructions: mcp.Instructions,
				Tools: mcp.Tools(mcp.Deps{
					Collect: func(ctx context.Context, network bool) collect.Snapshot {
						if network {
							return collect.Collect(ctx)
						}
						return (&collect.Sampler{}).Next(ctx)
					},
					Inventory: collect.ReadInventory,
					Baseline: func(now time.Time, since time.Duration) (history.Record, bool, error) {
						return store.Baseline(now, since)
					},
					FixEnv: fix.System{},
				}),
			}
			return s.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}
