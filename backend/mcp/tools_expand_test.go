package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"silt/backend/db"
	"silt/backend/parser"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMatchHeadings(t *testing.T) {
	blocks := []parser.ParsedBlock{
		{ID: "h1", Type: parser.BlockHeader, Depth: 1, CleanText: "Meeting"},
		{ID: "h2", Type: parser.BlockHeader, Depth: 2, CleanText: "Notes"},
		{ID: "n1", Type: parser.BlockNote, CleanText: "# fake"},
		{ID: "h3", Type: parser.BlockHeader, Depth: 1, CleanText: "Other"},
		{ID: "h4", Type: parser.BlockHeader, Depth: 2, CleanText: "Notes"},
	}
	if got := matchHeadings(blocks, "Notes"); len(got) != 2 {
		t.Fatalf("bare Notes: got %d want 2", len(got))
	}
	uniq := matchHeadings(blocks, "Meeting::Notes")
	if len(uniq) != 1 || uniq[0].ID != "h2" {
		t.Fatalf("path match: %+v", uniq)
	}
	if got := matchHeadings(blocks, "missing"); len(got) != 0 {
		t.Fatalf("missing: %+v", got)
	}
	// Fenced-looking NOTE is not HEADER.
	if got := matchHeadings(blocks, "# fake"); len(got) != 0 {
		t.Fatalf("note leaf should not match: %+v", got)
	}
}

func TestTools_AppendToPage(t *testing.T) {
	key := "Work\x00\x00Home"
	bridge := &fakeBridge{
		path: t.TempDir(),
		pages: map[string][]parser.ParsedBlock{
			key: {{ID: "b1", Type: parser.BlockNote, CleanText: "old", RawText: "old"}},
		},
	}
	cs, aud := connectTools(t, bridge, Config{Enabled: true, WriteEnabled: true})
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "append_to_page",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "Home", "text": "appended",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("append: %s", toolText(t, res))
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(toolText(t, res)), &payload); err != nil {
		t.Fatal(err)
	}
	id, _ := payload["id"].(string)
	if id == "" {
		t.Fatalf("missing id: %v", payload)
	}
	blocks := bridge.pages[key]
	if len(blocks) != 2 || blocks[len(blocks)-1].ID != id {
		t.Fatalf("block not at end: %+v", blocks)
	}
	if !auditHas(aud, "append_to_page", "ok") {
		t.Fatal("expected ok audit")
	}

	// TASK append
	res, err = cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "append_to_page",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "Home",
			"type": "TASK", "text": "todo item",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("TASK append: %s", toolText(t, res))
	}
	blocks = bridge.pages[key]
	if len(blocks) < 1 || blocks[len(blocks)-1].Type != parser.BlockTask {
		t.Fatalf("expected last block TASK: %+v", blocks)
	}

	// write denied
	cs2, aud2 := connectTools(t, bridge, Config{Enabled: true, WriteEnabled: false})
	res, err = cs2.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "append_to_page",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "Home", "text": "x",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected denied")
	}
	if !auditHas(aud2, "append_to_page", "denied") {
		t.Fatal("expected denied audit")
	}

	// missing page
	res, err = cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "append_to_page",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "Nope", "text": "x",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(toolText(t, res), "does not exist") {
		t.Fatalf("missing page: %s", toolText(t, res))
	}
}

