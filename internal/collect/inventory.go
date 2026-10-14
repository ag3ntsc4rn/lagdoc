package collect

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Inventory is what's installed and what's set to start automatically. It
// changes rarely, so it isn't part of the verdict; it's kept in history so
// `lagdoc changes` can say what's new.
type Inventory struct {
	StartupItems []StartupItem `json:"startup_items"`
	Apps         []App         `json:"apps"`
	// Failed lists sources that exist but couldn't be read. Their items are
	// missing, so a comparison must not read them as removed.
	Failed []string `json:"failed,omitempty"`
}

// StartupItem is something the OS starts at login or boot: a launchd
// agent or daemon, an XDG autostart entry, an enabled systemd unit, a Run
// key value or a Startup folder shortcut.
type StartupItem struct {
	Name   string `json:"name"`
	Source string `json:"source"` // where it was found, e.g. "~/Library/LaunchAgents"
}

type App struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"` // empty when the platform doesn't say
	Source  string `json:"source"`
}

// ReadInventory reads startup items and installed apps from the places
// each platform keeps them (inventory_darwin.go, _linux.go, _windows.go).
// It only lists files and reads small metadata files; nothing is run.
func ReadInventory() Inventory {
	inv := readInventory()
	slices.SortFunc(inv.StartupItems, func(a, b StartupItem) int {
		return strings.Compare(a.Source+"\x00"+a.Name, b.Source+"\x00"+b.Name)
	})
	inv.StartupItems = slices.Compact(inv.StartupItems)
	inv.Apps = dedupeApps(inv.Apps)
	return inv
}

// dedupeApps keeps one app per name (the first source wins, as sources are
// listed system-wide first) and sorts by name.
func dedupeApps(as []App) []App {
	seen := map[string]bool{}
	var out []App
	for _, a := range as {
		if a.Name == "" || seen[a.Name] {
			continue
		}
		seen[a.Name] = true
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b App) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// inventoryBuilder collects items and remembers which sources failed.
type inventoryBuilder struct {
	inv Inventory
}

func (b *inventoryBuilder) startup(source string, names []string, err error) {
	if err != nil {
		b.inv.Failed = append(b.inv.Failed, source)
		return
	}
	for _, n := range names {
		b.inv.StartupItems = append(b.inv.StartupItems, StartupItem{Name: n, Source: source})
	}
}

func (b *inventoryBuilder) apps(source string, apps []App, err error) {
	if err != nil {
		b.inv.Failed = append(b.inv.Failed, source)
		return
	}
	for _, a := range apps {
		a.Source = source
		b.inv.Apps = append(b.inv.Apps, a)
	}
}

// readDir lists a directory. One that doesn't exist is empty, not a failure.
func readDir(dir string) ([]os.DirEntry, error) {
	es, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return es, err
}

// filesWithSuffix lists the names of visible entries ending in suffix, with
// the suffix removed.
func filesWithSuffix(dir, suffix string) ([]string, error) {
	es, err := readDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range es {
		n := e.Name()
		if strings.HasPrefix(n, ".") || !strings.HasSuffix(strings.ToLower(n), strings.ToLower(suffix)) {
			continue
		}
		out = append(out, n[:len(n)-len(suffix)])
	}
	return out, nil
}

// displayPath shortens a path under the home directory to "~/…".
func displayPath(p, home string) string {
	if home != "" {
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join("~", rel)
		}
	}
	return p
}

// maxMetadataFile caps how much of an Info.plist or .desktop file is read.
const maxMetadataFile = 1 << 20

func readSmallFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxMetadataFile+1))
	if err == nil && len(data) > maxMetadataFile {
		err = errors.New("file too large")
	}
	return data, err
}

// desktopEntry is the part of a freedesktop .desktop file lagdoc needs.
type desktopEntry struct {
	Name, Type        string
	Hidden, NoDisplay bool
}

