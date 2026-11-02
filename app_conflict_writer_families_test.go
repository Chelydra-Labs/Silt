package main

// Regression tests for the external-edit machinery's contract with the other
// whole-file writer families (#1005): every in-lock writer re-baselines via
// trackedPageWrite (TestInLockWriters_RefreshBaseline_SaveMustPass), the
// cross-page plugin move's source rewrite consults the stash and anchors on
// disk, the refused-write stash follows the page's lifecycle
// (rename/delete/teardown), and a programmatic refusal never evicts an
// editor-owned stash.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"silt/backend/core"
	"silt/backend/parser"
)

// seedWriterFamilyPage stages a task page directly on disk + in the index
// (no save funnel, so no baseline yet) and returns the parsed blocks.
// blockID must be globally unique in the app — blocks.id is a global key.
func seedWriterFamilyPage(t *testing.T, app *App, notebook, section, page, blockID, body string) []parser.ParsedBlock {
	t.Helper()
	_ = blockID // ids are embedded in body by the caller (globally unique)
	content := "---\nnotebook: \"" + notebook + "\"\nsection: \"" + section + "\"\npage: \"" + page + "\"\ndate: \"2026-01-01\"\ntags: []\n---\n" + body
	filePath := filepath.Join(app.vaultPath, notebook, section, page+".md")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	blocks, meta, _, _, err := parser.ParseFileContent(content, notebook, section, page, "2026-01-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if err := app.db.IndexFileBlocks("vault", meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return blocks
}

// famID mints a deterministic UUID-shaped id per (row, slot) so rows never
// share a blocks.id (globally unique) while staying readable in failures.
func famID(row, slot byte) string {
	return fmt.Sprintf("%08x-1111-4111-8111-1111111111%02x", uint32(row)<<8|uint32(slot), slot)
}

// TestInLockWriters_RefreshBaseline_SaveMustPass pins the trackedPageWrite
// contract for every in-lock writer family: after the editor loads a page
// (baseline captured), a setter's rewrite must re-baseline in the same lock,
// so the editor's next save passes instead of false-refusing — a refusal that
// (under the stash-authoritative gate) would mint a phantom stash. Delete any
// family's refresh and its row fails.
func TestInLockWriters_RefreshBaseline_SaveMustPass(t *testing.T) {
	app := newTestApp(t)
	notebook, section := "FamNB", ""

	type row struct {
		name   string
		page   string
		idA    string
		idB    string
		body   func(idA, idB string) string
		mutate func(app *App, idA, idB string, blocks []parser.ParsedBlock) error
	}
	task := func(id, extra string) string {
		return "- [ ] ship " + extra + "<!-- id: " + id + " -->\n"
	}
	rows := []row{
		{
			name: "UpdateBlockState", page: "FamState", idA: famID(1, 1),
			body: func(idA, _ string) string { return task(idA, "") },
			mutate: func(app *App, idA, _ string, _ []parser.ParsedBlock) error {
				_, err := app.UpdateBlockState(idA, "DONE")
				return err
			},
		},
		{
			name: "MutateBlock", page: "FamMutate", idA: famID(2, 1),
			body: func(idA, _ string) string { return task(idA, "") },
			mutate: func(app *App, idA, _ string, _ []parser.ParsedBlock) error {
				return app.MutateBlock(idA, "mutated body")
			},
		},
		{
			name: "SetTaskOwner", page: "FamOwner", idA: famID(3, 1),
			body: func(idA, _ string) string { return task(idA, "") },
			mutate: func(app *App, idA, _ string, _ []parser.ParsedBlock) error {
				return app.SetTaskOwner(idA, "@bob")
			},
		},
		{
			name: "SetTaskPriority", page: "FamPriority", idA: famID(4, 1),
			body: func(idA, _ string) string { return task(idA, "") },
			mutate: func(app *App, idA, _ string, _ []parser.ParsedBlock) error {
				return app.SetTaskPriority(idA, 2)
			},
		},
		{
			name: "SetTaskRecurrence", page: "FamRecurrence", idA: famID(5, 1),
			body: func(idA, _ string) string { return task(idA, "[due:: 2026-08-01] ") },
			mutate: func(app *App, idA, _ string, _ []parser.ParsedBlock) error {
				return app.SetTaskRecurrence(idA, "every week")
			},
		},
		{
			name: "SetTaskBlockedBy", page: "FamBlockedBy", idA: famID(6, 1), idB: famID(6, 2),
			body: func(idA, idB string) string { return task(idA, "") + task(idB, "") },
			mutate: func(app *App, idA, idB string, _ []parser.ParsedBlock) error {
				return app.SetTaskBlockedBy(idA, []string{idB})
			},
		},
		{
			name: "SetTaskOrders", page: "FamOrders", idA: famID(7, 1), idB: famID(7, 2),
			body: func(idA, idB string) string { return task(idA, "") + task(idB, "") },
			mutate: func(app *App, idA, idB string, _ []parser.ParsedBlock) error {
				return app.SetTaskOrders([]string{idA, idB}, []int{2, 1})
			},
		},
		{
			name: "SaveSubtreeBlocks", page: "FamSubtree", idA: famID(8, 1),
			body: func(idA, _ string) string { return task(idA, "") },
			mutate: func(app *App, idA, _ string, _ []parser.ParsedBlock) error {
				_, err := app.SaveSubtreeBlocks(idA, nil)
				return err
			},
		},
		{
			name: "AppendTaskComment", page: "FamComment", idA: famID(9, 1),
			body: func(idA, _ string) string { return task(idA, "") },
			mutate: func(app *App, idA, _ string, _ []parser.ParsedBlock) error {
				_, err := app.AppendTaskComment(idA, "a comment", "", "", "")
				return err
			},
		},
	}

	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			blocks := seedWriterFamilyPage(t, app, notebook, section, r.page, r.idA, r.body(r.idA, r.idB))
			// The editor load — captures the baseline the setter is about to
			// invalidate with its own rewrite.
			if _, err := app.FetchPageBlocks(notebook, section, r.page); err != nil {
				t.Fatalf("FetchPageBlocks: %v", err)
			}
			if err := r.mutate(app, r.idA, r.idB, blocks); err != nil {
				t.Fatalf("%s: %v", r.name, err)
			}
			// The editor's small edit must save without a false conflict.
			if err := app.SaveFileBlocks(notebook, section, r.page,
				editBlocks(blocks, r.idA, "editor edit after "+r.name)); err != nil {
				t.Fatalf("%s did not re-baseline in-lock — editor save refused: %v", r.name, err)
			}
		})
	}
}

