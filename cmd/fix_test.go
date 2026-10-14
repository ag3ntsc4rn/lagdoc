package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ag3ntsc4rn/lagdoc/internal/fix"
)

type recordingRunner struct{ ran [][]string }

func (r *recordingRunner) Exec(_ context.Context, name string, args ...string) ([]byte, error) {
	r.ran = append(r.ran, append([]string{name}, args...))
	return nil, nil
}
func (r *recordingRunner) Alive([]int32) []int32       { return nil }
func (r *recordingRunner) Parent(int32) (int32, error) { return 0, nil }
func (r *recordingRunner) Terminate(int32) error       { return nil }
func (r *recordingRunner) Free(string) (uint64, bool)  { return 0, false }
func (r *recordingRunner) Sleep(time.Duration)         {}

func fixes(names ...string) []fix.Fix {
	var out []fix.Fix
	for _, n := range names {
		out = append(out, fix.Fix{ID: n, Action: "Run " + n, Title: "Run " + n, Command: []string{n}})
	}
	return out
}

func runWith(t *testing.T, input string, fs []fix.Fix) (*recordingRunner, string, error) {
	t.Helper()
	cmd := newFixCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetIn(strings.NewReader(input))
	cmd.SetContext(context.Background())
	r := &recordingRunner{}
	err := runChosen(cmd, fs, false, r)
	return r, out.String(), err
}

func TestRunChosenAsksEachAndDefaultsToNo(t *testing.T) {
	r, out, err := runWith(t, "y\n\nYES\n", fixes("a", "b", "c"))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.ran) != 2 || r.ran[0][0] != "a" || r.ran[1][0] != "c" {
		t.Errorf("ran %v; output:\n%s", r.ran, out)
	}
	if strings.Count(out, "[y/N/q]") != 3 {
		t.Errorf("each fix is asked about:\n%s", out)
	}
}

func TestRunChosenStops(t *testing.T) {
	if r, _, _ := runWith(t, "q\ny\n", fixes("a", "b")); len(r.ran) != 0 {
		t.Errorf("q must stop: ran %v", r.ran)
	}
	// End of input is a no to everything left, never a yes.
	if r, _, _ := runWith(t, "", fixes("a", "b")); len(r.ran) != 0 {
		t.Errorf("EOF must not run anything: ran %v", r.ran)
	}
	if r, _, _ := runWith(t, "y", fixes("a", "b")); len(r.ran) != 1 {
		t.Errorf("a last answer without newline still counts: ran %v", r.ran)
	}
}

func TestFixFlags(t *testing.T) {
	c := newFixCmd()
	for _, name := range []string{"json", "no-color", "dry-run"} {
		if c.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s", name)
		}
	}
}
