# lagdoc

Cross-platform CLI that answers "why is my machine slow right now?" with a ranked, plain-language verdict instead of raw numbers. One static Go binary (`CGO_ENABLED=0`) for macOS, Linux and Windows, amd64 and arm64.

Differentiators: plain-language verdict; "what changed since last time" history; safe targeted fixes with preview; JSON/MCP output for AI agents.

## Layout

```
main.go              calls cmd.Execute()
cmd/                 cobra wiring only (root verdict, changes, fix, top, watch)
internal/collect/    Snapshot and Inventory structs and collectors: data only, no judgement, no printing
internal/rules/      pure Snapshot -> []Finding; all thresholds in thresholds.go
internal/history/    Record (compact run summary) and Store (one gzipped JSON file per run)
internal/changes/    pure Record, Record -> Changes
internal/live/       bubbletea models for top and watch (sampler injected), and watch's log mode
internal/fix/        Plan (Snapshot + findings + Env -> []Fix), Run (Fix + Runner -> Result); System implements both
internal/render/     text.go (lipgloss) and json.go; changes.go for lagdoc changes
```

- Platform-specific code goes behind build tags in its own file (`memory_darwin.go` / `memory_other.go`).
- Every rule has tests built from fake snapshots. Keep collectors thin so logic stays testable.
- macOS memory is judged by kernel pressure (`kern.memorystatus_level`, `kern.memorystatus_vm_pressure_level`), not "used". Compressed memory is `vm.compressor_bytes_used`.
- Per-process memory is RSS. On macOS, Activity Monitor shows phys_footprint, which reads higher when pages are compressed or swapped. Getting footprint without cgo would need a new dependency (purego).
- Disks: pseudo/network/read-only filesystems are skipped (network ones before statfs, which can hang). Mounts with identical total+free bytes are one pool (APFS container) and are shown once.
- Verdict: `slow` if any check is crit, `warn` if only warnings, `ok`, or `unknown`.
- Output: severity colours only (red crit, amber warn, green ok), purple brand, blue commands. Every status has a symbol (✖ ▲ ✓ ?). All styling goes through `styles.paint`, so colour off means no escape codes. Colour is off when stdout isn't a TTY, `NO_COLOR` is set, `TERM=dumb`, or `--no-color`.
- `--json` is a stable contract: bump `render.SchemaVersion` only when a field is removed or changes meaning. `lagdoc changes --json` has its own, `render.ChangesSchemaVersion`, with the same rule.
- History (M2): every run (verdict and `changes`) saves a `history.Record` unless `--no-save` (a persistent root flag); a failed save is a stderr line, never a failed command. Dir: macOS `~/Library/Application Support/lagdoc/history`, Linux `$XDG_STATE_HOME` (default `~/.local/state`), Windows `%LocalAppData%`; dir 0700, files 0600, written via temp file + rename. File names are the UTC time (`20261014T203720Z.json.gz`), so listing needs no decoding. Retention (`Thin`): all for 1 h, newest per hour to 48 h, newest per UTC day to 90 days. `history.RecordVersion`: records with another version are skipped, as are corrupt ones. The inventory is read alongside the CPU sample (`collectAll`), so it adds no time.
- `lagdoc changes --since D` (Go durations plus `7d`, default 24h) compares with the newest record at least D old, else the oldest. Startup items are keyed by source + name, apps by name (moving one between folders isn't a change), "updated" needs a version on both sides. A source that exists but can't be read goes in `Inventory.Failed`; its items are never reported as added or removed. Disks: used space moved ≥ 5 GB. Heavy apps: ≥ 500 MiB or ≥ 50 % of a core now, and at least double (or +1 GiB memory) since; top 5. Thresholds are in `rules/thresholds.go`.
- Fixes (M3): lagdoc itself only (a) asks an app to quit politely (macOS: `osascript` "tell application id … to quit", the bundle ID passed as an argument, never in the script text; Windows: `taskkill` without `/F`, by absolute System32 path; elsewhere SIGTERM), signalling only an app's root processes (parent outside the app), then waits 15 s and says "still open" rather than forcing; or (b) runs a tool's own cache cleanup (go, brew, npm, pip, docker builder prune) when the tool is installed, the cache is ≥ `CacheNotable` and on a flagged volume (`mountFor`: longest mount prefix). **Never** delete files from lagdoc, never sudo/elevate, never `/F` or SIGKILL. Anything else is `Manual` (shown with steps, `Run` refuses it): Trash, Downloads (with its biggest files), apt/dnf/journal (sudo), Windows Storage settings, DNS flush on macOS/Linux, connection advice. Quits are offered only for the apps the memory/CPU findings name (`rules.TopMemoryApps`/`TopCPUApps`), owned by the user, not system (macOS: a .app outside /System etc. with a bundle ID; deny lists for Linux desktops and Windows shell processes), and never for lagdoc's ancestors (the terminal or editor it runs in: shown as a manual "won't quit" note). Prompts are per fix, default no, `q` stops, EOF is no; only when stdin and stdout are terminals. `--json`/`--dry-run` never run anything. `fix.Env`/`fix.Runner` keep planning and running testable with fakes.
- Live views (M4): `collect.Sampler` takes repeated snapshots; only its first call waits `SampleWindow`, later ones measure CPU since the previous call (~10 ms for ~1,100 processes). It caches process names per PID, probes the network only every `NetworkEvery` (0 = never: top; 30 s: watch) and reuses the last good CPU figure if the counters don't advance, so a live view doesn't flap to "not measured". `Collect` is a one-shot Sampler. Models (`internal/live`) are pure bubbletea models with a `SampleFunc`; render's `Screen` draws whole frames exactly as tall as the window (`frame`, not `indent`), padding by display width (`pad`/`padLeft`, never `%*s` with non-ASCII). Watch records `rules.Transitions` (severity changes only, newest first, 200 kept). Without a terminal, top prints once and watch logs one line per change (`--json`: JSON lines, `render.WatchSchemaVersion`); `--count` stops it. Zero window sizes (some ptys) are ignored.
- bubbletea v1's `init` calls `lipgloss.HasDarkBackground()`, which queries the terminal (OSC 11 + DSR) on every lagdoc run whose output is a terminal. Real terminals answer at once; a pty that never answers (`script`, some test harnesses) waits termenv's 5 s timeout and can swallow early keystrokes. It goes away with bubbletea v2.
- Inventory sources only list directories and read small metadata files (Info.plist, .desktop, registry values); nothing is executed. macOS: `/Library/Launch{Daemons,Agents}`, `~/Library/LaunchAgents`, `/Applications` and `~/Applications` (one folder deep), version from Info.plist (`plist.go` reads XML and binary plists; it's fuzzed by truncation/corruption in tests). SMAppService login items are in a root-only database and aren't seen. Linux: XDG autostart, systemd `*.wants` in `/etc/systemd/{system,user}` and the user config, desktop entries incl. Flatpak/Snap exports; packages aren't listed (an upgrade changes hundreds). Windows: Run keys (HKLM, HKLM WOW6432Node, HKCU), Startup folders, Uninstall keys (skipping SystemComponent and ParentKeyName entries), via `golang.org/x/sys/windows/registry`.

## Dependencies

Allowed: gopsutil/v4, cobra, lipgloss v1, bubbletea v1 (live views; v2 needs lipgloss v2), golang.org/x/sys (already a gopsutil dependency; used directly for the Windows registry), goreleaser (tool). Ask before adding anything else.

## Working agreement

- `main` is protected. Never commit or push to it. Branch, push, `gh pr create`, wait for CI (`ci` check) and the owner's approval.
- Commits use the GitHub noreply identity (`19154062+ag3ntsc4rn@users.noreply.github.com`), never a personal email. Check `git config user.email` in a fresh clone.
- Small, logical commits. `go build ./...`, `go vet ./...`, `go test ./...` and `gofmt -l .` must be clean.
- Every GitHub Action is pinned to a full commit SHA with its version in a comment; `workflows_test.go` enforces it. Dependabot bumps Go modules and actions weekly, minor/patch grouped.
- Releases: tag `vX.Y.Z` on `main` when something notable ships; `.github/workflows/release.yml` runs goreleaser (generated notes). `lagdoc --version` shows the goreleaser-stamped version, or Go's build info for `go install` and local builds (`cmd/version.go`).
- Repo hygiene: `SECURITY.md` (GitHub private vulnerability reporting only, 7-day acknowledgement, safe harbour, no bounty but credit), `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md` (Contributor Covenant; reports go through GitHub's private reporting form, titled "Code of conduct". lagdoc has no email address: never add one, the owner keeps personal email private), issue forms (blank issues off; security goes to private reporting) and a PR template. Repo settings: private vulnerability reporting, secret scanning + push protection, Dependabot alerts and security updates, CodeQL default setup; labels `security`, `dependencies`, `checks`, `platform`.

## Milestones

1. ✅ Verdict: `lagdoc`, `--json`, `--no-color`; memory, disk, CPU, network checks
2. ✅ `lagdoc changes`: compact snapshots in the OS data dir; diff login items, installed apps, disk growth, new heavy processes
3. ✅ `lagdoc fix`: targeted actions for failed checks, preview first, confirm, never delete user files
4. ✅ `lagdoc top` / `lagdoc watch` with bubbletea
5. `lagdoc mcp`: findings as an MCP server
