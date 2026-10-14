# Contributing to lagdoc

Thanks for helping. lagdoc is a small, free CLI maintained by one person. Bug reports, fixes and well-argued ideas are all welcome.

- **Found a security problem?** Don't open an issue. See [SECURITY.md](SECURITY.md).
- **Bug or idea?** Open an issue with the bug report or feature request form. A wrong verdict is a bug: include `lagdoc --json` output and what your OS's own tools (Activity Monitor, Task Manager, `free`, `df`) showed at the time.
- **Code?** For anything bigger than a small fix, open an issue first so we can agree the approach before you spend time on it.

Everyone taking part follows the [code of conduct](CODE_OF_CONDUCT.md).

## Build and run

You need Go (the version in `go.mod`).

```sh
go run .            # the verdict
go run . --json
go build -o lagdoc .
```

## Test

CI runs these on Linux, macOS and Windows for every pull request, and they must pass:

```sh
gofmt -l .          # must print nothing
go vet ./...
go test ./...
```

Cross-compile checks: `GOOS=linux go build ./...` and `GOOS=windows go build ./...`.

## How the code is laid out

- `internal/collect` gathers a `Snapshot`: data only, no judgement, no printing.
- `internal/rules` turns a snapshot into ranked findings. These are pure functions, and **every rule change needs a test** with a hand-built snapshot. All thresholds live in `thresholds.go`.
- `internal/render` prints text (lipgloss) or JSON. The JSON is a contract: don't rename or remove fields without bumping `SchemaVersion`.
- Platform-specific code goes in its own file behind build tags (`_darwin.go`, `_linux.go`, `_windows.go`).

## Rules that don't bend

- **Static, cgo-free binary.** Releases build with `CGO_ENABLED=0` for every OS and architecture.
- **No new dependencies** without agreeing it in an issue first.
- **Nothing leaves the machine** beyond the network probe the README describes. No telemetry.
- **Colour is never the only signal.** Every status has a symbol, and `NO_COLOR` / `--no-color` produce no escape codes.
- **Fixes never delete user files** and always preview and ask first.

## Pull requests

- `main` is protected: work on a branch and open a pull request. The `ci` check must pass.
- Keep commits small and well described. GitHub Actions are pinned to full commit SHAs (a test enforces it); Dependabot keeps them current.
- Use your GitHub noreply address if you'd rather not publish your email in the history.
