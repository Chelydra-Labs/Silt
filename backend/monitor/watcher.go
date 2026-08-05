package monitor

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"silt/backend/core"
	"silt/backend/db"
	"silt/backend/parser"

	"github.com/fsnotify/fsnotify"
)

// DefaultFocusLeaseTTL is how long a focus lease stays valid without a refresh.
// Picked well above any reasonable editor pause (typing/thinking) and the
// auto-save debounce (500ms) so a normally-focused editor never lets its lease
// lapse, while an editor that crashed / unmounted without releasing self-heals
// within a minute (#38).
const DefaultFocusLeaseTTL = 60 * time.Second

type DirectoryWatcher struct {
	watcher      *fsnotify.Watcher
	vaultPath    string
	dm           *db.DatabaseManager
	tracker      *WriteTracker
	coordinator  *core.ExecutionCoordinator
	spacesPerTab int
	closeChan    chan struct{}

	// wg tracks the goroutines Start spawns (listenLoop + lease sweeper).
	// Close drains it so an in-flight reindex finishes before the caller
	// closes the DB — otherwise the reindex goroutine races/null-derefs
	// the DB handle on shutdown and vault-switch.
	wg sync.WaitGroup

	// closeOnce makes Close idempotent: closeChan panics on a second close,
	// and Close now does real drain work (wg.Wait), so a future "stop on
	// error then close on shutdown" caller must not trip that.
	closeOnce sync.Once
	closeErr  error

	failedMu    sync.Mutex
	failedPaths []string

	// Focus suppression uses TTL leases instead of a plain boolean (#38). The
	// Svelte editor acquires on focus, refreshes via a heartbeat while focused,
	// and releases on blur. If the component unmounts without releasing (route
	// change, crash, hot-reload) the lease expires and the background sweeper
	// drops it, so fsnotify suppression self-heals instead of leaking forever.
	focusMu     sync.RWMutex
	focusLeases map[string]time.Time // path -> lease expiry
	focusTTL    time.Duration

	// Multi-root support (#100): the watcher observes the vault root PLUS any
	// number of linked (external) notebook roots. rootInfo maps each watched
	// root to its source + (for linked roots) the notebook display name, so
	// resolveFileMetadata can attribute an event to the right notebook without
	// consulting the registry on every fsnotify event. The vault root's
	// notebook is "" because a vault holds MANY notebooks (derived per-file
	// from the first path component); a linked root IS one notebook.
	rootMu   sync.RWMutex
	roots    []string
	rootInfo map[string]watchRoot

	// linkedConfigHandler is invoked when a linked notebook's co-located
	// <root>/.system/config.yaml changes (#133), so App can drop the cache
	// entry and emit a linked-config:changed event. The argument is the
	// source ('linked:<id>'). Optional: if nil, co-located config events are
	// a no-op (the watcher still observes them but does not act). Set via
	// SetLinkedConfigHandler. Guarded by linkedConfigHandlerMu.
	linkedConfigHandlerMu sync.RWMutex
	linkedConfigHandler   func(source string)

	// reMintWarningHandler is invoked when a re-parse of a previously-indexed
	// file mints far more block ids than expected (#443) — the signature of an
	// external tool/sync stripping `<!-- id: ... -->` comments, which silently
	// breaks every ((uuid)) reference to those blocks. Optional: if nil, the
	// watcher still logs the anomaly but does not surface it to the UI. Set
	// via SetReMintWarningHandler. Guarded by reMintWarningHandlerMu. Called
	// from the watcher goroutine.
	reMintWarningHandlerMu sync.RWMutex
	reMintWarningHandler   func(ReMintWarning)

	// pageChangedHandler notifies the App after an external fsnotify reindex
	// or clear so plugins (e.g. silt-ai-qa vector index) can stay in sync.
	// Optional. Called from the watcher goroutine with notebook/section/page.
	pageChangedHandlerMu sync.RWMutex
	pageChangedHandler   func(notebook, section, page string)

	// atomicReindexHandler is the App-supplied atomic block+projection
	// publish. When nil, reindexFile falls back to defaultAtomicReindex
	// (still atomic, but with an empty projection payload). Production
	// (initializeVaultServices) installs a schema-aware closure over
	// App.indexFile so external frontmatter edits publish blocks AND
	// page_types/page_properties in one transaction — closing the gap left
	// by the prior block-only IndexFileBlocks path.
	atomicReindexHandlerMu sync.RWMutex
	atomicReindexHandler   AtomicReindexFunc
}

