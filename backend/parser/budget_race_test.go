//go:build race

package parser

import "time"

// scanBudgetRegressionLimit returns the boot-scanner budget when the race
// detector is enabled. The detector adds ~2x overhead to the file-scan +
// parse workload (non-race baseline ~280ms → ~550ms under race on a fast
// dev machine). GitHub Actions runners are ~2x slower than a dev workstation
// for I/O-heavy workloads, so a normal CI run lands at ~1.0s under race. The
// gate is scaled to 1200ms to stay green on CI without masking a real
// regression: a 2x slowdown from the baseline lands at ~1.1s on a dev box
// / ~2.0s on CI — well above this threshold in both environments.
func scanBudgetRegressionLimit() time.Duration {
	return 1200 * time.Millisecond
}
