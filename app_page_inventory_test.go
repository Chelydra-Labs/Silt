package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"silt/backend/db"
	"silt/backend/parser"
	"silt/backend/vault"
)

// These tests pin the inventory lifecycle the set-based reconcile replaced the
// incremental path-keyed machinery with: a scan is the only complete view of
// the on-disk inventory, so it is where identity drift heals — and the only
// thing allowed to delete is a scan that saw every file.

// newInventoryVault scaffolds a vault in an isolated data dir, for tests that
// open it through the real initializeVaultServices path.
func newInventoryVault(t *testing.T) (vaultPath string) {
	t.Helper()
	t.Setenv("SILT_DATA_DIR", t.TempDir())
	hostConfigDir := t.TempDir()
	t.Setenv("APPDATA", hostConfigDir)
	t.Setenv("XDG_CONFIG_HOME", hostConfigDir)
	vaultPath = t.TempDir()
	if err := vault.ScaffoldVault(vaultPath); err != nil {
		t.Fatalf("ScaffoldVault: %v", err)
	}
	return vaultPath
}

// typedPageBody renders a page with stable block ids so a re-index does not
// rewrite the file (which would change its mtime and defeat warm-skip).
func typedPageBody(notebook, section, page, blockID string) string {
	return "---\n" +
		"notebook: \"" + notebook + "\"\n" +
		"section: \"" + section + "\"\n" +
		"page: \"" + page + "\"\n" +
		"date: \"2026-08-01\"\n" +
		"type: \"book\"\n" +
		"status: \"available\"\n" +
		"---\n# " + page + " <!-- id: " + blockID + " -->\n\nBody.\n"
}

// inventoryProjectionCounts counts the vault loc-keyed projection rows for one page.
func inventoryProjectionCounts(t *testing.T, app *App, notebook, section, page string) (types, props, core int) {
	t.Helper()
	return projectionCounts(t, app, "vault", notebook, section, page)
}

// projectionCounts counts the loc-keyed projection rows for one page in any source.
func projectionCounts(t *testing.T, app *App, source, notebook, section, page string) (types, props, core int) {
	t.Helper()
	for _, c := range []struct {
		table string
		out   *int
	}{
		{"page_types", &types},
		{"page_properties", &props},
		{"page_core", &core},
	} {
		q := "SELECT COUNT(*) FROM " + c.table + " WHERE source = ? AND notebook = ? AND section = ? AND page = ?"
		if err := app.db.SQLDB().QueryRow(q, source, notebook, section, page).Scan(c.out); err != nil {
			t.Fatalf("%s: %v", c.table, err)
		}
	}
	return types, props, core
}

// blockCount counts the indexed blocks at one page loc.
func blockCount(t *testing.T, app *App, source, notebook, section, page string) int {
	t.Helper()
	var n int
	q := "SELECT COUNT(*) FROM blocks WHERE source = ? AND notebook = ? AND section = ? AND page = ?"
	if err := app.db.SQLDB().QueryRow(q, source, notebook, section, page).Scan(&n); err != nil {
		t.Fatalf("count blocks: %v", err)
	}
	return n
}

// skipUnlessPermissionsEnforced skips a permission-denied fixture when the
// platform does not actually deny access after os.Chmod(0o000). Windows only
// toggles the read-only attribute (directories stay enumerable, files stay
// readable), and a privileged runner bypasses the mode bits entirely, so the
// probe runs the fixture's own blocked operation rather than guessing.
func skipUnlessPermissionsEnforced(t *testing.T, probe func() error) {
	t.Helper()
	if err := probe(); err == nil {
		t.Skip("os.Chmod(0o000) does not deny access on this platform (Windows toggles only the read-only attribute; privileged users bypass permissions)")
	}
}

