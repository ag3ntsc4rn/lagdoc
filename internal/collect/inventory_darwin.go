package collect

import (
	"os"
	"path/filepath"
)

// macOS: launchd agents and daemons, and app bundles. Apple's own (under
// /System) aren't listed: they change only with OS updates. Login items
// registered through SMAppService live in a root-only database, so they're
// missing unless the app also installs a launchd plist (most updaters do).
func readInventory() Inventory {
	var b inventoryBuilder
	home, _ := os.UserHomeDir()
	dirs := []string{"/Library/LaunchDaemons", "/Library/LaunchAgents"}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, "Library", "LaunchAgents"))
	}
	for _, d := range dirs {
		names, err := launchItems(d)
		b.startup(displayPath(d, home), names, err)
	}
	dirs = []string{"/Applications"}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	for _, d := range dirs {
		apps, err := macApps(d)
		b.apps(displayPath(d, home), apps, err)
	}
	return b.inv
}
