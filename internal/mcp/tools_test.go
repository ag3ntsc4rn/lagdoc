package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/fix"
	"github.com/ag3ntsc4rn/lagdoc/internal/history"
)

// validate checks v (decoded JSON) against the subset of JSON Schema the
// output schemas use: type (one or a list), properties, required,
// additionalProperties false, items, enum and anyOf.
func validate(schema map[string]any, v any, path string) error {
	if alts, ok := schema["anyOf"].([]any); ok {
		for _, a := range alts {
			if validate(a.(map[string]any), v, path) == nil {
				return nil
			}
		}
		return fmt.Errorf("%s: matches none of anyOf", path)
	}
	if t, ok := schema["type"]; ok {
		var types []string
		switch t := t.(type) {
		case string:
			types = []string{t}
		case []string:
			types = t
		}
		if !slices.ContainsFunc(types, func(t string) bool { return isType(v, t) }) {
			return fmt.Errorf("%s: %v (%T) isn't %v", path, v, v, types)
		}
	}
	if e, ok := schema["enum"].([]string); ok && !slices.Contains(e, fmt.Sprint(v)) {
		return fmt.Errorf("%s: %v not in %v", path, v, e)
	}
	switch v := v.(type) {
	case map[string]any:
		props, _ := schema["properties"].(map[string]any)
		req, _ := schema["required"].([]string)
		for _, r := range req {
			if _, ok := v[r]; !ok {
				return fmt.Errorf("%s: missing %q", path, r)
			}
		}
		for k, val := range v {
			p, ok := props[k]
			if !ok {
				if schema["additionalProperties"] == false {
					return fmt.Errorf("%s: undocumented field %q", path, k)
				}
				continue
			}
			if err := validate(p.(map[string]any), val, path+"."+k); err != nil {
				return err
			}
		}
	case []any:
		if items, ok := schema["items"].(map[string]any); ok {
			for i, it := range v {
				if err := validate(items, it, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func isType(v any, t string) bool {
	switch t {
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "number":
		_, ok := v.(float64)
		return ok
	case "integer":
		f, ok := v.(float64)
		return ok && f == float64(int64(f))
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "null":
		return v == nil
	}
	return false
}

// roundTrip turns a tool's result into what a client decodes.
func roundTrip(t *testing.T, doc any) any {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

type noFixEnv struct{}

func (noFixEnv) OS() string                                      { return "linux" }
func (noFixEnv) Home() string                                    { return "/home/me" }
func (noFixEnv) CacheDir() string                                { return "/home/me/.cache" }
func (noFixEnv) Getenv(string) string                            { return "" }
func (noFixEnv) LookPath(t string) bool                          { return t == "go" }
func (noFixEnv) SystemTool(n string) string                      { return n }
func (noFixEnv) DirSize(context.Context, string) (uint64, bool)  { return 6e9, true }
func (noFixEnv) LargestFiles(string, int) []fix.File             { return nil }
func (noFixEnv) Mine(int32) bool                                 { return true }
func (noFixEnv) Ancestors() map[int32]bool                       { return nil }
func (noFixEnv) BundleID(string) string                          { return "" }
func (noFixEnv) DockerBuildCache(context.Context) (uint64, bool) { return 0, false }

var t0 = time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)

func fakeSnapshot(network bool) collect.Snapshot {
	s := collect.Snapshot{
		TakenAt: t0,
		Host:    collect.Host{Hostname: "studio", OS: "linux", OSName: "Ubuntu 26.04", CPUs: 8, Uptime: time.Hour},
		Memory:  collect.Memory{Total: 16 << 30, Available: 1 << 30},
		CPU:     collect.CPU{Percent: 20, Sample: time.Second},
		Disks:   []collect.Disk{{Mount: "/", Total: 250e9, Free: 5e9}},
		Processes: []collect.Process{
			{PID: 10, Name: "firefox", RSS: 6 << 30, CPUPercent: 30},
			{PID: 11, Name: "firefox", RSS: 2 << 30, CPUPercent: 5},
			{PID: 20, Name: "code", RSS: 1 << 30, CPUPercent: 80},
		},
	}
	if network {
		s.Network = collect.Network{DNSHost: "cloudflare.com", DNS: 9 * time.Millisecond, DialAddr: "1.1.1.1:443", Dial: 12 * time.Millisecond}
	}
	return s
}

func fakeDeps(baseline *history.Record) (Deps, *[]bool) {
	var networkCalls []bool
	return Deps{
		Collect: func(_ context.Context, network bool) collect.Snapshot {
			networkCalls = append(networkCalls, network)
			return fakeSnapshot(network)
		},
		Inventory: func() collect.Inventory {
			return collect.Inventory{Apps: []collect.App{{Name: "Firefox", Version: "157.0", Source: "/usr/share/applications"}}}
		},
		Baseline: func(time.Time, time.Duration) (history.Record, bool, error) {
			if baseline == nil {
				return history.Record{}, false, nil
			}
			return *baseline, true, nil
		},
		FixEnv: noFixEnv{},
	}, &networkCalls
}

func tool(ts []Tool, name string) Tool {
	return ts[slices.IndexFunc(ts, func(t Tool) bool { return t.Name == name })]
}

func call(t *testing.T, tl Tool, args string) any {
	t.Helper()
	var a map[string]json.RawMessage
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		t.Fatal(err)
	}
	if err := checkArgs(tl.InputSchema, a); err != nil {
		t.Fatal(err)
	}
	doc, err := tl.Call(context.Background(), a)
	if err != nil {
		t.Fatalf("%s(%s): %v", tl.Name, args, err)
	}
	v := roundTrip(t, doc)
	if err := validate(tl.OutputSchema, v, tl.Name); err != nil {
		t.Errorf("output doesn't match outputSchema: %v", err)
	}
	return v
}

func TestToolsMatchTheirSchemas(t *testing.T) {
	before := history.NewRecord(fakeSnapshot(false), collect.Inventory{Apps: []collect.App{{Name: "Firefox", Version: "156.0", Source: "/usr/share/applications"}}})
	before.TakenAt = t0.Add(-48 * time.Hour)
	before.Running[0].RSS = 1 << 30 // firefox grew
	d, network := fakeDeps(&before)
	ts := Tools(d)

	check := call(t, tool(ts, "check"), `{}`).(map[string]any)
	if check["verdict"] != "slow" {
		t.Errorf("check: %v", check["verdict"])
	}
	call(t, tool(ts, "check"), `{"network": false}`)

	top := call(t, tool(ts, "top_apps"), `{"sort":"cpu","limit":1}`).(map[string]any)
	apps := top["apps"].([]any)
	if len(apps) != 1 || apps[0].(map[string]any)["name"] != "code" {
		t.Errorf("top_apps: %v", apps)
	}

	ch := call(t, tool(ts, "changes"), `{"since":"1d"}`).(map[string]any)
	updated := ch["apps"].(map[string]any)["updated"].([]any)
	if ch["since"] == nil || len(updated) != 1 || updated[0].(map[string]any)["to"] != "157.0" || ch["saved"] != false {
		t.Errorf("changes: %v", ch)
	}

	fx := call(t, tool(ts, "fixes"), `{}`).(map[string]any)
	if len(fx["fixes"].([]any)) == 0 {
		t.Errorf("fixes: %v", fx)
	}

	if !slices.Equal(*network, []bool{true, false, false, false, true}) {
		t.Errorf("only check and fixes probe the network, and only when asked: %v", *network)
	}
}

func TestChangesWithoutHistory(t *testing.T) {
	d, _ := fakeDeps(nil)
	ch := call(t, tool(Tools(d), "changes"), `{}`).(map[string]any)
	if ch["since"] != nil || ch["changes"] != float64(0) {
		t.Errorf("got %v", ch)
	}
}

func TestToolArgumentErrors(t *testing.T) {
	d, _ := fakeDeps(nil)
	ts := Tools(d)
	for name, args := range map[string]string{
		"top_apps": `{"limit":0}`, "changes": `{"since":"yesterday"}`, "check": `{"network":"yes"}`,
	} {
		var a map[string]json.RawMessage
		json.Unmarshal([]byte(args), &a)
		if _, err := tool(ts, name).Call(context.Background(), a); err == nil {
			t.Errorf("%s(%s) should fail", name, args)
		}
	}
	var a map[string]json.RawMessage
	json.Unmarshal([]byte(`{"sort":"name"}`), &a)
	if _, err := tool(ts, "top_apps").Call(context.Background(), a); err == nil || !strings.Contains(err.Error(), "memory") {
		t.Errorf("sort: %v", err)
	}
}

func TestEveryToolIsReadOnly(t *testing.T) {
	d, _ := fakeDeps(nil)
	for _, tl := range Tools(d) {
		a := tl.Annotations
		if a.ReadOnlyHint == nil || !*a.ReadOnlyHint || a.DestructiveHint == nil || *a.DestructiveHint {
			t.Errorf("%s must be marked read-only and not destructive", tl.Name)
		}
		if tl.OutputSchema == nil || tl.InputSchema["additionalProperties"] != false {
			t.Errorf("%s needs an output schema and a closed input schema", tl.Name)
		}
	}
}

// TestRealOutputMatchesSchemas runs the real collectors, so a field added
// to a --json document without updating its schema fails here.
func TestRealOutputMatchesSchemas(t *testing.T) {
	if testing.Short() {
		t.Skip("takes a few seconds of real sampling")
	}
	d := Deps{
		Collect:   func(ctx context.Context, _ bool) collect.Snapshot { return (&collect.Sampler{}).Next(ctx) },
		Inventory: collect.ReadInventory,
		Baseline: func(now time.Time, _ time.Duration) (history.Record, bool, error) {
			r := history.NewRecord((&collect.Sampler{}).Next(context.Background()), collect.ReadInventory())
			r.TakenAt = now.Add(-time.Hour)
			return r, true, nil
		},
		FixEnv: fix.System{},
	}
	ts := Tools(d)
	for _, name := range []string{"check", "top_apps", "changes", "fixes"} {
		call(t, tool(ts, name), `{}`)
	}
}

func TestToolListIsStable(t *testing.T) {
	d, _ := fakeDeps(nil)
	a, _ := marshal(Tools(d)[0].OutputSchema)
	b, _ := marshal(Tools(d)[0].OutputSchema)
	if string(a) != string(b) {
		t.Error("schemas must serialise the same way every time")
	}
}

func TestValidateCatchesMistakes(t *testing.T) {
	s := closed(map[string]any{"n": integer, "since": map[string]any{"type": []string{"string", "null"}}, "sev": severity})
	ok := map[string]any{"n": float64(2), "since": nil, "sev": "warn"}
	if err := validate(s, ok, "x"); err != nil {
		t.Errorf("valid: %v", err)
	}
	for name, bad := range map[string]map[string]any{
		"extra field": {"n": float64(2), "since": nil, "sev": "warn", "new": 1},
		"missing":     {"n": float64(2), "sev": "warn"},
		"not integer": {"n": 2.5, "since": nil, "sev": "warn"},
		"wrong type":  {"n": float64(2), "since": float64(1), "sev": "warn"},
		"not in enum": {"n": float64(2), "since": nil, "sev": "bad"},
	} {
		if validate(s, bad, "x") == nil {
			t.Errorf("%s should fail", name)
		}
	}
}
