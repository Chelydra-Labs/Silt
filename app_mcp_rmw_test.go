package main

// Regression tests for the MCP read-modify-write external-edit guard
// (#1005): MCP fetches must not disarm the editor's load baseline
// (captureNone) but they DO capture a scoped read-basis, so update_blocks
// refuses when the file drifted since the client read it — without this the
// RMW funnel had no baseline at all and silently clobbered concurrent
// external edits.

import (
	"context"
	"os"
	"strings"
	"testing"

	"silt/backend/core"
	"silt/backend/parser"
)

// reindexConflictPage mimics the watcher's post-debounce re-index of an
// externally edited page: parse the CURRENT disk bytes, write back the
// normalized form when the parse minted id comments (the watcher's own
// normalization write — minted ids are random per parse, so skipping the
// write would leave rows and disk carrying different ids), then publish
// through the same atomic index path reindexFile uses. Tests run a bare App
// with no watcher, so without this the DB rows would stay pre-external
// forever.
func reindexConflictPage(t *testing.T, app *App, notebook, section, page string) {
	t.Helper()
	filePath := conflictPagePath(app, notebook, section, page)
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read page for reindex: %v", err)
	}
	blocks, meta, newContent, modified, perr := parser.ParseFileContent(string(content),
		notebook, section, page, fileOrDefaultDate(filePath), app.spacesPerTab)
	if perr != nil {
		t.Fatalf("parse page for reindex: %v", perr)
	}
	if modified {
		if werr := os.WriteFile(filePath, []byte(newContent), 0o644); werr != nil {
			t.Fatalf("write normalized page: %v", werr)
		}
	}
	source := app.resolveSourceByName(sanitizePathSegment(notebook))
	if err := app.indexFile(source, meta.Notebook, meta.Section, meta.Page, blocks, meta); err != nil {
		t.Fatalf("reindex: %v", err)
	}
}

// requireNoStash pins the client-basis half's no-stash policy: an MCP
// refusal must never mint a sticky stash only the desktop UI can clear —
// the programmatic caller holds its payload and its typed error already
// tells it to re-read and retry (#1005).
func requireNoStash(t *testing.T, app *App) {
	t.Helper()
	app.refusedWritesMu.Lock()
	defer app.refusedWritesMu.Unlock()
	if len(app.refusedWrites) != 0 {
		t.Fatalf("MCP client-basis refusal must not mint a stash, got %d pending", len(app.refusedWrites))
	}
}

