package history

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseSince reads how old a baseline should be: a Go duration, or whole
// days as "7d".
func ParseSince(s string) (time.Duration, error) {
	if n, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.Atoi(n)
		if err == nil && days >= 0 {
			return time.Duration(days) * 24 * time.Hour, nil
		}
	} else if d, err := time.ParseDuration(s); err == nil && d >= 0 {
		return d, nil
	}
	return 0, fmt.Errorf("since %q: use a length of time like 30m, 6h or 7d", s)
}