// TestCreatePaths_RefreshAbsentBaseline_SaveMustPass covers the creation
// writers (advisory folded into the trackedPageWrite migration): an editor
// that visited a not-yet-existing name leaves an absent baseline; the
// creation write must re-baseline it or the first save phantom-refuses.
func TestCreatePaths_RefreshAbsentBaseline_SaveMustPass(t *testing.T) {
	app := newTestApp(t)
	notebook, page := "CreateNB", "Created"

	// Editor visits the not-yet-existing page — stores the absent baseline.
	if _, err := app.FetchPageBlocks(notebook, "", page); err != nil {
		t.Fatalf("FetchPageBlocks (absent): %v", err)
	}
	if _, err := app.CreatePage(notebook, "", page, "2026-01-01"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	blockID := famID(20, 1)
	body := "created body <!-- id: " + blockID + " -->\n"
	content := "---\nnotebook: \"CreateNB\"\nsection: \"\"\npage: \"Created\"\ndate: \"2026-01-01\"\ntags: []\n---\n" + body
	blocks, _, _, _, err := parser.ParseFileContent(content, notebook, "", page, "2026-01-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if err := app.SaveFileBlocks(notebook, "", page, blocks); err != nil {
		t.Fatalf("save after CreatePage must pass, got: %v", err)
	}
}

// TestPluginCrossPageMove_RefusesWhileStashPendingOnSource pins the
// source-page half of a cross-page move: a pending refused-write stash on the
// SOURCE page must refuse the source rewrite (the target-page half is already
// covered by TestExternalEdit_PluginRMWSaveRefusesWhileStashPending) — and
// the refusal must leave the source bytes untouched.
func TestPluginCrossPageMove_RefusesWhileStashPendingOnSource(t *testing.T) {
	app := newTestApp(t)
	notebook, section := "XMoveNB", ""
	srcPage, dstPage := "XSrc", "XDst"
	blockID := famID(21, 1)
	staysID := famID(21, 2)

	srcBody := "- [ ] moved block <!-- id: " + blockID + " -->\n- [ ] stays <!-- id: " + staysID + " -->\n"
	blocks := seedWriterFamilyPage(t, app, notebook, section, srcPage, blockID, srcBody)
	seedWriterFamilyPage(t, app, notebook, section, dstPage, famID(21, 3), "")

	if _, err := app.FetchPageBlocks(notebook, section, srcPage); err != nil {
		t.Fatalf("FetchPageBlocks source: %v", err)
	}
	srcPath := filepath.Join(app.vaultPath, notebook, section, srcPage+".md")
	external := "---\nnotebook: \"XMoveNB\"\npage: \"XSrc\"\ntags: []\n---\nexternal source bytes\n"
	if err := os.WriteFile(srcPath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	requireConflict(t, app.SaveFileBlocks(notebook, section, srcPage,
		editBlocks(blocks, blockID, "my unsaved source edit")))

	// Cross-page move while the stash is live: the source rewrite must
	// refuse, not revert the external bytes from a snapshot.
	err := app.applyBlocksOps([]PluginCreateBlockOp{{
		Kind: "move", BlockID: blockID,
		Notebook: notebook, Section: section, Page: dstPage,
	}}, historyReasonPlugin)
	requireConflict(t, err)

	got, _ := os.ReadFile(srcPath)
	if string(got) != external {
		t.Fatalf("refused move still rewrote the source file:\n got: %q\nwant: %q", string(got), external)
	}
}

// TestPluginCrossPageMove_SourceRewriteAnchorsOnDiskBytes pins the RMW anchor
// of the source rewrite: an external edit the watcher has not re-indexed must
// survive the move's source rewrite (managed blocks re-render from CURRENT
// disk bytes, never from the DB snapshot).
func TestPluginCrossPageMove_SourceRewriteAnchorsOnDiskBytes(t *testing.T) {
	app := newTestApp(t)
	notebook, section := "XMoveNB2", ""
	srcPage, dstPage := "XSrc2", "XDst2"
	blockID := famID(22, 1)

	srcBody := "- [ ] moved block <!-- id: " + blockID + " -->\n"
	seedWriterFamilyPage(t, app, notebook, section, srcPage, blockID, srcBody)
	seedWriterFamilyPage(t, app, notebook, section, dstPage, famID(22, 2), "")

	// External edit: replaces the file wholesale with content the DB has
	// never indexed (an unmanaged line + a different managed task).
	srcPath := filepath.Join(app.vaultPath, notebook, section, srcPage+".md")
	external := "---\nnotebook: \"XMoveNB2\"\npage: \"XSrc2\"\ntags: []\n---\nexternal prose line\n- [ ] external task <!-- id: " + famID(22, 3) + " -->\n"
	if err := os.WriteFile(srcPath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	if err := app.applyBlocksOps([]PluginCreateBlockOp{{
		Kind: "move", BlockID: blockID,
		Notebook: notebook, Section: section, Page: dstPage,
	}}, historyReasonPlugin); err != nil {
		t.Fatalf("cross-page move: %v", err)
	}

	got, _ := os.ReadFile(srcPath)
	gotStr := string(got)
	if !strings.Contains(gotStr, "external prose line") {
		t.Fatalf("source rewrite reverted external unmanaged content, got %q", gotStr)
	}
	if !strings.Contains(gotStr, "external task") {
		t.Fatalf("source rewrite reverted external managed content, got %q", gotStr)
	}
	if strings.Contains(gotStr, "moved block") {
		t.Fatalf("moved block still present on source after move, got %q", gotStr)
	}
}

// TestRenamePage_MigratesPendingStash: a refused-write stash must follow the
// page across a rename — resolution stays reachable at the new name, the old
// name inherits nothing.
func TestRenamePage_MigratesPendingStash(t *testing.T) {
	app := newTestApp(t)
	notebook, section := "RnmNB", ""
	page := "RnmOld"
	blockID := famID(23, 1)
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "- [ ] original task")

	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	filePath := conflictPagePath(app, notebook, section, page)
	external := "---\nnotebook: \"RnmNB\"\npage: \"RnmOld\"\ntags: []\n---\nexternal bytes\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	requireConflict(t, app.SaveFileBlocks(notebook, section, page,
		editBlocks(blocks, blockID, "my unsaved rename-era text")))

	if err := app.RenamePage(notebook, section, page, "RnmNew"); err != nil {
		t.Fatalf("RenamePage: %v", err)
	}

	// The old name has nothing pending; the new name resolves.
	if _, err := app.ResolveExternalConflict(notebook, section, page, "keep_mine"); err == nil {
		t.Fatal("ResolveExternalConflict at the OLD name must fail — the stash migrated to the new path")
	}
	if _, err := app.ResolveExternalConflict(notebook, section, "RnmNew", "keep_mine"); err != nil {
		t.Fatalf("ResolveExternalConflict at the NEW name: %v", err)
	}
	newPath := filepath.Join(app.vaultPath, notebook, section, "RnmNew.md")
	got, _ := os.ReadFile(newPath)
	if !strings.Contains(string(got), "my unsaved rename-era text") {
		t.Fatalf("keep_mine must replay the migrated stash at the new path, got %q", string(got))
	}
	// keep_mine archives theirs to the sibling (named after the stashed
	// page); both versions survive.
	siblings, _ := filepath.Glob(filepath.Join(app.vaultPath, notebook, section, "*.conflict-*.md"))
	if len(siblings) == 0 {
		t.Fatal("keep_mine must archive theirs to a conflict sibling")
	}
}

