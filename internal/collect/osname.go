package collect

import "strings"

// osName turns gopsutil's platform fields into what people call their OS:
// "macOS 15.7.9", "Ubuntu 24.04", "Microsoft Windows 11 Pro".
func osName(platform, version string) string {
	switch platform {
	case "":
		return ""
	case "darwin":
		platform = "macOS"
	default:
		if !strings.Contains(platform, " ") {
			platform = strings.ToUpper(platform[:1]) + platform[1:]
		}
	}
	// Windows already puts the edition in platform; its version is a build
	// number nobody recognises.
	if version == "" || strings.HasPrefix(platform, "Microsoft") {
		return platform
	}
	return platform + " " + version
}
