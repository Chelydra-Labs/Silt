package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTool_ListPageVersions_EmptyAndRows(t *testing.T) {
	bridge := &fakeBridge{
		path: t.TempDir(),
		versions: map[string][]PageVersionInfo{
			historyKey("Work", "Journal", "Daily"): {
				{ID: "v-new", Timestamp: "2026-08-16T18:00:00Z", Source: "editor", Bytes: 120},
				{ID: "v-old", Timestamp: "2026-08-15T09:30:00Z", Source: "mcp", Bytes: 96},
			},
		},
	}
	cs, _ := connectTools(t, bridge, Config{Enabled: true})
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "list_page_versions",
		Arguments: map[string]any{"notebook": "Work", "section": "Journal", "page": "Daily"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("list: %s", toolText(t, res))
	}
	var payload struct {
		Versions []PageVersionInfo `json:"versions"`
	}
	if err := json.Unmarshal([]byte(toolText(t, res)), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Versions) != 2 || payload.Versions[0].ID != "v-new" {
		t.Fatalf("versions = %+v", payload.Versions)
	}

	res, err = cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "list_page_versions",
		Arguments: map[string]any{"notebook": "Work", "section": "", "page": "Missing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("empty list should not error: %s", toolText(t, res))
	}
	if err := json.Unmarshal([]byte(toolText(t, res)), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Versions == nil || len(payload.Versions) != 0 {
		t.Fatalf("want empty versions, got %+v", payload.Versions)
	}
}

func TestTool_GetPageVersion_PreviewDoesNotRestore(t *testing.T) {
	bridge := &fakeBridge{
		path: t.TempDir(),
		versionBodies: map[string]string{
			historyKey("Work", "Journal", "Daily") + "\x00v-old": "# older body",
		},
	}
	cs, _ := connectTools(t, bridge, Config{Enabled: true, WriteEnabled: true})
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "get_page_version",
		Arguments: map[string]any{
			"notebook":   "Work",
			"section":    "Journal",
			"page":       "Daily",
			"version_id": "v-old",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("get: %s", toolText(t, res))
	}
	if !strings.Contains(toolText(t, res), "# older body") {
		t.Fatalf("body: %s", toolText(t, res))
	}
	if bridge.restoreN != 0 {
		t.Fatalf("preview must not restore, restoreN=%d", bridge.restoreN)
	}
}

func TestTool_GetPageVersion_TruncatesHugeBody(t *testing.T) {
	huge := strings.Repeat("x", MaxBlockTextRunes*4+50)
	bridge := &fakeBridge{
		path: t.TempDir(),
		versionBodies: map[string]string{
			historyKey("Work", "", "Big") + "\x00v1": huge,
		},
	}
	cs, _ := connectTools(t, bridge, Config{Enabled: true})
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "get_page_version",
		Arguments: map[string]any{
			"notebook":   "Work",
			"section":    "",
			"page":       "Big",
			"version_id": "v1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("get: %s", toolText(t, res))
	}
	text := toolText(t, res)
	if !strings.Contains(text, "…[truncated]") {
		t.Fatalf("expected truncation marker: %s", text[:min(200, len(text))])
	}
}

func TestTool_RestorePageVersion_RequiresWriteGrant(t *testing.T) {
	bridge := &fakeBridge{path: t.TempDir()}
	cs, aud := connectTools(t, bridge, Config{Enabled: true, WriteEnabled: false})
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "restore_page_version",
		Arguments: map[string]any{
			"notebook":   "Work",
			"section":    "Journal",
			"page":       "Daily",
			"version_id": "v-old",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected denied restore without write grant")
	}
	if bridge.restoreN != 0 {
		t.Fatalf("restore ran without grant: %d", bridge.restoreN)
	}
	found := false
	for _, e := range aud.Entries {
		if e.Tool == "restore_page_version" && e.Outcome == "denied" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected denied audit: %+v", aud.Entries)
	}

	cs2, _ := connectTools(t, bridge, Config{Enabled: true, WriteEnabled: true})
	res, err = cs2.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "restore_page_version",
		Arguments: map[string]any{
			"notebook":   "Work",
			"section":    "Journal",
			"page":       "Daily",
			"version_id": "v-old",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("restore with grant: %s", toolText(t, res))
	}
	if bridge.restoreN != 1 || bridge.lastRestore.VersionID != "v-old" {
		t.Fatalf("restore = %+v n=%d", bridge.lastRestore, bridge.restoreN)
	}
}

func TestTool_History_NoVaultAndMissingVersion(t *testing.T) {
	cs, _ := connectTools(t, nil, Config{Enabled: true, WriteEnabled: true})
	ctx := context.Background()
	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "list_page_versions",
		Arguments: map[string]any{"notebook": "Work", "section": "", "page": "X"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(toolText(t, res), "no vault") {
		t.Fatalf("list no-vault: %s", toolText(t, res))
	}
	for _, name := range []string{"get_page_version", "restore_page_version"} {
		res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
			Name: name,
			Arguments: map[string]any{
				"notebook":   "Work",
				"section":    "",
				"page":       "X",
				"version_id": "v1",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError || !strings.Contains(toolText(t, res), "no vault") {
			t.Fatalf("%s no-vault: %s", name, toolText(t, res))
		}
	}

	bridge := &fakeBridge{
		path:          t.TempDir(),
		getVersionErr: errors.New("page version not found"),
		restoreErr:    errors.New("page version not found"),
	}
	cs2, _ := connectTools(t, bridge, Config{Enabled: true, WriteEnabled: true})
	res, err = cs2.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "get_page_version",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "X", "version_id": "missing",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(toolText(t, res), "not found") {
		t.Fatalf("missing get: %s", toolText(t, res))
	}
	res, err = cs2.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "restore_page_version",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "X", "version_id": "missing",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("missing restore should error")
	}
	if bridge.restoreN != 1 {
		t.Fatalf("restore should be attempted then fail, n=%d", bridge.restoreN)
	}
}
