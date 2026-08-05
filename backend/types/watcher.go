package types

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// selfWriteWindow is the upper bound on how long a RegisterSelfWrite arm
// stays valid if no fsnotify event ever arrives for the path (e.g. fsnotify
// dropped the event, or the write was suppressed by an OS-level dedupe). In
// the normal case the arm is consumed by the post-write event burst and
// revalidated long before this expires. Mirrors templates.selfWriteWindow.
const selfWriteWindow = 500 * time.Millisecond

// reloadDebounce coalesces a burst of fsnotify events into a single
// revalidation decision, so an atomic save (temp + rename → several events)
// collapses to one read-and-compare rather than several. Mirrors
// templates.reloadDebounce.
const reloadDebounce = 120 * time.Millisecond

// SelfWriteSuppressionTimeout is the upper bound on how long a self-write
// suppression can be observed by external watchers. With content-identity
// revalidation the decision lands at reloadDebounce after the post-write
// event arrives; in the worst case fsnotify delivers that event just before
// the arm expires (selfWriteWindow), so the bound stays selfWriteWindow +
// reloadDebounce plus a small safety margin. Tests that assert no callback
// fires during suppression should reference this constant rather than a
// hardcoded value. Mirrors templates.SelfWriteSuppressionTimeout.
const SelfWriteSuppressionTimeout = selfWriteWindow + reloadDebounce + 80*time.Millisecond

