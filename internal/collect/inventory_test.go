package collect

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPlistStringXML(t *testing.T) {
	data := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
	<key>CFBundleVersion</key>
	<string>1234</string>
	<key>CFBundleShortVersionString</key>
	<string> 3.1 &amp; up </string>
</dict></plist>`)
	if got := plistString(data, "CFBundleShortVersionString"); got != "3.1 & up" {
		t.Errorf("got %q", got)
	}
	if got := plistString(data, "Missing"); got != "" {
		t.Errorf("missing key gave %q", got)
	}
}

func TestPlistStringBinary(t *testing.T) {
	// Made with Python's plistlib (FMT_BINARY).
	data, err := os.ReadFile("testdata/Info.binary.plist")
	if err != nil {
		t.Fatal(err)
	}
	if got := plistString(data, "CFBundleShortVersionString"); got != "2.5.20" {
		t.Errorf("ASCII string: got %q", got)
	}
	if got := plistString(data, "CFBundleName"); got != "Café" {
		t.Errorf("UTF-16 string: got %q", got)
	}
	if got := plistString(data, "Nope"); got != "" {
		t.Errorf("missing key gave %q", got)
	}
	data, err = os.ReadFile("testdata/Info.utf16.plist")
	if err != nil {
		t.Fatal(err)
	}
	if got := plistString(data, "CFBundleShortVersionString"); got != "版本 1.0" {
		t.Errorf("UTF-16 value: got %q", got)
	}
}

func TestPlistStringBinaryCorrupt(t *testing.T) {
	data, err := os.ReadFile("testdata/Info.binary.plist")
	if err != nil {
		t.Fatal(err)
	}
	// Every truncation and every single-byte corruption must be survived.
	for i := range len(data) {
		plistString(data[:i], "CFBundleShortVersionString")
		for _, v := range []byte{0x00, 0xFF, 0x5F, 0xDF} {
			bad := slices.Clone(data)
			bad[i] = v
			plistString(bad, "CFBundleShortVersionString")
		}
	}
}

func TestParseDesktopEntry(t *testing.T) {
	e := parseDesktopEntry([]byte(`# comment
[Desktop Entry]
Type=Application
Name[de]=Feuerfuchs
Name = Firefox
NoDisplay=true

[Desktop Action new-window]
Name=New Window
Hidden=true
`))
	if e.Name != "Firefox" || e.Type != "Application" || !e.NoDisplay || e.Hidden {
		t.Errorf("got %+v", e)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDesktopEntries(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "firefox.desktop"), "[Desktop Entry]\nType=Application\nName=Firefox\n")
	write(t, filepath.Join(dir, "helper.desktop"), "[Desktop Entry]\nType=Application\nName=Helper\nNoDisplay=true\n")
	write(t, filepath.Join(dir, "gone.desktop"), "[Desktop Entry]\nType=Application\nName=Gone\nHidden=true\n")
	write(t, filepath.Join(dir, "link.desktop"), "[Desktop Entry]\nType=Link\nName=A link\n")
	write(t, filepath.Join(dir, "README"), "not an entry")

	apps, err := desktopEntries(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(apps, []string{"Firefox"}) {
		t.Errorf("apps: %v", apps)
	}
	// Autostart lists NoDisplay entries too (they still start) by file name.
	auto, _ := desktopEntries(dir, false)
	slices.Sort(auto)
	if !slices.Equal(auto, []string{"firefox", "helper"}) {
		t.Errorf("autostart: %v", auto)
	}

	none, err := desktopEntries(filepath.Join(dir, "missing"), true)
	if err != nil || none != nil {
		t.Errorf("a missing directory is empty, not an error: %v %v", none, err)
	}
}

func TestLaunchItemsAndSystemd(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "com.example.agent.plist"), "")
	write(t, filepath.Join(dir, ".hidden.plist"), "")
	write(t, filepath.Join(dir, "notes.txt"), "")
	got, err := launchItems(dir)
	if err != nil || !slices.Equal(got, []string{"com.example.agent"}) {
		t.Errorf("launch items: %v %v", got, err)
	}

	sd := t.TempDir()
	write(t, filepath.Join(sd, "multi-user.target.wants", "ssh.service"), "")
	write(t, filepath.Join(sd, "timers.target.wants", "backup.timer"), "")
	write(t, filepath.Join(sd, "custom.service"), "") // defined but not enabled
	units, err := systemdWants(sd)
	slices.Sort(units)
	if err != nil || !slices.Equal(units, []string{"backup.timer", "ssh.service"}) {
		t.Errorf("systemd: %v %v", units, err)
	}
}

func TestMacApps(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "Editor.app", "Contents", "Info.plist"),
		"<plist><dict><key>CFBundleShortVersionString</key><string>2.0</string></dict></plist>")
	bin, err := os.ReadFile("testdata/Info.binary.plist")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "Suite 2026", "Suite Draw.app", "Contents", "Info.plist"), string(bin))
	if err := os.MkdirAll(filepath.Join(dir, "NoPlist.app"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, ".localized"), "")

	apps, err := macApps(dir)
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(apps, func(a, b App) int { return strings.Compare(a.Name, b.Name) })
	want := []App{{Name: "Editor", Version: "2.0"}, {Name: "NoPlist"}, {Name: "Suite Draw", Version: "2.5.20"}}
	if !slices.Equal(apps, want) {
		t.Errorf("got %+v", apps)
	}
}

func TestStartupFolder(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "Dropbox.lnk"), "")
	write(t, filepath.Join(dir, "desktop.ini"), "")
	write(t, filepath.Join(dir, "run.bat"), "")
	got, err := startupFolder(dir)
	slices.Sort(got)
	if err != nil || !slices.Equal(got, []string{"Dropbox", "run.bat"}) {
		t.Errorf("got %v %v", got, err)
	}
}

func TestInventoryBuilderRecordsFailures(t *testing.T) {
	var b inventoryBuilder
	b.startup("/ok", []string{"a"}, nil)
	b.startup("/denied", nil, os.ErrPermission)
	b.apps("/Applications", []App{{Name: "X"}}, nil)
	if len(b.inv.StartupItems) != 1 || b.inv.StartupItems[0].Source != "/ok" {
		t.Errorf("startup: %+v", b.inv.StartupItems)
	}
	if b.inv.Apps[0].Source != "/Applications" {
		t.Errorf("apps get their source: %+v", b.inv.Apps)
	}
	if !slices.Equal(b.inv.Failed, []string{"/denied"}) {
		t.Errorf("failed: %v", b.inv.Failed)
	}
}

func TestDedupeApps(t *testing.T) {
	got := dedupeApps([]App{{Name: "b", Source: "/sys"}, {Name: "a"}, {Name: "b", Source: "/home"}, {Name: ""}})
	want := []App{{Name: "a"}, {Name: "b", Source: "/sys"}}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v", got)
	}
}

func TestDisplayPath(t *testing.T) {
	home := filepath.Join(string(filepath.Separator)+"home", "me")
	if got := displayPath(filepath.Join(home, ".config", "autostart"), home); got != filepath.Join("~", ".config", "autostart") {
		t.Errorf("got %s", got)
	}
	if got := displayPath(filepath.Join(string(filepath.Separator)+"etc", "xdg"), home); got != filepath.Join(string(filepath.Separator)+"etc", "xdg") {
		t.Errorf("got %s", got)
	}
}

func TestReadInventoryRuns(t *testing.T) {
	inv := ReadInventory()
	if !slices.IsSortedFunc(inv.Apps, func(a, b App) int { return strings.Compare(a.Name, b.Name) }) {
		t.Error("apps should be sorted by name")
	}
}
