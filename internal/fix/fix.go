// Package fix plans and runs targeted fixes for the problems rules found.
//
// lagdoc itself does only two kinds of thing: it asks an app to quit the
// way the app's own Quit would (so the app can still ask to save work), and
// it runs a tool's own cache cleanup (go clean -cache, brew cleanup, …). It
// never deletes files itself and never runs anything as administrator.
// Fixes that would need either are Manual: shown with their steps, never
// run.
package fix

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/rules"
)

// Fix is one thing that could help. The JSON contract is render's.
type Fix struct {
	ID    string // stable: "quit:Google Chrome", "cache:go", "dns:flush"
	Check string // the check it addresses
	Title string // "Quit Google Chrome (23 processes), using 4.1 GB"
	// Action is the title's short form, for prompts and results: "Quit Google Chrome".
	Action string
	Note   string // what it does, or what to know first
	// Command is what lagdoc runs. Command[0] may be an absolute path to a
	// system tool; it's shown by its base name.
	Command []string
	Quit    *Quit
	// Manual fixes are shown, never run. Steps are commands or actions for
	// the person to do themselves.
	Manual bool
	Steps  []string
	Frees  uint64 // estimated bytes freed, 0 if unknown
	Mount  string // the volume a disk fix frees space on
}

// Quit asks an app to quit: on macOS by bundle ID, elsewhere by PID.
type Quit struct {
	App      string
	BundleID string
	PIDs     []int32
}

// Env is what planning needs from the machine, so plans can be tested.
type Env interface {
	OS() string
	Home() string
	CacheDir() string // os.UserCacheDir
	Getenv(key string) string
	LookPath(tool string) bool
	// SystemTool is the absolute path of an OS tool (taskkill, ipconfig),
	// so a stray one earlier in PATH isn't run instead.
	SystemTool(name string) string
	// DirSize is the total size of the files under dir; false if dir is
	// missing or unreadable.
	DirSize(ctx context.Context, dir string) (uint64, bool)
	LargestFiles(dir string, n int) []File
	Mine(pid int32) bool       // owned by the user running lagdoc
	Ancestors() map[int32]bool // lagdoc's parent, its parent, …
	BundleID(app string) string
	DockerBuildCache(ctx context.Context) (uint64, bool)
}

type File struct {
	Name string
	Size uint64
}

// Plan lists fixes for the warn and crit findings, in the findings' order
// (worst first). Within a check, things lagdoc can run come first.
func Plan(ctx context.Context, s collect.Snapshot, fs []rules.Finding, env Env) []Fix {
	p := planner{s: s, env: env, seen: map[string]bool{}}
	for _, f := range fs {
		if f.Severity < rules.Warn {
			continue
		}
		start := len(p.out)
		switch f.Check {
		case rules.CheckMemory:
			p.quits(f.Check, rules.TopMemoryApps(s.Processes), true)
		case rules.CheckCPU:
			p.quits(f.Check, rules.TopCPUApps(s.Processes), false)
		case rules.CheckDisk:
			p.disk(ctx)
		case rules.CheckNetwork:
			p.network()
		}
		slices.SortStableFunc(p.out[start:], func(a, b Fix) int {
			if a.Manual != b.Manual {
				if a.Manual {
					return 1
				}
				return -1
			}
			return cmp.Compare(b.Frees, a.Frees)
		})
	}
	return p.out
}

type planner struct {
	s    collect.Snapshot
	env  Env
	seen map[string]bool
	out  []Fix
}

func (p *planner) add(f Fix) {
	if p.seen[f.ID] {
		return
	}
	p.seen[f.ID] = true
	p.out = append(p.out, f)
}