// AtomicReindexFunc is the App-supplied atomic block+projection publish the
// watcher delegates the index step of reindexFile to. The handler MUST do
// its own coordinator.WithDBWrite internally — the watcher does not wrap
// the call (WithDBWrite is non-reentrant). meta carries the parsed
// frontmatter; the handler computes typeID + props from it using the live
// type schema. A non-nil return skips MarkFileIndexed for this event.
type AtomicReindexFunc func(source, notebook, section, page string, blocks []parser.ParsedBlock, meta parser.FileMetadata) error

// ReMintWarning is the payload handed to the reMintWarningHandler when the
// watcher's mass-re-mint heuristic fires (#443). Carries enough context for
// the UI to name the affected page and explain the recovery path.
type ReMintWarning struct {
	Path        string `json:"path"`
	Source      string `json:"source"`
	Notebook    string `json:"notebook"`
	Section     string `json:"section"`
	Page        string `json:"page"`
	MintedCount int    `json:"minted_count"` // freshly-minted ids this parse
	PriorCount  int    `json:"prior_count"`  // managed blocks before re-parse
}

// watchRoot records how to interpret paths beneath a watched root.
type watchRoot struct {
	source   string // 'vault' or 'linked:<id>'
	notebook string // linked: display name; vault: "" (derived per-file)
}

func NewDirectoryWatcher(vaultPath string, dm *db.DatabaseManager, tracker *WriteTracker, coordinator *core.ExecutionCoordinator, spacesPerTab int) (*DirectoryWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	cleanVault := filepath.Clean(vaultPath)
	return &DirectoryWatcher{
		watcher:      watcher,
		vaultPath:    vaultPath,
		dm:           dm,
		tracker:      tracker,
		coordinator:  coordinator,
		spacesPerTab: spacesPerTab,
		closeChan:    make(chan struct{}),
		focusLeases:  make(map[string]time.Time),
		focusTTL:     DefaultFocusLeaseTTL,
		// The vault root is registered up-front so resolveFileMetadata works
		// even before Start() (tests exercise reindexFile directly). Its
		// notebook is "" because a vault holds many notebooks (derived
		// per-file from the first path component).
		roots:    []string{cleanVault},
		rootInfo: map[string]watchRoot{cleanVault: {source: "vault", notebook: ""}},
	}, nil
}

// LockFocus acquires (or re-acquires) a focus lease for path, suppressing
// fsnotify-driven reindexes while the editor is focused on the file.
func (dw *DirectoryWatcher) LockFocus(path string) {
	dw.focusMu.Lock()
	defer dw.focusMu.Unlock()
	if dw.focusLeases == nil {
		dw.focusLeases = make(map[string]time.Time)
	}
	dw.focusLeases[filepath.Clean(path)] = time.Now().Add(dw.focusTTL)
}

// RefreshFocus extends an existing lease. Called by the Svelte editor's
// heartbeat while it stays focused, and on save. A no-op if there is no lease
// OR the lease already expired (an expired-but-not-yet-reaped entry is treated
// as gone, so a late heartbeat can't resurrect suppression — the editor must
// re-Acquire). This matches IsFocusLocked's expiry semantics.
func (dw *DirectoryWatcher) RefreshFocus(path string) {
	dw.focusMu.Lock()
	defer dw.focusMu.Unlock()
	if dw.focusLeases == nil {
		return
	}
	key := filepath.Clean(path)
	expiry, ok := dw.focusLeases[key]
	if !ok {
		return
	}
	if !time.Now().Before(expiry) {
		// Expired: reap it now rather than refresh, so the editor can't
		// silently hold suppression past a crash/unmount.
		delete(dw.focusLeases, key)
		return
	}
	dw.focusLeases[key] = time.Now().Add(dw.focusTTL)
}

func (dw *DirectoryWatcher) UnlockFocus(path string) {
	dw.focusMu.Lock()
	defer dw.focusMu.Unlock()
	if dw.focusLeases != nil {
		delete(dw.focusLeases, filepath.Clean(path))
	}
}

func (dw *DirectoryWatcher) IsFocusLocked(path string) bool {
	dw.focusMu.RLock()
	defer dw.focusMu.RUnlock()
	if dw.focusLeases == nil {
		return false
	}
	expiry, ok := dw.focusLeases[filepath.Clean(path)]
	if !ok {
		return false
	}
	// An expired lease reads as unlocked; the sweeper reaps it shortly. This
	// keeps IsFocusLocked correct even between sweeper ticks.
	return time.Now().Before(expiry)
}

// ReleaseAllFocus clears every outstanding focus lease. Called on shutdown so
// a clean exit can't strand a file under suppression, and on CloseVault.
func (dw *DirectoryWatcher) ReleaseAllFocus() {
	dw.focusMu.Lock()
	defer dw.focusMu.Unlock()
	dw.focusLeases = make(map[string]time.Time)
}