// settleInventoryVault opens and closes the vault twice so the parser's
// first-parse id normalization has rewritten every fixture file. After that the
// mtimes are stable, the warm skip applies, and no watcher event for a rewritten
// file can race an assertion.
func settleInventoryVault(t *testing.T, vaultPath string) {
	t.Helper()
	for i := 0; i < 2; i++ {
		app := &App{spacesPerTab: 4}
		if err := app.initializeVaultServices(vaultPath); err != nil {
			t.Fatalf("settling open %d: %v", i+1, err)
		}
		if err := app.CloseVault(); err != nil {
			t.Fatalf("settling CloseVault %d: %v", i+1, err)
		}
	}
}

// TestWatcher_IdentityEditAddsNewLocInSession pins the in-session half of the
// design: an external in-place frontmatter identity edit (the only way a file
// changes identity without moving) publishes the new loc immediately, and the
// old loc is deliberately left alone. There is no watcher-hook reconcile —
// index events are monotone ADD — so the old identity survives until the next
// scan.
func TestWatcher_IdentityEditAddsNewLocInSession(t *testing.T) {
	vaultPath := newInventoryVault(t)
	pagePath := filepath.Join(vaultPath, "Books", "Dune.md")
	writeFile(t, pagePath, typedPageBody("Books", "", "Dune", "aaaa1111-0000-4000-8000-000000000001"))

	app := &App{spacesPerTab: 4}
	if err := app.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("initializeVaultServices: %v", err)
	}
	defer func() { _ = app.CloseVault() }()

	// External edit: same file, frontmatter page: now names the new identity.
	raw, err := os.ReadFile(pagePath)
	if err != nil {
		t.Fatalf("read page: %v", err)
	}
	edited := strings.Replace(string(raw), `page: "Dune"`, `page: "Dune Messiah"`, 1)
	if err := os.WriteFile(pagePath, []byte(edited), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	app.watcher.ReindexFile(pagePath)

	if core, err := app.db.GetPageCoreProjection("vault", "Books", "", "Dune Messiah"); err != nil || core == nil {
		t.Fatalf("new identity not published in-session: %+v, %v", core, err)
	}
	if core, _ := app.db.GetPageCoreProjection("vault", "Books", "", "Dune"); core == nil {
		t.Error("old identity cleared in-session: the watcher path is add-only by design")
	}
}

// TestVaultOpen_HealsExternalIdentityEdit drives the same external identity edit
// through the real watcher path, then reopens the vault: the scan is the only
// complete inventory, so the abandoned loc's projections must be gone and the
// new identity intact.
func TestVaultOpen_HealsExternalIdentityEdit(t *testing.T) {
	vaultPath := newInventoryVault(t)
	pagePath := filepath.Join(vaultPath, "Books", "Dune.md")
	writeFile(t, pagePath, typedPageBody("Books", "", "Dune", "aaaa1111-0000-4000-8000-000000000002"))

	app := &App{spacesPerTab: 4}
	if err := app.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("initializeVaultServices: %v", err)
	}

	raw, err := os.ReadFile(pagePath)
	if err != nil {
		t.Fatalf("read page: %v", err)
	}
	edited := strings.Replace(string(raw), `page: "Dune"`, `page: "Dune Messiah"`, 1)
	if err := os.WriteFile(pagePath, []byte(edited), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	app.watcher.ReindexFile(pagePath)
	if err := app.CloseVault(); err != nil {
		t.Fatalf("CloseVault: %v", err)
	}

	app2 := &App{spacesPerTab: 4}
	if err := app2.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = app2.CloseVault() }()

	types, props, core := inventoryProjectionCounts(t, app2, "Books", "", "Dune")
	if types != 0 || props != 0 || core != 0 {
		t.Errorf("abandoned identity kept projections after reopen: page_types=%d page_properties=%d page_core=%d", types, props, core)
	}
	newCore, err := app2.db.GetPageCoreProjection("vault", "Books", "", "Dune Messiah")
	if err != nil || newCore == nil {
		t.Fatalf("new identity missing after reopen: %+v, %v", newCore, err)
	}
	if newCore.Type != "book" {
		t.Errorf("new identity core.Type = %q, want book", newCore.Type)
	}
	newTypes, _, newCoreCount := inventoryProjectionCounts(t, app2, "Books", "", "Dune Messiah")
	if newTypes != 1 || newCoreCount != 1 {
		t.Errorf("new identity projections = types:%d core:%d, want 1/1", newTypes, newCoreCount)
	}
}