// TestRenameSection_MigratesPendingStash: the tree-rename core migrates every
// descendant page's stash to its new path.
func TestRenameSection_MigratesPendingStash(t *testing.T) {
	app := newTestApp(t)
	notebook := "RnmSecNB"
	const oldSec, newSec = "OldSec", "NewSec"
	page := "Stashed"
	blockID := famID(24, 1)
	blocks := seedConflictPage(t, app, notebook, oldSec, page, blockID, "- [ ] original task")

	if _, err := app.FetchPageBlocks(notebook, oldSec, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	filePath := conflictPagePath(app, notebook, oldSec, page)
	external := "---\nnotebook: \"RnmSecNB\"\nsection: \"OldSec\"\npage: \"Stashed\"\ntags: []\n---\nexternal bytes\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	requireConflict(t, app.SaveFileBlocks(notebook, oldSec, page,
		editBlocks(blocks, blockID, "my stashed section text")))

	if err := app.RenameSection(notebook, oldSec, newSec); err != nil {
		t.Fatalf("RenameSection: %v", err)
	}
	if _, err := app.ResolveExternalConflict(notebook, newSec, page, "keep_mine"); err != nil {
		t.Fatalf("ResolveExternalConflict at the new section: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(app.vaultPath, notebook, newSec, page+".md"))
	if !strings.Contains(string(got), "my stashed section text") {
		t.Fatalf("keep_mine must replay the migrated stash, got %q", string(got))
	}
}

// TestDeletePage_DropsPendingStash: deleting a conflicted page drops its
// stash and baseline — a page later recreated at the path must save cleanly,
// never inheriting the dead conflict.
func TestDeletePage_DropsPendingStash(t *testing.T) {
	app := newTestApp(t)
	notebook, section := "DelNB", ""
	page := "DelPage"
	blockID := famID(25, 1)
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "- [ ] original task")

	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	filePath := conflictPagePath(app, notebook, section, page)
	external := "---\nnotebook: \"DelNB\"\npage: \"DelPage\"\ntags: []\n---\nexternal bytes\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	requireConflict(t, app.SaveFileBlocks(notebook, section, page,
		editBlocks(blocks, blockID, "my dead-end text")))

	if err := app.DeletePage(notebook, section, page); err != nil {
		t.Fatalf("DeletePage: %v", err)
	}
	if _, err := app.ResolveExternalConflict(notebook, section, page, "keep_mine"); err == nil {
		t.Fatal("ResolveExternalConflict after delete must fail — the stash died with the page")
	}

	// Recreate a page at the same path and load + save it: no phantom
	// refusal, no foreign stash replay.
	if _, err := app.CreatePage(notebook, section, page, "2026-01-01"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks (recreated): %v", err)
	}
	if err := app.SaveFileBlocks(notebook, section, page, blocks); err != nil {
		t.Fatalf("save on the recreated page must pass, got: %v", err)
	}
	got, _ := os.ReadFile(filePath)
	if strings.Contains(string(got), "my dead-end text") {
		t.Fatalf("recreated page must never inherit the foreign stash, got %q", string(got))
	}
}

