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

  → run lagdoc fix to see what can be done
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
lagdoc fix          # what could help, then asks before doing each one
lagdoc top          # live: apps by memory or CPU
lagdoc watch        # live: the verdict, and when each check changed
lagdoc mcp          # an MCP server, for AI agents and editors
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

## Fixing things

`lagdoc fix` lists what could help with each problem, then asks before running each one (the answer defaults to no):

```
  lagdoc fix · studio

  2 fixes lagdoc can run · 2 for you to do

  ✖ memory · under heavy pressure
    1 Quit Google Chrome (23 processes), using 4.1 GB
      Asks it to quit, as ⌘Q would; it can still ask you to save open work.

  ▲ disk · getting full
    2 Clear Go's build cache, about 6.3 GB
      runs go clean -cache
      Builds and tests are slower until it fills again. Downloaded modules are kept.
    · Empty the Trash, which holds 3.4 GB
      Only if you don't need what's in it. lagdoc never deletes your files.
    · Look through Downloads, which holds 12 GB
      └ Xcode_26.xip 7.9 GB

  Run 1, quit Google Chrome? [y/N/q]
```

lagdoc itself only does two kinds of thing:

- **Asks an app to quit**, the way its own Quit would, so it can still ask you to save your work. On macOS that's a normal Quit request (macOS may ask once whether your terminal may control the app); on Windows, `taskkill` without `/F`; on Linux, SIGTERM. It's only offered for your own apps: never for system processes, other users' processes, or the app you're running lagdoc in.
- **Runs a tool's own cache cleanup**, for caches on the volume that's full: `go clean -cache`, `brew cleanup --prune=all`, `npm cache clean --force`, `pip cache purge` and `docker builder prune --force` (images, containers and volumes are kept). Each is shown with its size before you're asked.

It never deletes files itself and never runs anything as administrator. Steps that would need either (emptying the Trash, looking through Downloads, `sudo apt-get clean`, shrinking the systemd journal, flushing the DNS cache on macOS or Linux) are listed for you to do yourself. On Windows, flushing the DNS cache (`ipconfig /flushdns`) needs no admin rights, so lagdoc can run it.

```sh
lagdoc fix --dry-run   # only show the fixes
lagdoc fix --json      # the fixes as JSON; never runs anything
```

Fixes are only offered to run when lagdoc is in a terminal; piped or scripted, it only previews.

## Live views

`lagdoc top` lists apps by memory (or CPU, press `c`), with each app's helper processes counted together, so Chrome is one line instead of thirty. It refreshes every 2 seconds (`--interval`); `↑ ↓` scroll and `q` quits. It doesn't touch the network.

```
 lagdoc top · studio · every 2s
 ▲ disk 93%   ✓ memory 42%   ✓ cpu 11%

 APP                                PROCS   MEMORY↓     CPU
 Firefox                               19    1.5 GB      3%
 Visual Studio Code                    20    905 MB      2%
 Docker                                10    392 MB    152%
```

`lagdoc watch` keeps re-running the checks (every 5 seconds, `--interval`) and keeps a list of when each one changed, so you can see *when* the machine got slow, not just that it is:

```
 lagdoc watch · studio · every 5s · since 11:05

 WARN  1 thing to watch

 ▲ disk      █████████░  93%  getting full
   └ / 18.4 GB free of 251 GB
 ✓ memory    ████░░░░░░  42%  plenty free
 ✓ cpu       ██░░░░░░░░  18%  mostly idle
 ✓ network   dns 12 ms

 changes
 14:05:10  ▲ disk     ok → warn · getting full
 13:05:10  ✓ memory   crit → ok · plenty free
```

The network check sends a DNS query and opens a connection, so `watch` repeats it only every 30 seconds.

When output isn't a terminal, `top` prints the list once and `watch` writes one line per change, which makes it easy to log:

```sh
lagdoc watch > lagdoc.log                        # runs until stopped
lagdoc watch --json --interval 1m --count 60     # an hour, as JSON lines
```

Each `--json` line has `schema_version`, `time`, `check`, `from` (`null` on the first reading), `to`, `summary` and `details`.

## For AI agents (MCP)

`lagdoc mcp` is a [Model Context Protocol](https://modelcontextprotocol.io) server over stdio, so an AI agent or editor can ask why the machine is slow. Add it to your client's MCP servers:

```sh
claude mcp add lagdoc -- lagdoc mcp          # Claude Code
```

```json
{ "mcpServers": { "lagdoc": { "command": "lagdoc", "args": ["mcp"] } } }
```

(The second form is for clients configured with JSON, such as Claude Desktop. Use the full path to `lagdoc` if it isn't on the `PATH` the client sees.)

| Tool | What it returns |
|---|---|
| `check` | The verdict and findings, as `lagdoc --json`. `network: false` skips the network check. |
| `top_apps` | Apps by memory or CPU (`sort`, `limit`), helpers counted with their app. |
| `changes` | What changed since an earlier run (`since`, default `24h`), as `lagdoc changes --json`. |
| `fixes` | What could help, as `lagdoc fix --json`. Nothing is run. |

Every tool only reads, and is marked read-only: nothing is changed and no history is saved. To apply a fix, the agent should ask you to run `lagdoc fix`, which previews each one and asks before doing it. Results come as structured content with an output schema, and as the same JSON in text for clients that don't read structured content.

lagdoc speaks MCP 2026-07-28 (the protocol version on every request) and, for clients that start with `initialize`, 2025-11-25, 2025-06-18, 2025-03-26 and 2024-11-05.

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

`lagdoc fix --json` has its own `schema_version` too. Each fix has a stable `id` (`quit:<app>`, `cache:go`, `trash`, …), `manual` (true: for you to do, never run by lagdoc), `command` (what lagdoc would run, `""` for quits and manual fixes), `steps`, and `frees_bytes` (an estimate, 0 if unknown).

`lagdoc changes --json` has its own `schema_version`. `since` is when the record it compared with was taken (`null` if there wasn't one yet); every list is an array, empty when nothing changed; `disks[].used_change_bytes` is negative when space was freed; `heavy_apps[].before` is `null` if the app wasn't running then, and `reasons` says whether `memory`, `cpu` or both made it count.

## Contributing

Issues and pull requests are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md) and the [code of conduct](CODE_OF_CONDUCT.md). Found a security problem? Please report it privately; see [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
