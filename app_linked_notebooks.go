package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"silt/backend/config"
	"silt/backend/db"
	"silt/backend/parser"
	"silt/backend/plugins"
	"silt/backend/vault"
)

// Linked-notebook subsystem: registration, fingerprint trust/verify, the
// quarantine cache, and the co-located per-notebook config override layer.
// A linked notebook is a folder OUTSIDE the vault (e.g. a synced SharePoint
// mount) registered so it can be browsed/searched/edited in place; its
// markdown is never copied into the vault. Also home to the linked-notebook
// security reconciliation (reconcileLinkedNotebookSecurityLocked /
// seedFirstPartyGrants) called from SaveSystemConfig / applyConfigLocked.

// --- Linked / external notebooks (#100) -------------------------------------
//
// A linked notebook is a folder OUTSIDE the vault (e.g. a synced SharePoint
// mount) registered into the vault so it can be browsed/searched/edited in
// place. Its markdown is NEVER copied into the vault — the external folder
// remains the source of truth. The link registry (config.yaml
// `linked_notebooks:`) is vault-scoped; the index rows carry source =
// 'linked:<id>' so same-named notebooks across roots cannot collide.

// LinkNotebook registers an external folder as a linked notebook: validates it,
// assigns a stable id, rejects collisions (with vault notebooks or existing
// links), persists the registry, watches the root, and indexes its tree. The
// external files (and any co-located <root>/.system/) are never modified.
func (a *App) LinkNotebook(folderPath string) (config.LinkedNotebook, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return config.LinkedNotebook{}, fmt.Errorf("vault not loaded")
	}
	absPath, err := filepath.Abs(folderPath)
	if err != nil {
		return config.LinkedNotebook{}, fmt.Errorf("invalid folder path: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return config.LinkedNotebook{}, fmt.Errorf("folder not found: %w", err)
	}
	if !info.IsDir() {
		return config.LinkedNotebook{}, fmt.Errorf("selected path is not a folder")
	}
	// A linked root must live OUTSIDE the vault (otherwise it's just an
	// in-vault notebook — use OpenNotebook). Refusing the vault prevents a
	// double-index (vault + linked) of the same tree.
	if isPathWithinRoot(absPath, a.vaultPath) {
		return config.LinkedNotebook{}, fmt.Errorf("that folder is already inside the vault — open it as a notebook instead of linking")
	}
	// Likewise refuse an ANCESTOR of the vault: the watcher would observe the
	// vault itself as part of the linked root and double-index it (#100).
	if isPathWithinRoot(a.vaultPath, absPath) {
		return config.LinkedNotebook{}, fmt.Errorf("cannot link a folder that contains the vault")
	}
	displayName := sanitizePathSegment(filepath.Base(absPath))
	if displayName == "" {
		return config.LinkedNotebook{}, fmt.Errorf("invalid folder name")
	}
	id := "linked-" + strings.ReplaceAll(uuid.New().String(), "-", "")[:12]
	// F3: capture a host-verified fingerprint at link time so a subsequent
	// synced edit to config.yaml's root_path cannot redirect the link to an
	// attacker-chosen folder. resolveNotebookDir recomputes and compares on
	// every access; mismatch → quarantine + re-link prompt.
	fp, fpErr := config.ComputeRootFingerprint(absPath)
	if fpErr != nil {
		return config.LinkedNotebook{}, fmt.Errorf("failed to fingerprint linked root: %w", fpErr)
	}
	ln := config.LinkedNotebook{
		ID:              id,
		RootPath:        filepath.Clean(absPath),
		DisplayName:     displayName,
		RootFingerprint: fp,
	}

	// Reject display-name collisions: a vault notebook or an existing link with
	// the same name would be ambiguous in the sidebar and in (notebook, ...)
	// lookups (source disambiguates the index, but the UX must stay clear).
	if err := a.rejectLinkCollision(ln); err != nil {
		return config.LinkedNotebook{}, err
	}

	// Persist the registry atomically under configMu (self-write suppressed so
	// the watcher doesn't bounce it back as an external edit). configMu is held
	// across config.Save: cfg would otherwise share the LinkedNotebooks backing
	// array with a.cfg, so a concurrent Link/Unlink mutating the slice during
	// the YAML marshal would be a data race. Mirrors UpdatePluginSetting (#120).
	a.configMu.Lock()
	// Re-validate the uniqueness invariant under the WRITE lock: rejectLink
	// Collision ran with only an RLock and then released, so two concurrent
	// LinkNotebook calls for same-basename folders could both pass it and
	// double-register. nameCollidesWithLink is the authority under the lock.
	if existing, dup := a.linkByRecordLocked(ln); dup {
		a.configMu.Unlock()
		return config.LinkedNotebook{}, fmt.Errorf("a linked notebook with %q already exists", existing.DisplayName)
	}
	a.cfg.LinkedNotebooks = append(a.cfg.LinkedNotebooks, ln)
	saveErr := a.saveConfigTracked(a.cfg)
	a.configMu.Unlock()
	if saveErr != nil {
		return config.LinkedNotebook{}, fmt.Errorf("failed to persist link registry: %w", saveErr)
	}

	// Watch the root so external edits re-index, then index the tree. Errors
	// here don't unwind the link — the notebook stays registered (the user can
	// re-link or the watcher picks it up later); we surface them as a return.
	if a.watcher != nil {
		_ = a.watcher.AddWatchRoot(ln.RootPath, ln.Source(), ln.DisplayName)
	}
	if _, idxErr := a.indexLinkedTree(ln); idxErr != nil {
		log.Printf("LinkNotebook(%s): indexTree failed: %v (link registered; will retry on next change)", ln.DisplayName, idxErr)
	}
	e := newAuditEntry("link")
	e.ID = ln.ID
	e.RootPath = ln.RootPath
	e.Fingerprint = ln.RootFingerprint
	appendAuditEntry(a.vaultPath, e)
	return ln, nil
}

