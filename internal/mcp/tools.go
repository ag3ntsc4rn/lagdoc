package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ag3ntsc4rn/lagdoc/internal/changes"
	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/fix"
	"github.com/ag3ntsc4rn/lagdoc/internal/history"
	"github.com/ag3ntsc4rn/lagdoc/internal/live"
	"github.com/ag3ntsc4rn/lagdoc/internal/render"
	"github.com/ag3ntsc4rn/lagdoc/internal/rules"
)

// Instructions is what the server tells the model about itself.
const Instructions = "lagdoc explains why this computer is slow. Start with `check`: it returns a " +
	"verdict (ok, warn, slow) and findings for memory, disk, CPU and network, worst first, in plain " +
	"words. `top_apps` lists the apps using the most memory or CPU, `changes` says what's different " +
	"since an earlier lagdoc run (new startup items, apps installed or updated, disks filling up, " +
	"apps that got heavy), and `fixes` lists what could help. Every tool only reads: none of them " +
	"changes the machine. To apply a fix, ask the person to run `lagdoc fix` in a terminal, which " +
	"previews each fix and asks before doing it."

// Deps is what the tools read the machine with, so they can be tested.
type Deps struct {
	// Collect takes one snapshot; with network false it doesn't probe the
	// network and the network finding is "not measured".
	Collect   func(ctx context.Context, network bool) collect.Snapshot
	Inventory func() collect.Inventory
	// Baseline returns the history record to compare with (history.Store.Baseline).
	Baseline func(now time.Time, since time.Duration) (history.Record, bool, error)
	FixEnv   fix.Env
}

var (
	yes = ptr(true)
	no  = ptr(false)
)

func ptr[T any](v T) *T { return &v }

// readOnly describes every lagdoc tool: nothing is changed, and calling
// again is harmless. openWorld is true when it probes the network.
func readOnly(title string, openWorld bool) Annotations {
	ow := no
	if openWorld {
		ow = yes
	}
	return Annotations{Title: title, ReadOnlyHint: yes, DestructiveHint: no, IdempotentHint: yes, OpenWorldHint: ow}
}

const networkArg = "Also check DNS and reachability (one lookup of cloudflare.com and one connection to 1.1.1.1:443). Default true."

