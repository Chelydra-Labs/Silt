package types

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// selfWriteWindow is how long after RegisterSelfWrite the watcher treats type
// events as self-generated. It must cover a full logical save (atomic temp +
// rename), which can emit several fsnotify events — the window suppresses all
// of them. Mirrors templates.selfWriteWindow.
const selfWriteWindow = 500 * time.Millisecond

// reloadDebounce coalesces a burst of fsnotify events into a single reload, so
// an atomic save triggers one onChange rather than several. Mirrors
// templates.reloadDebounce.
const reloadDebounce = 120 * time.Millisecond

// SelfWriteSuppressionTimeout is the upper bound on how long a self-write
// suppression can be observed by external watchers — selfWriteWindow plus
// reloadDebounce, with a small safety margin. Tests that assert no callback
// fires during the suppression window should reference this constant rather
// than a hardcoded value. Mirrors templates.SelfWriteSuppressionTimeout.
const SelfWriteSuppressionTimeout = selfWriteWindow + reloadDebounce + 80*time.Millisecond

// TypeWatcher observes <vault>/.system/types/ for external add/modify/delete of
// type files and invokes onChange (which the App wires to InvalidateTypesCache
// + re-projection + a types:changed event) so the type set and every typed page
// stay live without a restart — the same hot-reload posture as the config,
// theme, and template engines.
//
// Self-loop prevention is a local time-window: the App calls RegisterSelfWrite
// immediately before SaveType's atomic write, and the watcher ignores type
// events for the window so Silt's own multi-event save cannot feed back into a
// reload. If the save fails, UnregisterSelfWrite clears the window so a failed
// write doesn't leave it open and silently drop a real external edit.
//
// The watcher observes typesDir directly when it exists. When it does not yet
// exist (no user types saved), it observes the .system parent so the eventual
// creation of types/ is detected and added to the watch — mirroring how the
// template watcher watches the .system parent. Mirrors templates.TemplateWatcher.
type TypeWatcher struct {
	typesDir  string
	parentDir string

	watcher *fsnotify.Watcher

	onChange func() // invoked (from the watcher goroutine) after a settled external change

	selfMu    sync.Mutex
	selfUntil time.Time // suppress reloads until this time after RegisterSelfWrite

	watchingDir bool // whether typesDir has been added to the fsnotify watch

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewTypeWatcher creates (but does not start) a watcher for the vault's types
// directory. onChange is invoked from the watcher goroutine and must be safe to
// call concurrently. The watcher observes typesDir if it exists, otherwise the
// .system parent (so creation of types/ is caught).
func NewTypeWatcher(typesDir string, onChange func()) (*TypeWatcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create fsnotify watcher: %w", err)
	}
	parent := filepath.Dir(typesDir)
	w := &TypeWatcher{
		typesDir:  typesDir,
		parentDir: parent,
		watcher:   fw,
		onChange:  onChange,
		stopCh:    make(chan struct{}),
	}
	// Observe whichever of typesDir / parent currently exists. Both may be
	// absent on a fresh vault — in that case the watcher starts idle and the
	// App re-creates it once the vault is initialized.
	watchingDir := false
	if _, statErr := os.Stat(typesDir); statErr == nil {
		if err := fw.Add(typesDir); err != nil {
			fw.Close()
			return nil, fmt.Errorf("watch %s: %w", typesDir, err)
		}
		watchingDir = true
	} else if _, statErr := os.Stat(parent); statErr == nil {
		if err := fw.Add(parent); err != nil {
			fw.Close()
			return nil, fmt.Errorf("watch %s: %w", parent, err)
		}
	}
	w.watchingDir = watchingDir
	return w, nil
}

// Start launches the background event loop.
func (w *TypeWatcher) Start() {
	w.wg.Add(1)
	go w.loop()
}

// RegisterSelfWrite records that Silt is about to write a type file itself, so
// the resulting fsnotify event(s) are treated as self-generated and ignored for
// selfWriteWindow. Must be called immediately before the write; pair with
// UnregisterSelfWrite on the write's error path. Mirrors
// templates.TemplateWatcher.RegisterSelfWrite.
func (w *TypeWatcher) RegisterSelfWrite() {
	w.selfMu.Lock()
	w.selfUntil = time.Now().Add(selfWriteWindow)
	w.selfMu.Unlock()
}

func (w *TypeWatcher) isSelfWrite() bool {
	w.selfMu.Lock()
	defer w.selfMu.Unlock()
	return time.Now().Before(w.selfUntil)
}

// UnregisterSelfWrite clears a self-write suppression window opened by
// RegisterSelfWrite. Call it when a save that armed the window fails, so a
// failed write does not leave the window open and silently drop a legitimate
// external edit. No-op if no window is open. Mirrors
// templates.TemplateWatcher.UnregisterSelfWrite.
func (w *TypeWatcher) UnregisterSelfWrite() {
	w.selfMu.Lock()
	w.selfUntil = time.Time{}
	w.selfMu.Unlock()
}

// Close stops the loop and closes the fsnotify watcher. Safe to call multiple
// times (stopOnce guarantees a single close of stopCh).
func (w *TypeWatcher) Close() error {
	w.stopOnce.Do(func() { close(w.stopCh) })
	err := w.watcher.Close()
	w.wg.Wait()
	return err
}

// isTypeFile reports whether an event path is a .yaml/.yml file directly inside
// typesDir (the only events the watcher reacts to). Path comparison uses
// EqualFold because case-insensitive filesystems can report the same path with
// different casing. Mirrors templates.TemplateWatcher.isTemplateFile.
func (w *TypeWatcher) isTypeFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".yaml" && ext != ".yml" {
		return false
	}
	return strings.EqualFold(filepath.Clean(filepath.Dir(name)), filepath.Clean(w.typesDir))
}

func (w *TypeWatcher) loop() {
	defer w.wg.Done()
	var debounce <-chan time.Time
	for {
		select {
		case <-w.stopCh:
			return
		case ev, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			// Detect the creation of the types directory itself (seen via the
			// parent watch) and add it to the fsnotify watcher so future
			// file-level events are observed.
			if !w.watchingDir && strings.EqualFold(filepath.Clean(ev.Name), filepath.Clean(w.typesDir)) {
				if ev.Op&(fsnotify.Create) != 0 {
					if err := w.watcher.Add(w.typesDir); err == nil {
						w.watchingDir = true
					}
				}
			}
			// Only react to type files directly inside typesDir.
			if !w.isTypeFile(ev.Name) {
				continue
			}
			// Ignore events Silt just produced itself.
			if w.isSelfWrite() {
				continue
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			debounce = time.After(reloadDebounce)
		case <-debounce:
			debounce = nil
			if w.onChange != nil {
				w.onChange()
			}
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			// Log so a persistent fsnotify failure is diagnosable rather than
			// silently disabling hot-reload. The watcher is still best-effort.
			if err != nil {
				log.Printf("types: watcher error: %v", err)
			}
		}
	}
}