// UnlinkNotebook removes a linked notebook from the registry, stops watching
// it, and drops its local index rows. The external files are left completely
// untouched (safe default). Idempotent.
func (a *App) UnlinkNotebook(id string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	// Mutate the registry AND persist under configMu so a concurrent
	// Link/Unlink or config.Save can't race the LinkedNotebooks slice. A fresh
	// `kept` slice is allocated (not a.cfg.LinkedNotebooks[:0]) so we never
	// overwrite the backing array a concurrent reader may be marshalling.
	a.configMu.Lock()
	removed := false
	var kept []config.LinkedNotebook
	var rootPath string
	for _, ln := range a.cfg.LinkedNotebooks {
		if ln.ID == id {
			removed = true
			rootPath = ln.RootPath
			continue
		}
		kept = append(kept, ln)
	}
	var saveErr error
	if removed {
		a.cfg.LinkedNotebooks = kept
		saveErr = a.saveConfigTracked(a.cfg)
	}
	a.configMu.Unlock()
	if saveErr != nil {
		return fmt.Errorf("failed to persist link registry: %w", saveErr)
	}
	if !removed {
		return nil // idempotent: unknown id is a no-op
	}

	// Drop the co-located config cache entry for this source (#133);
	// a re-link of the same root will re-populate it lazily. Done AFTER
	// releasing configMu so the dedicated linkedConfigsMu is the only lock
	// held (no nested locking).
	a.invalidateLinkedConfig("linked:" + id)

	if a.watcher != nil && rootPath != "" {
		a.watcher.RemoveWatchRoot(rootPath)
	}
	// Drop the local index rows for this source. The files table rows (keyed by
	// absolute path) are pruned by PruneStaleFiles on the next startup scan;
	// dropping them eagerly here would race the watcher's Remove events.
	a.coordinator.WithDBWrite(func() {
		_ = a.db.ClearSourceBlocks("linked:" + id)
	})
	e := newAuditEntry("unlink")
	e.ID = id
	e.RootPath = rootPath
	appendAuditEntry(a.vaultPath, e)
	return nil
}