func TestTools_InsertUnderHeading(t *testing.T) {
	key := "Work\x00\x00Doc"
	bridge := &fakeBridge{
		path: t.TempDir(),
		pages: map[string][]parser.ParsedBlock{
			key: {
				{ID: "h1", Type: parser.BlockHeader, Depth: 1, CleanText: "Meeting", RawText: "# Meeting"},
				{ID: "h2", Type: parser.BlockHeader, Depth: 2, CleanText: "Notes", RawText: "## Notes"},
				{ID: "n1", Type: parser.BlockNote, CleanText: "# fake", RawText: "# fake"},
				{ID: "h3", Type: parser.BlockHeader, Depth: 1, CleanText: "Other", RawText: "# Other"},
				{ID: "h4", Type: parser.BlockHeader, Depth: 2, CleanText: "Notes", RawText: "## Notes"},
			},
		},
	}
	cs, _ := connectTools(t, bridge, Config{Enabled: true, WriteEnabled: true})
	ctx := context.Background()

	// unique path
	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "insert_under_heading",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "Doc",
			"heading": "Meeting::Notes", "text": "under notes",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("insert: %s", toolText(t, res))
	}
	blocks := bridge.pages[key]
	// h2 then new block
	found := false
	for i, b := range blocks {
		if b.ID == "h2" && i+1 < len(blocks) && blocks[i+1].CleanText == "under notes" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("insert not after h2: %+v", blocks)
	}

	// ambiguous bare leaf
	res, err = cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "insert_under_heading",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "Doc",
			"heading": "Notes", "text": "x",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected ambiguous")
	}
	txt := toolText(t, res)
	if !strings.Contains(txt, "candidates") || !strings.Contains(txt, "Meeting::Notes") {
		t.Fatalf("candidates: %s", txt)
	}

	// not found — structured candidates teach the model available paths
	res, err = cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "insert_under_heading",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "Doc",
			"heading": "Absent", "text": "x",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected not found")
	}
	nf := toolText(t, res)
	if !strings.Contains(nf, "not found") || !strings.Contains(nf, "candidates") || !strings.Contains(nf, "Meeting") {
		t.Fatalf("not found candidates: %s", nf)
	}

	// write denied
	cs2, aud2 := connectTools(t, bridge, Config{Enabled: true, WriteEnabled: false})
	res, err = cs2.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "insert_under_heading",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "Doc",
			"heading": "Meeting", "text": "x",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected denied")
	}
	if !auditHas(aud2, "insert_under_heading", "denied") {
		t.Fatal("expected denied audit")
	}
}

