package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newFixCmd is a placeholder so the hint at the end of the verdict doesn't
// lead to "unknown command". Fixes arrive in milestone 3.
func newFixCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fix",
		Short: "Preview safe fixes for the problems found (coming soon)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(),
				"lagdoc fix isn't built yet. It will preview each fix and ask before changing anything.")
			return err
		},
	}
}