// TestVaultOpen_DuplicateIDCarrierKeepsWarmPage pins the first-post-upgrade
// shape the deleted ownership records existed to guard: a changed file whose
// block set contains a warm-skipped file's block (a sync-conflict copy). The
// warm page must keep its page_core and typed projections; without a record
// table there is nothing to be "unarmed", because the batch never deletes
// inventory rows at all.
func TestVaultOpen_DuplicateIDCarrierKeepsWarmPage(t *testing.T) {
	vaultPath := newInventoryVault(t)
	origPath := filepath.Join(vaultPath, "Books", "Original.md")
	copyPath := filepath.Join(vaultPath, "Books", "Original Copy.md")
	writeFile(t, origPath, typedPageBody("Books", "", "Original", "aaaa1111-0000-4000-8000-000000000003"))
	writeFile(t, copyPath, typedPageBody("Books", "", "Original Copy", "aaaa1111-0000-4000-8000-000000000004"))

	// Two settling opens: the parser's own id normalization can rewrite a
	// fixture on its first parse, and only a stable file is warm-skipped.
	for i := 0; i < 2; i++ {
		app := &App{spacesPerTab: 4}
		if err := app.initializeVaultServices(vaultPath); err != nil {
			t.Fatalf("open %d: %v", i+1, err)
		}
		if i == 1 {
			// Sentinel core field: only a re-index can overwrite it, so observing
			// it after the reopen proves the original was warm-skipped (the state
			// the deleted ownership records used to be "unarmed" in).
			if _, err := app.db.SQLDB().Exec(
				`UPDATE page_core SET type = 'warm-sentinel' WHERE page = 'Original'`); err != nil {
				t.Fatalf("seed sentinel: %v", err)
			}
		}
		if err := app.CloseVault(); err != nil {
			t.Fatalf("CloseVault %d: %v", i+1, err)
		}
	}

	// The copy becomes a carrier of the original's block id (what a sync
	// conflict / OS copy produces) while the original stays untouched, so only
	// the copy is "changed" on the next open and the original is warm-skipped.
	writeFile(t, copyPath, typedPageBody("Books", "", "Original Copy", "aaaa1111-0000-4000-8000-000000000004")+
		"# Original <!-- id: aaaa1111-0000-4000-8000-000000000003 -->\n")

	app2 := &App{spacesPerTab: 4}
	if err := app2.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = app2.CloseVault() }()

	types, props, core := inventoryProjectionCounts(t, app2, "Books", "", "Original")
	if types != 1 || props != 1 || core != 1 {
		t.Errorf("warm duplicate-ID carrier lost its page: page_types=%d page_properties=%d page_core=%d (want 1/1/1)", types, props, core)
	}
	if row, _ := app2.db.GetPageCoreProjection("vault", "Books", "", "Original"); row == nil || row.Type != "warm-sentinel" {
		t.Errorf("original was re-indexed, so this test did not exercise the warm-skip shape: %+v", row)
	}
}