// startLeaseSweeper runs a background goroutine that drops expired focus
// leases every ttl/2 so a crashed/unmounted editor self-heals. Stopped by
// closeChan (closed in Close).
func (dw *DirectoryWatcher) startLeaseSweeper() {
	interval := dw.focusTTL / 2
	if interval <= 0 {
		interval = 30 * time.Second
	}
	dw.wg.Add(1)
	go func() {
		defer dw.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				dw.sweepExpiredLeases()
			case <-dw.closeChan:
				return
			}
		}
	}()
}

func (dw *DirectoryWatcher) sweepExpiredLeases() {
	now := time.Now()
	dw.focusMu.Lock()
	defer dw.focusMu.Unlock()
	for path, expiry := range dw.focusLeases {
		if !now.Before(expiry) {
			delete(dw.focusLeases, path)
		}
	}
}

func (dw *DirectoryWatcher) Close() error {
	dw.closeOnce.Do(func() {
		close(dw.closeChan)
		dw.closeErr = dw.watcher.Close()
		// Join the listenLoop + lease sweeper so an in-flight reindex (which
		// reads/writes the DB) finishes before we return. Without this the
		// caller's subsequent db.Close() races and nil-derefs the handle — the
		// shutdown/vault-switch bug the -race detector caught.
		dw.wg.Wait()
	})
	return dw.closeErr
}

func (dw *DirectoryWatcher) AddRecursive(path string) error {
	return filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			// Skip system and hidden directories
			if strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			// Skip the attachments/ directory (#101): it holds binary assets,
			// not markdown pages. Watching it wastes fds and could index stray
			// .md files dropped there by the user.
			if strings.EqualFold(name, "attachments") {
				return filepath.SkipDir
			}
			if err := dw.watcher.Add(p); err != nil {
				dw.failedMu.Lock()
				dw.failedPaths = append(dw.failedPaths, fmt.Sprintf("%s: %v", p, err))
				dw.failedMu.Unlock()
				return fmt.Errorf("failed to add path to watcher %s: %w", p, err)
			}
		} else if d.Type()&fs.ModeSymlink != 0 {
			// Explicit symlink skip: WalkDir already does not follow them, but
			// we short-circuit here so a symlinked directory is never added to
			// the watch set (#32 — matches the scanner's WalkMarkdown).
			return nil
		}
		return nil
	})
}

// FailedPaths returns a copy of the list of paths that the watcher could
// not subscribe to (fsnotify limits, permissions, removed during
// traversal, etc.). A non-empty slice means these subtrees are not being
// monitored.
func (dw *DirectoryWatcher) FailedPaths() []string {
	dw.failedMu.Lock()
	defer dw.failedMu.Unlock()
	return append([]string(nil), dw.failedPaths...)
}

func (dw *DirectoryWatcher) Start() error {
	// The vault root is registered in the constructor; just watch it.
	if err := dw.AddRecursive(dw.vaultPath); err != nil {
		return err
	}

	// Standalone-tasks watcher carve-out (#372).
	//
	// AddRecursive skips dot-prefixed directories by design — that's what
	// keeps `.system/`, `.silt/`, and user dot-folders out of the watcher
	// (and out of the index — see also WalkMarkdown's matching skip in
	// backend/parser/scanner.go:75). But the standalone-tasks file
	// (#368) lives at <vault>/.silt/tasks.md, a single well-known file
	// inside that skipped directory. Without targeted observation an
	// external edit (sync conflict, manual edit, non-Silt editor) to that
	// file is invisible until the next cold start. Recovery still
	// round-trips through markdown so this is a parity gap, not a data-
	// loss bug — but every other `.md` file in the vault gets
	// incremental re-index on external change, and a focused watch on
	// the parent `.silt/` directory closes the gap without generalizing
	// the dot-prefix skip (which risks reintroducing the .system index
	// feedback loop the skip was added to prevent).
	//
	// Strategy: watch `<vault>/.silt/` so any Create/Write/Remove on
	// `.silt/tasks.md` arrives via the existing listen loop. The loop's
	// `.md` filter, focus-lock check, WriteTracker self-write
	// suppression, and `reindexFile` path are reused verbatim — none of
	// them need to know about the synthetic notebook. `resolveFileMetadata`
	// already derives notebook=".silt", section="", page="tasks" from
	// the path (same path ScanStandaloneTasks uses on cold start), so
	// the indexer needs no changes either.
	//
	// Guarded on `.silt/` existing at start time. If the directory is
	// created mid-session by a non-Silt tool, the cold-start
	// ScanStandaloneTasks on the next launch picks it up. The watcher
	// on the dot-prefix-skip-path is a parity improvement, not a new
	// discovery path.
	dotDir := filepath.Join(dw.vaultPath, parser.StandaloneTasksNotebook)
	if info, statErr := os.Stat(dotDir); statErr == nil && info.IsDir() {
		if addErr := dw.watcher.Add(dotDir); addErr != nil {
			// Don't fail Start() over the carve-out — the rest of the
			// watcher is up, and the next cold start will re-index the
			// standalone-tasks file via ScanStandaloneTasks. Log so a
			// human can diagnose if the failure pattern matters.
			log.Printf("DirectoryWatcher: failed to watch standalone-tasks dir %s: %v",
				dotDir, addErr)
		}
	}

	dw.wg.Add(1)
	go func() {
		defer dw.wg.Done()
		dw.listenLoop()
	}()
	dw.startLeaseSweeper()
	return nil
}

