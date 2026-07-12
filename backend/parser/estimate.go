package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// estimateTokenRe matches a duration like "30m", "2h", "2.5d", "1.5 H".
// Unit is required; bare numbers are rejected so typos fail loudly.
var estimateTokenRe = regexp.MustCompile(`(?i)^\s*(\d+(?:\.\d+)?)\s*([mhd])\s*$`)

// ParseEstimateMinutes converts a raw [estimate::] value to whole minutes.
// Units: m = minutes, h = hours (×60), d = work-days (×480 = 8h).
// ok is false for empty or invalid input (caller clears the token).
func ParseEstimateMinutes(raw string) (minutes int, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	m := estimateTokenRe.FindStringSubmatch(raw)
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil || n < 0 {
		return 0, false
	}
	// Zero is not a useful estimate — treat as invalid so callers clear the
	// token rather than storing an invisible [estimate:: 0m] on disk.
	if n == 0 {
		return 0, false
	}
	switch strings.ToLower(m[2]) {
	case "m":
		minutes = int(n + 0.5)
	case "h":
		minutes = int(n*60 + 0.5)
	case "d":
		// 8-hour work day for capacity planning (#439).
		minutes = int(n*480 + 0.5)
	default:
		return 0, false
	}
	if minutes <= 0 {
		return 0, false
	}
	return minutes, true
}

// FormatEstimateMinutes renders minutes as a compact duration for UI rollups
// and cache→display reconstruction (e.g. 90 → "1.5h", 480 → "1d", 1200 → "2.5d",
// 540 → "9h", 30 → "30m"). Prefer day units only for whole/half work-days so
// "2.5d" does not display as "20h", while "9h" does not become "1.125d".
// Keep in sync with frontend formatEstimateMinutes (types.ts).
func FormatEstimateMinutes(minutes int) string {
	if minutes <= 0 {
		return ""
	}
	// Whole or half work-days only (480m / 240m steps): 1d, 1.5d, 2d, 2.5d…
	if minutes >= 480 && minutes%240 == 0 {
		d := float64(minutes) / 480
		return fmt.Sprintf("%gd", d)
	}
	if minutes%60 == 0 {
		return fmt.Sprintf("%dh", minutes/60)
	}
	if minutes > 60 && minutes%30 == 0 {
		h := float64(minutes) / 60
		return fmt.Sprintf("%gh", h)
	}
	return fmt.Sprintf("%dm", minutes)
}