// TestTeardownClearsConflictState: vault teardown drops every stash and
// baseline — they are keyed by absolute path into the closed vault.
func TestTeardownClearsConflictState(t *testing.T) {
	app := newTestApp(t)
	notebook, section := "TearNB", ""
	page := "TearPage"
	blockID := famID(26, 1)
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "- [ ] original task")

	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	filePath := conflictPagePath(app, notebook, section, page)
	external := "---\nnotebook: \"TearNB\"\npage: \"TearPage\"\ntags: []\n---\nexternal bytes\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	requireConflict(t, app.SaveFileBlocks(notebook, section, page,
		editBlocks(blocks, blockID, "my teardown-era text")))

	app.teardownVaultServices()

	if app.refusedWrites != nil || app.pageLoadFingerprints != nil {
		t.Fatalf("teardown must clear conflict state, got stash=%v fingerprints=%v",
			app.refusedWrites != nil, app.pageLoadFingerprints != nil)
	}
}

// TestMCPWriteDuringPendingEditorStash_NeverEvictsEditorStash pins the funnel
// identity: an MCP whole-page write refused while an editor save is stashed
// must never replace the stash — keep_mine replays the user's typed buffer,
// not the programmatic payload.
func TestMCPWriteDuringPendingEditorStash_NeverEvictsEditorStash(t *testing.T) {
	app := newTestApp(t)
	notebook, section := "McpNB", ""
	page := "McpPage"
	blockID := famID(27, 1)
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "- [ ] original task")

	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	filePath := conflictPagePath(app, notebook, section, page)
	external := "---\nnotebook: \"McpNB\"\npage: \"McpPage\"\ntags: []\n---\nexternal bytes\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	// Editor refusal first: the stash is the user's typed buffer.
	requireConflict(t, app.SaveFileBlocks(notebook, section, page,
		editBlocks(blocks, blockID, "my typed editor text")))

	// MCP whole-page write refused through the same gate: same-funnel shape
	// (blocks), but programmatic — it must not evict the editor's stash.
	requireConflict(t, app.saveFileBlocksProgrammatic(notebook, section, page,
		editBlocks(blocks, blockID, "mcp client payload"), historyReasonMCP))

	if _, err := app.ResolveExternalConflict(notebook, section, page, "keep_mine"); err != nil {
		t.Fatalf("ResolveExternalConflict: %v", err)
	}
	got, _ := os.ReadFile(filePath)
	if !strings.Contains(string(got), "my typed editor text") {
		t.Fatalf("keep_mine must replay the EDITOR's stashed buffer, not the MCP payload, got %q", string(got))
	}
}