// Processes that keep the desktop or the session running. Asking them to
// exit would log you out or blank the screen, so they're never offered.
var (
	keepLinux = []string{
		"systemd", "sd-pam", "dbus-daemon", "dbus-broker", "dbus-broker-launch",
		"gnome-shell", "gnome-session-binary", "gnome-session", "mutter", "plasmashell",
		"kwin_x11", "kwin_wayland", "ksmserver", "kded5", "kded6", "xorg", "xwayland",
		"sway", "hyprland", "weston", "pipewire", "pipewire-pulse", "wireplumber",
		"pulseaudio", "sshd", "gdm", "gdm-x-session", "gdm-wayland-session", "sddm",
		"lightdm", "xdg-desktop-portal", "xdg-desktop-portal-gnome", "xdg-desktop-portal-kde",
		"xdg-desktop-portal-gtk", "xdg-document-portal", "xdg-permission-store",
		"at-spi-bus-launcher", "at-spi2-registryd", "ibus-daemon", "polkitd", "login",
	}
	keepWindows = []string{
		"explorer", "dwm", "csrss", "winlogon", "wininit", "lsass", "services", "smss",
		"svchost", "sihost", "ctfmon", "runtimebroker", "shellexperiencehost",
		"startmenuexperiencehost", "searchhost", "searchapp", "textinputhost", "fontdrvhost",
		"taskhostw", "dllhost", "conhost", "openconsole", "securityhealthsystray",
		"msmpeng", "applicationframehost", "systemsettings", "lockapp", "widgets",
	}
	// macOS apps under these paths are part of the OS.
	systemPrefixes = []string{"/System/", "/Library/Apple/", "/usr/", "/bin/", "/sbin/"}
)

// appBundle is the outermost .app bundle of an executable path, or "".
func appBundle(exe string) string {
	if i := strings.Index(exe, ".app/"); i >= 0 {
		return exe[:i+len(".app")]
	}
	return ""
}

func (p *planner) quits(check string, groups []rules.AppGroup, byMemory bool) {
	ancestors := p.env.Ancestors()
	osName := p.env.OS()
	for _, g := range groups {
		var procs []collect.Process
		for _, pr := range p.s.Processes {
			if rules.AppName(pr) == g.Name {
				procs = append(procs, pr)
			}
		}
		if slices.ContainsFunc(procs, func(pr collect.Process) bool { return ancestors[pr.PID] }) {
			p.add(Fix{
				ID: "keep:" + g.Name, Check: check, Manual: true,
				Title: fmt.Sprintf("%s is running this terminal, so lagdoc won't quit it", g.Name),
				Note:  "Quit it yourself once your work is saved, if you don't need it.",
			})
			continue
		}
		q := &Quit{App: g.Name}
		for _, pr := range procs {
			if p.env.Mine(pr.PID) {
				q.PIDs = append(q.PIDs, pr.PID)
			}
		}
		if len(q.PIDs) == 0 {
			continue // someone else's, or the system's
		}
		var note string
		switch osName {
		case "darwin":
			bundle := ""
			for _, pr := range procs {
				if bundle = appBundle(pr.Exe); bundle != "" {
					break
				}
			}
			if bundle == "" || slices.ContainsFunc(systemPrefixes, func(s string) bool { return strings.HasPrefix(bundle, s) }) {
				continue // not an app, or part of macOS
			}
			if q.BundleID = p.env.BundleID(bundle); q.BundleID == "" {
				continue
			}
			note = "Asks it to quit, as ⌘Q would; it can still ask you to save open work. macOS may ask once whether your terminal may control it."
		case "windows":
			if slices.Contains(keepWindows, strings.ToLower(g.Name)) {
				continue
			}
			note = "Asks it to close, as clicking × would; it can still ask you to save open work."
		default:
			if slices.Contains(keepLinux, strings.ToLower(g.Name)) {
				continue
			}
			note = "Asks it to exit (SIGTERM). Most apps close cleanly, but save open work first."
		}
		use := fmt.Sprintf("%.0f%% CPU", g.CPUPercent)
		if byMemory {
			use = rules.MemSize(g.RSS)
		}
		p.add(Fix{
			ID: "quit:" + g.Name, Check: check, Quit: q, Note: note, Action: "Quit " + g.Name,
			Title: fmt.Sprintf("Quit %s, using %s", g.Label(), use),
		})
	}
}