// registerRoot records a watched root + its source/notebook metadata. Idempotent
// (an already-registered root is a no-op). Caller holds the write lock or is in
// single-threaded setup.
func (dw *DirectoryWatcher) registerRoot(rootPath string, info watchRoot) {
	root := filepath.Clean(rootPath)
	dw.rootMu.Lock()
	defer dw.rootMu.Unlock()
	if _, ok := dw.rootInfo[root]; ok {
		return
	}
	dw.roots = append(dw.roots, root)
	dw.rootInfo[root] = info
}

// AddWatchRoot registers an additional (linked) notebook root and begins
// watching it, sharing the existing fsnotify watcher, tracker, coordinator and
// lease maps (#100). source is 'linked:<id>'; notebook is the display name (a
// linked root IS one notebook).
func (dw *DirectoryWatcher) AddWatchRoot(rootPath, source, notebook string) error {
	root := filepath.Clean(rootPath)
	dw.registerRoot(root, watchRoot{source: source, notebook: notebook})
	return dw.AddRecursive(root)
}

// RemoveWatchRoot stops watching a linked root and drops its metadata. The
// markdown content is left untouched (unlink semantics — safe default).
func (dw *DirectoryWatcher) RemoveWatchRoot(rootPath string) {
	root := filepath.Clean(rootPath)
	dw.rootMu.Lock()
	if _, ok := dw.rootInfo[root]; !ok {
		dw.rootMu.Unlock()
		return
	}
	delete(dw.rootInfo, root)
	for i, r := range dw.roots {
		if r == root {
			dw.roots = append(dw.roots[:i], dw.roots[i+1:]...)
			break
		}
	}
	dw.rootMu.Unlock()
	// Best-effort unwatch of every dir under the root. fsnotify Remove is a
	// no-op for paths it isn't watching, so WalkDir is safe.
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, _ error) error {
		if d != nil && d.IsDir() {
			_ = dw.watcher.Remove(p)
		}
		return nil
	})
}

// SetLinkedConfigHandler registers a callback invoked when a linked
// notebook's co-located <root>/.system/config.yaml changes (#133). The
// callback receives the source ('linked:<id>'). Pass nil to unregister.
// The handler is called from the watcher goroutine; it MUST not block on
// the watcher's own locks (the App-side handler only touches configMu and
// the event emitter, both safe).
func (dw *DirectoryWatcher) SetLinkedConfigHandler(fn func(source string)) {
	dw.linkedConfigHandlerMu.Lock()
	dw.linkedConfigHandler = fn
	dw.linkedConfigHandlerMu.Unlock()
}

// SetPageChangedHandler registers a callback invoked after the watcher
// reindexes or clears a note page from an external filesystem event. The App
// uses this to emit block:changed so plugin indexes stay consistent (#850).
func (dw *DirectoryWatcher) SetPageChangedHandler(fn func(notebook, section, page string)) {
	dw.pageChangedHandlerMu.Lock()
	dw.pageChangedHandler = fn
	dw.pageChangedHandlerMu.Unlock()
}

func (dw *DirectoryWatcher) notifyPageChanged(notebook, section, page string) {
	if notebook == "" || page == "" {
		return
	}
	dw.pageChangedHandlerMu.RLock()
	handler := dw.pageChangedHandler
	dw.pageChangedHandlerMu.RUnlock()
	if handler != nil {
		handler(notebook, section, page)
	}
}

// SetReMintWarningHandler registers a callback invoked when the watcher's
// mass-re-mint heuristic fires on a re-parsed file (#443). Pass nil to
// unregister. The handler is called from the watcher goroutine; it MUST not
// block on the watcher's own locks (the App-side handler only emits a Wails
// event, which is safe).
func (dw *DirectoryWatcher) SetReMintWarningHandler(fn func(ReMintWarning)) {
	dw.reMintWarningHandlerMu.Lock()
	dw.reMintWarningHandler = fn
	dw.reMintWarningHandlerMu.Unlock()
}

