package main

import (
	"os"
	"path/filepath"
	"testing"

	"silt/backend/core"
)

// BenchmarkSavePath_StatUnderLock isolates os.Stat cost inside a file lock,
// matching SaveFileBlocks / SavePageMarkdown. Baseline Windows amd64 2026-07-19:
//
//	with_stat  ~18 µs/op
//	lock_only  ~0.2 µs/op
//	stat_only  ~17 µs/op
//
// Stat dominates this microbench but remains negligible vs parse + atomic write
// + reindex on the autosave path. Keep fail-closed Stat (no gen-counter revival).
func BenchmarkSavePath_StatUnderLock(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "page.md")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		b.Fatal(err)
	}
	ec := core.NewExecutionCoordinator(nil)

	b.Run("with_stat", func(b *testing.B) {
		b.ReportAllocs()
		var sink error
		for i := 0; i < b.N; i++ {
			ec.LockFileWrite(path, func() {
				_, sink = os.Stat(path)
			})
		}
		_ = sink
	})
	b.Run("lock_only", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ec.LockFileWrite(path, func() {})
		}
	})
	b.Run("stat_only", func(b *testing.B) {
		b.ReportAllocs()
		var sink error
		for i := 0; i < b.N; i++ {
			_, sink = os.Stat(path)
		}
		_ = sink
	})
}
