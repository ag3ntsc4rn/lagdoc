//go:build !darwin && !linux && !windows

package collect

func readInventory() Inventory { return Inventory{} }
