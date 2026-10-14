package cmd

import (
	"bytes"
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
