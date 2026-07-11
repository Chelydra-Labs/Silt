//go:build race

package parser

import "time"

// scanBudgetRegressionLimit returns the boot-scanner budget when the race
// detector is enabled. The detector adds ~2x overhead to the file-scan +
// parse workload (non-race baseline ~280ms → ~550ms under race on a fast
// dev machine). GitHub Actions runners are ~2x slower than a dev workstation
// for I/O-heavy workloads, and a shared runner under load can land a best-of-3
// at ~1.25s even with no code regression (observed: 1.34/1.52/1.25s). The gate
// is therefore scaled to 1600ms: ~60% headroom over the typical ~1.0s CI
// result so a noisy neighbor no longer flakes it, while a real 2x regression
// still lands at ~2.0s on CI — well above this threshold. The best-of-3
// sampling in the test further dampens single-run spikes.
func scanBudgetRegressionLimit() time.Duration {
	return 1600 * time.Millisecond
}
