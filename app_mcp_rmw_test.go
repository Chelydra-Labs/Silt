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
	rows, err := bridge.FetchPageBlocks(context.Background(), notebook, section, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
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
		editBlocks(rows, blockID, "mcp edited body"))
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
	rows2, err := bridge.FetchPageBlocks(context.Background(), notebook, section, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks (retry): %v", err)
	}
	if err := bridge.UpdateBlocks(context.Background(), notebook, section, page,
		editBlocks(rows2, blockID, "mcp edited body")); err != nil {
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
	rows, err := bridge.FetchPageBlocks(context.Background(), notebook, section, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}

	// Client A commits.
	if err := bridge.UpdateBlocks(context.Background(), notebook, section, page,
		editBlocks(rows, blockID, "client a edit")); err != nil {
		t.Fatalf("client A update must pass, got: %v", err)
	}

	// Client B's update, built from the same pre-write fetch, must refuse —
	// and must not mint a stash (headless recovery is re-read + retry).
	requireConflict(t, bridge.UpdateBlocks(context.Background(), notebook, section, page,
		editBlocks(rows, blockID, "client b edit")))
	requireNoStash(t, app)
	got, _ := os.ReadFile(filePath)
	if !strings.Contains(string(got), "client a edit") {
		t.Fatalf("stale update must not revert the committed write, got %q", string(got))
	}

	// The documented recovery: re-read (fresh basis on the settled rows)
	// then write — B's edit now lands.
	rows2, err := bridge.FetchPageBlocks(context.Background(), notebook, section, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks (re-read): %v", err)
	}
	if err := bridge.UpdateBlocks(context.Background(), notebook, section, page,
		editBlocks(rows2, blockID, "client b edit")); err != nil {
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
	rows, err := bridge.FetchPageBlocks(context.Background(), notebook, section, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	requireConflict(t, bridge.UpdateBlocks(context.Background(), notebook, section, page,
		editBlocks(rows, blockID, "mcp edit")))
	got, _ := os.ReadFile(filePath)
	if string(got) != external {
		t.Fatalf("update built from stale rows must leave the external bytes untouched:\n got: %q\nwant: %q", string(got), external)
	}

	// After the watcher re-indexes, the documented re-read → retry recovery
	// passes: the re-fetch now serves post-external rows and the basis
	// re-anchors on the settled bytes.
	reindexConflictPage(t, app, notebook, section, page)
	rows2, err := bridge.FetchPageBlocks(context.Background(), notebook, section, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks (post-reindex): %v", err)
	}
	if err := bridge.UpdateBlocks(context.Background(), notebook, section, page,
		editBlocks(rows2, blockID, "mcp edit")); err != nil {
		t.Fatalf("update after reindex+refetch must pass, got: %v", err)
	}
	got, _ = os.ReadFile(filePath)
	if !strings.Contains(string(got), "mcp edit") {
		t.Fatalf("retry write must land, got %q", string(got))
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

	if _, err := bridge.FetchPageMarkdown(context.Background(), notebook, section, page); err != nil {
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
	if _, err := bridge.FetchPageMarkdown(context.Background(), notebook, section, page); err != nil {
		t.Fatalf("FetchPageMarkdown (recreated): %v", err)
	}
	app.teardownVaultServices()
	if app.mcpReadBaselines != nil {
		t.Fatalf("teardown must clear the MCP read-basis map, got %v", app.mcpReadBaselines)
	}
}
