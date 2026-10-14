## What and why

<!-- What does this change, and what does it add for people using lagdoc? Link the issue: "Fixes #123". -->

## Checklist

- [ ] **Tests:** every rule change has a test with a hand-built snapshot; `gofmt -l .`, `go vet ./...` and `go test ./...` pass.
- [ ] **Platforms:** builds for Linux and Windows too; anything OS-specific is behind build tags.
- [ ] **JSON:** no field renamed or removed, or `SchemaVersion` is bumped.
- [ ] **Output:** every status still has a symbol, and `--no-color` writes no escape codes.
- [ ] **Nothing new leaves the machine,** or the README says so.
- [ ] **No new dependencies,** or the maintainer agreed to them in an issue first.
- [ ] **CLAUDE.md** is updated if a decision or workflow it records changed.
