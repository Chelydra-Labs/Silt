package types

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const validUserType = `name: Demo
description: A demo type
properties:
  - name: title
    type: text
    required: true
  - name: count
    type: number
`

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// TestTypeWatcher_AddModifyDelete drives a real fsnotify instance (the
// recommended approach for fsnotify tests: "drive a real fsnotify instance
// in tests rather than mocking") and verifies the onChange callback fires
// on external add/modify/delete of a type file.
func TestTypeWatcher_AddModifyDelete(t *testing.T) {
	dir := t.TempDir()
	typesDir := filepath.Join(dir, "types")
	// Pre-create the dir so the watcher observes it directly.
	writeTypeFile(t, typesDir, ".gitkeep", "")

	var mu sync.Mutex
	var callCount int
	changed := make(chan struct{}, 16)

	w, err := NewTypeWatcher(typesDir, func() {
		mu.Lock()
		callCount++
		mu.Unlock()
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("NewTypeWatcher: %v", err)
	}
	w.Start()
	defer w.Close()
	time.Sleep(100 * time.Millisecond)

	waitForTypeChange(t, changed, "add file", func() {
		writeTypeFile(t, typesDir, "demo.yaml", validUserType)
	})
	waitForTypeChange(t, changed, "modify file", func() {
		if err := writeFile(filepath.Join(typesDir, "demo.yaml"), validUserType+"\n# extra\n"); err != nil {
			t.Fatalf("modify: %v", err)
		}
	})
	waitForTypeChange(t, changed, "delete file", func() {
		_ = os.Remove(filepath.Join(typesDir, "demo.yaml"))
	})

	mu.Lock()
	if callCount < 3 {
		t.Errorf("expected at least 3 onChange calls, got %d", callCount)
	}
	mu.Unlock()
}

func TestTypeWatcher_SelfWriteSuppressed(t *testing.T) {
	dir := t.TempDir()
	typesDir := filepath.Join(dir, "types")
	writeTypeFile(t, typesDir, "demo.yaml", validUserType)

	changed := make(chan struct{}, 8)
	w, err := NewTypeWatcher(typesDir, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("NewTypeWatcher: %v", err)
	}
	w.Start()
	defer w.Close()

	w.RegisterSelfWrite()
	if err := writeFile(filepath.Join(typesDir, "demo.yaml"), validUserType+"\n# self\n"); err != nil {
		t.Fatalf("self-write: %v", err)
	}
	select {
	case <-changed:
		t.Error("self-write should be suppressed, but callback fired")
	case <-time.After(SelfWriteSuppressionTimeout):
		// Good — no callback within the suppression window.
	}
}

func TestTypeWatcher_UnregisterSelfWrite_ClearsWindow(t *testing.T) {
	dir := t.TempDir()
	typesDir := filepath.Join(dir, "types")
	writeTypeFile(t, typesDir, "demo.yaml", validUserType)

	changed := make(chan struct{}, 8)
	w, err := NewTypeWatcher(typesDir, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("NewTypeWatcher: %v", err)
	}
	w.Start()
	defer w.Close()

	// Arm then clear (a failed save's cleanup), then an external edit must land.
	w.RegisterSelfWrite()
	w.UnregisterSelfWrite()
	if err := writeFile(filepath.Join(typesDir, "demo.yaml"), validUserType+"\n# external\n"); err != nil {
		t.Fatalf("external write: %v", err)
	}
	select {
	case <-changed:
		// expected
	case <-time.After(SelfWriteSuppressionTimeout):
		t.Fatalf("external edit was suppressed — UnregisterSelfWrite did not clear the window")
	}
}

func TestTypeWatcher_IgnoresNonYaml(t *testing.T) {
	dir := t.TempDir()
	typesDir := filepath.Join(dir, "types")
	writeTypeFile(t, typesDir, ".gitkeep", "")

	changed := make(chan struct{}, 8)
	w, err := NewTypeWatcher(typesDir, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("NewTypeWatcher: %v", err)
	}
	w.Start()
	defer w.Close()
	time.Sleep(100 * time.Millisecond)

	// A .txt file must NOT trigger; a .yml file MUST.
	writeTypeFile(t, typesDir, "readme.txt", "not a type")
	select {
	case <-changed:
		t.Error("non-yaml file should not trigger callback")
	case <-time.After(500 * time.Millisecond):
		// Good.
	}

	waitForTypeChange(t, changed, "add .yml file", func() {
		writeTypeFile(t, typesDir, "alt.yml", validUserType)
	})
}

func TestTypeWatcher_Close(t *testing.T) {
	dir := t.TempDir()
	typesDir := filepath.Join(dir, "types")
	w, err := NewTypeWatcher(typesDir, func() {})
	if err != nil {
		t.Fatalf("NewTypeWatcher: %v", err)
	}
	w.Start()
	if err := w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	_ = w.Close() // double-close is safe
}

func TestIsTypeFile(t *testing.T) {
	w := &TypeWatcher{typesDir: filepath.Join("vault", ".system", "types")}
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join("vault", ".system", "types", "book.yaml"), true},
		{filepath.Join("vault", ".system", "types", "book.YML"), true},
		{filepath.Join("vault", ".system", "types", "readme.txt"), false},
		{filepath.Join("vault", ".system", "types", "sub", "x.yaml"), false}, // not directly in dir
	}
	for _, c := range cases {
		if got := w.isTypeFile(c.path); got != c.want {
			t.Errorf("isTypeFile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// waitForTypeChange performs an action that triggers a file-system change, then
// waits up to 2s for the watcher's onChange callback to fire. Mirrors
// templates.waitFor.
func waitForTypeChange(t *testing.T, ch <-chan struct{}, label string, action func()) {
	t.Helper()
	action()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("type watcher did not fire onChange for: %s", label)
	}
}
