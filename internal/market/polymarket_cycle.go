package market

import (
	"strconv"
	"strings"
	"time"
)

// cycleStartFromSlug parses btc-updown-15m-<timestamp> and returns the timestamp as UTC time.
// If parsing fails, returns zero time.
func cycleStartFromSlug(slug string) time.Time {
	// Expected: btc-updown-15m-1766175300
	parts := strings.Split(slug, "-")
	if len(parts) < 4 {
		return time.Time{}
	}
	// last part is unix ts (seconds)
	tsStr := parts[len(parts)-1]
	sec, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil || sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

func (p *Polymarket) minTickSizeFloat() float64 {
	if p.minTickSize == "" {
		return 0
	}
	v, err := strconv.ParseFloat(p.minTickSize, 64)
	if err != nil {
		return 0
	}
	return v
}