func TestMCPReadModifyWrite_RefusesExternalEditDrift(t *testing.T) {
	app := newTestApp(t)
	bridge := mcpBridge{app: app, vaultPath: app.vaultPath}
	notebook, section, page := "McpRMW", "", "McpRMW"
	blockID := "dddddd6d-dddd-4ddd-8ddd-dddddddddddd"
	seedConflictPage(t, app, notebook, section, page, blockID, "original body")
	filePath := conflictPagePath(app, notebook, section, page)

	// The client reads the page (rows + scoped read-basis).
	rows, gen, err := bridge.FetchPageBlocks(context.Background(), notebook, section, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	if gen == 0 {
		t.Fatalf("blocks fetch must return a read generation")
	}

	// An external edit lands while the client is composing its update; the
	// watcher has not re-indexed.
	external := "---\nnotebook: \"McpRMW\"\npage: \"McpRMW\"\ntags: []\n---\nexternal body\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	// The client's write must refuse on the drift and leave the external
	// bytes untouched — at head the RMW funnel had no baseline and passed.
	err = bridge.UpdateBlocks(context.Background(), notebook, section, page,
		editBlocks(rows, blockID, "mcp edited body"), gen)
	requireConflict(t, err)
	requireNoStash(t, app)
	got, _ := os.ReadFile(filePath)
	if string(got) != external {
		t.Fatalf("refused MCP write must leave the external bytes untouched:\n got: %q\nwant: %q", string(got), external)
	}

	// The documented headless recovery — re-read + retry, no desktop
	// resolve: after the watcher re-indexes the external bytes, a fresh
	// read → write cycle passes because the basis re-anchored on the
	// settled rows (a fetch during the debounce window pins the basis to
	// the stale rows and would keep refusing — that pin is the point of
	// the rows-anchored capture).
	reindexConflictPage(t, app, notebook, section, page)
	rows2, gen2, err := bridge.FetchPageBlocks(context.Background(), notebook, section, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks (retry): %v", err)
	}
	if err := bridge.UpdateBlocks(context.Background(), notebook, section, page,
		editBlocks(rows2, blockID, "mcp edited body"), gen2); err != nil {
		t.Fatalf("update after re-read must pass, got: %v", err)
	}
	got, _ = os.ReadFile(filePath)
	if !strings.Contains(string(got), "mcp edited body") {
		t.Fatalf("retry write must land, got %q", string(got))
	}
}

// TestMCPUpdateBlocks_StrictRMW pins the basis lifecycle around a committed
// write (#1005): the write invalidates every prior fetch's basis, so an
// update built from the pre-write fetch — a second client's stale read, or
// a chained write without re-reading — refuses instead of silently
// reverting the committed edit. At the reviewed head the post-write
// recapture re-anchored the SHARED per-path basis on the written bytes in a
// second lock session, so exactly that stale update gated basis==disk,
// passed, and reverted the first client's write; the recapture could also
// adopt interleaved bytes the client never observed.
func TestMCPUpdateBlocks_StrictRMW(t *testing.T) {
	app := newTestApp(t)
	bridge := mcpBridge{app: app, vaultPath: app.vaultPath}
	notebook, section, page := "McpStrict", "", "McpStrict"
	blockID := "dddddd9d-dddd-4ddd-8ddd-dddddddddddd"
	seedConflictPage(t, app, notebook, section, page, blockID, "original body")
	filePath := conflictPagePath(app, notebook, section, page)

	// Both clients fetch the same D0.
	rows, gen, err := bridge.FetchPageBlocks(context.Background(), notebook, section, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}

	// Client A commits.
	if err := bridge.UpdateBlocks(context.Background(), notebook, section, page,
		editBlocks(rows, blockID, "client a edit"), gen); err != nil {
		t.Fatalf("client A update must pass, got: %v", err)
	}

	// Client B's update, built from the same pre-write fetch, must refuse —
	// and must not mint a stash (headless recovery is re-read + retry).
	requireConflict(t, bridge.UpdateBlocks(context.Background(), notebook, section, page,
		editBlocks(rows, blockID, "client b edit"), gen))
	requireNoStash(t, app)
	got, _ := os.ReadFile(filePath)
	if !strings.Contains(string(got), "client a edit") {
		t.Fatalf("stale update must not revert the committed write, got %q", string(got))
	}

	// The documented recovery: re-read (fresh basis on the settled rows)
	// then write — B's edit now lands.
	rows2, gen2, err := bridge.FetchPageBlocks(context.Background(), notebook, section, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks (re-read): %v", err)
	}
	if err := bridge.UpdateBlocks(context.Background(), notebook, section, page,
		editBlocks(rows2, blockID, "client b edit"), gen2); err != nil {
		t.Fatalf("update after re-read must pass, got: %v", err)
	}
	got, _ = os.ReadFile(filePath)
	if !strings.Contains(string(got), "client b edit") {
		t.Fatalf("re-read update must land, got %q", string(got))
	}
}

// TestMCPUpdateBlocks_StrictRMW_InterleavingFetchDoesNotRearmStaleGenerations
// pins the boundary of the strict-RMW invalidation (#1005): the read-basis
// slot is one per path and last-writer-wins, so the invalidation marker dies
// the moment ANY fetch re-arms the slot — including the committing client's
// own re-read. Named trigger: A fetch → B fetch → A commits → A re-reads →
// B's stale pre-commit update. At the reviewed head B's update gated
// basis==disk (the slot now described A's committed bytes), passed, and
// silently reverted A's write; only the per-path monotonic fetch generation
// (echoed as read_generation) still identifies B's fetch as pre-commit.
func TestMCPUpdateBlocks_StrictRMW_InterleavingFetchDoesNotRearmStaleGenerations(t *testing.T) {
	app := newTestApp(t)
	bridge := mcpBridge{app: app, vaultPath: app.vaultPath}
	notebook, section, page := "McpStrict2", "", "McpStrict2"
	blockID := "dddddd3d-dddd-4ddd-8ddd-dddddddddd3d"
	seedConflictPage(t, app, notebook, section, page, blockID, "original body")
	filePath := conflictPagePath(app, notebook, section, page)

	// A fetch → B fetch: each gets its own generation over the same D0.
	rowsA, genA, err := bridge.FetchPageBlocks(context.Background(), notebook, section, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks (A): %v", err)
	}
	rowsB, genB, err := bridge.FetchPageBlocks(context.Background(), notebook, section, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks (B): %v", err)
	}
	if genA == 0 || genB == 0 || genA == genB {
		t.Fatalf("each fetch must mint a fresh monotonic generation, got A=%d B=%d", genA, genB)
	}

	// A commits W — the write records the newest issued generation as the
	// invalidation boundary.
	if err := bridge.UpdateBlocks(context.Background(), notebook, section, page,
		editBlocks(rowsA, blockID, "client a edit"), genA); err != nil {
		t.Fatalf("client A update must pass, got: %v", err)
	}

	// A's own re-read (the normal RMW loop) re-arms the shared slot on W's
	// bytes — the invalidation marker is gone from this point on.
	if _, _, err := bridge.FetchPageBlocks(context.Background(), notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks (A re-read): %v", err)
	}

	// B's stale update — built from the pre-commit fetch — must refuse on
	// the echoed generation alone and must not mint a stash.
	requireConflict(t, bridge.UpdateBlocks(context.Background(), notebook, section, page,
		editBlocks(rowsB, blockID, "client b edit"), genB))
	requireNoStash(t, app)
	got, _ := os.ReadFile(filePath)
	if !strings.Contains(string(got), "client a edit") {
		t.Fatalf("stale update must not revert the committed write after an interleaving fetch, got %q", string(got))
	}

	// Recovery unchanged: B re-reads (fetch generation newer than the
	// invalidation) and its edit lands.
	rowsB2, genB2, err := bridge.FetchPageBlocks(context.Background(), notebook, section, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks (B re-read): %v", err)
	}
	if err := bridge.UpdateBlocks(context.Background(), notebook, section, page,
		editBlocks(rowsB2, blockID, "client b edit"), genB2); err != nil {
		t.Fatalf("update after re-read must pass, got: %v", err)
	}
	got, _ = os.ReadFile(filePath)
	if !strings.Contains(string(got), "client b edit") {
		t.Fatalf("re-read update must land, got %q", string(got))
	}
}

// TestMCPReadBasis_BlocksFetchAnchorsOnRowsDuringDebounce pins the
// rows-anchored blocks-fetch basis (#1005): during the watcher's re-index
// debounce the DB rows are still pre-external while disk already holds the
// edit. The fetch serves those rows, so the client's basis must anchor on
// THEM — an update built from the served rows refuses instead of silently
// rendering them over the external edit. A byte-anchored basis (the reviewed
// head) snapshotted the post-external disk bytes, so the gate passed and the
// stale rows reverted the external managed edit — headless MCP use has no
// other gate that catches this window.
func TestMCPReadBasis_BlocksFetchAnchorsOnRowsDuringDebounce(t *testing.T) {
	app := newTestApp(t)
	bridge := mcpBridge{app: app, vaultPath: app.vaultPath}
	notebook, section, page := "McpDebounce", "", "McpDebounce"
	blockID := "dddddd8d-dddd-4ddd-8ddd-dddddddddddd"
	seedConflictPage(t, app, notebook, section, page, blockID, "stable body")
	filePath := conflictPagePath(app, notebook, section, page)

	// External edit lands; the watcher has not re-indexed (rows stay
	// pre-external, exactly like the debounce window).
	external := "---\nnotebook: \"McpDebounce\"\npage: \"McpDebounce\"\ntags: []\n---\nexternal body\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	// The client fetches blocks and is served the stale rows; its update
	// built from them must refuse and leave the external bytes untouched.
	rows, gen, err := bridge.FetchPageBlocks(context.Background(), notebook, section, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	requireConflict(t, bridge.UpdateBlocks(context.Background(), notebook, section, page,
		editBlocks(rows, blockID, "mcp edit"), gen))
	got, _ := os.ReadFile(filePath)
	if string(got) != external {
		t.Fatalf("update built from stale rows must leave the external bytes untouched:\n got: %q\nwant: %q", string(got), external)
	}

	// After the watcher re-indexes, the documented re-read → retry recovery
	// passes: the re-fetch now serves post-external rows and the basis
	// re-anchors on the settled bytes.
	reindexConflictPage(t, app, notebook, section, page)
	rows2, gen2, err := bridge.FetchPageBlocks(context.Background(), notebook, section, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks (post-reindex): %v", err)
	}
	if err := bridge.UpdateBlocks(context.Background(), notebook, section, page,
		editBlocks(rows2, blockID, "mcp edit"), gen2); err != nil {
		t.Fatalf("update after reindex+refetch must pass, got: %v", err)
	}
	got, _ = os.ReadFile(filePath)
	if !strings.Contains(string(got), "mcp edit") {
		t.Fatalf("retry write must land, got %q", string(got))
	}
}

// TestMCPReadBasis_MissingFileArmsAbsentBasis pins the fail-closed shape of
// the blocks-fetch capture when the file is gone but rows are still served
// (#1005): without an armed basis the absent entry reads as the legacy
// no-guard contract, and an update built from the stale rows would write over
// whatever externally recreates the file inside this window.
func TestMCPReadBasis_MissingFileArmsAbsentBasis(t *testing.T) {
	app := newTestApp(t)
	bridge := mcpBridge{app: app, vaultPath: app.vaultPath}
	notebook, section, page := "McpAbsent", "", "McpAbsent"
	blockID := "dddddd7d-dddd-4ddd-8ddd-dddddddddddd"
	seedConflictPage(t, app, notebook, section, page, blockID, "stable body")
	filePath := conflictPagePath(app, notebook, section, page)
	key := core.NormalizeFileLockPath(filePath)

	// The file disappears externally (rows remain in the DB, debounce
	// pending), then the client fetches blocks.
	if err := os.Remove(filePath); err != nil {
		t.Fatalf("external remove: %v", err)
	}
	rows, gen, err := bridge.FetchPageBlocks(context.Background(), notebook, section, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("expected stale rows to be served")
	}
	if gen == 0 {
		t.Fatal("the missing-file capture must still return a read generation")
	}
	app.mcpReadBaselinesMu.RLock()
	size := app.mcpReadBaselines[key].Size
	app.mcpReadBaselinesMu.RUnlock()
	if size != absentFileSize {
		t.Fatalf("expected an absent-file basis armed on the missing-file fetch, got size %d", size)
	}

	// The file is externally recreated with new content; an update built
	// from the stale rows must refuse instead of writing over it.
	recreated := "---\nnotebook: \"McpAbsent\"\npage: \"McpAbsent\"\ntags: []\n---\nrecreated body\n"
	if err := os.WriteFile(filePath, []byte(recreated), 0o644); err != nil {
		t.Fatalf("recreate: %v", err)
	}
	requireConflict(t, bridge.UpdateBlocks(context.Background(), notebook, section, page,
		editBlocks(rows, blockID, "mcp edit"), gen))
	requireNoStash(t, app)
	got, _ := os.ReadFile(filePath)
	if string(got) != recreated {
		t.Fatalf("stale update must leave the recreated bytes untouched:\n got: %q\nwant: %q", string(got), recreated)
	}
}

// TestMCPReadBasis_FollowsLifecycle: the read-basis must die with the page
// (delete) and the vault (teardown) like the stash and editor baselines — a
// basis keyed into a closed vault would phantom-refuse the next session's
// client.
func TestMCPReadBasis_FollowsLifecycle(t *testing.T) {
	app := newTestApp(t)
	bridge := mcpBridge{app: app, vaultPath: app.vaultPath}
	notebook, section, page := "McpRMW2", "", "McpRMW2"
	blockID := "eeeeee7e-eeee-4eee-8eee-eeeeeeeeeeee"
	seedConflictPage(t, app, notebook, section, page, blockID, "stable body")

	if _, _, err := bridge.FetchPageMarkdown(context.Background(), notebook, section, page); err != nil {
		t.Fatalf("FetchPageMarkdown: %v", err)
	}
	if len(app.mcpReadBaselines) != 1 {
		t.Fatalf("markdown fetch must capture a read-basis, got %v", app.mcpReadBaselines)
	}

	if err := app.DeletePage(notebook, section, page); err != nil {
		t.Fatalf("DeletePage: %v", err)
	}
	if len(app.mcpReadBaselines) != 0 {
		t.Fatalf("delete must drop the page's read-basis, got %v", app.mcpReadBaselines)
	}

	// Teardown clears whatever remains (recreated page, fresh basis).
	if _, err := app.CreatePage(notebook, section, page, "2026-01-01"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if _, _, err := bridge.FetchPageMarkdown(context.Background(), notebook, section, page); err != nil {
		t.Fatalf("FetchPageMarkdown (recreated): %v", err)
	}
	app.teardownVaultServices()
	if app.mcpReadBaselines != nil {
		t.Fatalf("teardown must clear the MCP read-basis map, got %v", app.mcpReadBaselines)
	}
}