// parseDesktopEntry reads the [Desktop Entry] group. Localised keys
// (Name[de]=…) are ignored, so the name is the untranslated one.
func parseDesktopEntry(data []byte) desktopEntry {
	var e desktopEntry
	in := false
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			in = line == "[Desktop Entry]"
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !in || !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "Name":
			e.Name = v
		case "Type":
			e.Type = v
		case "Hidden":
			e.Hidden = v == "true"
		case "NoDisplay":
			e.NoDisplay = v == "true"
		}
	}
	return e
}

// desktopEntries reads the .desktop files in dir. For autostart, an entry
// is listed unless Hidden (the spec's "deleted"); for app menus, NoDisplay
// entries are left out too, as menus don't show them.
func desktopEntries(dir string, apps bool) ([]string, error) {
	files, err := filesWithSuffix(dir, ".desktop")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		data, err := readSmallFile(filepath.Join(dir, f+".desktop"))
		if err != nil {
			continue // a dangling symlink or an odd file shouldn't fail the whole source
		}
		e := parseDesktopEntry(data)
		if e.Hidden || (e.Type != "" && e.Type != "Application") || (apps && e.NoDisplay) {
			continue
		}
		name := e.Name
		if name == "" || !apps {
			name = f // autostart entries are identified by file, which is stable
		}
		out = append(out, name)
	}
	return out, nil
}

// launchItems lists launchd job files (macOS LaunchAgents/LaunchDaemons).
func launchItems(dir string) ([]string, error) {
	return filesWithSuffix(dir, ".plist")
}

// systemdWants lists units enabled through the *.wants directories under
// dir (what `systemctl enable` creates).
func systemdWants(dir string) ([]string, error) {
	es, err := readDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range es {
		if !strings.HasSuffix(e.Name(), ".wants") {
			continue
		}
		units, err := readDir(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		for _, u := range units {
			if !strings.HasPrefix(u.Name(), ".") {
				out = append(out, u.Name())
			}
		}
	}
	return out, nil
}

// macApps lists *.app bundles in dir, and one folder down for apps that
// install into their own folder ("Adobe Photoshop 2026/…app").
func macApps(dir string) ([]App, error) {
	es, err := readDir(dir)
	if err != nil {
		return nil, err
	}
	var out []App
	for _, e := range es {
		n := e.Name()
		if strings.HasPrefix(n, ".") {
			continue
		}
		p := filepath.Join(dir, n)
		if strings.HasSuffix(n, ".app") {
			out = append(out, App{Name: strings.TrimSuffix(n, ".app"), Version: bundleVersion(p)})
			continue
		}
		if !e.IsDir() {
			continue
		}
		sub, err := readDir(p)
		if err != nil {
			continue
		}
		for _, s := range sub {
			if sn := s.Name(); strings.HasSuffix(sn, ".app") && !strings.HasPrefix(sn, ".") {
				out = append(out, App{Name: strings.TrimSuffix(sn, ".app"), Version: bundleVersion(filepath.Join(p, sn))})
			}
		}
	}
	return out, nil
}

// bundleVersion is an app bundle's user-facing version, or "".
func bundleVersion(app string) string {
	data, err := readSmallFile(filepath.Join(app, "Contents", "Info.plist"))
	if err != nil {
		return ""
	}
	if v := plistString(data, "CFBundleShortVersionString"); v != "" {
		return v
	}
	return plistString(data, "CFBundleVersion")
}

// startupFolder lists shortcuts in a Windows Startup folder.
func startupFolder(dir string) ([]string, error) {
	es, err := readDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range es {
		n := e.Name()
		if e.IsDir() || strings.EqualFold(n, "desktop.ini") || strings.HasPrefix(n, ".") {
			continue
		}
		if strings.EqualFold(filepath.Ext(n), ".lnk") {
			n = n[:len(n)-4]
		}
		out = append(out, n)
	}
	return out, nil
}
