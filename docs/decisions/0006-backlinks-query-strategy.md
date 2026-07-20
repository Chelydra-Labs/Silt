# ADR 0006: Backlinks query strategy — lazy resolution, three legs, bounded scan

Date: 2026-07-19
Status: Accepted

## Context

Backlinks — showing every inbound reference to the current page — is a
core PKM feature. Silt has three reference types that can point *into* a
page:

1. **Page links** (`[[target]]`) — stored in a derived `page_links` reverse
   index (rebuilt on re-index, FK cascade from `blocks`).
2. **Block references** (`((uuid))`) — stored as literal text in
   `blocks.raw_content`.
3. **Embeds** (`{{embed:uuid}}`) — also literal text in `blocks.raw_content`.

The question is *when* and *how* to resolve these references for the panel.

### Why not pre-index all backlinks?

A dedicated `backlinks` table mapping every `(target_page → source_block)`
triple is the most obvious approach. It would make the panel query a single
indexed lookup. However:

- **Write amplification.** Every `MutateBlock` / `SaveFileBlocks` call that
  touches `((uuid))` or `[[…]]` syntax would need to delete stale rows and
  insert new ones, doubling the index-maintenance work on every save.
- **Index bloat.** A page with 200 blocks referenced by 50 other pages
  generates 10,000 rows in the backlinks table — all re-derivable from the
  `blocks` and `page_links` tables that already exist.
- **Cascade correctness.** The backlinks table would need its own cascade
  rules (block delete → prune rows; page rename → rewrite target rows) on
  top of the existing `page_links` cascade, creating a second surface for
  rename/rewrite bugs.

Given that the core index is working memory (ARCHITECTURE §0 rule 4), adding
a fourth re-derivable projection table is only justified if the query cost
of the lazy approach exceeds the write-amplification cost of the eager
approach. For a single-page backlinks panel, it does not.

## Decision

**Lazy resolution on panel open, three independent legs, merge + dedupe.**

`GetBacklinks(source, notebook, section, page)` runs when the panel mounts
or when the active page changes. It executes three sub-queries under a single
DB read lease:

### Leg 1: Page links (indexed)

1. Compute `LinkTargetRawCandidates` for the target page (path variants +
   intermediate suffixes).
2. Query `page_links` by `target_raw IN (candidates)` — a single indexed
   lookup, no full-table scan.
3. Resolve-gate each result against `ListDistinctPages` + `ResolvePageLinkAgainst`
   to exclude ambiguous targets (same contract as the rename-rewrite path —
   only nonambiguous canonical targets are backlinks).
4. Batch-fetch source-block info (source discriminator, `clean_content` for
   snippet) by `source_block_id IN (...)`.

### Legs 2+3: Block refs and embeds (bounded LIKE scan)

1. Collect every block ID for the target page (`SELECT id FROM blocks WHERE
   source=? AND notebook=? AND section=? AND page=?`).
2. Build parameterized `LIKE` clauses: `raw_content LIKE '%((uuid))%'` and
   `raw_content LIKE '%{{embed:uuid}}%'` for each target UUID. UUIDs are
   fixed 36-char hex with no LIKE-special characters, so the pattern
   construction is safe without escaping.
3. Batch in groups of 400 (each UUID contributes 2 bind args = 800; well
   under SQLite's 999 variable limit).
4. Classify each hit against the exact target tokens (not the LIKE pattern)
   to avoid false positives from substring overlap.

### Merge

All three legs produce `[]Backlink` entries. Results are deduped by
`(kind, source, source_notebook, source_section, source_page, source_block_id)`
and stably sorted by `(source_notebook, source_section, source_page, kind,
source_block_id)` for deterministic panel rendering.

## Source-aware behavior

The caller (`App.GetBacklinks`) resolves the notebook's `source` server-side
via `resolveSourceByName` before passing it to the DB layer. This ensures
target-page blocks are scoped correctly and that backlinks from linked
notebooks carry their own `source` discriminator.

## Tradeoffs

### Accepted costs

- **Panel-open latency on large vaults.** The block-ref/embed legs scan
  `blocks.raw_content` with LIKE for every block ID on the target page. A
  page with 500 blocks against a 100,000-block index is a bounded scan of
  ~1000 LIKE clauses across the full table (no covering index). In practice
  this completes in under 50 ms on WAL-mode SQLite for typical vault sizes.
   For exceptionally large vaults (10k+ pages), the latency grows with both
   the total indexed block count (each LIKE clause scans across the table) and
   the target page's block count (which determines the number of LIKE clauses
   issued). Batching keeps the per-query bind-variable count bounded at 800,
   but the per-batch scan cost still scales with vault size.
- **No pagination.** The panel returns every backlink; there is no
  "load more" for the panel. This is acceptable because the number of
  inbound references to a single page is typically small (< 100).
- **Not real-time.** The panel refreshes on `block:changed` (debounced 200 ms
  in the frontend), not on every keystroke. A user typing a `((uuid))` in
  another page sees the backlink appear after the save lands + re-index +
  debounce. This matches the existing embed refresh model.

### Rejected alternatives

**A. Pre-indexed backlinks table.** Rejected because the write amplification
on every block save (delete+insert backlink rows) and the cascade-maintenance
burden outweigh the query-speed benefit for a panel that loads once per page
navigation. The existing `page_links` reverse index already covers leg 1; legs
2 and 3 scan only the target page's block IDs (bounded).

**B. FTS5 for block-refs.** Using the existing `blocks_fts` virtual table to
find `((uuid))` references. Rejected because FTS5 tokenizes on word
boundaries — `((uuid))` is a single token or may not tokenize usefully
(depending on the tokenizer), making FTS5 unreliable for exact UUID matching.
Parameterized LIKE is exact and predictable.

**C. In-memory backlink cache.** Maintaining a Go-side map of
`page → []Backlink` refreshed on every `block:changed`. Rejected because it
duplicates the DB query logic, needs its own invalidation (stale on external
edits until fsnotify re-index), and adds memory pressure for a panel that
is not always visible.

## Post-decision schema evolution

The `page_links` reverse index gained a `source` discriminator column
(`'vault' | 'linked:<id>'`) and a revised 6-column primary key
`(source, source_notebook, source_section, source_page, source_block_id,
target_raw)` to support source-qualified linked notebooks. Pre-existing
vaults are migrated by adding the column, backfilling from `blocks` via
`source_block_id`, and rebuilding the table under the new PK. The migration
is restart-safe (probes the actual PK shape from `sqlite_master` after
ALTER). This is safe because `page_links` is working memory — re-index
regenerates it from markdown.

## Consequences

- `GetBacklinks` is a read-only query (no new write path). It reuses the
  existing `page_links`, `blocks`, and `blocks_fts` infrastructure.
- The three-leg architecture means adding a new reference type (e.g.
  `![[embed-page]]`) is one new leg function + one new `BacklinkKind` constant
  — no schema migration.
- The resolve-gate on leg 1 matches the rename-rewrite contract exactly,
  so a page that was renamed correctly rewrites all its inbound `[[…]]`
  links, and the backlinks panel sees the updated targets on the next
  refresh.