// PickLinkedNotebook opens the native folder picker and links the chosen
// external folder. Returns the linked notebook, or a zero value (no error) when
// the user cancels.
func (a *App) PickLinkedNotebook() (config.LinkedNotebook, error) {
	if a.wailsApp == nil {
		return config.LinkedNotebook{}, fmt.Errorf("application context not ready")
	}
	selectedPath, err := a.openDirectoryDialog("Link External Notebook Folder")
	if err != nil {
		return config.LinkedNotebook{}, fmt.Errorf("failed to open folder picker: %w", err)
	}
	if selectedPath == "" {
		return config.LinkedNotebook{}, nil // user cancelled
	}
	return a.LinkNotebook(selectedPath)
}

// rejectLinkCollision fails loud if the linked notebook's display name collides
// with an in-vault notebook folder or an already-registered link.
func (a *App) rejectLinkCollision(ln config.LinkedNotebook) error {
	// Existing links.
	a.configMu.RLock()
	for _, existing := range a.cfg.LinkedNotebooks {
		if existing.ID == ln.ID || existing.RootPath == ln.RootPath || existing.DisplayName == ln.DisplayName {
			a.configMu.RUnlock()
			return fmt.Errorf("a linked notebook with this name/path is already registered")
		}
	}
	a.configMu.RUnlock()
	// Vault notebooks (top-level dirs, excluding dot/system).
	entries, err := os.ReadDir(a.vaultPath)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				if e.Name() == ln.DisplayName {
					return fmt.Errorf("a vault notebook named %q already exists; choose a different folder", ln.DisplayName)
				}
			}
		}
	}
	return nil
}

// linkByRecordLocked reports whether a LinkedNotebook with the same ID,
// RootPath, or DisplayName is already registered. The caller MUST hold
// configMu (read or write). Used to re-validate under the LinkNotebook write
// lock (rejectLinkCollision ran RLock-then-release, so a concurrent link could
// race it).
func (a *App) linkByRecordLocked(ln config.LinkedNotebook) (config.LinkedNotebook, bool) {
	for _, existing := range a.cfg.LinkedNotebooks {
		if existing.ID == ln.ID || existing.RootPath == ln.RootPath || existing.DisplayName == ln.DisplayName {
			return existing, true
		}
	}
	return config.LinkedNotebook{}, false
}

// linkedConfigFor returns the linked notebook's co-located config.yaml
// (<linkedRoot>/.system/config.yaml, #133), mtime-cached. If the on-disk
// mtime is unchanged since the last load, the cached parsed config is
// returned; otherwise the file is re-read and the cache is updated. Thread-
// safe via linkedConfigsMu (a dedicated mutex, NOT configMu) so concurrent
// callers resolving different linked notebooks cannot trigger a
// concurrent-map-write panic. A missing co-located file yields
// config.Defaults() with no error (the normal case — the vault-scoped
// config.yaml is the baseline). An unparseable file yields a real error so
// the user can fix it; the cache is not populated with garbage on error.
//
// The PLAN (Phase 5) called for pre-populating the cache in
// initializeVaultServices; the implementation uses lazy population instead
// (the cache fills on the first GetPluginSettingsForNotebook call for each
// source). This avoids blocking startup on N co-located-config reads for N
// linked notebooks and is functionally equivalent: the mtime check on every
// call guarantees freshness, and a cache miss is a single stat + read.
func (a *App) linkedConfigFor(ln config.LinkedNotebook) (config.SystemConfig, error) {
	source := ln.Source()
	path := config.LinkedConfigPath(ln.RootPath)

	// Stat OUTSIDE the lock — the mtime is the cache key, and stat is fast
	// even on a network mount (no file content read). Holding linkedConfigsMu
	// during stat would serialize concurrent cache-miss resolutions for
	// different linked notebooks (#133 review).
	st, statErr := os.Stat(path)
	var mtime time.Time
	fileExists := false
	if statErr == nil {
		mtime = st.ModTime()
		fileExists = true
	} else if !os.IsNotExist(statErr) {
		return config.Defaults(), fmt.Errorf("stat linked config: %w", statErr)
	}

	// Cache check under lock (no I/O — quick map lookup).
	a.linkedConfigsMu.Lock()
	if a.linkedConfigs == nil {
		a.linkedConfigs = make(map[string]linkedConfigEntry)
	}
	if cached, ok := a.linkedConfigs[source]; ok {
		// Hit conditions: file still missing (zero mtime cached) or
		// mtime unchanged.
		if (!fileExists && cached.mtime.IsZero()) || (fileExists && cached.mtime.Equal(mtime)) {
			a.linkedConfigsMu.Unlock()
			return cached.cfg, nil
		}
	}
	a.linkedConfigsMu.Unlock()

	// Cache miss: load OUTSIDE the lock (disk read + YAML parse). Two
	// concurrent goroutines may both miss and both load — that is fine;
	// last writer wins and the data converges (identical or next-access
	// refresh). The lock is only held for the map mutation.
	cfg, err := config.LoadLinked(ln.RootPath)
	if err != nil {
		return config.Defaults(), err
	}

	// Update cache under lock.
	a.linkedConfigsMu.Lock()
	a.linkedConfigs[source] = linkedConfigEntry{cfg: cfg, mtime: mtime}
	a.linkedConfigsMu.Unlock()

	return cfg, nil
}