func TestTools_CreateTask(t *testing.T) {
	bridge := &fakeBridge{path: t.TempDir()}
	cs, aud := connectTools(t, bridge, Config{Enabled: true, WriteEnabled: true})
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "create_task",
		Arguments: map[string]any{"text": "buy milk"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("create: %s", toolText(t, res))
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(toolText(t, res)), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["id"] == nil || payload["id"] == "" {
		t.Fatalf("missing id: %v", payload)
	}
	if bridge.standaloneN != 1 {
		t.Fatalf("standaloneN=%d", bridge.standaloneN)
	}
	if !auditHas(aud, "create_task", "ok") {
		t.Fatal("expected ok")
	}

	// owner + tags
	res, err = cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "create_task",
		Arguments: map[string]any{
			"text": "owned", "owner": "Alice", "tags": []any{"a", "b"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("meta: %s", toolText(t, res))
	}
	if len(bridge.setOwnerCalls) != 1 || bridge.setOwnerCalls[0].Owner != "Alice" {
		t.Fatalf("owner calls: %+v", bridge.setOwnerCalls)
	}
	if len(bridge.setTagsCalls) != 1 || len(bridge.setTagsCalls[0].Tags) != 2 {
		t.Fatalf("tags calls: %+v", bridge.setTagsCalls)
	}

	// write denied
	cs2, aud2 := connectTools(t, bridge, Config{Enabled: true, WriteEnabled: false})
	res, err = cs2.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "create_task",
		Arguments: map[string]any{"text": "nope"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected denied")
	}
	if !auditHas(aud2, "create_task", "denied") {
		t.Fatal("expected denied audit")
	}

	// page override + due + non-default status
	key := "Work\x00\x00Inbox"
	bridge.pages = map[string][]parser.ParsedBlock{key: {}}
	before := bridge.createBlockN
	dueBefore := len(bridge.setDueCalls)
	stateBefore := len(bridge.setStateCalls)
	res, err = cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "create_task",
		Arguments: map[string]any{
			"text": "on page", "notebook": "Work", "section": "", "page": "Inbox",
			"due": "2026-08-01", "status": "DOING",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("page task: %s", toolText(t, res))
	}
	if bridge.createBlockN != before+1 {
		t.Fatalf("expected CreateBlock, createBlockN=%d", bridge.createBlockN)
	}
	if len(bridge.pages[key]) != 1 || bridge.pages[key][0].Type != parser.BlockTask {
		t.Fatalf("page blocks: %+v", bridge.pages[key])
	}
	if len(bridge.setDueCalls) != dueBefore+1 || bridge.setDueCalls[dueBefore].Due != "2026-08-01" {
		t.Fatalf("expected page-path SetTaskDueDate: %+v", bridge.setDueCalls)
	}
	if len(bridge.setStateCalls) != stateBefore+1 || bridge.setStateCalls[stateBefore].Status != "DOING" {
		t.Fatalf("expected page-path UpdateBlockState DOING: %+v", bridge.setStateCalls)
	}

	// standalone due + status forwarded to CreateStandaloneTask
	res, err = cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "create_task",
		Arguments: map[string]any{
			"text": "standalone meta", "due": "2026-09-01", "status": "DONE",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("standalone meta: %s", toolText(t, res))
	}
	if bridge.lastStandalone.Due != "2026-09-01" || bridge.lastStandalone.Status != "DONE" {
		t.Fatalf("standalone args: %+v", bridge.lastStandalone)
	}

	// invalid due / status rejected before bridge write
	res, err = cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "create_task",
		Arguments: map[string]any{"text": "bad", "due": "not-a-date"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(toolText(t, res), "YYYY-MM-DD") {
		t.Fatalf("invalid due: %s", toolText(t, res))
	}
	res, err = cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "create_task",
		Arguments: map[string]any{"text": "bad", "status": "BOGUS"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(toolText(t, res), "TODO") {
		t.Fatalf("invalid status: %s", toolText(t, res))
	}
}

func TestTools_GetBacklinks(t *testing.T) {
	bridge := &fakeBridge{
		path: t.TempDir(),
		backlinks: db.BacklinksResult{
			Results: []db.Backlink{
				{Kind: db.BacklinkPageLink, SourceNotebook: "A", SourcePage: "P", Snippet: "see [[T]]"},
				{Kind: db.BacklinkBlockRef, SourceNotebook: "B", SourcePage: "Q", Snippet: "((uuid))"},
				{Kind: db.BacklinkEmbed, SourceNotebook: "C", SourcePage: "R", Snippet: "{{embed:x}}"},
			},
			Cursor:  "next-cursor",
			HasMore: true,
		},
	}
	cs, _ := connectTools(t, bridge, Config{Enabled: true})
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "get_backlinks",
		Arguments: map[string]any{
			"notebook": "T", "section": "", "page": "Target", "limit": 99,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("backlinks: %s", toolText(t, res))
	}
	txt := toolText(t, res)
	if !strings.Contains(txt, "see [[T]]") {
		t.Fatalf("payload: %s", txt)
	}
	// kinds distinguished in JSON (linkKind field)
	if !strings.Contains(txt, "page") || !strings.Contains(txt, "block-ref") || !strings.Contains(txt, "embed") {
		t.Fatalf("expected all kinds: %s", txt)
	}
	if !strings.Contains(txt, "next-cursor") || !strings.Contains(txt, "has_more") {
		t.Fatalf("expected pagination fields: %s", txt)
	}

	// empty ok
	bridge.backlinks = db.BacklinksResult{Results: nil}
	res, err = cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "get_backlinks",
		Arguments: map[string]any{
			"notebook": "T", "section": "", "page": "Empty",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("empty: %s", toolText(t, res))
	}
}

func TestTools_GetBlock(t *testing.T) {
	bridge := &fakeBridge{
		path: t.TempDir(),
		blocksByID: map[string]BlockRefResult{
			"emb": {
				ID: "emb", Exists: true, CleanText: "hi", Type: "NOTE",
				Notebook: "W", Page: "P", Embedded: true, LineNumber: 3,
			},
			"plain": {
				ID: "plain", Exists: true, CleanText: "yo", Type: "NOTE",
				Notebook: "W", Page: "P", Embedded: false,
			},
		},
	}
	cs, _ := connectTools(t, bridge, Config{Enabled: true})
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "get_block",
		Arguments: map[string]any{"id": "emb"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("get emb: %s", toolText(t, res))
	}
	var emb BlockRefResult
	if err := json.Unmarshal([]byte(toolText(t, res)), &emb); err != nil {
		t.Fatal(err)
	}
	if !emb.Exists || !emb.Embedded {
		t.Fatalf("emb: %+v", emb)
	}

	res, err = cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "get_block",
		Arguments: map[string]any{"id": "plain"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var plain BlockRefResult
	_ = json.Unmarshal([]byte(toolText(t, res)), &plain)
	if !plain.Exists || plain.Embedded {
		t.Fatalf("plain: %+v", plain)
	}

	res, err = cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "get_block",
		Arguments: map[string]any{"id": "missing-uuid"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("missing should not IsError: %s", toolText(t, res))
	}
	var miss BlockRefResult
	if err := json.Unmarshal([]byte(toolText(t, res)), &miss); err != nil {
		t.Fatal(err)
	}
	if miss.Exists {
		t.Fatalf("expected exists=false: %+v", miss)
	}
}
