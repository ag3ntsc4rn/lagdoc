package collect

import (
	"context"
	"errors"

	"github.com/shirou/gopsutil/v4/mem"
)

var errNoData = errors.New("no data")

func collectMemory(ctx context.Context) (Memory, error) {
	v, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return Memory{}, err
	}
	m := Memory{Total: v.Total, Available: v.Available}
	if sw, err := mem.SwapMemoryWithContext(ctx); err == nil {
		m.SwapTotal, m.SwapUsed = sw.Total, sw.Used
	}
	addPlatformMemory(&m) // memory_darwin.go / memory_other.go
	return m, nil
}