// TestVaultOpen_UnreadableThenWarmReopenKeepsTypedProjections pins the blind-scan
// gate across a recovery: a file that is unreadable on one open is still on
// disk, so that open must not delete its projections. It stays warm-skipped
// once readable again (mtime unchanged), so nothing would ever re-derive typed
// metadata that the blind open dropped.
func TestVaultOpen_UnreadableThenWarmReopenKeepsTypedProjections(t *testing.T) {
	vaultPath := newInventoryVault(t)
	pagePath := filepath.Join(vaultPath, "Books", "Dune.md")
	// Zero-block typed page: its inventory row has no block evidence to fall back
	// on, so only the completeness gate can keep it alive across a blind open.
	writeFile(t, pagePath, "---\n"+
		"notebook: \"Books\"\n"+
		"section: \"\"\n"+
		"page: \"Dune\"\n"+
		"date: \"2026-08-01\"\n"+
		"type: \"book\"\n"+
		"status: \"available\"\n"+
		"---\n")

	// Settle the files row first: the parser's id normalization can rewrite the
	// fixture on its first parse, and a warm reopen requires a stable mtime.
	for i := 0; i < 2; i++ {
		app := &App{spacesPerTab: 4}
		if err := app.initializeVaultServices(vaultPath); err != nil {
			t.Fatalf("settling open %d: %v", i+1, err)
		}
		if i == 1 {
			// Only a re-index can overwrite this, so it proves the recovery open
			// warm-skipped the file instead of re-deriving what was lost.
			if _, err := app.db.SQLDB().Exec(
				`UPDATE page_core SET type = 'warm-sentinel' WHERE page = 'Dune'`); err != nil {
				t.Fatalf("seed sentinel: %v", err)
			}
		}
		if err := app.CloseVault(); err != nil {
			t.Fatalf("CloseVault %d: %v", i+1, err)
		}
	}

	if err := os.Chmod(pagePath, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	restored := false
	restore := func() {
		if !restored {
			_ = os.Chmod(pagePath, 0o644)
			restored = true
		}
	}
	defer restore()
	skipUnlessPermissionsEnforced(t, func() error {
		_, err := os.ReadFile(pagePath)
		return err
	})

	app2 := &App{spacesPerTab: 4}
	if err := app2.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("unreadable open: %v", err)
	}
	if err := app2.CloseVault(); err != nil {
		t.Fatalf("CloseVault (unreadable): %v", err)
	}
	restore()

	// Warm reopen: mtime is unchanged since the successful open, so the file is
	// skipped and only the preserved projections can be observed.
	app3 := &App{spacesPerTab: 4}
	if err := app3.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("recovery open: %v", err)
	}
	defer func() { _ = app3.CloseVault() }()

	types, props, core := inventoryProjectionCounts(t, app3, "Books", "", "Dune")
	if types != 1 || props != 1 || core != 1 {
		t.Errorf("typed projections lost across an unreadable open: page_types=%d page_properties=%d page_core=%d (want 1/1/1)", types, props, core)
	}
	if row, _ := app3.db.GetPageCoreProjection("vault", "Books", "", "Dune"); row == nil || row.Type != "warm-sentinel" {
		t.Errorf("recovery open re-indexed the file, so this test did not exercise the warm-skip shape: %+v", row)
	}
}