// TestEditorWriteDuringPendingProgrammaticStash_ReplacesIt: the reverse order
// — a programmatic refusal stashed first is superseded by the user's later
// editor refusal; keep_mine replays the editor's buffer (the user's typing is
// the version worth keeping; the programmatic caller already got its error).
func TestEditorWriteDuringPendingProgrammaticStash_ReplacesIt(t *testing.T) {
	app := newTestApp(t)
	notebook, section := "McpNB2", ""
	page := "McpPage2"
	blockID := famID(28, 1)
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "- [ ] original task")

	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	filePath := conflictPagePath(app, notebook, section, page)
	external := "---\nnotebook: \"McpNB2\"\npage: \"McpPage2\"\ntags: []\n---\nexternal bytes\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	// Programmatic refusal first (MCP client tried to write the externally
	// edited page and got the typed error back).
	requireConflict(t, app.saveFileBlocksProgrammatic(notebook, section, page,
		editBlocks(blocks, blockID, "mcp client payload"), historyReasonMCP))
	// The user's editor save refused afterwards replaces the programmatic
	// stash (the editor's newest buffer is the version worth keeping).
	requireConflict(t, app.SaveFileBlocks(notebook, section, page,
		editBlocks(blocks, blockID, "my later editor text")))

	if _, err := app.ResolveExternalConflict(notebook, section, page, "keep_mine"); err != nil {
		t.Fatalf("ResolveExternalConflict: %v", err)
	}
	got, _ := os.ReadFile(filePath)
	if !strings.Contains(string(got), "my later editor text") {
		t.Fatalf("keep_mine must replay the editor's buffer over the programmatic stash, got %q", string(got))
	}
}