// mountFor is the mount holding path: the longest mount path it's under.
func mountFor(path string, disks []collect.Disk, windows bool) string {
	norm := func(s string) string {
		if windows {
			return strings.ToLower(strings.ReplaceAll(s, "/", `\`))
		}
		return s
	}
	path = norm(path)
	best := ""
	for _, d := range disks {
		m := norm(d.Mount)
		if !strings.HasPrefix(path, m) || len(m) <= len(best) {
			continue
		}
		if len(path) == len(m) || strings.HasSuffix(m, "/") || strings.HasSuffix(m, `\`) ||
			path[len(m)] == '/' || path[len(m)] == '\\' {
			best = d.Mount
		}
	}
	return best
}

type cache struct {
	id, tool, title, note, dir string
	command                    []string
}

// caches are tool caches that the tool itself can clear, with where each
// keeps its files by default.
func (p *planner) caches() []cache {
	env := p.env
	cacheDir, osName := env.CacheDir(), env.OS()
	or := func(v, def string) string {
		if v != "" {
			return v
		}
		return def
	}
	var cs []cache
	if osName != "windows" {
		cs = append(cs, cache{
			id: "brew", tool: "brew", command: []string{"brew", "cleanup", "--prune=all"},
			title: "Clear Homebrew's downloads and old versions",
			note:  "Installed packages stay; brew downloads anything it needs again.",
			dir:   or(env.Getenv("HOMEBREW_CACHE"), filepath.Join(cacheDir, "Homebrew")),
		})
	}
	if gocache := or(env.Getenv("GOCACHE"), filepath.Join(cacheDir, "go-build")); gocache != "off" {
		cs = append(cs, cache{
			id: "go", tool: "go", command: []string{"go", "clean", "-cache"},
			title: "Clear Go's build cache", dir: gocache,
			note: "Builds and tests are slower until it fills again. Downloaded modules are kept.",
		})
	}
	npm := filepath.Join(env.Home(), ".npm")
	if osName == "windows" {
		npm = filepath.Join(cacheDir, "npm-cache")
	}
	cs = append(cs, cache{
		id: "npm", tool: "npm", command: []string{"npm", "cache", "clean", "--force"},
		title: "Clear npm's download cache", dir: filepath.Join(or(env.Getenv("npm_config_cache"), npm), "_cacache"),
		note: "Packages are downloaded again the next time they're installed.",
	})
	pipDir := filepath.Join(cacheDir, "pip")
	if osName == "windows" {
		pipDir = filepath.Join(cacheDir, "pip", "Cache")
	}
	pip := "pip3"
	if !env.LookPath(pip) {
		pip = "pip"
	}
	cs = append(cs, cache{
		id: "pip", tool: pip, command: []string{pip, "cache", "purge"},
		title: "Clear pip's download cache", dir: or(env.Getenv("PIP_CACHE_DIR"), pipDir),
		note: "Packages are downloaded again the next time they're installed.",
	})
	return cs
}

func (p *planner) disk(ctx context.Context) {
	env := p.env
	windows := env.OS() == "windows"
	flagged := map[string]bool{}
	for _, d := range p.s.Disks {
		if rules.DiskSeverity(d) >= rules.Warn {
			flagged[d.Mount] = true
		}
	}
	on := func(path string) (string, bool) {
		m := mountFor(path, p.s.Disks, windows)
		return m, m != "" && flagged[m]
	}

	for _, c := range p.caches() {
		if !env.LookPath(c.tool) {
			continue
		}
		mount, ok := on(c.dir)
		if !ok {
			continue
		}
		if size, ok := env.DirSize(ctx, c.dir); ok && size >= rules.CacheNotable {
			p.add(Fix{
				ID: "cache:" + c.id, Check: rules.CheckDisk, Command: c.command, Note: c.note, Action: c.title,
				Title: fmt.Sprintf("%s, about %s", c.title, rules.DiskSize(size)),
				Frees: size, Mount: mount,
			})
		}
	}

	if env.LookPath("docker") {
		data := map[string]string{
			"darwin":  filepath.Join(env.Home(), "Library", "Containers", "com.docker.docker"),
			"windows": filepath.Join(env.CacheDir(), "Docker"),
		}[env.OS()]
		if data == "" {
			data = "/var/lib/docker"
		}
		if mount, ok := on(data); ok {
			if size, ok := env.DockerBuildCache(ctx); ok && size >= rules.CacheNotable {
				p.add(Fix{
					ID: "cache:docker", Check: rules.CheckDisk, Action: "Clear Docker's build cache",
					Command: []string{"docker", "builder", "prune", "--force"},
					Title:   fmt.Sprintf("Clear Docker's build cache, about %s", rules.DiskSize(size)),
					Note:    "Images, containers and volumes are kept; builds are slower until it fills again.",
					Frees:   size, Mount: mount,
				})
			}
		}
	}

	p.manualDisk(ctx, on)
}

// manualDisk suggests what only the person should do: anything touching
// their own files, or needing administrator rights.
func (p *planner) manualDisk(ctx context.Context, on func(string) (string, bool)) {
	env := p.env
	home := env.Home()
	sized := func(dir string, min uint64) (string, uint64, bool) {
		mount, ok := on(dir)
		if !ok {
			return "", 0, false
		}
		size, ok := env.DirSize(ctx, dir)
		return mount, size, ok && size >= min
	}

	trash := ""
	switch env.OS() {
	case "darwin":
		trash = filepath.Join(home, ".Trash")
	case "windows":
	default:
		data := env.Getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
		trash = filepath.Join(data, "Trash")
	}
	if trash != "" {
		if mount, size, ok := sized(trash, rules.FolderNotable); ok {
			p.add(Fix{
				ID: "trash", Check: rules.CheckDisk, Manual: true, Mount: mount, Frees: size,
				Title: fmt.Sprintf("Empty the Trash, which holds %s", rules.DiskSize(size)),
				Note:  "Only if you don't need what's in it. lagdoc never deletes your files.",
			})
		}
	}

	downloads := filepath.Join(home, "Downloads")
	if mount, size, ok := sized(downloads, rules.FolderNotable); ok {
		f := Fix{
			ID: "downloads", Check: rules.CheckDisk, Manual: true, Mount: mount,
			Title: fmt.Sprintf("Look through Downloads, which holds %s", rules.DiskSize(size)),
			Note:  "Installers and archives you've already used are often safe to remove.",
		}
		for _, file := range env.LargestFiles(downloads, 3) {
			f.Steps = append(f.Steps, fmt.Sprintf("%s %s", file.Name, rules.DiskSize(file.Size)))
		}
		p.add(f)
	}

	switch env.OS() {
	case "linux":
		system := []struct {
			id, tool, dir, title, step string
			min                        uint64
		}{
			{"apt", "apt-get", "/var/cache/apt/archives", "Clear apt's downloaded packages", "sudo apt-get clean", rules.CacheNotable},
			{"dnf", "dnf", "/var/cache/dnf", "Clear dnf's downloaded packages", "sudo dnf clean packages", rules.CacheNotable},
			{"journal", "journalctl", "/var/log/journal", "Shrink the system journal", "sudo journalctl --vacuum-size=500M", rules.FolderNotable},
		}
		for _, s := range system {
			if !env.LookPath(s.tool) {
				continue
			}
			if mount, size, ok := sized(s.dir, s.min); ok {
				p.add(Fix{
					ID: s.id, Check: rules.CheckDisk, Manual: true, Mount: mount, Frees: size,
					Title: fmt.Sprintf("%s, about %s", s.title, rules.DiskSize(size)),
					Note:  "Needs administrator rights, so lagdoc won't run it for you.",
					Steps: []string{s.step},
				})
			}
		}
	case "windows":
		if mount, ok := on(home); ok {
			p.add(Fix{
				ID: "windows-storage", Check: rules.CheckDisk, Manual: true, Mount: mount,
				Title: "Remove temporary files in Settings",
				Note:  "Windows shows how much each kind would free before removing anything.",
				Steps: []string{"Settings → System → Storage → Temporary files"},
			})
		}
	}
}

func (p *planner) network() {
	n := p.s.Network
	dialProblem := n.DialErr != "" || n.Dial >= rules.DialWarn
	dnsProblem := n.DNSErr != "" || n.DNS >= rules.DNSWarn
	if dialProblem {
		p.add(Fix{
			ID: "network:check", Check: rules.CheckNetwork, Manual: true,
			Title: "Check the connection",
			Steps: []string{
				"Wi-Fi or the cable first, then restart the router",
				"If you use a VPN, try without it",
			},
		})
		return
	}
	if !dnsProblem {
		return
	}
	switch p.env.OS() {
	case "windows":
		p.add(Fix{
			ID: "dns:flush", Check: rules.CheckNetwork, Action: "Flush the DNS cache",
			Command: []string{p.env.SystemTool("ipconfig"), "/flushdns"},
			Title:   "Flush the DNS cache",
			Note:    "Forgets cached lookups, so names are looked up afresh.",
		})
	case "darwin":
		p.add(Fix{
			ID: "dns:flush", Check: rules.CheckNetwork, Manual: true,
			Title: "Flush the DNS cache",
			Note:  "Needs administrator rights, so lagdoc won't run it for you.",
			Steps: []string{"sudo dscacheutil -flushcache", "sudo killall -HUP mDNSResponder"},
		})
	default:
		if p.env.LookPath("resolvectl") {
			p.add(Fix{
				ID: "dns:flush", Check: rules.CheckNetwork, Manual: true,
				Title: "Flush the DNS cache",
				Note:  "It may ask for your password.",
				Steps: []string{"resolvectl flush-caches"},
			})
		}
	}
}
