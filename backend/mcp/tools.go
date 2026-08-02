package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"silt/backend/db"
	"silt/backend/parser"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolEnv is closed over by tool handlers.
type toolEnv struct {
	bridge Bridge
	cfg    func() Config
	audit  Auditor
	client func(ctx context.Context) string
}

func (e *toolEnv) writeOK() bool {
	return e.cfg().WriteEnabled
}

func (e *toolEnv) record(tool, outcome, errMsg string, args map[string]any) {
	if e.audit == nil {
		return
	}
	vp := ""
	if e.bridge != nil {
		vp = e.bridge.VaultPath()
	}
	client := ""
	if e.client != nil {
		client = e.client(context.Background())
	}
	e.audit.Record(AuditEntry{
		Client:   client,
		Tool:     tool,
		Vault:    VaultPathHash(vp),
		Outcome:  outcome,
		Error:    errMsg,
		ArgsMeta: RedactArgs(args),
	})
}

func toolErr(msg string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}, nil, nil
}

func toolJSON(v any) (*mcp.CallToolResult, any, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return toolErr(fmt.Sprintf("marshal result: %v", err))
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}, v, nil
}

// registerTools attaches the initial tool surface to s.
func registerTools(s *mcp.Server, env *toolEnv) {
	type searchIn struct {
		Query    string `json:"query" jsonschema:"FTS search query"`
		Offset   int    `json:"offset,omitempty" jsonschema:"pagination offset"`
		Limit    int    `json:"limit,omitempty" jsonschema:"page size (max 50)"`
		Notebook string `json:"notebook,omitempty" jsonschema:"optional notebook filter"`
		Section  string `json:"section,omitempty" jsonschema:"optional section filter"`
		Tag      string `json:"tag,omitempty" jsonschema:"optional tag filter"`
		Type     string `json:"type,omitempty" jsonschema:"optional block type TASK|NOTE|HEADER"`
	}
	searchHandler := func(ctx context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, any, error) {
		args := map[string]any{"query": in.Query, "offset": in.Offset, "limit": in.Limit, "notebook": in.Notebook}
		if env.bridge == nil {
			env.record("search_blocks", "error", "no vault", args)
			return toolErr("no vault open — open a vault in Silt first")
		}
		q := strings.TrimSpace(in.Query)
		if q == "" {
			env.record("search_blocks", "error", "empty query", args)
			return toolErr("query is required")
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 20
		}
		if limit > MaxSearchLimit {
			limit = MaxSearchLimit
		}
		offset := in.Offset
		if offset < 0 {
			offset = 0
		}
		filters := db.SearchFilters{
			Notebook: in.Notebook,
			Section:  in.Section,
			Tag:      in.Tag,
			Type:     in.Type,
		}
		res, err := env.bridge.SearchBlocksPaged(ctx, q, offset, limit, filters)
		if err != nil {
			env.record("search_blocks", "error", err.Error(), args)
			return toolErr(err.Error())
		}
		env.record("search_blocks", "ok", "", args)
		return toolJSON(res)
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_blocks",
		Description: "Full-text search vault blocks (FTS). Prefer this before reading whole pages. Bounded results with offset/limit.",
	}, searchHandler)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_notes",
		Description: "Alias of search_blocks — full-text search across notes and tasks.",
	}, searchHandler)

	type readPageIn struct {
		Notebook string `json:"notebook" jsonschema:"notebook name"`
		Section  string `json:"section" jsonschema:"section path (may be empty for root pages)"`
		Page     string `json:"page" jsonschema:"page name without .md"`
		Markdown bool   `json:"markdown,omitempty" jsonschema:"if true return raw markdown instead of blocks"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "read_page",
		Description: "Read a page as structured blocks (default) or raw markdown. Cite notebook/section/page when answering.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in readPageIn) (*mcp.CallToolResult, any, error) {
		args := map[string]any{"notebook": in.Notebook, "section": in.Section, "page": in.Page}
		if env.bridge == nil {
			env.record("read_page", "error", "no vault", args)
			return toolErr("no vault open")
		}
		if strings.TrimSpace(in.Notebook) == "" || strings.TrimSpace(in.Page) == "" {
			env.record("read_page", "error", "missing path", args)
			return toolErr("notebook and page are required")
		}
		if in.Markdown {
			md, err := env.bridge.FetchPageMarkdown(ctx, in.Notebook, in.Section, in.Page)
			if err != nil {
				env.record("read_page", "error", err.Error(), args)
				return toolErr(err.Error())
			}
			// Bound markdown size in the tool result.
			runes := []rune(md)
			if len(runes) > MaxBlockTextRunes*4 {
				md = string(runes[:MaxBlockTextRunes*4]) + "\n…[truncated]"
			}
			env.record("read_page", "ok", "", args)
			return toolJSON(map[string]any{"notebook": in.Notebook, "section": in.Section, "page": in.Page, "markdown": md})
		}
		blocks, err := env.bridge.FetchPageBlocks(ctx, in.Notebook, in.Section, in.Page)
		if err != nil {
			env.record("read_page", "error", err.Error(), args)
			return toolErr(err.Error())
		}
		if len(blocks) > MaxBlocksRead {
			blocks = blocks[:MaxBlocksRead]
		}
		env.record("read_page", "ok", "", args)
		return toolJSON(map[string]any{
			"notebook": in.Notebook,
			"section":  in.Section,
			"page":     in.Page,
			"blocks":   blocks,
			"count":    len(blocks),
		})
	})

	type readBlocksIn struct {
		Notebook string `json:"notebook" jsonschema:"notebook name"`
		Section  string `json:"section" jsonschema:"section path"`
		Page     string `json:"page" jsonschema:"page name"`
		Offset   int    `json:"offset,omitempty"`
		Limit    int    `json:"limit,omitempty" jsonschema:"max blocks (default 50, max 200)"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "read_blocks",
		Description: "Read a slice of blocks from a page (paginated).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in readBlocksIn) (*mcp.CallToolResult, any, error) {
		args := map[string]any{"notebook": in.Notebook, "section": in.Section, "page": in.Page}
		if env.bridge == nil {
			env.record("read_blocks", "error", "no vault", args)
			return toolErr("no vault open")
		}
		blocks, err := env.bridge.FetchPageBlocks(ctx, in.Notebook, in.Section, in.Page)
		if err != nil {
			env.record("read_blocks", "error", err.Error(), args)
			return toolErr(err.Error())
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 50
		}
		if limit > MaxBlocksRead {
			limit = MaxBlocksRead
		}
		off := in.Offset
		if off < 0 {
			off = 0
		}
		total := len(blocks)
		if off > total {
			off = total
		}
		end := off + limit
		if end > total {
			end = total
		}
		slice := blocks[off:end]
		env.record("read_blocks", "ok", "", args)
		return toolJSON(map[string]any{
			"blocks": slice, "total": total, "offset": off, "limit": limit, "has_more": end < total,
		})
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_notebooks",
		Description: "List vault navigation structure (notebooks → sections → pages).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		if env.bridge == nil {
			env.record("list_notebooks", "error", "no vault", nil)
			return toolErr("no vault open")
		}
		tree, err := env.bridge.ListNavigation(ctx)
		if err != nil {
			env.record("list_notebooks", "error", err.Error(), nil)
			return toolErr(err.Error())
		}
		env.record("list_notebooks", "ok", "", nil)
		return toolJSON(tree)
	})

	type createPageIn struct {
		Notebook string `json:"notebook" jsonschema:"notebook name"`
		Section  string `json:"section" jsonschema:"section path (empty for root)"`
		Page     string `json:"page" jsonschema:"new page name"`
		Date     string `json:"date,omitempty" jsonschema:"optional YYYY-MM-DD file date"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_page",
		Description: "Create an empty page. Requires write grant. Confirm with the user before calling.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createPageIn) (*mcp.CallToolResult, any, error) {
		args := map[string]any{"notebook": in.Notebook, "section": in.Section, "page": in.Page}
		if !env.writeOK() {
			env.record("create_page", "denied", "write not granted", args)
			return toolErr("write tools are disabled — enable write grant in Silt Settings → AI → Local MCP")
		}
		if env.bridge == nil {
			env.record("create_page", "error", "no vault", args)
			return toolErr("no vault open")
		}
		path, err := env.bridge.CreatePage(ctx, in.Notebook, in.Section, in.Page, in.Date)
		if err != nil {
			env.record("create_page", "error", err.Error(), args)
			return toolErr(err.Error())
		}
		env.record("create_page", "ok", "", args)
		return toolJSON(map[string]any{"path": path, "notebook": in.Notebook, "section": in.Section, "page": in.Page})
	})

	type blockIn struct {
		ID   string `json:"id,omitempty" jsonschema:"existing block UUID (preserve identity)"`
		Type string `json:"type,omitempty" jsonschema:"TASK|NOTE|HEADER"`
		Text string `json:"text" jsonschema:"block body text"`
	}
	type updateBlocksIn struct {
		Notebook string    `json:"notebook"`
		Section  string    `json:"section"`
		Page     string    `json:"page"`
		Blocks   []blockIn `json:"blocks" jsonschema:"full replacement block list for the page"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_blocks",
		Description: "Identity-preserving update of an existing page's blocks. Prefer keeping existing block ids. Fails if the page does not exist — use create_page first. Requires write grant. Confirm before edits. No delete/move/bulk tools exist.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateBlocksIn) (*mcp.CallToolResult, any, error) {
		blockIDs := make([]string, 0, len(in.Blocks))
		for _, b := range in.Blocks {
			if id := strings.TrimSpace(b.ID); id != "" {
				blockIDs = append(blockIDs, id)
			}
		}
		args := map[string]any{
			"notebook":  in.Notebook,
			"section":   in.Section,
			"page":      in.Page,
			"blocks":    make([]any, len(in.Blocks)),
			"block_ids": blockIDs,
		}
		if !env.writeOK() {
			env.record("update_blocks", "denied", "write not granted", args)
			return toolErr("write tools are disabled — enable write grant in Silt Settings → AI → Local MCP")
		}
		if env.bridge == nil {
			env.record("update_blocks", "error", "no vault", args)
			return toolErr("no vault open")
		}
		exists, exErr := env.bridge.PageExists(ctx, in.Notebook, in.Section, in.Page)
		if exErr != nil {
			env.record("update_blocks", "error", exErr.Error(), args)
			return toolErr(exErr.Error())
		}
		if !exists {
			env.record("update_blocks", "error", "page does not exist", args)
			return toolErr("page does not exist; use create_page first")
		}
		if len(in.Blocks) == 0 {
			env.record("update_blocks", "error", "empty blocks", args)
			return toolErr("blocks must be non-empty (refusing empty wipe)")
		}
		if len(in.Blocks) > MaxBlocksRead {
			env.record("update_blocks", "error", "too many blocks", args)
			return toolErr(fmt.Sprintf("at most %d blocks per update", MaxBlocksRead))
		}
		parsed := make([]parser.ParsedBlock, 0, len(in.Blocks))
		typeDefaulted := 0
		for i, b := range in.Blocks {
			if utf8.RuneCountInString(b.Text) > MaxBlockTextRunes {
				env.record("update_blocks", "error", "block too large", args)
				return toolErr(fmt.Sprintf("block %d text exceeds %d runes", i, MaxBlockTextRunes))
			}
			bt := parser.BlockType(strings.ToUpper(strings.TrimSpace(b.Type)))
			if bt == "" {
				// Explicit omit → NOTE; garbage types still hard-error below.
				bt = parser.BlockNote
				typeDefaulted++
			}
			if bt != parser.BlockTask && bt != parser.BlockNote && bt != parser.BlockHeader {
				env.record("update_blocks", "error", "bad type", args)
				return toolErr(fmt.Sprintf("block %d: invalid type %q", i, b.Type))
			}
			pb := parser.ParsedBlock{
				ID:        strings.TrimSpace(b.ID),
				Type:      bt,
				RawText:   b.Text,
				CleanText: b.Text,
			}
			parsed = append(parsed, pb)
		}
		if typeDefaulted > 0 {
			args["type_defaulted_count"] = typeDefaulted
		}
		if err := env.bridge.UpdateBlocks(ctx, in.Notebook, in.Section, in.Page, parsed); err != nil {
			env.record("update_blocks", "error", err.Error(), args)
			return toolErr(err.Error())
		}
		env.record("update_blocks", "ok", "", args)
		return toolJSON(map[string]any{"ok": true, "count": len(parsed)})
	})

	type getPageMetadataIn struct {
		Notebook string `json:"notebook" jsonschema:"notebook name"`
		Section  string `json:"section" jsonschema:"section path (empty for root)"`
		Page     string `json:"page" jsonschema:"page name without .md"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_page_metadata",
		Description: "Get page type, properties (schema-merged), and raw frontmatter. Read-only.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getPageMetadataIn) (*mcp.CallToolResult, any, error) {
		args := map[string]any{"notebook": in.Notebook, "section": in.Section, "page": in.Page}
		if env.bridge == nil {
			env.record("get_page_metadata", "error", "no vault", args)
			return toolErr("no vault open")
		}
		res, err := env.bridge.GetPageMetadata(ctx, in.Notebook, in.Section, in.Page)
		if err != nil {
			env.record("get_page_metadata", "error", err.Error(), args)
			return toolErr(err.Error())
		}
		env.record("get_page_metadata", "ok", "", args)
		return toolJSON(res)
	})

	type setPagePropertyIn struct {
		Notebook string `json:"notebook" jsonschema:"notebook name"`
		Section  string `json:"section" jsonschema:"section path (empty for root)"`
		Page     string `json:"page" jsonschema:"page name without .md"`
		Property string `json:"property" jsonschema:"property name from the type schema"`
		Value    string `json:"value" jsonschema:"property value (validated against the type schema before writing)"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_page_property",
		Description: "Set a single typed property. Schema-validated write — invalid values are rejected before any file I/O. Requires write grant.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in setPagePropertyIn) (*mcp.CallToolResult, any, error) {
		args := map[string]any{"notebook": in.Notebook, "section": in.Section, "page": in.Page, "property": in.Property}
		if !env.writeOK() {
			env.record("set_page_property", "denied", "write not granted", args)
			return toolErr("write tools are disabled — enable write grant in Silt Settings → AI → Local MCP")
		}
		if env.bridge == nil {
			env.record("set_page_property", "error", "no vault", args)
			return toolErr("no vault open")
		}
		if err := env.bridge.SetPageProperty(ctx, in.Notebook, in.Section, in.Page, in.Property, in.Value); err != nil {
			// Validation rejected the value before any file I/O — the file is
			// byte-identical to its pre-call state.
			env.record("set_page_property", "error", err.Error(), args)
			return toolErr(err.Error())
		}
		env.record("set_page_property", "ok", "", args)
		return toolJSON(map[string]any{"ok": true})
	})

	type setPageTypeIn struct {
		Notebook string `json:"notebook" jsonschema:"notebook name"`
		Section  string `json:"section" jsonschema:"section path (empty for root)"`
		Page     string `json:"page" jsonschema:"page name without .md"`
		Type     string `json:"type" jsonschema:"type id (from ListTypes) or empty to clear"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_page_type",
		Description: "Assign or clear (empty type) a page's note type. Schema-validated write — existing values are validated against the new schema before any file I/O. Requires write grant.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in setPageTypeIn) (*mcp.CallToolResult, any, error) {
		args := map[string]any{"notebook": in.Notebook, "section": in.Section, "page": in.Page, "type": in.Type}
		if !env.writeOK() {
			env.record("set_page_type", "denied", "write not granted", args)
			return toolErr("write tools are disabled — enable write grant in Silt Settings → AI → Local MCP")
		}
		if env.bridge == nil {
			env.record("set_page_type", "error", "no vault", args)
			return toolErr("no vault open")
		}
		if err := env.bridge.SetPageType(ctx, in.Notebook, in.Section, in.Page, in.Type); err != nil {
			env.record("set_page_type", "error", err.Error(), args)
			return toolErr(err.Error())
		}
		env.record("set_page_type", "ok", "", args)
		return toolJSON(map[string]any{"ok": true})
	})
}