// TestScanWorkspace_WalkErrorYieldsIncompleteInventory pins the other
// blind-scan channel on a real tree: a subtree the walker cannot enter is
// absent from the results entirely (not even flagged per file), which is
// exactly why the completeness signal must come from the walk's error channel.
//
// This is a scan-level test rather than a full vault-open one because the
// watcher refuses to start on a permission-denied subtree — an app open with
// such a vault fails earlier, so the gate's job here is to protect against
// transient walk failures (a directory vanishing mid-traversal) during an open
// that otherwise proceeds.
func TestScanWorkspace_WalkErrorYieldsIncompleteInventory(t *testing.T) {
	vaultPath := t.TempDir()
	writeFile(t, filepath.Join(vaultPath, "Books", "Dune.md"), typedPageBody("Books", "", "Dune", "aaaa1111-0000-4000-8000-000000000006"))
	deepDir := filepath.Join(vaultPath, "Books", "Deep")
	writeFile(t, filepath.Join(deepDir, "Hidden.md"), typedPageBody("Books", "Deep", "Hidden", "aaaa1111-0000-4000-8000-000000000007"))

	if err := os.Chmod(deepDir, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() { _ = os.Chmod(deepDir, 0o755) }()
	skipUnlessPermissionsEnforced(t, func() error {
		_, err := os.ReadDir(deepDir)
		return err
	})

	results, skips, walkErrs, err := parser.ScanWorkspace(vaultPath, 4)
	if err != nil {
		t.Fatalf("ScanWorkspace: %v", err)
	}
	if len(walkErrs) == 0 {
		t.Fatalf("expected a walk error for the unreadable subtree, got skips=%v", skips)
	}
	live, complete := scanInventory(results, walkErrs, "vault")
	if complete {
		t.Fatal("scan with an un-walkable subtree reported complete")
	}
	for _, loc := range live {
		if loc.Section == "Deep" {
			t.Errorf("unreachable page reported live: %+v", loc)
		}
	}
}

// TestVaultOpen_SymlinkSkipDoesNotBlockHealing pins that a deliberate skip stays
// off the completeness channel: a vault containing a symlink must still heal,
// or one shortcut would suppress the reconcile's destructive half forever.
func TestVaultOpen_SymlinkSkipDoesNotBlockHealing(t *testing.T) {
	vaultPath := newInventoryVault(t)
	writeFile(t, filepath.Join(vaultPath, "Books", "Dune.md"), typedPageBody("Books", "", "Dune", "aaaa1111-0000-4000-8000-000000000008"))
	link := filepath.Join(vaultPath, "Shortcut")
	if err := os.Symlink(filepath.Join(vaultPath, "Books"), link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	app := &App{spacesPerTab: 4}
	if err := app.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("initializeVaultServices: %v", err)
	}
	if _, err := app.db.SQLDB().Exec(
		`INSERT INTO page_core (source, notebook, section, page, type, date, aliases, created)
		 VALUES ('vault', 'Books', '', 'Ghost', 'book', '2025-01-01', '[]', '')`); err != nil {
		t.Fatalf("seed ghost: %v", err)
	}
	if err := app.CloseVault(); err != nil {
		t.Fatalf("CloseVault: %v", err)
	}

	app2 := &App{spacesPerTab: 4}
	if err := app2.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = app2.CloseVault() }()
	if core, _ := app2.db.GetPageCoreProjection("vault", "Books", "", "Ghost"); core != nil {
		t.Errorf("symlink skip blocked healing: ghost survived %+v", core)
	}
}

// TestVaultOpen_KeepsLinkedInventory pins source scoping at the app boundary: a
// vault open must not touch linked-source rows (there is no linked startup
// scan), and the linked root's own scan is what heals its ghost.
func TestVaultOpen_KeepsLinkedInventory(t *testing.T) {
	vaultPath := newInventoryVault(t)
	// A vault page keeps the vault scan non-empty. With zero vault files the
	// prune takes an accidentally-safe branch (nothing to compare against), so
	// the source-scoping invariant this test names would never run.
	writeFile(t, filepath.Join(vaultPath, "Books", "Dune.md"),
		typedPageBody("Books", "", "Dune", "aaaa1111-0000-4000-8000-000000000010"))
	ext := t.TempDir()
	writeFile(t, filepath.Join(ext, "Linked Page.md"), typedPageBody("Ext", "", "Linked Page", "aaaa1111-0000-4000-8000-000000000009"))

	app := &App{spacesPerTab: 4}
	if err := app.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("initializeVaultServices: %v", err)
	}
	ln, err := app.LinkNotebook(ext)
	if err != nil {
		t.Fatalf("LinkNotebook: %v", err)
	}
	src := ln.Source()
	if _, err := app.db.SQLDB().Exec(
		`INSERT INTO page_core (source, notebook, section, page, type, date, aliases, created)
		 VALUES (?, ?, '', 'Linked Ghost', 'book', '2025-01-01', '[]', '')`, src, ln.DisplayName); err != nil {
		t.Fatalf("seed linked ghost: %v", err)
	}
	if err := app.CloseVault(); err != nil {
		t.Fatalf("CloseVault: %v", err)
	}

	app2 := &App{spacesPerTab: 4}
	if err := app2.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = app2.CloseVault() }()

	if core, _ := app2.db.GetPageCoreProjection(src, ln.DisplayName, "", "Linked Page"); core == nil {
		t.Error("vault open deleted the linked live page")
	}
	if blocks := blockCount(t, app2, src, ln.DisplayName, "", "Linked Page"); blocks == 0 {
		t.Error("vault open cleared the linked live page's blocks")
	}
	if core, _ := app2.db.GetPageCoreProjection(src, ln.DisplayName, "", "Linked Ghost"); core == nil {
		t.Error("vault open deleted a linked-source row: the reconcile is not source-scoped")
	}

	// The linked root's own scan is the authority for its ghosts.
	if _, err := app2.indexLinkedTree(ln); err != nil {
		t.Fatalf("indexLinkedTree: %v", err)
	}
	if core, _ := app2.db.GetPageCoreProjection(src, ln.DisplayName, "", "Linked Ghost"); core != nil {
		t.Errorf("linked scan did not heal its ghost: %+v", core)
	}
	if core, _ := app2.db.GetPageCoreProjection(src, ln.DisplayName, "", "Linked Page"); core == nil {
		t.Error("linked live page lost its inventory row")
	}
}

