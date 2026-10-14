package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A tag can be moved to different code; a commit SHA can't. Dependabot keeps
// the pins and their version comments current.
var pinned = regexp.MustCompile(`uses: [\w.-]+/[\w./-]+@[0-9a-f]{40} # v\d+\.\d+\.\d+`)

func TestWorkflowsPinActionsToCommitSHAs(t *testing.T) {
	files, err := filepath.Glob(".github/workflows/*.yml")
	if err != nil || len(files) < 2 {
		t.Fatalf("expected the workflows, found %v (%v)", files, err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
			if strings.HasPrefix(trimmed, "uses:") && !pinned.MatchString(trimmed) {
				t.Errorf("%s:%d: pin to a full commit SHA with a version comment: %s", f, i+1, trimmed)
			}
		}
	}
}