// TestCrossFunnelEditorStashes_KeepFirst pins the preserved cross-funnel
// semantics after the funnel-discriminator migration: an editor Source-mode
// refusal never evicts an editor blocks-mode stash (and vice versa).
func TestCrossFunnelEditorStashes_KeepFirst(t *testing.T) {
	app := newTestApp(t)
	notebook, section := "XFunNB", ""
	page := "XFunPage"
	blockID := famID(29, 1)
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "- [ ] original task")

	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	filePath := conflictPagePath(app, notebook, section, page)
	external := "---\nnotebook: \"XFunNB\"\npage: \"XFunPage\"\ntags: []\n---\nexternal bytes\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	requireConflict(t, app.SaveFileBlocks(notebook, section, page,
		editBlocks(blocks, blockID, "my blocks-funnel text")))
	_, err := app.SavePageMarkdown(notebook, section, page, "my source-funnel text\n")
	requireConflict(t, err)

	// The blocks-funnel stash (first refusal) is the one keep_mine replays.
	if _, rerr := app.ResolveExternalConflict(notebook, section, page, "keep_mine"); rerr != nil {
		t.Fatalf("ResolveExternalConflict: %v", rerr)
	}
	got, _ := os.ReadFile(filePath)
	if !strings.Contains(string(got), "my blocks-funnel text") {
		t.Fatalf("cross-funnel keep-first must preserve the blocks stash, got %q", string(got))
	}
}

// TestMigrateConflictStateAcrossRename_TargetStashIsDropped: a stale stash
// keyed at the rename TARGET path belongs to a vanished page — the renamed
// page's live stash replaces it rather than dead-ending behind a foreign one.
func TestMigrateConflictStateAcrossRename_TargetStashIsDropped(t *testing.T) {
	app := newTestApp(t)
	oldPath := filepath.Join(app.vaultPath, "TgtNB", "Old.md")
	newPath := filepath.Join(app.vaultPath, "TgtNB", "New.md")
	oldKey, newKey := core.NormalizeFileLockPath(oldPath), core.NormalizeFileLockPath(newPath)
	app.storeRefusedWrite(oldKey, refusedPageWrite{notebook: "TgtNB", page: "Old", funnel: funnelEditorBlocks})
	app.storeRefusedWrite(newKey, refusedPageWrite{notebook: "TgtNB", page: "New", funnel: funnelEditorBlocks})

	app.migrateConflictStateAcrossRename(oldPath, newPath)

	if _, ok := app.popRefusedWrite(oldKey); ok {
		t.Fatal("old-path stash must migrate away")
	}
	got, ok := app.popRefusedWrite(newKey)
	if !ok || got.page != "Old" {
		t.Fatalf("new path must hold the MIGRATED stash, got %+v ok=%v", got, ok)
	}
}