// TestVaultOpen_MoveWhileClosedKeepsLiveLoc pins the prune's live guard: a
// folder move made while Silt was closed, with the page's frontmatter identity
// retaining the old location, resolves both the vanished path and the live loc
// to the same page. Pruning the old path must not clear what the scan just
// indexed (the moved file warm-skips afterwards, so nothing would re-derive it).
func TestVaultOpen_MoveWhileClosedKeepsLiveLoc(t *testing.T) {
	vaultPath := newInventoryVault(t)
	oldPath := filepath.Join(vaultPath, "Books", "Sec", "Dune.md")
	newPath := filepath.Join(vaultPath, "Books", "Archive", "Dune.md")
	writeFile(t, oldPath, typedPageBody("Books", "Sec", "Dune", "aaaa1111-0000-4000-8000-000000000011"))
	settleInventoryVault(t, vaultPath)

	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatalf("move: %v", err)
	}

	app2 := &App{spacesPerTab: 4}
	if err := app2.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = app2.CloseVault() }()

	types, props, core := inventoryProjectionCounts(t, app2, "Books", "Sec", "Dune")
	if types != 1 || props != 1 || core != 1 {
		t.Errorf("move-while-closed wiped the live loc: page_types=%d page_properties=%d page_core=%d (want 1/1/1)", types, props, core)
	}
	if blocks := blockCount(t, app2, "vault", "Books", "Sec", "Dune"); blocks == 0 {
		t.Error("move-while-closed wiped the live loc's blocks")
	}
}

