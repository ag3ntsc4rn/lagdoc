# lagdoc

**Why is my machine slow right now?** lagdoc answers with a verdict, not a dashboard.

Tools like btop and glances show you every number and leave the diagnosis to you. lagdoc checks memory, disk, CPU and network, decides what's actually wrong, and tells you in plain words, worst cause first.

```
  lagdoc · studio · macOS 15.7.9 · up 3d 4h

  SLOW  2 causes found

  ✖ memory    █████████░  92%  under heavy pressure
    └ Google Chrome (23 processes) 4.1 GB
    └ swap in use 6.2 GB
  ▲ disk      █████████░  94%  getting full
    └ / 14.0 GB free of 250 GB
  ✓ cpu       ██░░░░░░░░  23%  mostly idle
  ✓ network   dns 12 ms

  → run lagdoc fix to preview 2 fixes
```

One static binary for macOS, Linux and Windows (amd64 and arm64). No daemon, no config, no root needed.

## Install

Download the archive for your platform from [Releases](https://github.com/ag3ntsc4rn/lagdoc/releases), unpack it and put `lagdoc` on your `PATH`. Check the download against `checksums.txt`.

The macOS binaries aren't notarised yet, so the first run may be blocked. Clear the quarantine flag with:

```sh
xattr -d com.apple.quarantine ./lagdoc
```

Or build from source with Go 1.27 or later:

```sh
go install github.com/ag3ntsc4rn/lagdoc@latest
```

## Usage

```sh
lagdoc              # the verdict
lagdoc --json       # the same findings as JSON, for scripts and AI agents
lagdoc --no-color   # plain text
```

A run takes about a second, because CPU use is measured over a one-second sample.

Colour is used only for severity (red = critical, amber = warning, green = fine). Every status also has a symbol (✖ ▲ ✓), so the output reads the same without colour. Colour is turned off automatically when output isn't a terminal or when [`NO_COLOR`](https://no-color.org) is set.

## What it checks

| Check | What counts as a problem |
|---|---|
| **memory** | RAM is genuinely short. Names the apps using the most, with each app's helper processes counted together, plus swap and compressed memory. |
| **disk** | A volume is nearly full. Big drives aren't flagged for a high percentage while they still have plenty of space. Pseudo, network and read-only filesystems are skipped. |
| **cpu** | The machine is busy over a one-second sample. Names the apps using it. |
| **network** | DNS resolution time through your system resolver, and whether `1.1.1.1:443` can be reached. |

All thresholds are in one file: [`internal/rules/thresholds.go`](internal/rules/thresholds.go).

**On macOS**, "memory used" is misleading: macOS fills spare RAM with cache and compressed pages on purpose. lagdoc uses the kernel's memory pressure figure instead, the same number `memory_pressure` reports and Activity Monitor graphs.

**Network:** the network check looks up `cloudflare.com` and opens (then closes) a TCP connection to `1.1.1.1:443`. Nothing else leaves your machine, and lagdoc sends no telemetry.

## JSON output

`lagdoc --json` prints a stable document. `schema_version` changes only if a field is removed or changes meaning.

```json
{
  "schema_version": 1,
  "taken_at": "2026-10-14T20:37:20Z",
  "host": { "hostname": "studio.local", "os": "darwin", "os_name": "macOS 15.7.9", "uptime_seconds": 273900, "cpus": 8 },
  "verdict": "warn",
  "causes": 1,
  "findings": [
    { "check": "disk", "severity": "warn", "summary": "getting full", "percent": 91.3, "details": ["/ 21.9 GB free of 251 GB"] },
    { "check": "network", "severity": "ok", "summary": "dns 7 ms", "details": ["cloudflare.com resolved in 7 ms", "1.1.1.1:443 answered in 14 ms"] }
  ]
}
```

- `verdict` is `slow` (something critical), `warn`, `ok` or `unknown`.
- `severity` is `crit`, `warn`, `ok` or `unknown` (the data couldn't be read).
- Findings are ranked worst first.
- `percent` is left out for checks that have no single percentage.

## Roadmap

- `lagdoc changes`: what changed since last time (login items, installed apps, disk growth, new heavy processes)
- `lagdoc fix`: safe, targeted fixes, previewed first and confirmed before anything is done; never deletes your files
- `lagdoc top` and `lagdoc watch`: live views
- `lagdoc mcp`: findings as an MCP server for AI agents

## Contributing

Issues and pull requests are welcome. Run `go vet ./...` and `go test ./...` before opening a PR. Rules are pure functions of a snapshot, so each one is tested with a hand-built snapshot.

## License

[MIT](LICENSE)
