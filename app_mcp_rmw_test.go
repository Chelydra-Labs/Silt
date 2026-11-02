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
)

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
	got, _ := os.ReadFile(filePath)
	if string(got) != external {
		t.Fatalf("refused MCP write must leave the external bytes untouched:\n got: %q\nwant: %q", string(got), external)
	}

	// The refusal stashed under the programmatic funnel: settling it
	// (keep_theirs) unblocks the page, and a fresh read → write cycle passes
	// because the basis re-anchored on the settled bytes.
	if _, rerr := app.ResolveExternalConflict(notebook, section, page, "keep_theirs"); rerr != nil {
		t.Fatalf("keep_theirs: %v", rerr)
	}
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
