# ADR 0006: Backlinks query strategy — indexed reverse lookup, three legs

Date: 2026-07-19
Revised: 2026-07-20 (#704)
Status: Accepted

## Revision note

The original 0006 (lazy resolution, three legs, bounded scan) accepted a
leading-wildcard `raw_content` LIKE scan for the block-ref and embed legs
because the write-amplification cost of an eager index looked higher than
the panel-open scan cost. Issue #704 reversed that trade-off: an indexed
reverse-lookup table (`block_references`) now backs legs 2 and 3, the
LIKE scans are gone, and the write-amplification cost turned out to be
trivial (cascade-driven delete-then-insert inside the indexer's existing
transaction, with one prepared `INSERT OR IGNORE` per block). This
revision records the new decision; the original rationale is preserved
under [Original decision (superseded)](#original-decision-superseded) for
traceability.

## Context

Backlinks — showing every inbound reference to the current page — is a
core PKM feature. Silt has three reference types that can point *into* a
page:

1. **Page links** (`[[target]]`) — stored in a derived `page_links`
   reverse index (rebuilt on re-index, FK cascade from `blocks`).
2. **Block references** (`((uuid))`) — stored as literal text in
   `blocks.raw_content`.
3. **Embeds** (`{{embed:uuid}}`) — also literal text in
   `blocks.raw_content`.

The question is *when* and *how* to materialize and resolve these
references for the panel.

## Decision

**Indexed reverse lookup on panel open, three independent legs, merge +
dedupe.** All three legs read derived reverse-index tables; no leg
touches `blocks.raw_content` at query time.

`GetBacklinks(source, notebook, section, page)` runs when the panel
mounts or when the active page changes. It executes three sub-queries
under a single DB read lease:

### Leg 1: Page links (page_links index)

1. Compute `LinkTargetRawCandidates` for the target page (path variants
   + intermediate suffixes).
2. Query `page_links` by `target_raw IN (candidates)` — a single
   indexed lookup.
3. Resolve-gate each result against `ListDistinctPages` +
   `ResolvePageLinkAgainst` to exclude ambiguous targets (same contract
   as the rename-rewrite path — only nonambiguous canonical targets are
   backlinks).
4. Batch-fetch source-block info (source discriminator, `clean_content`
   for snippet) by `source_block_id IN (...)`.

### Legs 2+3: Block refs and embeds (block_references index)

1. Collect every block ID for the target page (`SELECT id FROM blocks
   WHERE source=? AND notebook=? AND section=? AND page=?`).
2. Query the derived `block_references` reverse index by
   `target_block_id IN (...)`, joining `blocks` on
   `block_references.source_block_id = blocks.id` to recover the source
   discriminator + notebook/section/page + `clean_content` for the
   snippet.
3. Map the stored `kind` column directly to `BacklinkBlockRef` /
   `BacklinkEmbed`; reconstruct the snippet token
   (`((target_block_id))` / `{{embed:target_block_id}}`) from the edge
   row so `snippet()` keeps its contextual behavior.
4. Batch in groups of 500 UUIDs (one bind arg each; well under SQLite's
   variable limit).

### Index maintenance

`block_references` is populated incrementally by `IndexFileBlocks` and
`IndexScanResults` inside their existing per-file transactions: the
shared `indexBlockReferences` helper extracts `((uuid))` and
`{{embed:uuid}}` tokens from each block's `RawText` with the existing
`parser.BlockRefRegex` and `parser.EmbedRegex` and emits one
`INSERT OR IGNORE` per distinct `(source_block_id, target_block_id,
kind)` edge. Cleanup is fully cascade-driven: the per-block
`DELETE FROM blocks` at the top of each indexer cascades to
`block_references` via `FOREIGN KEY(source_block_id) REFERENCES
blocks(id) ON DELETE CASCADE`, so re-indexing a source after a token
was edited away drops the stale edge automatically. The same path
covers `ClearFileBlocks`, `DeleteBlockFromPage`, `ClearSourceBlocks`,
and the file pruning flows — no competing manual cleanup is needed.

### Source-only foreign key (deliberate)

`block_references` carries `FOREIGN KEY(source_block_id) … ON DELETE
CASCADE` and **no** target FK. Markdown may reference an ID before the
target is indexed, after it was deleted, or in a file indexed later. A
target FK would force the row to be dropped whenever the target was
absent and the edge would never re-resolve when the target subsequently
appeared without a source re-index. The source-only FK keeps the edge
alive as a derived projection of the source-side markdown intent;
target existence is resolved at query time by joining against the live
`blocks` rows for the target page, so a dangling edge is silently inert
until the target reappears.

### Warm-upgrade backfill

The initial migration backfills `block_references` from pre-existing
`blocks.raw_content` in one restart-safe transaction, because warm
startup skips unchanged Markdown files (the `files`-table mtime+size
gate) and an upgraded vault would otherwise silently lose every
pre-existing block-ref/embed backlink until each source file was
touched. A `schema_migrations(name, applied_at)` ledger row
(`block_references_backfill`) is committed in the same tx as the
backfill body, so a crash rolls both back and the next open redoes the
work. `INSERT OR IGNORE` against the PK collapses same-kind duplicates.
A scoped `LEFT JOIN blocks b ON b.id = br.source_block_id WHERE b.id IS
NULL` post-check asserts catalog integrity without bricking vault opens
on unrelated FK orphans in other tables.

### Merge

All three legs produce `[]Backlink` entries. Results are deduped by
`(kind, source, source_notebook, source_section, source_page,
source_block_id)` and stably sorted by `(source, source_notebook,
source_section, source_page, kind, source_block_id)` for deterministic
panel rendering. The IPC returns a cursor-paged projection (50 rows by
default) so the panel appends results on an explicit **Load more**
action rather than materializing an unbounded payload.

## Source-aware behavior

The caller (`App.GetBacklinks`) resolves the notebook's `source`
server-side via `resolveSourceByName` before passing it to the DB layer.
This ensures target-page blocks are scoped correctly and that backlinks
from linked notebooks carry their own `source` discriminator.

## Tradeoffs

### Accepted costs

- **Index maintenance write cost.** Every `IndexFileBlocks` /
  `IndexScanResults` call extracts block-ref + embed tokens per block
  and emits one `INSERT OR IGNORE` per distinct edge inside the
  indexer's existing transaction. The cascade through `DELETE FROM
  blocks` clears stale edges without a dedicated cleanup pass. This
  cost is paid once per file re-index and is negligible against the
  parse + block-insert work the indexer already does.
- **Paged projection.** The panel exposes results in cursor pages with
  an explicit **Load more** action. This bounds initial IPC and DOM
  work, but the underlying collection still merges all three legs in
  Go before slicing. Each leg is now an indexed lookup against its
  derived reverse table, so collection cost is proportional to inbound
  edge count rather than total block count.
- **Not real-time.** The panel refreshes on `block:changed` (debounced
  200 ms in the frontend), not on every keystroke. A user typing a
  `((uuid))` in another page sees the backlink appear after the save
  lands + re-index + debounce. This matches the existing embed refresh
  model.

### Why the eager index won (vs. the original lazy scan)

- **Linear scan cost grew.** The block-ref/embed legs scanned
  `blocks.raw_content` with `LIKE '%((uuid))%'` for every block ID on
  the target page. Each leading-wildcard clause scanned across the
  table (no covering index), so latency grew with both indexed block
  count and target-page block count. On a vault with 50K+ blocks and a
  page with 30+ blocks, one panel open could issue 60+ parameterized
  `%...%` clauses, each scanning the full table.
- **Write cost was overstated.** The original decision assumed the
  eager index would double index-maintenance work on every save. In
  practice the extractor walks `RawText` once per block (the parser
  already does this for `links_count`) and the cascade handles cleanup
  with no extra DELETE. The marginal cost is one prepared-statement
  `Exec` per distinct edge — typically 0–3 per block.
- **False-positive defense was a smell.** The LIKE path needed a
  per-row token-rescan (`targetRefTokens` map + `strings.Contains`
  loop) to avoid classifying a row that incidentally contained an
  unrelated non-target `((uuidX))` token. The indexed approach is
  structurally exact: each edge row is created by a regex match at
  extraction time, so substring false positives are impossible and the
  rescan disappears.

### Rejected alternatives

**A. FTS5 for block-refs.** Using the existing `blocks_fts` virtual
table to find `((uuid))` references. Rejected because FTS5 tokenizes on
word boundaries — `((uuid))` is a single token or may not tokenize
usefully (depending on the tokenizer), making FTS5 unreliable for exact
UUID matching. A dedicated reverse-index table with `INSERT OR IGNORE`
is exact and predictable, and it carries the `kind` discriminator
(block-ref vs. embed) for free.

**B. In-memory backlink cache.** Maintaining a Go-side map of
`page → []Backlink` refreshed on every `block:changed`. Rejected because
it duplicates the DB query logic, needs its own invalidation (stale on
external edits until fsnotify re-index), and adds memory pressure for a
panel that is not always visible.

## Consequences

- `GetBacklinks` is a read-only query (no write path beyond indexer
  maintenance). It uses the `page_links`, `block_references`, and
  `blocks` infrastructure.
- The three-leg architecture means adding a new reference type (e.g.
`![[embed-page]]`) is one new leg function + one new `BacklinkKind`
constant + one new `kind` value in `block_references` — no schema
migration beyond seeding the new edges.
- The resolve-gate on leg 1 matches the rename-rewrite contract exactly,
  so a page that was renamed correctly rewrites all its inbound `[[…]]`
  links, and the backlinks panel sees the updated targets on the next
  refresh.
- The `block_references` table is the fourth re-derivable projection
  alongside `task_dependencies`, `block_meta`, and `page_links`.
  ARCHITECTURE.md §0 rule 4 (SQLite is discardable working memory) covers
  all of them: drop the index, re-index, and every derived table
  regenerates from markdown.

## Original decision (superseded)

The original 0006 chose **lazy resolution on panel open with two
batched `raw_content LIKE '%((uuid))%'` scans** for legs 2 and 3,
accepting linear cost in total block count. The rationale was that the
write-amplification cost of an eager `backlinks` table would outweigh
the query-time scan cost for a panel that loads once per page
navigation. The three rejected alternatives at the time (pre-indexed
backlinks table, FTS5 for block-refs, in-memory cache) are reconsidered
above; only the FTS5 and in-memory options remain rejected. The
"pre-indexed table" alternative is now the accepted design, refined to
a source-only-FK edge table with cascade-driven maintenance.

## Post-decision schema evolution

The `page_links` reverse index gained a `source` discriminator column
(`'vault' | 'linked:<id>'`) and a revised 6-column primary key
`(source, source_notebook, source_section, source_page, source_block_id,
target_raw)` to support source-qualified linked notebooks. Pre-existing
vaults are migrated by adding the column, backfilling from `blocks` via
`source_block_id`, and rebuilding the table under the new PK. The
migration is restart-safe (probes the actual PK shape from
`sqlite_master` after ALTER). The `block_references` migration follows
the same restart-safe pattern via the `schema_migrations` ledger. Both
tables are working memory — re-index regenerates them from markdown.