// TypeWatcher observes <vault>/.system/types/ for external add/modify/delete of
// type files and invokes onChange (which the App wires to InvalidateTypesCache
// + re-projection + a types:changed event) so the type set and every typed page
// stay live without a restart — the same hot-reload posture as the config,
// theme, and template engines.
//
// Self-loop prevention is content-identity based and path-scoped. The App
// calls RegisterSelfWrite(path, expectedBytes) immediately before SaveType's
// atomic write, recording the bytes that should be on disk afterwards (nil for
// a delete). The watcher does NOT drop the resulting fsnotify events outright
// — that would also drop a coincident external/sync edit to the same file.
// Instead it debounces the burst, reads the file once, and compares: a match is
// a confirmed self-write (suppressed, no feedback loop); a mismatch means an
// external edit landed on top of our write and onChange fires anyway. Events
// for other type files are never suppressed. If the save fails,
// UnregisterSelfWrite clears every armed path so a failed write doesn't leave
// a stale arm that silently suppresses a real external edit.
//
// Mirrors templates.TemplateWatcher (which still uses the older time-window
// model); the type watcher was upgraded to content-identity in #872.
type TypeWatcher struct {
	typesDir  string
	parentDir string

	watcher *fsnotify.Watcher

	onChange func() // invoked (from the watcher goroutine) after a settled external change

	// selfArmed maps a cleaned path to the bytes Silt expects on disk after
	// its own atomic write (nil = delete — file should be absent). Consumed
	// by the next debounced revalidation of that path; expires at
	// armedAt+selfWriteWindow as a safety net if fsnotify drops the event.
	selfMu    sync.Mutex
	selfArmed map[string]selfEntry

	// pending holds cleaned paths whose events are being coalesced by the
	// current debounce timer; flushed and reset on each debounce fire.
	pendingMu sync.Mutex
	pending   map[string]bool

	watchingDir bool // whether typesDir has been added to the fsnotify watch

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// selfEntry is one armed self-write: the bytes expected on disk after our
// atomic save (nil/empty arms a delete — the file should be absent), plus
// when the arm was raised so it can expire if fsnotify drops its event.
type selfEntry struct {
	expected []byte
	armedAt  time.Time
}

// NewTypeWatcher creates (but does not start) a watcher for the vault's types
// directory. onChange is invoked from the watcher goroutine and must be safe
// to call concurrently. The watcher observes typesDir if it exists, otherwise
// the .system parent (so creation of types/ is caught).
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
		selfArmed: make(map[string]selfEntry),
		pending:   make(map[string]bool),
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

// RegisterSelfWrite arms a content-identity check for path: Silt is about to
// write expected to that file (or delete it when expected is nil). The event
// burst is debounced, then the watcher reads the file once and compares — a
// match is a confirmed self-write (suppressed); a mismatch means an
// external/sync edit landed on top of our write and onChange fires anyway.
// Must be called immediately before the write; pair with UnregisterSelfWrite
// on the write's error path so a failed write does not leave a stale arm.
func (w *TypeWatcher) RegisterSelfWrite(path string, expected []byte) {
	if path == "" {
		return
	}
	w.selfMu.Lock()
	if w.selfArmed == nil {
		w.selfArmed = make(map[string]selfEntry)
	}
	w.selfArmed[filepath.Clean(path)] = selfEntry{expected: expected, armedAt: time.Now()}
	w.selfMu.Unlock()
}

// UnregisterSelfWrite clears every armed self-write entry. Prefer
// UnregisterSelfWritePath for single-file failures so a concurrent arm for a
// different file is not collateral damage (matters for RestoreExampleTypes'
// batched saves). No-op if no arm is open.
func (w *TypeWatcher) UnregisterSelfWrite() {
	w.selfMu.Lock()
	w.selfArmed = make(map[string]selfEntry)
	w.selfMu.Unlock()
}

// UnregisterSelfWritePath removes only the arm for path, leaving arms for
// other files intact. Use this on a single save/delete failure inside a
// batch (e.g. RestoreExampleTypes) so a coincident arm for a successfully
// saved sibling type is not cleared. No-op if path was not armed.
func (w *TypeWatcher) UnregisterSelfWritePath(path string) {
	if path == "" {
		return
	}
	clean := filepath.Clean(path)
	w.selfMu.Lock()
	if len(w.selfArmed) > 0 {
		for p := range w.selfArmed {
			if strings.EqualFold(p, clean) {
				delete(w.selfArmed, p)
				break
			}
		}
	}
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

// lookupEntry returns the live self-entry for clean (matched case-insensitively
// because case-insensitive filesystems can report a differently-cased path),
// the map key it was stored under (so the caller can delete it), and whether a
// live arm exists. Expired arms (armedAt older than selfWriteWindow) are
// pruned as a safety net for fsnotify dropping the post-write event. Caller
// holds selfMu.
func (w *TypeWatcher) lookupEntry(clean string, now time.Time) (selfEntry, string, bool) {
	if len(w.selfArmed) == 0 {
		return selfEntry{}, "", false
	}
	for p, e := range w.selfArmed {
		if now.Sub(e.armedAt) > selfWriteWindow {
			delete(w.selfArmed, p)
			continue
		}
		if strings.EqualFold(p, clean) {
			return e, p, true
		}
	}
	return selfEntry{}, "", false
}

// contentMatches reports whether the on-disk bytes at path equal expected.
// A nil/empty expected arms a delete: only a confirmed absence
// (os.IsNotExist) satisfies it — any other read error (permission denied,
// I/O, directory-in-place-of-file) fails loudly as a mismatch so the reload
// runs rather than silently suppressing a state we cannot verify.
func contentMatches(path string, expected []byte) bool {
	actual, err := os.ReadFile(path)
	if err != nil {
		return len(expected) == 0 && os.IsNotExist(err)
	}
	return bytes.Equal(actual, expected)
}

// flushPending runs the deferred revalidation for the just-settled event
// burst: each pending path is compared against its armed expectation. A match
// is a confirmed self-write (suppress + consume the arm); anything else (not
// armed, OR armed but content differs) invokes onChange. Reading once after
// the burst settles collapses an atomic save's full event train into a single
// comparison against the final bytes, and an external edit that races in is
// detected rather than silently suppressed. onChange fires at most once per
// burst.
func (w *TypeWatcher) flushPending() {
	w.pendingMu.Lock()
	pending := w.pending
	w.pending = make(map[string]bool)
	w.pendingMu.Unlock()
	if len(pending) == 0 {
		return
	}

	now := time.Now()
	needReload := false
	w.selfMu.Lock()
	for path := range pending {
		_, key, ok := w.lookupEntry(path, now)
		if !ok {
			// Not armed (or arm expired) → external event, always reload.
			needReload = true
			continue
		}
		entry := w.selfArmed[key]
		if contentMatches(path, entry.expected) {
			// Confirmed self-write: consume the arm, no reload.
			delete(w.selfArmed, key)
		} else {
			// External edit landed during the suppression window: drop the
			// stale arm and reload so the new content is picked up.
			delete(w.selfArmed, key)
			needReload = true
		}
	}
	w.selfMu.Unlock()

	if needReload && w.onChange != nil {
		w.onChange()
	}
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
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			// Record this path as part of the current burst and (re)arm
			// the timer. We do NOT drop self-write events here — that
			// would also drop a coincident external edit to the same
			// file. flushPending reads the settled bytes and compares.
			clean := filepath.Clean(ev.Name)
			w.pendingMu.Lock()
			w.pending[clean] = true
			w.pendingMu.Unlock()
			debounce = time.After(reloadDebounce)
		case <-debounce:
			debounce = nil
			w.flushPending()
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