// invalidateLinkedConfig drops the cached co-located config for a source so
// the next read re-loads from disk. Called by the watcher hook on an external
// edit of <linkedRoot>/.system/config.yaml and by UnlinkNotebook. Thread-safe
// via linkedConfigsMu.
func (a *App) invalidateLinkedConfig(source string) {
	a.linkedConfigsMu.Lock()
	defer a.linkedConfigsMu.Unlock()
	if a.linkedConfigs == nil {
		return
	}
	delete(a.linkedConfigs, source)
}

// onLinkedConfigChange is the watcher hook for external edits to a linked
// notebook's co-located <root>/.system/config.yaml (#133). It drops the
// cached parsed config for the source (so the next GetPluginSettingsForNotebook
// call re-reads from disk) and emits a linked-config:changed Wails event so
// the frontend can refresh any per-active-notebook settings it derived from
// the old config. Called from the watcher goroutine.
func (a *App) onLinkedConfigChange(source string) {
	a.invalidateLinkedConfig(source)
	a.emit(EventLinkedConfigChanged, source)
}

// quarantineLink adds a linked notebook ID to the quarantine set and emits
// linked-notebook:quarantined so the frontend shows a re-link prompt. A
// quarantined link is excluded from resolveNotebookDir, indexing, and
// ListNavigation until the user re-links (UnlinkNotebook + LinkNotebook,
// which captures a fresh fingerprint). Guarded by configMu (write).
func (a *App) quarantineLink(id, reason string) {
	a.configMu.Lock()
	if a.quarantinedLinks == nil {
		a.quarantinedLinks = make(map[string]struct{})
	}
	a.quarantinedLinks[id] = struct{}{}
	displayName := ""
	for _, ln := range a.cfg.LinkedNotebooks {
		if ln.ID == id {
			displayName = ln.DisplayName
			break
		}
	}
	a.configMu.Unlock()
	a.emitOrQueue(EventLinkedNotebookQuarantined, map[string]string{
		"id":           id,
		"display_name": displayName,
		"reason":       reason,
	})
	log.Printf("quarantineLink: %s (%s) quarantined: %s", id, displayName, reason)
}

// ResolveQuarantinedLinks returns the list of currently-quarantined linked
// notebooks so the frontend can render re-link prompts.
func (a *App) ResolveQuarantinedLinks() ([]QuarantinedLinkInfo, error) {
	if a.vaultPath == "" {
		return nil, fmt.Errorf("vault not loaded")
	}
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	out := make([]QuarantinedLinkInfo, 0, len(a.quarantinedLinks))
	for _, ln := range a.cfg.LinkedNotebooks {
		if _, q := a.quarantinedLinks[ln.ID]; q {
			out = append(out, QuarantinedLinkInfo{
				ID:          ln.ID,
				DisplayName: ln.DisplayName,
				RootPath:    ln.RootPath,
			})
		}
	}
	return out, nil
}

