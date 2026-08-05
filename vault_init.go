package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"silt/backend/config"
	"silt/backend/core"
	"silt/backend/db"
	"silt/backend/monitor"
	"silt/backend/parser"
	"silt/backend/paths"
	"silt/backend/templates"
	"silt/backend/types"
	"silt/backend/vault"
)

func (a *App) initializeVaultServices(vaultPath string) error {
	// Caller (InitializeVault / SwitchVault / rollbackMove) holds vaultMu.Lock.
	// Reopen the vault for AI calls: a prior CloseVault/SwitchVault set
	// closing=true and would have reset it on teardown, but a reinit reaching
	// here after a failed/partial close must not inherit a stuck flag, or AI
	// calls would reject forever (#452).
	a.closing = false
	// (Re)create the vault-scoped AI context. Cancel any PRIOR context first:
	// CloseVault/SwitchVault cancel proactively, but MoveVault/rollbackMove
	// re-enter here via teardownVaultServices → initializeVaultServices WITHOUT
	// cancelling (teardown doesn't touch vaultCtx), so without this guard each
	// vault move / failed-move rollback would orphan the old context in
	// aiCtx.children until shutdown. aiCtx may be nil for a bare App used only
	// in unit tests that bypass startup() — fall back to context.Background()
	// in that case so direct initializeVaultServices callers
	// (app_lifecycle_drain_test.go) still get a cancellable per-vault context.
	parent := a.aiCtx
	if parent == nil {
		parent = context.Background()
	}
	if a.vaultCtxCancel != nil {
		a.vaultCtxCancel()
	}
	a.vaultCtx, a.vaultCtxCancel = context.WithCancel(parent)
	// Load system config first: its editor.tab_indent_spaces drives
	// ScanWorkspace and every subsequent parse, so it must be applied before
	// the initial index is built. A missing/invalid config is non-fatal —
	// defaults keep the vault usable — but a parse error is surfaced.
	cfg, cfgErr := config.Load(vaultPath)
	if cfgErr != nil {
		a.emit(EventConfigError, cfgErr.Error())
	}
	// F4: load the per-host grants store BEFORE applyConfigLocked so the
	// first-party seed merges into the real store, not a transient empty one.
	// Grants live in <configDir>/silt/grants.json (NOT vault-scoped config.yaml)
	// so a synced vault cannot carry the counterpart's grant decisions.
	grantsStore, grantsErr := vault.LoadGrants()
	if grantsErr != nil {
		// A corrupt grants file is non-fatal — log + start with an empty
		// store. The user re-grants on first use (the safe default). Every
		// third-party plugin will prompt; first-party plugins seed regardless.
		log.Printf("initializeVaultServices: grants load failed (starting with empty store): %v", grantsErr)
		grantsStore = vault.GrantsStore{}
	}
	a.configMu.Lock()
	a.grants = grantsStore
	a.configMu.Unlock()
	a.applyConfigLocked(cfg) // sets a.cfg + a.spacesPerTab + seeds first-party grants into a.grants
	// The config:error event above fires before the frontend mounts and
	// subscribes, so it is typically lost. Stash the error for
	// GetConfigLoadError() to surface on the frontend's initial loadConfig().
	a.configMu.Lock()
	a.configLoadErr = cfgErr
	a.configMu.Unlock()

	// F4 migration: if the vault's config.yaml still carries a legacy
	// `plugins.grants:` block AND the host store was empty before we seeded
	// first-party grants, this is a pre-F4 vault opening on a host that has
	// never seen it. Emit grants:migration-required so the frontend shows a
	// one-time confirmation dialog. The user's confirm calls
	// ConfirmGrantsMigration, which writes the legacy grants to the host file
	// and rewrites config.yaml without the grants block. If the user denies,
	// the host store stays seeded with first-party only; every third-party
	// plugin re-prompts on first use (the safe default).
	if len(grantsStore) == 0 && grantsErr == nil {
		legacy := vault.LoadLegacyVaultGrants(vaultPath)
		// Strip first-party entries — they are always seeded implicitly, never
		// migrated (the user never granted them manually).
		hasThirdParty := false
		for pid := range legacy {
			if !isFirstPartyPlugin(pid) {
				hasThirdParty = true
				break
			}
		}
		if hasThirdParty {
			a.emitOrQueue(EventGrantsMigrationRequired, legacy)
		}
	}

	// #218: move any plaintext AI provider keys into the OS keyring on first
	// run after upgrade. Best-effort + idempotent — if the keyring is off or
	// unavailable, plaintext keys are left in config (the documented fallback).
	// Runs AFTER applyConfigLocked so a.cfg is populated. Keyring writes run
	// UNDER the caller's vaultMu hold (locked variant) so a concurrent
	// SwitchVault/MoveVault cutover cannot retarget the write to another
	// vault (#654). Caller holds vaultMu.Lock — use the locked variant
	// (Lock→RLock deadlocks).
	//
	// Path-scoped keyring user ids need the target vault path before migrate
	// (teardown cleared a.vaultPath; the final assignment below is after the
	// watcher starts). Set it here so migrate does not hash an empty path.
	a.vaultPath = vaultPath
	// vaultMu held exclusively — pass enablement without re-locking (#684).
	devToolsMenuOn := strings.EqualFold(os.Getenv("SILT_DEBUG"), "1")
	if !devToolsMenuOn {
		a.configMu.RLock()
		devToolsMenuOn = a.cfg.UI.OpenDevtoolsOnStartup != nil && *a.cfg.UI.OpenDevtoolsOnStartup
		a.configMu.RUnlock()
	}
	a.syncOpenDevToolsMenuItemEnabled(devToolsMenuOn)
	a.migrateAIKeysToKeyringLocked()

	// F3: verify linked-notebook fingerprints before the vault scan. Legacy
	// links (pre-F3, no fingerprint) get one assigned silently; mismatched
	// links are quarantined (excluded from indexing/reads/writes) and emit
	// linked-notebook:quarantined so the frontend shows a re-link prompt.
	a.configMu.Lock()
	a.quarantinedLinks = make(map[string]struct{})
	a.configMu.Unlock()
	a.verifyLinkedNotebookFingerprints()

	// storageWarnings collects non-fatal caveats surfaced during vault init:
	// the index relocation/migration notes (paths.ResolveAndMigrateIndexPath,
	// below) and the DB layer's WAL-fallback warning (dbMgr.Warnings, below).
	// They ride the vault:init-warnings event so the user is informed without
	// the vault open being blocked. The index lives in a per-user local
	// DataDir (out of the synced vault), so the vault's own sync status is not
	// a Silt hazard and no longer produces a warning here.
	var storageWarnings []string

	// The .system dir holds per-vault app data (config.yaml, themes,
	// templates, plugins, trash, logs); ensure it exists.
	systemDir := filepath.Join(vaultPath, ".system")
	if err := os.MkdirAll(systemDir, 0o700); err != nil {
		return fmt.Errorf("failed to ensure .system dir: %w", err)
	}

	// The SQLite index lives in a per-user local DataDir (NOT in the synced
	// vault), so a cloud-sync engine or antivirus cannot lock or corrupt it.
	// On first open after upgrade, a legacy in-vault index is migrated to the
	// new location, preserving warm-start performance. Markdown remains the
	// source of truth — the index is reproducible working memory.
	indexPath, migrateWarnings, err := paths.ResolveAndMigrateIndexPath(vaultPath)
	if err != nil {
		return fmt.Errorf("failed to resolve index location: %w", err)
	}
	storageWarnings = append(storageWarnings, migrateWarnings...)
	dbMgr, err := db.NewDatabaseManager(indexPath)
	if err != nil {
		return fmt.Errorf("failed to start database: %w", err)
	}
	storageWarnings = append(storageWarnings, dbMgr.Warnings()...)

	// SQLDB() only at vault open: handle is live and single-threaded here.
	// Query/write paths must use DatabaseManager package methods (handle/
	// withDB) so Close returns ErrDBClosed instead of nil-derefing.
	coord := core.NewExecutionCoordinator(dbMgr.SQLDB())
	tracker := monitor.NewWriteTracker()

	// Migrate old per-day file model: <page>/<date>.md → <page>.md.
	// Runs before the scan so the indexer sees the new model. Idempotent.
	migrationWarnings := vault.MigratePerDayFiles(vaultPath, a.spacesPerTab)

	results, walkWarnings, err := parser.ScanWorkspace(vaultPath, a.spacesPerTab)
	if err != nil {
		_ = dbMgr.Close()
		return fmt.Errorf("failed to scan workspace: %w", err)
	}

	// Append the standalone-tasks file (<vault>/.silt/tasks.md) if it exists.
	// WalkMarkdown skips dot-directories so this targeted read is the only
	// way the file enters the index (#368). parseSingleFile derives
	// notebook=".silt" from the path; ListNavigation hides dot-prefixed
	// notebooks so it never surfaces in the page browser.
	results = append(results, parser.ScanStandaloneTasks(vaultPath, a.spacesPerTab)...)

	// Incremental re-index: keep only files whose mtime+size differ from the
	// last recorded index (or that were never indexed). On a cold start (no
	// index file yet) every file is "changed" and gets a full index. Pruning
	// stale `files` rows for paths no longer on disk handles deletes/renames.
	var changed []parser.ScanResult
	var warmSkipped []parser.ScanResult
	var seenPaths []string
	for _, res := range results {
		seenPaths = append(seenPaths, res.Path)
		if res.Err != nil || res.Notebook == "" {
			// Unreadable or unresolvable files are forwarded to the indexer so
			// they appear in the skipped list; they do not get a files row.
			changed = append(changed, res)
			continue
		}
		unchanged, uerr := dbMgr.IsFileUnchanged(res.Path, res.MTime.UnixNano(), res.Size)
		if uerr != nil {
			log.Printf("initializeVaultServices: IsFileUnchanged(%s): %v", res.Path, uerr)
			changed = append(changed, res)
			continue
		}
		if unchanged {
			warmSkipped = append(warmSkipped, res)
			continue
		}
		changed = append(changed, res)
	}

	// indexedCount = files that passed metadata validation and were actually
	// written to the index (NOT len(changed); errored/unresolvable files in
	// `changed` are reported in `skipped` and excluded from this count). Used
	// below to decide whether a post-index WAL checkpoint is worth running.
	//
	// Atomic batch: each changed file's typed projection is published in the
	// SAME transaction as its blocks (IndexScanResultsWithProjection) so a
	// reader can never see a freshly-scanned typed page without its
	// projection (or vice versa). The projection payload is computed against
	// the live schema before the tx opens.
	changedProjections := computeBatchProjections(a, changed)
	indexedCount, skipped, err := dbMgr.IndexScanResultsWithProjection(changed, changedProjections)
	if err != nil {
		_ = dbMgr.Close()
		return fmt.Errorf("failed to index scan results: %w", err)
	}

	// Record the freshly-indexed files' stats and prune paths that vanished
	// since the last run (rename/delete). Only files that were actually
	// indexed (valid metadata, no scan error) get a files row — a file that
	// failed to parse shouldn't be marked "unchanged" next time.
	var allWarnings []string
	// Surface storage-layer caveats (index-migration notes + WAL fallback)
	// alongside the per-file warnings below, all via vault:init-warnings.
	allWarnings = append(allWarnings, storageWarnings...)
	for _, res := range changed {
		if res.Err != nil {
			allWarnings = append(allWarnings, fmt.Sprintf("%s: %v", res.Path, res.Err))
			continue
		}
		if res.Notebook == "" {
			for _, w := range res.Warnings {
				allWarnings = append(allWarnings, fmt.Sprintf("%s: %s", res.Path, w))
			}
			if len(res.Warnings) == 0 {
				allWarnings = append(allWarnings, fmt.Sprintf("%s: missing notebook/section/page", res.Path))
			}
			continue
		}
		if res.MTime.IsZero() {
			// No stat → can't record a skip key; leave it to be re-parsed
			// next time rather than risk a false "unchanged".
			continue
		}
		if err := dbMgr.MarkFileIndexed(nil, res.Path, res.MTime.UnixNano(), res.Size); err != nil {
			log.Printf("initializeVaultServices: MarkFileIndexed(%s): %v", res.Path, err)
		}
	}
	pruned, pruneErr := dbMgr.PruneStaleFiles(seenPaths)
	if pruneErr != nil {
		log.Printf("initializeVaultServices: PruneStaleFiles: %v", pruneErr)
	}
	for _, p := range pruned {
		allWarnings = append(allWarnings, fmt.Sprintf("%s: removed from index (file no longer exists)", p))
		// Drop blocks + typed projection for paths that vanished while the app
		// was closed. PruneStaleFiles only removes the files-table skip key;
		// without this, page_types/page_properties (and blocks) linger as
		// dashboard ghosts and relation targets until a cold index wipe.
		a.clearIndexedPageForPath(dbMgr, vaultPath, p)
	}

	// Merge the indexer's per-file skip list into the warning stream.
	allWarnings = append(allWarnings, skipped...)
	// Surface walk-level warnings (symlink skips, permission errors) from #32.
	allWarnings = append(allWarnings, walkWarnings...)
	allWarnings = append(allWarnings, migrationWarnings...)

	if indexedCount > 0 {
		// A checkpoint after the bulk insert keeps the WAL bounded for the
		// session. No-op on in-memory.
		if err := dbMgr.Checkpoint(); err != nil {
			log.Printf("initializeVaultServices: post-index checkpoint: %v", err)
		}
	}
	if len(allWarnings) > 0 {
		a.emitOrQueue(EventVaultInitWarnings, allWarnings)
	}

	watcher, err := monitor.NewDirectoryWatcher(vaultPath, dbMgr, tracker, coord, a.spacesPerTab)
	if err != nil {
		_ = dbMgr.Close()
		return fmt.Errorf("failed to start watcher: %w", err)
	}
	if err := watcher.Start(); err != nil {
		_ = watcher.Close()
		_ = dbMgr.Close()
		return fmt.Errorf("failed to execute watcher start: %w", err)
	}

	a.db = dbMgr
	a.coordinator = coord
	a.tracker = tracker
	a.watcher = watcher
	a.vaultPath = vaultPath

	// Start the scoped reprojection worker (#866) so subsequent SaveType /
	// DeleteType / type-watcher / ReloadTypes / RestoreExampleTypes calls
	// coalesce onto a background goroutine that performs disk reads + DB
	// writes WITHOUT holding vaultMu. Started BEFORE the type watcher so
	// the very first external type edit has somewhere to enqueue.
	a.reprojectWorker = newProjectionReprojectWorker(a)
	a.reprojectWorker.start()

	// Typed-projection backfill. Two scenarios:
	//   - Marker already set (warm restart): changed files were projected
	//     atomically by IndexScanResultsWithProjection above. Nothing left.
	//   - Marker not set (cold start or warm upgrade): project warm-skipped
	//     files whose blocks were not re-indexed this session. Changed files
	//     were already projected by the atomic batch, so only warmSkipped
	//     needs the standalone pass. On a true cold start warmSkipped is
	//     empty (every file was "changed") and the loop is a no-op.
	backfillDone, berr := dbMgr.SchemaMigrationApplied(db.PageProjectionBackfillMarker)
	if berr != nil {
		log.Printf("initializeVaultServices: probe page_projection_backfill: %v", berr)
	}
	var toProject []parser.ScanResult
	if !backfillDone {
		toProject = warmSkipped
	}
	// Only record the one-shot backfill marker when every page projected
	// cleanly. projectPageType returns DB errors; swallowing them and still
	// marking done would leave failed pages invisible until a file touch or
	// schema edit (AC5 warm path). Re-run is idempotent (delete-then-insert).
	backfillFailed := false
	for _, res := range toProject {
		if res.Notebook == "" || res.Err != nil {
			continue
		}
		a.backfillProjectionCount++
		if err := a.projectPageType(res.Source, parser.FileMetadata{
			Notebook:    res.Notebook,
			Section:     res.Section,
			Page:        res.Page,
			Type:        res.Type,
			Frontmatter: res.Frontmatter,
		}); err != nil {
			backfillFailed = true
		}
	}
	if berr == nil && !backfillDone && !backfillFailed {
		if err := dbMgr.RecordSchemaMigration(db.PageProjectionBackfillMarker); err != nil {
			log.Printf("initializeVaultServices: record page_projection_backfill: %v", err)
		}
	} else if backfillFailed && !backfillDone {
		log.Printf("initializeVaultServices: page_projection_backfill incomplete; marker not recorded (will retry next open)")
	}

	// Route co-located per-notebook config edits to the cache invalidator +
	// linked-config:changed event (#133). The handler is called from the
	// watcher goroutine; it only touches configMu + the event emitter.
	watcher.SetLinkedConfigHandler(a.onLinkedConfigChange)
	// Route mass-re-mint detections to the index:re-mint-warning event (#443).
	// The handler is called from the watcher goroutine; it only emits a Wails
	// event (safe — no vaultMu/configMu access).
	watcher.SetReMintWarningHandler(a.onReMintWarning)
	// External fsnotify reindex delegates the index step to App's atomic
	// block+projection publish (indexFile → IndexFileWithProjection), so an
	// external frontmatter edit publishes blocks AND page_types/
	// page_properties in one transaction. The handler does its own
	// WithDBWrite; the watcher still owns mtime marking, re-mint detection,
	// deletion, and notifyPageChanged.
	watcher.SetAtomicReindexHandler(func(source, notebook, section, page string, blocks []parser.ParsedBlock, meta parser.FileMetadata) error {
		return a.indexFile(source, notebook, section, page, blocks, meta, meta.Warnings...)
	})
	// External fsnotify reindex/clear → block:changed so plugin indexes (QA
	// vectors) stay consistent with the note store (#850). Projection is
	// published atomically by the reindex handler above, and clearIndexForFile
	// already drops projection via ClearFileBlocks(tx==nil), so this callback
	// only emits the plugin-notification event.
	watcher.SetPageChangedHandler(func(notebook, section, page string) {
		a.emitBlockChanged("", notebook, section, page, "")
	})

	// Start hot-reload of .system/config.yaml. External edits re-parse and
	// emit config:changed without a restart (SPECS.md §9.2). Silt's own
	// SaveSystemWrite is ignored via the watcher's self-loop tracker.
	if a.ctx != nil {
		cw, wErr := config.NewConfigWatcher(vaultPath,
			func(reloaded config.SystemConfig) { a.applyConfig(reloaded) },
			func(e error) { a.emit(EventConfigError, e.Error()) })
		if wErr != nil {
			log.Printf("config watcher disabled: %v", wErr)
		} else {
			cw.Start()
			a.configWatcher = cw
		}
	}

	// Start hot-reload of .system/templates/ so the picker stays live when a
	// user adds/edits/deletes a custom template externally (the same posture
	// as the config and theme watchers). The onChange callback invalidates the
	// cache and emits templates:changed; the frontend store re-lists.
	if a.ctx != nil {
		tw, tErr := templates.NewTemplateWatcher(a.templatesDir(), func() {
			templates.InvalidateTemplateCache()
			a.emit(EventTemplatesChanged, struct{}{})
		})
		if tErr != nil {
			log.Printf("template watcher disabled: %v", tErr)
		} else {
			tw.Start()
			a.templateWatcher = tw
		}
	}

	// Start hot-reload of .system/types/ so typed pages and the type manager
	// stay live when a user adds/edits/deletes a type externally (the same
	// posture as the template watcher). The onChange callback invalidates the
	// type cache, emits types:changed, AND schedules a coalescing reprojection
	// (Phase 5 / #866) so the dashboard reflects the new schema without
	// waiting for each page to be re-touched. The worker scopes disk reads +
	// DB writes outside the held Lock; onChange takes Lock only briefly as a
	// lifecycle handoff (the worker re-checks vaultMu mid-flight).
	if a.ctx != nil {
		yw, yErr := types.NewTypeWatcher(a.typesDir(), func() {
			types.InvalidateTypesCache()
			a.vaultMu.Lock()
			a.enqueueReprojection(true)
			a.vaultMu.Unlock()
			a.emit(EventTypesChanged, struct{}{})
		})
		if yErr != nil {
			log.Printf("type watcher disabled: %v", yErr)
		} else {
			yw.Start()
			a.typeWatcher = yw
		}
	}

	// Seed the in-memory network + AI audit logs from the on-disk per-plugin
	// log files (network.log / ai.log) so entries survive a restart (#157 /
	// #446). The writers are started AFTER seeding so they never race the
	// seed (#235).
	seedNetworkAuditFromDisk(vaultPath)
	startNetworkAuditWriter(vaultPath)
	seedAIAuditFromDisk(vaultPath)
	startAIAuditWriter(vaultPath)

	// Report any paths the watcher could not subscribe to (fsnotify
	// limits, permissions, etc.) so the UI can inform the user.
	if failed := watcher.FailedPaths(); len(failed) > 0 {
		a.emitOrQueue(EventVaultWatchCoverage, failed)
	}

	// Local MCP host (#687): start when enabled in config.yaml. Safe no-op
	// when disabled. Caller holds vaultMu.Lock — must use Locked variant
	// (Go RWMutex deadlocks on RLock while the same goroutine holds Lock).
	a.syncMCPHostLocked()

	return nil
}

