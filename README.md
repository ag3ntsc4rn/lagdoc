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
lagdoc changes      # what's different since an earlier run
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

## What changed?

Slowness often starts with something new: an app that installed a background updater, an update, a download that filled the disk. `lagdoc changes` compares this machine now with an earlier run:

```
  lagdoc changes · studio · since Mon 12 Oct 14:03 (2 days ago)

  6 changes

  startup items
    + com.docker.vmnetd (/Library/LaunchDaemons)

  apps
    + Docker 4.90.0
    ↑ Firefox 156.0 → 157.0.1
    − zoom.us

  disk
    ↑ / 18.2 GB more used, 17.4 GB free of 250 GB

  heavy apps
    ↑ Firefox (19 processes) 3.4 GB, was 1.1 GB
```

```sh
lagdoc changes             # compare with the newest record at least a day old
lagdoc changes --since 7d  # …at least a week old (also 30m, 6h)
lagdoc changes --json      # as JSON
```

If no record is that old yet, it compares with the oldest one it has. It reports:

| Section | What's listed |
|---|---|
| **startup items** | Added or removed. macOS: launchd agents and daemons in `/Library` and `~/Library`. Linux: XDG autostart entries and systemd units enabled in `/etc` or your user config. Windows: Run registry keys and Startup folders. |
| **apps** | Installed, removed or updated. macOS: `/Applications` and `~/Applications`, with versions. Linux: desktop entries (including Flatpak and Snap), no versions. Windows: the apps Settings lists, with versions. |
| **disk** | Volumes whose used space went up or down by 5 GB or more. |
| **heavy apps** | Apps now using 500 MB or more memory, or half a core or more, that at least doubled (or grew by 1 GB) since then. |

Things lagdoc can't see aren't reported: on macOS, login items an app registers through System Settings (without a launchd file) are kept in a database only root can read, and on Windows, services and scheduled tasks aren't read yet. A place lagdoc couldn't read is listed at the end, and its items aren't reported as removed.

### History stays on your machine

Every `lagdoc` and `lagdoc changes` run saves a small record (about 7 KB): disk space, running apps' memory and CPU, startup items and installed apps. Nothing is sent anywhere. Records are thinned to one an hour after an hour, one a day after two days, and deleted after 90 days. They're kept in:

| OS | Folder |
|---|---|
| macOS | `~/Library/Application Support/lagdoc/history` |
| Linux | `$XDG_STATE_HOME/lagdoc/history` (default `~/.local/state/lagdoc/history`) |
| Windows | `%LocalAppData%\lagdoc\history` |

Use `--no-save` to run without saving, or delete the folder to clear the history.

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

`lagdoc changes --json` has its own `schema_version`. `since` is when the record it compared with was taken (`null` if there wasn't one yet); every list is an array, empty when nothing changed; `disks[].used_change_bytes` is negative when space was freed; `heavy_apps[].before` is `null` if the app wasn't running then, and `reasons` says whether `memory`, `cpu` or both made it count.

## Roadmap

- `lagdoc fix`: safe, targeted fixes, previewed first and confirmed before anything is done; never deletes your files
- `lagdoc top` and `lagdoc watch`: live views
- `lagdoc mcp`: findings as an MCP server for AI agents

## Contributing

Issues and pull requests are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md) and the [code of conduct](CODE_OF_CONDUCT.md). Found a security problem? Please report it privately; see [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
