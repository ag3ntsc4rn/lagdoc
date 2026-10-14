//go:build !darwin

package collect

// addPlatformMemory has nothing to add here: Linux's MemAvailable and
// Windows's ullAvailPhys already account for reclaimable cache.
func addPlatformMemory(*Memory) {}