// TestProgrammaticFetchDoesNotRebaselineEditorGate pins the frontend capture
// contract from the batch side (#1005): global replace / template seeding
// fetch pages through the non-capturing variant, so a page drifted under a
// clean editor keeps refusing the editor's saves. The editor-facing fetch is
// asserted to re-baseline (documents exactly what the non-capturing variant
// must never do — the silent disarm the modal used to perform).
func TestProgrammaticFetchDoesNotRebaselineEditorGate(t *testing.T) {
	app := newTestApp(t)
	notebook, section := "CapNB", ""
	page := "CapPage"
	blockID := famID(25, 1)
	blocks := seedWriterFamilyPage(t, app, notebook, section, page, blockID,
		"- [ ] original task <!-- id: "+blockID+" -->\n")

	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks (editor load): %v", err)
	}
	filePath := filepath.Join(app.vaultPath, notebook, section, page+".md")
	external := "---\nnotebook: \"CapNB\"\npage: \"CapPage\"\ntags: []\n---\nexternal bytes\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	// Contrast first: the editor-facing fetch DOES re-baseline (that is the
	// disarm — asserted so a regression of the non-capturing routing shows
	// up as this branch passing where the fix branch fails).
	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks (editor re-load): %v", err)
	}
	if err := app.SaveFileBlocks(notebook, section, page,
		editBlocks(blocks, blockID, "post-rebaseline clobber")); err != nil {
		t.Fatalf("editor-facing re-baseline disarmed the gate — save should pass (contrast arm): %v", err)
	}
	// Restore the external bytes and re-arm the baseline honestly.
	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks (post-contrast load): %v", err)
	}
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatalf("external rewrite: %v", err)
	}

	// The fix arm: the programmatic fetch must NOT move the baseline, so
	// the editor's next save still refuses.
	if _, err := app.FetchPageBlocksProgrammatic(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocksProgrammatic: %v", err)
	}
	requireConflict(t, app.SaveFileBlocks(notebook, section, page,
		editBlocks(blocks, blockID, "editor edit after programmatic fetch")))
	got, _ := os.ReadFile(filePath)
	if string(got) != external {
		t.Fatalf("refused editor save must leave the external bytes untouched:\n got: %q\nwant: %q", string(got), external)
	}
}

// TestGlobalReplaceProgrammaticFunnel_NeverEvictsEditorStash pins the batch
// save funnel (#1005): global replace's SaveFileBlocksProgrammatic refusal
// while an editor-owned stash is live must refuse WITHOUT replacing the
// stash — the editor buffer may be the user's only surviving copy, while the
// modal's rendered page is re-derivable from its own state.
func TestGlobalReplaceProgrammaticFunnel_NeverEvictsEditorStash(t *testing.T) {
	app := newTestApp(t)
	notebook, section := "GrNB", ""
	page := "GrPage"
	blockID := famID(26, 1)
	blocks := seedWriterFamilyPage(t, app, notebook, section, page, blockID,
		"- [ ] original task <!-- id: "+blockID+" -->\n")

	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	filePath := filepath.Join(app.vaultPath, notebook, section, page+".md")
	external := "---\nnotebook: \"GrNB\"\npage: \"GrPage\"\ntags: []\n---\nexternal bytes\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	// The editor's refused save — the stash holding the user's typed text.
	requireConflict(t, app.SaveFileBlocks(notebook, section, page,
		editBlocks(blocks, blockID, "my only typed copy")))

	// Global replace's batch save against the same page: refused, and the
	// refusal must not have replaced the editor's stash.
	requireConflict(t, app.SaveFileBlocksProgrammatic(notebook, section, page,
		editBlocks(blocks, blockID, "replace-rendered buffer")))

	// keep_mine must replay the EDITOR's buffer, not the modal's.
	if _, err := app.ResolveExternalConflict(notebook, section, page, "keep_mine"); err != nil {
		t.Fatalf("ResolveExternalConflict: %v", err)
	}
	got, _ := os.ReadFile(filePath)
	if !strings.Contains(string(got), "my only typed copy") {
		t.Fatalf("keep_mine must replay the editor's stashed buffer, got %q", string(got))
	}
	if strings.Contains(string(got), "replace-rendered buffer") {
		t.Fatalf("the modal's payload displaced the editor stash, got %q", string(got))
	}
}