// Tools are lagdoc's MCP tools. None of them saves history or changes
// anything.
func Tools(d Deps) []Tool {
	return []Tool{
		{
			Name:  "check",
			Title: "Why is this computer slow?",
			Description: "Checks memory, disk, CPU and network and returns a verdict (ok, warn, slow or unknown) " +
				"with findings ranked worst first. Each finding has a severity (crit, warn, ok, unknown), a " +
				"few plain words, and details naming the apps, volumes or errors behind it. Takes about a second.",
			InputSchema: object(map[string]any{
				"network": prop("boolean", networkArg),
			}),
			OutputSchema: reportSchema,
			Annotations:  readOnly("Check this computer", true),
			Call: func(ctx context.Context, args map[string]json.RawMessage) (any, error) {
				network, err := boolArg(args, "network", true)
				if err != nil {
					return nil, err
				}
				return render.ReportDoc(render.NewReport(d.Collect(ctx, network))), nil
			},
		},
		{
			Name:  "top_apps",
			Title: "Apps using the most memory or CPU",
			Description: "Lists running apps by memory or CPU, with each app's helper processes counted together " +
				"(Chrome's 30 processes are one line). Memory is resident memory in bytes; cpu_percent is of one " +
				"core over about a second, so 200 means two cores busy.",
			InputSchema: object(map[string]any{
				"sort":  map[string]any{"type": "string", "enum": []string{render.SortMemory, render.SortCPU}, "description": "memory (default) or cpu"},
				"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "description": "How many apps, 1 to 100. Default 10."},
			}),
			OutputSchema: topSchema,
			Annotations:  readOnly("Top apps", false),
			Call: func(ctx context.Context, args map[string]json.RawMessage) (any, error) {
				sortBy, err := stringArg(args, "sort", render.SortMemory)
				if err != nil {
					return nil, err
				}
				if sortBy != render.SortMemory && sortBy != render.SortCPU {
					return nil, fmt.Errorf("sort must be %q or %q", render.SortMemory, render.SortCPU)
				}
				limit, err := intArg(args, "limit", 10)
				if err != nil {
					return nil, err
				}
				if limit < 1 || limit > 100 {
					return nil, fmt.Errorf("limit must be between 1 and 100")
				}
				s := d.Collect(ctx, false)
				apps := live.SortApps(rules.GroupByApp(s.Processes), sortBy)
				return render.TopDoc(render.NewReport(s), apps[:min(limit, len(apps))], sortBy), nil
			},
		},
		{
			Name:  "changes",
			Title: "What changed since an earlier run",
			Description: "Compares this computer now with a record from an earlier lagdoc run: startup items added " +
				"or removed, apps installed, removed or updated, volumes whose used space moved by 5 GB or more, and " +
				"apps that got heavy. Records are saved by `lagdoc` and `lagdoc changes` run in a terminal, not by " +
				"this tool; `since` is null when there is no record yet.",
			InputSchema: object(map[string]any{
				"since": prop("string", "Compare with the newest record at least this old, e.g. 6h or 7d (the oldest if none is). Default 24h."),
			}),
			OutputSchema: changesSchema,
			Annotations:  readOnly("What changed", false),
			Call: func(ctx context.Context, args map[string]json.RawMessage) (any, error) {
				sinceArg, err := stringArg(args, "since", "24h")
				if err != nil {
					return nil, err
				}
				since, err := history.ParseSince(sinceArg)
				if err != nil {
					return nil, err
				}
				s := d.Collect(ctx, false)
				now := history.NewRecord(s, d.Inventory())
				r := render.ChangesReport{TakenAt: s.TakenAt, Host: s.Host, Since: since}
				base, ok, err := d.Baseline(s.TakenAt, since)
				if err != nil {
					return nil, fmt.Errorf("couldn't read lagdoc's history: %w", err)
				}
				if ok {
					r.Baseline = &base.TakenAt
					r.Changes = changes.Diff(base, now)
				}
				return render.ChangesDoc(r), nil
			},
		},
		{
			Name:  "fixes",
			Title: "What could fix the problems found",
			Description: "Runs the same checks as `check` and lists what could help with each problem: apps to quit, " +
				"tool caches to clear (with the space each frees) and steps for the person to do themselves. " +
				"Nothing is run. manual=true means lagdoc never runs it (it needs admin rights or touches the " +
				"person's files); the rest can be applied by the person with `lagdoc fix`, which asks before each one.",
			InputSchema: object(map[string]any{
				"network": prop("boolean", networkArg),
			}),
			OutputSchema: fixSchema,
			Annotations:  readOnly("List fixes", true),
			Call: func(ctx context.Context, args map[string]json.RawMessage) (any, error) {
				network, err := boolArg(args, "network", true)
				if err != nil {
					return nil, err
				}
				s := d.Collect(ctx, network)
				findings := rules.Evaluate(s)
				return render.FixDoc(render.FixReport{
					TakenAt: s.TakenAt, Host: s.Host, Findings: findings,
					Fixes: fix.Plan(ctx, s, findings, d.FixEnv),
				}), nil
			},
		},
	}
}

func boolArg(args map[string]json.RawMessage, name string, def bool) (bool, error) {
	raw, ok := args[name]
	if !ok || string(raw) == "null" {
		return def, nil
	}
	var v bool
	if json.Unmarshal(raw, &v) != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return v, nil
}

func stringArg(args map[string]json.RawMessage, name, def string) (string, error) {
	raw, ok := args[name]
	if !ok || string(raw) == "null" {
		return def, nil
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return "", fmt.Errorf("%s must be a string", name)
	}
	return v, nil
}

func intArg(args map[string]json.RawMessage, name string, def int) (int, error) {
	raw, ok := args[name]
	if !ok || string(raw) == "null" {
		return def, nil
	}
	var f float64
	if json.Unmarshal(raw, &f) != nil || f != float64(int(f)) {
		return 0, fmt.Errorf("%s must be a whole number", name)
	}
	return int(f), nil
}
