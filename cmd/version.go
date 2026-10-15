package cmd

import "runtime/debug"

// versionString is "0.1.0 (1b5e4a7)" for release builds, which goreleaser
// stamps through -ldflags. Other builds fall back to what Go recorded:
// the module version for `go install …@v0.1.0`, the commit for a checkout.
func versionString() string {
	v, c := resolveVersion()
	return v + " (" + c + ")"
}

func resolveVersion() (string, string) {
	v, c := version, commit
	if info, ok := debug.ReadBuildInfo(); ok {
		v, c = fromBuildInfo(info, v, c)
	}
	return v, c
}

func fromBuildInfo(info *debug.BuildInfo, v, c string) (string, string) {
	if v == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		v = info.Main.Version
	}
	if c == "none" {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				c = s.Value[:7]
			}
		}
	}
	return v, c
}
