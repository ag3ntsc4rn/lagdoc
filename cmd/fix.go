package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/fix"
	"github.com/ag3ntsc4rn/lagdoc/internal/render"
	"github.com/ag3ntsc4rn/lagdoc/internal/rules"
)

func newFixCmd() *cobra.Command {
	var asJSON, noColor, dryRun bool
	c := &cobra.Command{
		Use:   "fix",
		Short: "Preview safe fixes for the problems found, then choose which to run",
		Long: "lagdoc fix lists what could help with each problem, then asks before\n" +
			"running each one. It only asks apps to quit (they can still ask to save\n" +
			"your work) and runs tools' own cache cleanups. It never deletes your\n" +
			"files and never runs anything as administrator; those steps are listed\n" +
			"for you to do yourself.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			s := collect.Collect(ctx)
			findings := rules.Evaluate(s)
			r := render.FixReport{
				TakenAt: s.TakenAt, Host: s.Host, Findings: findings,
				Fixes: fix.Plan(ctx, s, findings, fix.System{}),
			}
			out := cmd.OutOrStdout()
			if asJSON {
				return render.FixJSON(out, r)
			}
			color := useColor(out, noColor)
			if err := render.FixPreview(out, r, color); err != nil {
				return err
			}
			runnable := r.Runnable()
			if len(runnable) == 0 {
				return nil
			}
			switch {
			case dryRun:
				_, err := fmt.Fprintln(out, "  Nothing was changed (--dry-run).")
				return err
			case !isTerminal(cmd.InOrStdin()) || !isTerminal(out):
				_, err := fmt.Fprintln(out, "  Nothing was changed. Run lagdoc fix in a terminal to choose fixes.")
				return err
			}
			return runChosen(cmd, runnable, color, fix.System{})
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "only show the fixes; don't offer to run them")
	c.Flags().BoolVar(&asJSON, "json", false, "list the fixes as JSON (never runs anything)")
	c.Flags().BoolVar(&noColor, "no-color", false, "don't use colour")
	return c
}

// runChosen asks about each fix in turn; the default is no, and q stops.
func runChosen(cmd *cobra.Command, fixes []fix.Fix, color bool, runner fix.Runner) error {
	out := cmd.OutOrStdout()
	in := bufio.NewReader(cmd.InOrStdin())
	failed := 0
	fmt.Fprintln(out)
	for i, f := range fixes {
		fmt.Fprint(out, render.FixPrompt(i+1, f))
		answer, err := in.ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
		case "q", "quit":
			return finish(out, failed)
		default:
			if err == io.EOF {
				fmt.Fprintln(out)
				return finish(out, failed)
			}
			continue
		}
		res := fix.Run(cmd.Context(), f, runner)
		if res.Status == fix.Failed {
			failed++
		}
		if err := render.FixResult(out, f, res, color); err != nil {
			return err
		}
	}
	return finish(out, failed)
}

func finish(out io.Writer, failed int) error {
	fmt.Fprintln(out)
	if failed > 0 {
		return fmt.Errorf("%d %s failed", failed, map[bool]string{true: "fix", false: "fixes"}[failed == 1])
	}
	return nil
}

func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
