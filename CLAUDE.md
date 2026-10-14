# lagdoc

Cross-platform CLI that answers "why is my machine slow right now?" with a ranked, plain-language verdict instead of raw numbers. One static Go binary (`CGO_ENABLED=0`) for macOS, Linux and Windows, amd64 and arm64.

Differentiators: plain-language verdict; "what changed since last time" history; safe targeted fixes with preview; JSON/MCP output for AI agents.

## Layout

```
main.go              calls cmd.Execute()
cmd/                 cobra wiring only (root verdict, fix placeholder)
internal/collect/    Snapshot struct and collectors: data only, no judgement, no printing
internal/rules/      pure Snapshot -> []Finding; all thresholds in thresholds.go
internal/render/     text.go (lipgloss) and json.go
```

- Platform-specific code goes behind build tags in its own file (`memory_darwin.go` / `memory_other.go`).
- Every rule has tests built from fake snapshots. Keep collectors thin so logic stays testable.
- macOS memory is judged by kernel pressure (`kern.memorystatus_level`, `kern.memorystatus_vm_pressure_level`), not "used". Compressed memory is `vm.compressor_bytes_used`.
- Per-process memory is RSS. On macOS, Activity Monitor shows phys_footprint, which reads higher when pages are compressed or swapped. Getting footprint without cgo would need a new dependency (purego).
- Disks: pseudo/network/read-only filesystems are skipped (network ones before statfs, which can hang). Mounts with identical total+free bytes are one pool (APFS container) and are shown once.
- Verdict: `slow` if any check is crit, `warn` if only warnings, `ok`, or `unknown`.
- Output: severity colours only (red crit, amber warn, green ok), purple brand, blue commands. Every status has a symbol (✖ ▲ ✓ ?). All styling goes through `styles.paint`, so colour off means no escape codes. Colour is off when stdout isn't a TTY, `NO_COLOR` is set, `TERM=dumb`, or `--no-color`.
- `--json` is a stable contract: bump `render.SchemaVersion` only when a field is removed or changes meaning.

## Dependencies

Allowed: gopsutil/v4, cobra, lipgloss (v1; bubbletea later for live views), goreleaser (tool). Ask before adding anything else.

## Working agreement

- `main` is protected. Never commit or push to it. Branch, push, `gh pr create`, wait for CI (`ci` check) and the owner's approval.
- Commits use the GitHub noreply identity (`19154062+ag3ntsc4rn@users.noreply.github.com`), never a personal email. Check `git config user.email` in a fresh clone.
- Small, logical commits. `go build ./...`, `go vet ./...`, `go test ./...` and `gofmt -l .` must be clean.
- Every GitHub Action is pinned to a full commit SHA with its version in a comment.
- Releases: tag `vX.Y.Z` on `main`; `.github/workflows/release.yml` runs goreleaser.

## Milestones

1. ✅ Verdict: `lagdoc`, `--json`, `--no-color`; memory, disk, CPU, network checks
2. `lagdoc changes`: compact snapshots in the OS data dir; diff login items, installed apps, disk growth, new heavy processes
3. `lagdoc fix`: targeted actions for failed checks, preview first, confirm, never delete user files
4. `lagdoc top` / `lagdoc watch` with bubbletea
5. `lagdoc mcp`: findings as an MCP server