// SetAtomicReindexHandler installs the App-side atomic block+projection
// publish that reindexFile delegates the index step to. Production
// (initializeVaultServices) installs a closure over App.indexFile so an
// external frontmatter edit publishes blocks AND page_types/page_properties
// in one transaction. Pass nil to revert to defaultAtomicReindex (still
// atomic; empty projection payload — used by tests that don't care about
// typed projection). The handler is invoked from the watcher goroutine
// inside LockFileWrite; it MUST do its own WithDBWrite and MUST NOT block
// on the watcher's own locks.
func (dw *DirectoryWatcher) SetAtomicReindexHandler(fn AtomicReindexFunc) {
	dw.atomicReindexHandlerMu.Lock()
	dw.atomicReindexHandler = fn
	dw.atomicReindexHandlerMu.Unlock()
}

// defaultAtomicReindex is the fallback AtomicReindexFunc used when no
// App-side handler is installed. It publishes blocks via
// IndexFileWithProjection with an empty projection payload (typeID=""),
// which atomically clears any existing projection for the page. Production
// always installs the schema-aware handler via SetAtomicReindexHandler
// before watcher.Start returns, so this default only covers tests that
// don't exercise typed projection. It exists so the watcher has NO
// non-atomic route: every reindex goes through IndexFileWithProjection
// regardless of whether an App handler is set.
func (dw *DirectoryWatcher) defaultAtomicReindex(source, notebook, section, page string, blocks []parser.ParsedBlock, meta parser.FileMetadata) error {
	// Default path: no schema awareness, so typeID is "" (the App-installed
	// handler owns schema-aware projection). Core fields are derived directly
	// from parsed frontmatter so the page_core row stays fresh for the panel
	// even when the App handler isn't installed (#867).
	core := db.PageCoreFields{
		Type:    meta.Type,
		Date:    meta.Date,
		Aliases: meta.Aliases,
		Created: meta.Created,
	}
	var err error
	dw.coordinator.WithDBWrite(func() {
		err = dw.dm.IndexFileWithProjection(source, notebook, section, page, blocks, meta.Tags, "", nil, core, meta.Warnings...)
	})
	return err
}

// linkedConfigSourceForPath returns the source ('linked:<id>') if path is a
// linked root's co-located config.yaml (<root>/.system/config.yaml), or "" +
// false otherwise. Used by the event loop to route co-located config edits
// to the linkedConfigHandler without consulting the registry on every event.
func (dw *DirectoryWatcher) linkedConfigSourceForPath(path string) (string, bool) {
	dw.rootMu.RLock()
	defer dw.rootMu.RUnlock()
	for root, info := range dw.rootInfo {
		if info.source == "vault" {
			continue
		}
		if path == filepath.Join(root, ".system", "config.yaml") {
			return info.source, true
		}
	}
	return "", false
}

// resolveFileMetadata derives (notebook, section, page, date) for a markdown
// file from its path relative to the governing watched root (#100).
//
// Vault root: a vault holds MANY notebooks, so the notebook is the first path
// component under the vault:
//
//	<vault>/<notebook>/[<section>/...]<page>.md
//
// Linked root: the root IS one notebook (its display name is registered with
// the root), so the path components are sections/page directly:
//
//	<linkedRoot>/[<section>/...]<page>.md
//
// The governing root is the longest registered root that is an ancestor of the
// path (correct under nesting; a linked root inside another root is unlikely
// but the longest-prefix match handles it deterministically).
func (dw *DirectoryWatcher) resolveFileMetadata(path string) (source, notebook, section, page, dateStr string) {
	root, info, ok := dw.governingRoot(path)
	if !ok {
		return "", "", "", "", time.Now().Format("2006-01-02")
	}

	relPath, err := filepath.Rel(root, path)
	if err != nil {
		return "", "", "", "", time.Now().Format("2006-01-02")
	}

	relPathClean := filepath.ToSlash(relPath)
	parts := strings.Split(relPathClean, "/")
	filename := parts[len(parts)-1]
	ancestors := parts[:len(parts)-1]

	pageName := filename
	if strings.HasSuffix(strings.ToLower(pageName), ".md") {
		pageName = pageName[:len(pageName)-3]
	}

	if info.source == "vault" {
		// Vault: notebook = first component; section = the rest (if any).
		if len(ancestors) >= 1 {
			notebook = ancestors[0]
			page = pageName
			if len(ancestors) > 1 {
				section = strings.Join(ancestors[1:], "/")
			}
		}
	} else {
		// Linked: the root is the notebook; everything beneath it is section/page.
		notebook = info.notebook
		page = pageName
		if len(ancestors) > 0 {
			section = strings.Join(ancestors, "/")
		}
		if notebook == "" {
			// Registered linked root without a display name — can't attribute.
			notebook, section, page = "", "", ""
		}
	}

	dateStr = ""
	info2, err := os.Stat(path)
	if err == nil {
		dateStr = info2.ModTime().Format("2006-01-02")
	} else {
		dateStr = time.Now().Format("2006-01-02")
	}

	return info.source, notebook, section, page, dateStr
}

