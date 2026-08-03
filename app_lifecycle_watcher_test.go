package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"silt/backend/types"
)

// installGatedTypeWatcher attaches a real TypeWatcher to app whose onChange
// blocks on a test-controlled gate AND acquires vaultMu (mirroring the real
// vault_init.go handler). This is the exact MB-1 deadlock trigger: the watcher
// loop goroutine, while inside onChange needing vaultMu, cannot drain if
// CloseVault closes the watcher UNDER the teardown Lock. Returns the entered
// signal (onChange parked at the gate) and the release gate.
func installGatedTypeWatcher(t *testing.T, app *App) (entered, release chan struct{}) {
	t.Helper()
	typesDir := app.typesDir()
	if err := os.MkdirAll(typesDir, 0o755); err != nil {
		t.Fatalf("mkdir types dir: %v", err)
	}
	entered = make(chan struct{}, 1)
	release = make(chan struct{})
	w, err := types.NewTypeWatcher(typesDir, func() {
		// Signal that the loop goroutine has entered onChange and parked.
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		// Acquire vaultMu like the real handler — this is what deadlocks when
		// CloseVault holds the teardown Lock across the watcher's Close().
		app.vaultMu.Lock()
		app.vaultMu.Unlock()
	})
	if err != nil {
		t.Fatalf("NewTypeWatcher: %v", err)
	}
	w.Start()
	app.typeWatcher = w
	return entered, release
}

// TestCloseVault_DoesNotDeadlockWithTypeWatcher is the MB-1 regression test.
// The type watcher's onChange runs in its loop goroutine and takes vaultMu.Lock;
// CloseVault used to call typeWatcher.Close() (which wg.Waits the loop) UNDER the
// teardown Lock, so an in-flight onChange could never acquire it → deadlock. The
// fix closes the watcher OUTSIDE the lock (stopWatchersOutsideLock). On the buggy
// code the in-flight onChange can never acquire the teardown-held Lock, so
// CloseVault hangs; with the fix it returns.
func TestCloseVault_DoesNotDeadlockWithTypeWatcher(t *testing.T) {
	app := newTestApp(t)
	entered, release := installGatedTypeWatcher(t, app)
	// Safety net: always release the gate (idempotent) so a buggy code path
	// cannot wedge the test binary past the per-call timeout below.
	var releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }
	defer closeRelease()

	// Trigger one external type-file write so the watcher's onChange enters and
	// parks at <-release.
	if err := os.WriteFile(filepath.Join(app.typesDir(), "demo.yaml"),
		[]byte("name: Demo\nproperties:\n  - name: title\n    type: text\n"), 0o644); err != nil {
		t.Fatalf("write type file: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("type watcher onChange did not fire — fsnotify did not deliver the event")
	}

	// Start CloseVault. It will park in the watcher's Close (wg.Wait) until the
	// gate releases. Let it reach that point before releasing — a short wait is
	// ample since CloseVault's path to the watcher-close is a few ms (the
	// timeout below is the deterministic pass/fail oracle either way).
	done := make(chan error, 1)
	go func() { done <- app.CloseVault() }()
	time.Sleep(200 * time.Millisecond)

	// Release the gate: onChange proceeds to vaultMu.Lock(). On the buggy code
	// that Lock is held by CloseVault's teardown → onChange never returns →
	// wg.Wait never completes → CloseVault hangs → timeout fails the test. On
	// the fixed code the Lock is free (stopWatchersOutsideLock released it), so
	// onChange completes, the loop drains, and CloseVault returns.
	closeRelease()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("CloseVault: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CloseVault deadlocked — type watcher Close ran under the teardown Lock (MB-1)")
	}

	// Teardown ran: the watcher field is nil.
	app.vaultMu.RLock()
	twNil := app.typeWatcher == nil
	app.vaultMu.RUnlock()
	if !twNil {
		t.Error("typeWatcher should be nil after CloseVault")
	}
}

// TestCloseVault_ConcurrentTypeWrites_NoHang_NoRace is the -race stress loop:
// ~30 iterations of concurrent external type-file writes + CloseVault, each with
// a timeout. The watcher's onChange mirrors the real handler (InvalidateTypesCache
// + Lock + reproject + Unlock + emit), so any lock inversion or data race
// surfaces under -race.
func TestCloseVault_ConcurrentTypeWrites_NoHang_NoRace(t *testing.T) {
	const iters = 30
	for i := 0; i < iters; i++ {
		app := newTestApp(t)
		typesDir := app.typesDir()
		if err := os.MkdirAll(typesDir, 0o755); err != nil {
			t.Fatalf("mkdir types dir: %v", err)
		}
		// onChange mirrors the production handler at vault_init.go.
		w, err := types.NewTypeWatcher(typesDir, func() {
			types.InvalidateTypesCache()
			app.vaultMu.Lock()
			app.reprojectAllTypedPages()
			app.vaultMu.Unlock()
		})
		if err != nil {
			t.Fatalf("NewTypeWatcher: %v", err)
		}
		w.Start()
		app.typeWatcher = w

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				_ = os.WriteFile(filepath.Join(typesDir, "stress.yaml"),
					[]byte("name: S\nproperties:\n  - name: t\n    type: text\n"), 0o644)
			}
		}()

		done := make(chan error, 1)
		go func() { done <- app.CloseVault() }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("iter %d: CloseVault: %v", i, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("iter %d: CloseVault hung under concurrent type writes (MB-1)", i)
		}
		wg.Wait()
	}
}