// TestPluginBlockOp_TargetSaveAnchorsOnDiskBytes pins the target-page half of
// a plugin block op (the source half is
// TestPluginCrossPageMove_SourceRewriteAnchorsOnDiskBytes): an external edit
// the watcher has not re-indexed must survive the op's whole-page render —
// the block base is reconstructed from CURRENT disk bytes inside the file
// lock, never from the DB snapshot.
func TestPluginBlockOp_TargetSaveAnchorsOnDiskBytes(t *testing.T) {
	app := newTestApp(t)
	notebook, section := "TgtAnchorNB", ""
	page := "TgtAnchor"
	blockID := famID(27, 1)

	body := "- [ ] indexed task <!-- id: " + blockID + " -->\n"
	seedWriterFamilyPage(t, app, notebook, section, page, blockID, body)

	// External edit the DB has never indexed: a new managed task text plus
	// an unmanaged prose line.
	filePath := filepath.Join(app.vaultPath, notebook, section, page+".md")
	external := "---\nnotebook: \"TgtAnchorNB\"\npage: \"TgtAnchor\"\ntags: []\n---\nexternal prose line\n- [ ] externally edited task <!-- id: " + blockID + " -->\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	if err := app.applyBlocksOps([]PluginCreateBlockOp{{
		Kind:     "create",
		Type:     "task",
		Text:     "plugin-created task",
		Notebook: notebook, Section: section, Page: page,
	}}, historyReasonPlugin); err != nil {
		t.Fatalf("applyBlocksOps: %v", err)
	}

	got, _ := os.ReadFile(filePath)
	gotStr := string(got)
	if !strings.Contains(gotStr, "externally edited task") {
		t.Fatalf("plugin block-op target save reverted an externally edited managed block, got %q", gotStr)
	}
	if !strings.Contains(gotStr, "external prose line") {
		t.Fatalf("plugin block-op target save reverted external unmanaged content, got %q", gotStr)
	}
	if !strings.Contains(gotStr, "plugin-created task") {
		t.Fatalf("plugin-created block missing after op, got %q", gotStr)
	}
}

// TestMigrateConflictStateAcrossRename_OrphanedTargetStashIsDropped: with NO
// stash at the old path, a stash keyed at the rename TARGET still belongs to
// a vanished page and must be dropped — it used to survive nested inside the
// old-path branch and phantom-refuse the renamed page.
func TestMigrateConflictStateAcrossRename_OrphanedTargetStashIsDropped(t *testing.T) {
	app := newTestApp(t)
	oldPath := filepath.Join(app.vaultPath, "TgtOnlyNB", "Old.md")
	newPath := filepath.Join(app.vaultPath, "TgtOnlyNB", "New.md")
	newKey := core.NormalizeFileLockPath(newPath)
	app.storeRefusedWrite(newKey, refusedPageWrite{notebook: "TgtOnlyNB", page: "Vanished", funnel: funnelEditorBlocks})

	app.migrateConflictStateAcrossRename(oldPath, newPath)

	if _, ok := app.popRefusedWrite(newKey); ok {
		t.Fatal("orphaned target-keyed stash must be dropped by the rename migration")
	}
}

// TestUnverifiedConflictRefusal_DoesNotStash: a refusal that fired because
// the file's state could not be verified (stat ok / read failure — here a
// directory squatting on the page path) must not mint a refused-write stash:
// a transient error would otherwise phantom-hold the page behind a manual
// resolution that outlives the error (#1005).
func TestUnverifiedConflictRefusal_DoesNotStash(t *testing.T) {
	app := newTestApp(t)
	notebook, section := "UnverifiedNB", ""
	page := "UnverifiedPage"
	blockID := famID(28, 1)
	blocks := seedWriterFamilyPage(t, app, notebook, section, page, blockID,
		"- [ ] original task <!-- id: "+blockID+" -->\n")

	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	// Replace the page file with a directory: os.Stat succeeds (so the save
	// passes its page_moved guard) but the size drifts from the baseline
	// and os.ReadFile fails with EISDIR — a deterministic stat-ok/read-fail.
	filePath := filepath.Join(app.vaultPath, notebook, section, page+".md")
	if err := os.Remove(filePath); err != nil {
		t.Fatalf("remove page file: %v", err)
	}
	if err := os.Mkdir(filePath, 0o755); err != nil {
		t.Fatalf("mkdir over page path: %v", err)
	}

	err := app.SaveFileBlocks(notebook, section, page,
		editBlocks(blocks, blockID, "edit under unreadable state"))
	requireConflict(t, err)
	if !errors.Is(err, errExternalEditUnverified) {
		t.Fatalf("read-failure refusal must carry the unverified marker, got %v", err)
	}
	if app.refusedWritePending(core.NormalizeFileLockPath(filePath)) {
		t.Fatal("an unverifiable conflict must not mint a refused-write stash")
	}
}