// governingRoot returns the longest registered root that is an ancestor of path
// (or equals it), plus its metadata. Returns ok=false if the path is not under
// any watched root.
func (dw *DirectoryWatcher) governingRoot(path string) (string, watchRoot, bool) {
	cleaned := filepath.Clean(path)
	dw.rootMu.RLock()
	defer dw.rootMu.RUnlock()
	var best string
	bestInfo := watchRoot{}
	found := false
	sep := string(os.PathSeparator)
	for _, r := range dw.roots {
		// Only append a separator when r doesn't already end with one: a drive
		// root (e.g. "C:\" on Windows, "/" on Unix) is separator-terminated
		// after filepath.Clean, so blindly appending yields "C:\\" / "//" and
		// fails to match descendants like "C:\file.md".
		prefix := r
		if !strings.HasSuffix(prefix, sep) {
			prefix += sep
		}
		if cleaned == r || strings.HasPrefix(cleaned, prefix) {
			if !found || len(r) > len(best) {
				best = r
				bestInfo = dw.rootInfo[r]
				found = true
			}
		}
	}
	return best, bestInfo, found
}

func (dw *DirectoryWatcher) listenLoop() {
	for {
		select {
		case event, ok := <-dw.watcher.Events:
			if !ok {
				return
			}

			path := filepath.Clean(event.Name)

			// Check if directory
			info, err := os.Stat(path)
			isDir := false
			if err == nil {
				isDir = info.IsDir()
			}

			if isDir {
				if event.Has(fsnotify.Create) {
					// Deferred registration for the standalone-tasks parent
					// directory (#372 hardening). AddRecursive skips dot-
					// prefix dirs by design, so a `.silt/` that didn't
					// exist at Start() time wouldn't be added by the
					// normal recursive walk. Watch it directly here:
					// subsequent events on `.silt/tasks.md` then flow
					// through the standard `.md` filter + tracker +
					// reindexFile path. Idempotent — re-Add'ing an
					// already-watched path is a fsnotify no-op.
					if path == filepath.Join(dw.vaultPath, parser.StandaloneTasksNotebook) {
						if addErr := dw.watcher.Add(path); addErr != nil {
							log.Printf("DirectoryWatcher: failed to watch standalone-tasks dir %s: %v",
								path, addErr)
						}
					} else if err := dw.AddRecursive(path); err != nil {
						log.Printf("DirectoryWatcher: failed to watch new directory %s: %v", path, err)
					}
				}
				continue
			}

			// Co-located per-notebook config (#133): if the event is on a
			// linked root's <root>/.system/config.yaml, route it to the
			// linked-config handler (cache invalidation + event emit) and
			// skip the markdown-indexing path below — it's a YAML file, not
			// a page. This check runs BEFORE the .md filter so the event is
			// not silently dropped.
			if source, ok := dw.linkedConfigSourceForPath(path); ok {
				dw.linkedConfigHandlerMu.RLock()
				handler := dw.linkedConfigHandler
				dw.linkedConfigHandlerMu.RUnlock()
				if handler != nil {
					handler(source)
				}
				continue
			}

			// Only process markdown files
			if strings.ToLower(filepath.Ext(path)) != ".md" {
				continue
			}

			// Ignore events if the file is focus-locked by Svelte editor
			if dw.IsFocusLocked(path) {
				continue
			}

			// Ignore self-generated writes
			if dw.tracker.IsSelfGenerated(path) {
				continue
			}

			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				dw.reindexFile(path)
			} else if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				clearedIDs := dw.clearIndexForFile(path)
				// Evict the per-file IO mutex so ioMu doesn't grow linearly with
				// the cumulative set of distinct paths ever touched. Safe against
				// an in-flight LockFileWrite via map-identity re-check after lock.
				dw.coordinator.ReleaseFileMutex(path)
				// Evict the per-block mutex entries for the blocks that lived in
				// this file so blockMu doesn't grow with the cumulative history of
				// every block UUID ever locked. Safe via the same map-identity
				// re-check; an in-flight MutateBlock keeps its holder until unlock.
				dw.coordinator.ReleaseBlockMutexes(clearedIDs)
			}

		case err, ok := <-dw.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("DirectoryWatcher error: %v", err)

		case <-dw.closeChan:
			return
		}
	}
}

