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
	// IDs when the client supplies them (SaveFileBlocks path). Page must already
	// exist — callers should not use this to create pages.
	UpdateBlocks(ctx context.Context, notebook, section, page string, blocks []parser.ParsedBlock) error
	// PageExists reports whether the notebook/section/page file is on disk.
	PageExists(ctx context.Context, notebook, section, page string) (bool, error)
	// GetPageMetadata returns a page's resolved type, schema-merged properties,
	// and the raw parsed frontmatter in one snapshot. An untyped page yields an
	// empty Type (or the raw ref if the schema is unknown) and an empty
	// Properties slice; Frontmatter still reflects every parsed key.
	GetPageMetadata(ctx context.Context, notebook, section, page string) (PageMetadataResult, error)
	// SetPageProperty writes a single typed property value. The App layer
	// validates (structural + relation-target) BEFORE any file I/O, so an
	// invalid value leaves the file byte-identical — implementers MUST preserve
	// that contract (no pre-write side effects).
	SetPageProperty(ctx context.Context, notebook, section, page, property, value string) error
	// SetPageType assigns or clears (empty typeName) the page's note type. The
	// App layer validates existing frontmatter against the new schema before
	// writing; on validation failure the file is untouched. The returned
	// []string is the keep-and-flag list: property names whose current values
	// do not fit the new schema (kept verbatim on disk). Empty on a clear or
	// when every value validates.
	SetPageType(ctx context.Context, notebook, section, page, typeName string) ([]string, error)
	// CreateBlock inserts a block after afterID (empty = page end). Page must
	// already exist (callers check PageExists). Returns the new block UUID.
	CreateBlock(ctx context.Context, afterID, notebook, section, page, blockType, text string) (id string, err error)
	// CreateStandaloneTask appends a TASK to the vault standalone task store
	// (.silt/tasks.md). dueDate is YYYY-MM-DD or empty; status defaults at App.
	CreateStandaloneTask(ctx context.Context, title, dueDate, status string) (id string, err error)
	SetTaskOwner(ctx context.Context, blockID, owner string) error
	SetTaskTags(ctx context.Context, blockID string, tags []string) error
	// SetTaskDueDate sets or clears (empty) [due:: YYYY-MM-DD] on an existing task.
	SetTaskDueDate(ctx context.Context, blockID, dueDate string) error
	// UpdateBlockState sets task status (TODO|DOING|DONE). Used after page-scoped
	// create_task when the client requests a non-default status.
	UpdateBlockState(ctx context.Context, blockID, status string) error
	// ResolveBlock looks up a block UUID (Exists=false when missing, not error)
	// and sets Embedded when any inbound embed references the id.
	ResolveBlock(ctx context.Context, blockID string) (BlockRefResult, error)
	GetBacklinksPaged(ctx context.Context, notebook, section, page, cursor string, limit int) (db.BacklinksResult, error)
	// ListPageVersions returns retained snapshots newest-first. Missing
	// history is an empty slice, not an error.
	ListPageVersions(ctx context.Context, notebook, section, page string) ([]PageVersionInfo, error)
	// GetPageVersion returns the stored markdown body (no frontmatter).
	GetPageVersion(ctx context.Context, notebook, section, page, versionID string) (string, error)
	// RestorePageVersion replaces the live page body with a stored version.
	RestorePageVersion(ctx context.Context, notebook, section, page, versionID string) error
	VaultPath() string
}

// BlockRefResult is the get_block payload: ResolveBlockReference fields plus
// whether any {{embed:uuid}} targets this id.
type BlockRefResult struct {
	ID         string `json:"id"`
	CleanText  string `json:"clean_text"`
	RawText    string `json:"raw_text"`
	Type       string `json:"type"`
	Notebook   string `json:"notebook"`
	Section    string `json:"section"`
	Page       string `json:"page"`
	FileDate   string `json:"file_date"`
	Exists     bool   `json:"exists"`
	LineNumber int    `json:"line_number"`
	Embedded   bool   `json:"embedded"`
}

// PropertyValue mirrors the app layer's PagePropertyValue so the MCP host can
// return schema-merged property data without importing the main package. Field
// shape (and JSON tags) match the IPC contract exactly so AI clients see the
// same form whether they read via IPC or MCP.
type PropertyValue struct {
	Name     string   `json:"name"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Value    any      `json:"value"`
	IsSet    bool     `json:"isSet"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
}

// PageMetadataResult is the read model returned by GetPageMetadata. Type is the
// canonical id (empty for an untyped page; the raw frontmatter value when the
// id does not resolve to a known schema, so clients can surface a raw chip).
// Properties is schema-merged — every declared property appears in declaration
// order, with IsSet=false for unset ones. Frontmatter is the raw parsed YAML
// map (all keys, not just schema-declared ones).
type PageMetadataResult struct {
	Notebook    string          `json:"notebook"`
	Section     string          `json:"section"`
	Page        string          `json:"page"`
	Type        string          `json:"type"`
	Properties  []PropertyValue `json:"properties"`
	Frontmatter map[string]any  `json:"frontmatter"`
}

// MaxSearchLimit caps search_blocks / search_notes page size.
const MaxSearchLimit = 50

// MaxBlocksRead caps blocks returned from read_page / read_blocks.
const MaxBlocksRead = 200

// MaxBlockTextRunes caps a single block body in write tools.
const MaxBlockTextRunes = 32_000

// PageVersionInfo is one retained snapshot row. Field names match the App
// IPC contract so MCP clients see the same shape as ListPageVersions.
type PageVersionInfo struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Source    string `json:"source"`
	Bytes     int    `json:"bytes"`
}
