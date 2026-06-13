package parser

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestWriteFileAtomic_ConcurrentSafety(t *testing.T) {
	// Two concurrent writes to the same target path must not clobber each
	// other's temp files. Each writer must see its own content land on
	// disk, not the other writer's. The final content depends on which
	// os.Rename wins, but neither call should fail.
	dir := t.TempDir()
	target := filepath.Join(dir, "target.md")

	var wg sync.WaitGroup
	errs := make(chan error, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		content := []byte("writer-" + string(rune('A'+i)))
		go func() {
			defer wg.Done()
			if err := WriteFileAtomic(target, content); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("WriteFileAtomic returned an error under concurrency: %v", err)
	}

	// The file should exist and have non-empty content from one of the
	// writers. We don't care which one won the race — only that the file
	// is not truncated by a race on the temp path.
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("could not read target after concurrent write: %v", err)
	}
	if len(data) == 0 {
		t.Errorf("target file is empty after concurrent writes")
	}
}