// TestPruneStaleVaultPaths_IncompleteScanPrunesNothing pins the prune's
// completeness gate and its root scoping at the unit that owns the decision,
// with an explicit complete=false as scanInventory derives it from a walk error.
// (A full open with an un-walkable subtree cannot reach the prune: the watcher
// refuses to start on it, which is why the walk-error channel is pinned at scan
// level.) No app/watcher is involved, so a delete handler cannot mask the prune.
func TestPruneStaleVaultPaths_IncompleteScanPrunesNothing(t *testing.T) {
	vaultPath := t.TempDir()
	gonePath := filepath.Join(vaultPath, "Books", "Gone.md")
	linkedPath := filepath.Join(t.TempDir(), "Linked.md")

	dbMgr, err := db.NewDatabaseManager(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("NewDatabaseManager: %v", err)
	}
	defer func() { _ = dbMgr.Close() }()

	blocks := []parser.ParsedBlock{
		{ID: "aaaa1111-0000-4000-8000-000000000014", Type: parser.BlockNote,
			RawText: "body", CleanText: "body", LineNumber: 1},
	}
	props := []db.ProjectedProperty{
		{Property: "status", ValueText: "active", ValueSort: "active", ValueType: "text"},
	}
	if err := dbMgr.IndexFileWithProjection("vault", "Books", "", "Gone",
		blocks, nil, "book", props, db.PageCoreFields{Type: "book"}); err != nil {
		t.Fatalf("index gone page: %v", err)
	}
	if err := dbMgr.MarkFileIndexed(nil, gonePath, 1, 1); err != nil {
		t.Fatalf("mark gone: %v", err)
	}
	// A linked notebook's skip key lives in the same path-keyed table; a vault
	// open never saw its tree, so it is not this prune's to drop.
	if err := dbMgr.MarkFileIndexed(nil, linkedPath, 1, 1); err != nil {
		t.Fatalf("mark linked: %v", err)
	}

	count := func(table string) int {
		t.Helper()
		var n int
		if err := dbMgr.SQLDB().QueryRow(
			"SELECT COUNT(*) FROM " + table + " WHERE source = 'vault' AND notebook = 'Books' AND section = '' AND page = 'Gone'",
		).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}

	if warnings := pruneStaleVaultPaths(dbMgr, vaultPath, nil, nil, false); len(warnings) != 0 {
		t.Errorf("incomplete pass pruned %v, want nothing", warnings)
	}
	for _, table := range []string{"blocks", "page_core", "page_types", "page_properties"} {
		if n := count(table); n == 0 {
			t.Errorf("incomplete pass cleared %s of a path it may simply not have seen", table)
		}
	}

	// The same inputs on a complete pass are allowed to clear the vanished page,
	// and only under the vault root.
	pruned := pruneStaleVaultPaths(dbMgr, vaultPath, nil, nil, true)
	if len(pruned) != 1 || !strings.Contains(pruned[0], gonePath) {
		t.Fatalf("complete pass pruned %v, want exactly %q", pruned, gonePath)
	}
	for _, table := range []string{"blocks", "page_core", "page_types", "page_properties"} {
		if n := count(table); n != 0 {
			t.Errorf("complete pass did not clear the vanished page's %s", table)
		}
	}
	known, err := dbMgr.KnownFiles()
	if err != nil {
		t.Fatalf("KnownFiles: %v", err)
	}
	if _, ok := known[linkedPath]; !ok {
		t.Error("vault prune dropped a path outside the vault root (linked notebook skip key)")
	}
	if _, ok := known[gonePath]; ok {
		t.Error("complete pass kept the vanished path's skip key")
	}
}

