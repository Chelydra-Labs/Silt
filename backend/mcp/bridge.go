package mcp

import (
	"context"

	"silt/backend/db"
	"silt/backend/parser"
)

// Bridge is the content surface the MCP host calls. Implemented by the App
// layer so tools reuse the same vault writer/indexer paths as Plugin* APIs.
// All methods must be safe under concurrent vault lifecycle (return clear
// errors when no vault is open or the vault is switching).
type Bridge interface {
	SearchBlocksPaged(ctx context.Context, query string, offset, limit int, filters db.SearchFilters) (parser.SearchResult, error)
	FetchPageBlocks(ctx context.Context, notebook, section, page string) ([]parser.ParsedBlock, error)
	FetchPageMarkdown(ctx context.Context, notebook, section, page string) (string, error)
	ListNavigation(ctx context.Context) (parser.NavigationTree, error)
	CreatePage(ctx context.Context, notebook, section, page, dateStr string) (string, error)
	// UpdateBlocks identity-preserving: replaces page body while keeping block
	// IDs when the client supplies them (SaveFileBlocks path).
	UpdateBlocks(ctx context.Context, notebook, section, page string, blocks []parser.ParsedBlock) error
	MutateBlock(ctx context.Context, blockID, newText string) error
	VaultPath() string
}

// MaxSearchLimit caps search_blocks / search_notes page size.
const MaxSearchLimit = 50

// MaxBlocksRead caps blocks returned from read_page / read_blocks.
const MaxBlocksRead = 200

// MaxBlockTextRunes caps a single block body in write tools.
const MaxBlockTextRunes = 32_000
