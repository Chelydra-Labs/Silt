//go:build race

package parser

import "time"

// scanBudgetRegressionLimit returns the boot-scanner budget when the race
// detector is enabled. The detector adds ~2x overhead to the file-scan +
// parse workload (non-race baseline ~280ms → ~550ms under race on a fast
// dev machine). GitHub Actions runners are ~2x slower than a dev workstation
// for I/O-heavy workloads, and a shared runner under load can land a single
// best-of-N measurement well above the uncontended ~1.0s CI baseline — a bad
// runner day reached ~1.62s (2026-08-02, no code regression). The gate is
// scaled to 1800ms so that level of contention no longer flakes it, while a
// real 2x regression still lands at ~2.0s on CI — above this threshold. The
// best-of-5 sampling in the test further dampens single-run spikes.
func scanBudgetRegressionLimit() time.Duration {
	return 1800 * time.Millisecond
}
