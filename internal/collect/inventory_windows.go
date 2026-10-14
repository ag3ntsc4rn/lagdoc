package collect

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// Windows: Run keys and Startup folders start things at login; the
// Uninstall keys are what "Installed apps" in Settings lists. Scheduled
// tasks and services aren't read.
func readInventory() Inventory {
	var b inventoryBuilder
	for _, k := range runKeys {
		names, err := registryValueNames(k.root, k.path)
		b.startup(k.label, names, err)
	}
	home, _ := os.UserHomeDir()
	var folders []string
	if v := os.Getenv("APPDATA"); v != "" {
		folders = append(folders, filepath.Join(v, `Microsoft\Windows\Start Menu\Programs\Startup`))
	}
	if v := os.Getenv("ProgramData"); v != "" {
		folders = append(folders, filepath.Join(v, `Microsoft\Windows\Start Menu\Programs\StartUp`))
	}
	for _, d := range folders {
		names, err := startupFolder(d)
		b.startup(displayPath(d, home), names, err)
	}
	for _, k := range uninstallKeys {
		apps, err := uninstallEntries(k.root, k.path)
		b.apps(k.label, apps, err)
	}
	return b.inv
}

type regKey struct {
	root  registry.Key
	path  string
	label string
}

const (
	runPath       = `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`
	runPath32     = `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Run`
	uninstallPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`
	uninstall32   = `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`
)

var runKeys = []regKey{
	{registry.LOCAL_MACHINE, runPath, `HKLM\` + runPath},
	{registry.LOCAL_MACHINE, runPath32, `HKLM\` + runPath32},
	{registry.CURRENT_USER, runPath, `HKCU\` + runPath},
}

var uninstallKeys = []regKey{
	{registry.LOCAL_MACHINE, uninstallPath, `HKLM\` + uninstallPath},
	{registry.LOCAL_MACHINE, uninstall32, `HKLM\` + uninstall32},
	{registry.CURRENT_USER, uninstallPath, `HKCU\` + uninstallPath},
}

// registryValueNames lists a key's value names. A missing key is empty.
func registryValueNames(root registry.Key, path string) ([]string, error) {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err == registry.ErrNotExist {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	defer k.Close()
	return k.ReadValueNames(0)
}

// uninstallEntries lists apps the way Settings does: entries with a
// DisplayName that aren't system components or updates of another entry.
func uninstallEntries(root registry.Key, path string) ([]App, error) {
	k, err := registry.OpenKey(root, path, registry.ENUMERATE_SUB_KEYS)
	if err == registry.ErrNotExist {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	defer k.Close()
	subs, err := k.ReadSubKeyNames(0)
	if err != nil {
		return nil, err
	}
	var out []App
	for _, s := range subs {
		sk, err := registry.OpenKey(k, s, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		name, _, _ := sk.GetStringValue("DisplayName")
		version, _, _ := sk.GetStringValue("DisplayVersion")
		system, _, _ := sk.GetIntegerValue("SystemComponent")
		_, _, parentErr := sk.GetStringValue("ParentKeyName")
		sk.Close()
		if name == "" || system == 1 || parentErr == nil {
			continue
		}
		out = append(out, App{Name: name, Version: version})
	}
	return out, nil
}
