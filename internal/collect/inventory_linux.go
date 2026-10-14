package collect

import (
	"os"
	"path/filepath"
)

// Linux: XDG autostart entries and enabled systemd units start things;
// desktop entries are the apps a person would recognise. Packages (dpkg,
// rpm, pacman) aren't listed: an ordinary upgrade changes hundreds.
func readInventory() Inventory {
	var b inventoryBuilder
	home, _ := os.UserHomeDir()
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" && home != "" {
		config = filepath.Join(home, ".config")
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" && home != "" {
		data = filepath.Join(home, ".local", "share")
	}

	autostart := []string{"/etc/xdg/autostart"}
	if config != "" {
		autostart = append(autostart, filepath.Join(config, "autostart"))
	}
	for _, d := range autostart {
		names, err := desktopEntries(d, false)
		b.startup(displayPath(d, home), names, err)
	}
	units := []string{"/etc/systemd/system", "/etc/systemd/user"}
	if config != "" {
		units = append(units, filepath.Join(config, "systemd", "user"))
	}
	for _, d := range units {
		names, err := systemdWants(d)
		b.startup(displayPath(d, home), names, err)
	}

	apps := []string{
		"/usr/share/applications", "/usr/local/share/applications",
		"/var/lib/flatpak/exports/share/applications", "/var/lib/snapd/desktop/applications",
	}
	if data != "" {
		apps = append(apps,
			filepath.Join(data, "applications"),
			filepath.Join(data, "flatpak", "exports", "share", "applications"))
	}
	for _, d := range apps {
		names, err := desktopEntries(d, true)
		var as []App
		for _, n := range names {
			as = append(as, App{Name: n})
		}
		b.apps(displayPath(d, home), as, err)
	}
	return b.inv
}