// TestVaultOpen_IncompleteScanDoesNotPruneOrClear pins the gate end-to-end: a
// page deleted while Silt was closed is pruned on the next complete open, but
// an open that could not read another file is blind, so the vanished path must
// survive that pass — otherwise a transient read failure permanently wipes a
// page whose file is still on disk. Recovery on the following complete open
// also proves the blindness was what held it, not a broken prune.
func TestVaultOpen_IncompleteScanDoesNotPruneOrClear(t *testing.T) {
	vaultPath := newInventoryVault(t)
	gonePath := filepath.Join(vaultPath, "Books", "Gone.md")
	blindPath := filepath.Join(vaultPath, "Books", "Blind.md")
	writeFile(t, gonePath, typedPageBody("Books", "", "Gone", "aaaa1111-0000-4000-8000-000000000015"))
	writeFile(t, blindPath, typedPageBody("Books", "", "Blind", "aaaa1111-0000-4000-8000-000000000016"))
	settleInventoryVault(t, vaultPath)

	if err := os.Remove(gonePath); err != nil {
		t.Fatalf("remove gone page: %v", err)
	}

	if err := os.Chmod(blindPath, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(blindPath, 0o644) })
	skipUnlessPermissionsEnforced(t, func() error {
		_, err := os.ReadFile(blindPath)
		return err
	})

	app2 := &App{spacesPerTab: 4}
	if err := app2.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("blind open: %v", err)
	}
	if blocks := blockCount(t, app2, "vault", "Books", "", "Gone"); blocks == 0 {
		t.Error("blind open cleared blocks of a path it could not confirm was deleted")
	}
	if types, props, core := inventoryProjectionCounts(t, app2, "Books", "", "Gone"); types != 1 || props != 1 || core != 1 {
		t.Errorf("blind open cleared projections: page_types=%d page_properties=%d page_core=%d", types, props, core)
	}
	if known, err := app2.db.KnownFiles(); err != nil {
		t.Fatalf("KnownFiles: %v", err)
	} else if _, ok := known[gonePath]; !ok {
		t.Error("blind open pruned the vanished path's skip key")
	}
	if err := app2.CloseVault(); err != nil {
		t.Fatalf("CloseVault (blind): %v", err)
	}

	if err := os.Chmod(blindPath, 0o644); err != nil {
		t.Fatalf("restore chmod: %v", err)
	}
	app3 := &App{spacesPerTab: 4}
	if err := app3.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("complete open: %v", err)
	}
	defer func() { _ = app3.CloseVault() }()
	if blocks := blockCount(t, app3, "vault", "Books", "", "Gone"); blocks != 0 {
		t.Error("complete open did not prune the vanished page")
	}
	if _, props, core := inventoryProjectionCounts(t, app3, "Books", "", "Gone"); props != 0 || core != 0 {
		t.Error("complete open did not clear the vanished page's projections")
	}
}

// TestScanInventory_CompletenessTable pins the default-deny derivation case by
// case: only a resolved file is live evidence, and only a pass with no walk
// error and no per-file error may arm deletion.
func TestScanInventory_CompletenessTable(t *testing.T) {
	resolved := parser.ScanResult{Notebook: "NB", Section: "Sec", Page: "Page"}
	rootLevel := parser.ScanResult{Path: "/vault/Root.md"}
	readErr := parser.ScanResult{Notebook: "NB", Page: "Broken", Err: os.ErrPermission}
	parseErr := parser.ScanResult{Path: "/vault/NB/Bad.md", Err: os.ErrInvalid}

	cases := []struct {
		name      string
		results   []parser.ScanResult
		walkErrs  []string
		wantLive  int
		wantCompl bool
		describe  string
	}{
		{"clean scan", []parser.ScanResult{resolved}, nil, 1, true, "every file resolved"},
		{"read error", []parser.ScanResult{resolved, readErr}, nil, 1, false, "an unreadable file may still exist"},
		{"parse error", []parser.ScanResult{resolved, parseErr}, nil, 1, false, "an unparsed file may still exist"},
		{"walk error", []parser.ScanResult{resolved}, []string{"/vault/NB: permission denied"}, 1, false, "an un-walkable subtree may still exist"},
		{"root-level file", []parser.ScanResult{resolved, rootLevel}, nil, 1, true, "a file the layout rules reject is never indexable"},
		{"symlink skip", []parser.ScanResult{resolved}, nil, 1, true, "skips are not passed as walk errors"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			live, complete := scanInventory(c.results, c.walkErrs, "vault")
			if complete != c.wantCompl {
				t.Errorf("complete = %v, want %v (%s)", complete, c.wantCompl, c.describe)
			}
			if len(live) != c.wantLive {
				t.Errorf("live = %d loc(s), want %d", len(live), c.wantLive)
			}
			for _, loc := range live {
				if loc.Source != "vault" {
					t.Errorf("live loc not source-scoped: %+v", loc)
				}
			}
		})
	}

	// A linked scan stamps its own source rather than inheriting the vault's.
	fromLinked := []parser.ScanResult{{Notebook: "Ext", Page: "P", Source: "linked:x"}}
	live, _ := scanInventory(fromLinked, nil, "linked:x")
	if len(live) != 1 || live[0].Source != "linked:x" {
		t.Errorf("linked scan loc = %+v, want source linked:x", live)
	}
}