// reMintThreshold returns the minimum number of re-minted ids that, on a
// previously-indexed file, trips the mass-re-mint warning (#443). It is
// max(3, priorCount/2): at least half the file's managed blocks must be
// re-minted, with a floor of 3 so a 1-2 block file doesn't flag on a single
// normal edit. A brand-new file (priorCount == 0) is exempt by construction
// (the caller gates on priorCount > 0).
//
// Known false-positive mode: this is a heuristic, not a detector. It cannot
// distinguish "a tool stripped the id comments from N existing blocks
// (keeping their prose)" from "the user deleted N tasks and pasted N fresh
// ones (new prose, new ids)" — both produce MintedCount == N against a
// nonzero priorCount. The bulk-replace case is a legitimate edit that trips
// the warning. This is accepted: the warning is non-blocking, hedges with
// "another app likely removed them", and is dismissible. A future tightening
// could cross-check whether the surviving prose bodies roughly match the
// prior blocks (stripping preserves prose; bulk-replace changes it), but that
// is standalone work and not warranted without evidence of real false
// positives in practice.
func reMintThreshold(priorCount int) int {
	if t := priorCount / 2; t > 3 {
		return t
	}
	return 3
}

func (dw *DirectoryWatcher) reindexFile(path string) {
	// Serialize the read/parse/write/index sequence against concurrent
	// app-driven file mutations (UpdateBlockState). Without this lock a
	// user-driven checkbox click could land between our initial read and
	// our eventual write, and the watcher's stale write would silently
	// clobber the user's change. The WriteTracker cooldown only covers
	// self-generated writes — it does not protect against genuine
	// external mutations racing the watcher.
	//
	// notifyPageChanged must run AFTER this closure returns: the App's
	// page-changed handler takes vaultMu.RLock, and App writers hold
	// vaultMu.RLock then block on LockFileWrite — opposite order deadlocks
	// once a lifecycle writer queues vaultMu.Lock. Mirrors clearIndexForFile.
	var notifyNB, notifySec, notifyPage string
	dw.coordinator.LockFileWrite(path, func() {
		if dw.IsFocusLocked(path) {
			return
		}

		source, notebook, section, page, dateStr := dw.resolveFileMetadata(path)
		// Skip files that do not map to a notebook/section/page (e.g. living
		// too shallow in the vault). They are surfaced as init-warnings on
		// the full scan; here we just ignore them.
		if notebook == "" {
			return
		}
		contentBytes, err := os.ReadFile(path)
		if err != nil {
			log.Printf("reindexFile: os.ReadFile failed for %s: %v", path, err)
			return
		}

		blocks, meta, newContent, modified, err := parser.ParseFileContent(string(contentBytes), notebook, section, page, dateStr, dw.spacesPerTab)
		if err != nil {
			log.Printf("reindexFile: ParseFileContent failed for %s: %v", path, err)
			return
		}

		// Read the PRIOR managed-block count for this page BEFORE
		// IndexFileBlocks clears + reinserts the rows (the count is the input
		// to the mass-re-mint heuristic, #443). The query is cheap (one COUNT
		// on a covered index) and runs outside the write transaction below so
		// it observes the pre-replace state.
		priorCount, countErr := dw.dm.CountBlocksForPage(source, meta.Notebook, meta.Section, meta.Page)
		if countErr != nil {
			// On DB error priorCount stays 0, which silently disables the
			// heuristic for this event (the safe direction — no false
			// positive). Log so a maintainer can diagnose "why did the re-mint
			// warning stop firing" if the index gets into a bad state.
			log.Printf("reindexFile: CountBlocksForPage failed for %s (re-mint heuristic disabled for this event): %v", path, countErr)
		}

		if modified {
			dw.tracker.RegisterWrite(path)
			_ = parser.WriteFileAtomic(path, []byte(newContent))
		}

		// Mass-re-mint heuristic (#443): a previously-indexed file (priorCount
		// > 0) that re-mints a large fraction of its block ids signals an
		// external tool/sync stripped the `<!-- id: ... -->` comments — which
		// silently breaks every ((uuid)) reference to those blocks. Brand-new
		// files (priorCount == 0) and normal one-block edits (MintedCount == 1)
		// stay well under the threshold. The threshold is max(3, priorCount/2):
		// flag when at least half the file's blocks were re-minted, with a
		// minimum of 3 so tiny files don't trip on noise.
		if priorCount > 0 && meta.MintedCount >= reMintThreshold(priorCount) {
			warning := ReMintWarning{
				Path:        path,
				Source:      source,
				Notebook:    meta.Notebook,
				Section:     meta.Section,
				Page:        meta.Page,
				MintedCount: meta.MintedCount,
				PriorCount:  priorCount,
			}
			log.Printf("parser: anomalous mass id re-mint on %s: %d of %d prior blocks re-minted — external id-comment stripping suspected, ((uuid)) references may be broken",
				path, meta.MintedCount, priorCount)
			dw.reMintWarningHandlerMu.RLock()
			handler := dw.reMintWarningHandler
			dw.reMintWarningHandlerMu.RUnlock()
			if handler != nil {
				handler(warning)
			}
		}

		indexedOK := false
		// Atomic block+projection publish: the handler (production: App's
		// schema-aware indexFile) opens its own WithDBWrite and publishes
		// blocks + page_types/page_properties in one transaction. When no
		// handler is installed (tests), defaultAtomicReindex still goes
		// through IndexFileWithProjection so there is no non-atomic route.
		// The handler runs INSIDE LockFileWrite so the read+publish pair
		// stays serialized against concurrent App writers; the WithDBWrite
		// lives inside the handler to keep the coordinator's write lock
		// non-reentrant.
		dw.atomicReindexHandlerMu.RLock()
		handler := dw.atomicReindexHandler
		dw.atomicReindexHandlerMu.RUnlock()
		if handler == nil {
			handler = dw.defaultAtomicReindex
		}
		// Capture the mtime/size BEFORE the index write so the files-table row
		// records the mtime of the content being indexed — not whatever mtime a
		// concurrent external edit (Obsidian/Dropbox/second Silt window) leaves
		// on disk between the index commit and a post-commit stat. With a
		// pre-commit snapshot, an edit in the index window makes the recorded
		// mtime stale relative to the file, so the next *startup* scan treats it
		// as changed and re-parses instead of skipping and persisting stale
		// content (#29). The App handler (indexFile) captures its own pre-index
		// snapshot too; this mark is the source of truth for the
		// defaultAtomicReindex fallback path and a redundant idempotent write
		// for the indexFile path.
		var fileMtime, fileSize int64
		hasStat := false
		if st, serr := os.Stat(path); serr == nil {
			fileMtime, fileSize, hasStat = st.ModTime().UnixNano(), st.Size(), true
		}
		if err := handler(source, meta.Notebook, meta.Section, meta.Page, blocks, meta); err != nil {
			log.Printf("reindexFile: atomic index failed for %s: %v", path, err)
		} else {
			indexedOK = true
			if hasStat {
				dw.coordinator.WithDBWrite(func() {
					if err := dw.dm.MarkFileIndexed(nil, path, fileMtime, fileSize); err != nil {
						log.Printf("reindexFile: MarkFileIndexed failed for %s: %v", path, err)
					}
				})
			}
		}
		// Capture coords for notify outside LockFileWrite — must not call
		// notifyPageChanged (→ vaultMu.RLock) while holding the file lock:
		// App writers take vaultMu.RLock then LockFileWrite (opposite order).
		if indexedOK {
			notifyNB, notifySec, notifyPage = meta.Notebook, meta.Section, meta.Page
		}
	})
	if notifyNB != "" || notifyPage != "" {
		dw.notifyPageChanged(notifyNB, notifySec, notifyPage)
	}
}

