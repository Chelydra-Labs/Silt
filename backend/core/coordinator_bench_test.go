package core

import (
	"fmt"
	"path/filepath"
	"testing"
)

// BenchmarkLockPathsWrite_N measures multi-path mutex acquire/release cost.
// Baseline (Windows amd64, 2026-07-19, empty task body):
//
//	N=10    ~2.6 µs
//	N=100   ~23 µs
//	N=1000  ~280 µs
//	N=5000  ~1.4 ms
//
// Still far below disk rename + inbound rewrite for large notebooks — keep
// exact-path LockPathsWrite (no hierarchical dir locks).
func BenchmarkLockPathsWrite_N(b *testing.B) {
	for _, n := range []int{10, 100, 1000, 5000} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			ec := NewExecutionCoordinator(nil)
			dir := b.TempDir()
			paths := make([]string, n)
			for i := 0; i < n; i++ {
				paths[i] = filepath.Join(dir, fmt.Sprintf("p%05d.md", i))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ec.LockPathsWrite(paths, func() {})
			}
		})
	}
}