// clearIndexedPageForPath drops blocks + typed projection for a markdown path
// that disappeared from disk (startup prune). Best-effort: unknown layouts are
// skipped. Caller holds vaultMu.Lock; uses dbMgr directly (a.db may not be set).
func (a *App) clearIndexedPageForPath(dbMgr *db.DatabaseManager, vaultPath, absPath string) {
	if dbMgr == nil || absPath == "" || !strings.HasSuffix(strings.ToLower(absPath), ".md") {
		return
	}
	source, notebook, section, page := "", "", "", ""
	if rel, err := filepath.Rel(vaultPath, absPath); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) >= 2 {
			notebook = parts[0]
			page = strings.TrimSuffix(parts[len(parts)-1], filepath.Ext(parts[len(parts)-1]))
			if len(parts) > 2 {
				section = strings.Join(parts[1:len(parts)-1], "/")
			}
			source = "vault"
		}
	}
	if source == "" {
		a.configMu.RLock()
		links := append([]config.LinkedNotebook(nil), a.cfg.LinkedNotebooks...)
		a.configMu.RUnlock()
		for _, ln := range links {
			if ln.RootPath == "" {
				continue
			}
			rel, err := filepath.Rel(ln.RootPath, absPath)
			if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
				continue
			}
			parts := strings.Split(filepath.ToSlash(rel), "/")
			if len(parts) < 1 {
				continue
			}
			page = strings.TrimSuffix(parts[len(parts)-1], filepath.Ext(parts[len(parts)-1]))
			if len(parts) > 1 {
				section = strings.Join(parts[:len(parts)-1], "/")
			}
			notebook = ln.DisplayName
			source = ln.Source()
			break
		}
	}
	if source == "" || notebook == "" || page == "" {
		return
	}
	if err := dbMgr.ClearFileBlocks(nil, source, notebook, section, page); err != nil {
		log.Printf("initializeVaultServices: ClearFileBlocks pruned %s: %v", absPath, err)
	}
}