// ReindexFile is the exported entry point for triggering the watcher's
// full read+parse+atomic-index sequence on a specific path. It is the
// integration-test seam for the atomic handler: App-level tests simulate an
// external fsnotify event by calling this directly. Production fsnotify
// events go through reindexFile via the listen loop.
func (dw *DirectoryWatcher) ReindexFile(path string) {
	dw.reindexFile(path)
}

func (dw *DirectoryWatcher) clearIndexForFile(path string) []string {
	source, notebook, section, page, _ := dw.resolveFileMetadata(path)
	if notebook == "" {
		return nil
	}
	var ids []string
	// Serialize the DB deletion through the coordinator, matching reindexFile
	// and all other DB-touching paths. Without this, a concurrent file event
	// can race an in-flight query and produce database-locked errors.
	dw.coordinator.WithDBWrite(func() {
		ids, _ = dw.dm.BlockIDsForPage(source, notebook, section, page)
		_ = dw.dm.ClearFileBlocks(nil, source, notebook, section, page)
		// Drop the files row so a future startup scan doesn't think the
		// deleted/renamed file is still "unchanged" and skip re-indexing the
		// new occupant of that path.
		_ = dw.dm.ForgetFile(path)
	})
	dw.notifyPageChanged(notebook, section, page)
	return ids
}