// QuarantinedLinkInfo describes a quarantined linked notebook for the frontend.
type QuarantinedLinkInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	RootPath    string `json:"root_path"`
}

// verifyLinkedNotebookFingerprints walks cfg.LinkedNotebooks at vault-open time
// and either silently assigns a fingerprint to legacy links (pre-F3 vaults) or
// quarantines links whose stored fingerprint no longer matches the on-disk
// root. Called from initializeVaultServices after config.Load, before the
// linked-tree index pass. Must be called WITHOUT holding configMu (it
// acquires it internally via quarantineLink).
func (a *App) verifyLinkedNotebookFingerprints() {
	a.configMu.RLock()
	links := append([]config.LinkedNotebook(nil), a.cfg.LinkedNotebooks...)
	a.configMu.RUnlock()

	for _, ln := range links {
		if ln.RootFingerprint == "" {
			// F3 migration: pre-F3 vault has a link with no fingerprint.
			// Assign one silently (the user linked this on THIS host).
			fp, err := config.ComputeRootFingerprint(ln.RootPath)
			if err != nil {
				log.Printf("verifyLinkedNotebookFingerprints: skip legacy %s (inaccessible): %v", ln.DisplayName, err)
				continue
			}
			a.configMu.Lock()
			for i := range a.cfg.LinkedNotebooks {
				if a.cfg.LinkedNotebooks[i].ID == ln.ID {
					a.cfg.LinkedNotebooks[i].RootFingerprint = fp
					break
				}
			}
			saveErr := a.saveConfigTracked(a.cfg)
			a.configMu.Unlock()
			if saveErr != nil {
				log.Printf("verifyLinkedNotebookFingerprints: persist FP for %s: %v", ln.DisplayName, saveErr)
			}
			continue
		}
		currentFP, err := config.ComputeRootFingerprint(ln.RootPath)
		if err != nil {
			log.Printf("verifyLinkedNotebookFingerprints: skip %s (inaccessible): %v", ln.DisplayName, err)
			continue
		}
		if currentFP != ln.RootFingerprint {
			a.quarantineLink(ln.ID, "fingerprint_mismatch")
		}
	}
}

