package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ag3ntsc4rn/lagdoc/internal/changes"
	"github.com/ag3ntsc4rn/lagdoc/internal/history"
	"github.com/ag3ntsc4rn/lagdoc/internal/render"
)

func newChangesCmd(noSave *bool) *cobra.Command {
	var asJSON, noColor bool
	var since string
	c := &cobra.Command{
		Use:   "changes",
		Short: "Show what changed since an earlier run",
		Long: "lagdoc keeps a small record each time it runs. lagdoc changes compares\n" +
			"this machine now with an earlier record: startup items and apps added\n" +
			"or removed, apps updated, disks filling up and apps that got heavy.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			d, err := history.ParseSince(since)
			if err != nil {
				return err
			}
			dir, err := history.Dir()
			if err != nil {
				return fmt.Errorf("can't find a place to keep history: %w", err)
			}
			store := history.Store{Dir: dir}

			snap, inv := collectAll(cmd.Context())
			now := history.NewRecord(snap, inv)
			r := render.ChangesReport{TakenAt: snap.TakenAt, Host: snap.Host, Since: d}
			base, ok, err := store.Baseline(snap.TakenAt, d)
			if err != nil {
				return fmt.Errorf("can't read history in %s: %w", dir, err)
			}
			if ok {
				r.Baseline = &base.TakenAt
				r.Changes = changes.Diff(base, now)
			}
			if !*noSave {
				r.Saved = save(cmd, store, now)
			}

			out := cmd.OutOrStdout()
			if asJSON {
				return render.ChangesJSON(out, r)
			}
			return render.ChangesText(out, r, useColor(out, noColor))
		},
	}
	c.Flags().StringVar(&since, "since", "24h",
		"compare with the newest record at least this old, e.g. 2h, 3d (the oldest if none is)")
	c.Flags().BoolVar(&asJSON, "json", false, "print changes as JSON")
	c.Flags().BoolVar(&noColor, "no-color", false, "don't use colour")
	return c
}
