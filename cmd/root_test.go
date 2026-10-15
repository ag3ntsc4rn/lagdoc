package cmd

import (
	"bytes"
	"runtime/debug"
	"testing"
)

func TestUseColor(t *testing.T) {
	// A buffer is never a terminal.
	if useColor(&bytes.Buffer{}, false) {
		t.Error("non-terminal output must not be coloured")
	}
	t.Setenv("NO_COLOR", "1")
	if useColor(&bytes.Buffer{}, false) {
		t.Error("NO_COLOR must turn colour off")
	}
}

func TestFlags(t *testing.T) {
	root := newRootCmd()
	for _, name := range []string{"json", "no-color"} {
		if root.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s", name)
		}
	}
	root.SetArgs([]string{"unexpected"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err == nil {
		t.Error("stray arguments should be rejected")
	}
}

func TestFromBuildInfo(t *testing.T) {
	info := &debug.BuildInfo{
		Main:     debug.Module{Version: "v0.1.0"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "1b5e4a7d8aba0199"}},
	}
	if v, c := fromBuildInfo(info, "dev", "none"); v != "v0.1.0" || c != "1b5e4a7" {
		t.Errorf("go install build: got %s (%s)", v, c)
	}
	if v, c := fromBuildInfo(info, "0.1.0", "abcdef0"); v != "0.1.0" || c != "abcdef0" {
		t.Errorf("release build values must win: got %s (%s)", v, c)
	}
	info.Main.Version = "(devel)"
	if v, _ := fromBuildInfo(info, "dev", "none"); v != "dev" {
		t.Errorf("a local checkout stays dev, got %s", v)
	}
}

func TestChangesFlags(t *testing.T) {
	root := newRootCmd()
	c, _, err := root.Find([]string{"changes"})
	if err != nil || c.Name() != "changes" {
		t.Fatalf("no changes command: %v", err)
	}
	for _, name := range []string{"json", "no-color", "since", "no-save"} {
		if c.Flags().Lookup(name) == nil && c.InheritedFlags().Lookup(name) == nil {
			t.Errorf("changes is missing --%s", name)
		}
	}
}

func TestLiveFlagsValidated(t *testing.T) {
	for _, args := range [][]string{
		{"top", "--interval", "100ms"}, {"top", "--sort", "name"},
		{"watch", "--interval", "10ms"}, {"watch", "--count", "-1"},
	} {
		root := newRootCmd()
		root.SetArgs(args)
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		if err := root.Execute(); err == nil {
			t.Errorf("%v should be rejected", args)
		}
	}
}