// indexLinkedTree walks a linked root's markdown and indexes it under the
// linked source in a SINGLE batched transaction (#134). The notebook name is
// the link's DisplayName (the root IS one notebook); sections/pages are
// derived from the path relative to the root. Returns the number of files
// indexed.
//
// Batched (was per-file): the previous implementation called IndexFileBlocks
// (which begins/commits its own transaction) plus MarkFileIndexed for every
// file, producing N transactions for N files. On a large synced mount (the
// headline #100 workload) that was WAL-checkpoint thrash and slow first-link
// UX. The batched path threads `source` through IndexScanResults (the same
// function the vault startup scan uses) and does the files-table
// (MarkFileIndexed) pass after the index commit, preserving linked warm
// restart. Per-file read/parse errors are surfaced in the skipped list
// (IndexScanResults collects them) instead of logged inline.
func (a *App) indexLinkedTree(ln config.LinkedNotebook) (int, error) {
	files, warnings, err := parser.WalkMarkdown(ln.RootPath)
	for _, w := range warnings {
		log.Printf("LinkNotebook(%s): %s", ln.DisplayName, w)
	}
	if err != nil {
		return 0, fmt.Errorf("walk linked root: %w", err)
	}
	source := ln.Source()

	// Build the per-file ScanResult set in one pass. Read/parse errors are
	// recorded on the result (Err) so IndexScanReports reports them in the
	// skipped list rather than aborting the whole batch — same visibility
	// as the per-file path, one transaction instead of N.
	results := make([]parser.ScanResult, 0, len(files))
	for _, file := range files {
		rel, relErr := filepath.Rel(ln.RootPath, file)
		if relErr != nil {
			results = append(results, parser.ScanResult{
				Path: file,
				Err:  fmt.Errorf("resolve relative path: %w", relErr),
			})
			continue
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		pageName := parts[len(parts)-1]
		if strings.HasSuffix(strings.ToLower(pageName), ".md") {
			pageName = pageName[:len(pageName)-3]
		}
		section := ""
		if len(parts) > 1 {
			section = strings.Join(parts[:len(parts)-1], "/")
		}

		st, statErr := os.Stat(file)
		contentBytes, readErr := os.ReadFile(file)
		if readErr != nil {
			log.Printf("LinkNotebook(%s): read %s failed: %v", ln.DisplayName, file, readErr)
			results = append(results, parser.ScanResult{Path: file, Err: readErr})
			continue
		}
		// Force the linked notebook's display name: an external file's
		// frontmatter may declare a different `notebook:`, which would
		// make the row miss ListNavigation's DisplayName filter. The
		// linked root IS this one notebook (#100).
		blocks, meta, _, _, perr := parser.ParseFileContent(string(contentBytes), ln.DisplayName, section, pageName, fileOrDefaultDate(file), a.spacesPerTab)
		if perr != nil {
			log.Printf("LinkNotebook(%s): parse %s failed: %v", ln.DisplayName, file, perr)
			results = append(results, parser.ScanResult{Path: file, Err: perr})
			continue
		}
		res := parser.ScanResult{
			Path:     file,
			Notebook: ln.DisplayName,
			Section:  section,
			Page:     pageName,
			Source:   source,
			Blocks:   blocks,
			Tags:     meta.Tags,
			Warnings: meta.Warnings,
		}
		if statErr == nil {
			res.MTime = st.ModTime()
			res.Size = st.Size()
		}
		results = append(results, res)
	}

	var (
		indexedCount int
		skipped      []string
		idxErr       error
	)
	a.coordinator.WithDBWrite(func() {
		indexedCount, skipped, idxErr = a.db.IndexScanResults(results)
	})
	if idxErr != nil {
		return indexedCount, fmt.Errorf("index linked tree: %w", idxErr)
	}
	for _, s := range skipped {
		log.Printf("LinkNotebook(%s): skipped %s", ln.DisplayName, s)
	}

	// Post-commit files-table pass: record mtime+size for each successfully
	// indexed file so a warm restart skips re-parsing it. A file is
	// considered indexed iff IndexScanResults counted it (Err == nil &&
	// Notebook != ""). Batched under one lease + transaction via
	// MarkFilesIndexed so App does not hold SQLDB().Begin across teardown.
	var fileStats []db.FileIndexStat
	for _, res := range results {
		if res.Err != nil || res.Notebook == "" {
			continue
		}
		if res.MTime.IsZero() {
			// No stat → can't record a skip key; leave it to be re-parsed
			// next time rather than risk a false "unchanged".
			continue
		}
		fileStats = append(fileStats, db.FileIndexStat{
			Path:  res.Path,
			MTime: res.MTime.UnixNano(),
			Size:  res.Size,
		})
	}
	a.coordinator.WithDBWrite(func() {
		if err := a.db.MarkFilesIndexed(fileStats); err != nil {
			log.Printf("LinkNotebook(%s): MarkFilesIndexed: %v", ln.DisplayName, err)
		}
	})
	return indexedCount, nil
}

// reconcileLinkedNotebookSecurityLocked applies the trusted-root rules to a
// candidate config. The caller holds configMu and commits the candidate after
// this function returns.
func (a *App) reconcileLinkedNotebookSecurityLocked(cfg *config.SystemConfig) []map[string]string {
	// F3: when a config reloads from disk (fsnotify), preserve the in-memory
	// RootFingerprint for each linked notebook and quarantine any link whose
	// root changed or was added by an external edit. The M2 (synced-vault)
	// adversary can edit config.yaml freely — without these checks they could
	// redirect an existing link's root_path to an attacker folder, or inject a
	// brand-new link pointing at a hostile root, both with no fingerprint.
	var quarantined []map[string]string
	if a.quarantinedLinks == nil {
		a.quarantinedLinks = make(map[string]struct{})
	}
	// Snapshot the set of known link IDs so we can detect new entries.
	knownIDs := make(map[string]bool, len(a.cfg.LinkedNotebooks))
	for _, existing := range a.cfg.LinkedNotebooks {
		knownIDs[existing.ID] = true
	}
	newlyQuarantined := make(map[string]bool) // IDs quarantined in THIS call
	for i, reloaded := range cfg.LinkedNotebooks {
		if !knownIDs[reloaded.ID] {
			// NEW link from an external edit — the M2 adversary injected a
			// link to an attacker-chosen root. Quarantine immediately; the
			// user confirms via the re-link modal or unlinks.
			a.quarantinedLinks[reloaded.ID] = struct{}{}
			newlyQuarantined[reloaded.ID] = true
			quarantined = append(quarantined, map[string]string{
				"id":           reloaded.ID,
				"display_name": reloaded.DisplayName,
				"reason":       "new_link_from_external_edit",
			})
			log.Printf("applyConfigLocked: quarantined new link %s (appeared in external config edit)", reloaded.DisplayName)
			continue
		}
		for _, existing := range a.cfg.LinkedNotebooks {
			if reloaded.ID != existing.ID {
				continue
			}
			if reloaded.RootPath != existing.RootPath {
				// root_path changed via external edit — quarantine and
				// preserve the trusted in-memory root + fingerprint.
				a.quarantinedLinks[reloaded.ID] = struct{}{}
				newlyQuarantined[reloaded.ID] = true
				cfg.LinkedNotebooks[i].RootPath = existing.RootPath
				cfg.LinkedNotebooks[i].RootFingerprint = existing.RootFingerprint
				quarantined = append(quarantined, map[string]string{
					"id":           reloaded.ID,
					"display_name": reloaded.DisplayName,
					"reason":       "root_path_changed",
				})
				log.Printf("applyConfigLocked: quarantined %s (root_path changed in external edit)", reloaded.DisplayName)
			} else {
				// RootPath unchanged — preserve the fingerprint captured at link time.
				cfg.LinkedNotebooks[i].RootFingerprint = existing.RootFingerprint
			}
			break
		}
	}
	// P2 prune: remove stale quarantine entries for links that no longer exist
	// in the reloaded config (user unlinked, or synced config removed them).
	// Keep entries for links quarantined in THIS call (they ARE in the config).
	for id := range a.quarantinedLinks {
		if newlyQuarantined[id] {
			continue
		}
		stillExists := false
		for _, ln := range cfg.LinkedNotebooks {
			if ln.ID == id {
				stillExists = true
				break
			}
		}
		if !stillExists {
			delete(a.quarantinedLinks, id)
		}
	}
	return quarantined
}

// seedFirstPartyGrants populates the per-host grants store with every
// capability for each first-party plugin ID, so bundled plugins are implicitly
// trusted WITHOUT a special-case bypass in requireGrant. This closes the
// spoofing vector where a third-party plugin passes 'silt-attachments' as
// pluginID to bypass all capability checks (#113 security hardening).
//
// F4: grants now live in the per-host store (a.grants), not vault-scoped
// config.yaml. Seeding is in-memory only for the session — the store is NOT
// re-persisted on every applyConfigLocked (that would write grants.json on
// every config reload for no reason). The seeded entries persist for the vault
// session; a fresh launch re-seeds from LoadGrants + this function.
func (a *App) seedFirstPartyGrants() {
	if a.grants == nil {
		a.grants = vault.GrantsStore{}
	}
	for id := range plugins.FirstPartyPluginIDs {
		if a.grants[id] == nil {
			a.grants[id] = map[string]string{}
		}
		for cap := range plugins.KnownCapabilities {
			a.grants[id][string(cap)] = plugins.QualGranted
		}
	}
}
