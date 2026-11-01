# ADR 0010: Requirement priorities — the critical few, and why storage stays markdown

Date: 2026-11-01
Status: Accepted

## Context

Silt's goals had accumulated implicitly across README.md, SPECS.md, and
ARCHITECTURE.md. Several were aspirational rather than load-bearing: a fixed
65MB idle-RAM figure, a 450ms/1000-file ingest threshold, and a "renders
correctly on GitHub, Obsidian, VS Code, **and** through Pandoc" dialect
checklist all read as hard requirements while constraining design without
serving a user-facing outcome.

Two questions forced the accounting:

1. **Does the block model earn its cost?** Every non-empty line is a block
   carrying a trailing `<!-- id: uuid @ DATE -->` comment, and hierarchy is
   encoded as a `depth: 0-6` integer on sibling nodes rather than real
   nesting.
2. **Should storage move to HTML or ODT?** Prompted by the "HTML is the new
   markdown" argument circulating in 2026.

Both were evaluated against the only two priorities the owner has: **(A)
taking notes** and **(B) keeping and tracking actions/to-dos**. Migration
cost was excluded from the comparison by explicit instruction — only the
end state was scored. Ongoing maintenance cost counts, because that is a
property of the end state.

## Decision

### 1. Requirement tiers

Adopt the tier list in SPECS §1.3 as the authoritative tie-breaker. In
summary:

- **Tier 0 (existential):** the file is the product; no unrecoverable loss
  on any write path.
- **Tier 1A (notes):** fast capture; external folders editable in place;
  round-trip fidelity; one retrieval surface over notes *and* tasks;
  attachments and task↔note context.
- **Tier 1B (tasks):** one-gesture capture; metadata travels in plain text;
  reversible destructive mutation; overdue/today at a glance; a rapid
  multi-dimension management surface; dependency enforcement.
- **Tier 2:** Calendar; recursive page-tree nesting; recurrence authoring;
  dependency-authoring UX; AI-derived artifacts; plugin modularity; typed
  notes; themes; graph.
- **Tier 3:** persistent generated-artifact surfaces; semantic retrieval as
  a required path.

### 2. Storage stays GFM markdown

HTML and ODT are rejected as storage formats. Markdown is retained,
including the documented Silt Markdown extensions (callouts, Dataview
inline metadata, `((uuid))` refs, `{{embed:uuid}}`, block-identity
comments, mentions, math, footnotes).

### 3. The block model stays; hierarchy encoding does not change

Block-typed line nodes, the flat `depth` attribute, and inline task tokens
are retained. The case for replacing them with a real recursive tree rested
on reading SPECS §1.2's "organized by days" as an outliner/daily-journal
product model. SPECS §3.1 explicitly supersedes that line — a Page is "a
focused topic (not an unbounded daily journal)" — so recursive page-tree
nesting is Tier 2 maintenance, not a product requirement. Four independent
reviews concurred; see Dissent and consensus below.

### 4. Retrieval is the one genuinely missing Tier 1 capability

Search exists and is not the gap: `blocks_fts` (FTS5) covers block bodies
with type/notebook/section/tag/scope/sort filters, is rebuildable, and is
watcher-invalidated. The gap is narrower and real:

- `scanTaskTokens` strips `[key:: value]` from the text **before** it
  becomes `clean_content`, and FTS indexes only `clean_content` — so an
  owner name or a due date is unsearchable although both are on disk.
- `SearchFilters` carries no owner/status/due/priority dimension, and the
  result shape does not expose them.
- The Tasks hub has no text query at all; its "Find task…" action leaves the
  hub for the global search modal.

## Options considered

**A. Change storage to HTML.** Rejected. It voids the plaintext contract
and the in-place external-folder workflow (Silt writes into Obsidian and
OneDrive directories). No note app has migrated markdown → HTML; every
documented migration goes to a structured store (Logseq → SQLite, SiYuan →
JSON, Confluence → ADF). Evernote, which does store HTML (ENML), prohibits
`id`, `class`, and `data` — the exact attributes block identity needs — and
cannot re-import its own HTML export. Measured cost on Silt-shaped content
is roughly 2× tokens for no measured comprehension gain, and HTML
serializers are not byte-idempotent (the HTML spec changed attribute
escaping in 2025).

**B. Change storage to ODT.** Rejected. Measured at 2.07× the bytes;
`content.xml` is a single XML line inside a ZIP, so a one-word edit changes
bytes throughout and `git diff` reports a binary difference. Unreadable by
grep, by external editors, and by agents.

**C. Block JSON storage (ProseMirror/ADF-style).** Rejected for now. It is
what comparable apps do, and TipTap is a JSON document model underneath, so
it is the most defensible alternative — but it makes files opaque and
converts Silt from "an app that reads your folder" into "an app that owns
your data," which contradicts Tier 0.

**D. Replace the flat `depth` model with a real recursive tree.** Deferred
to Tier 2. The bug classes that motivated it are real (see Consequences),
but the product premise did not hold, and four reviews held that neither
the page model nor ordinary nested lists requires recursive tree editing.

**E. CM6 text-is-truth (Obsidian's architecture).** Rejected. It deletes
the projection layer entirely, which is the strongest fidelity answer
available — but it downgrades the editing experience from WYSIWYG to
syntax-visible live preview, which is the dimension the product competes
on.

## Why these won

- **Over-determined by Tier 0.** External-folder editing plus "the file is
  the product" make HTML and ODT non-viable regardless of their merits.
- **The requirement set is smaller and sharper.** Removing aspirational
  thresholds means design decisions are no longer made to satisfy numbers
  nobody validated.
- **The missing capability is cheap and compounding.** A text predicate in
  the task hub plus task-attribute filters answers most of "find my task
  about X" and serves A and B with one mechanism.

## Consequences

### Accepted

- **Known correctness debts remain in Tier 1/2 scope.** The validator that
  would enforce structural invariants does not exist; several defects were
  verified during this analysis and are recorded here rather than fixed:
  - `indentActiveBlock` is not subtree-aware: indenting a parent can set it
    to its child's depth, so the child becomes a sibling.
  - `DEPTH_BLOCK_TYPES` includes `headerBlock`, where `depth` means heading
    level; Tab on an H6 produces depth 7 and renders `#######`, which is not
    a heading in CommonMark. `MAX_DEPTH` is enforced only on the drag path.
  - Region blocks hardcode `depth: 0`, so an indented code/table/callout
    un-indents on save.
  - The tree is derived four times (Go stack walk, a TypeScript port that is
    computed and discarded, a depth-run scan, and a `ParentID` graph walk),
    and two of those readers can disagree.
  - Cross-file duplicate block IDs are accepted by the parser while
    `blocks.id` is a global primary key; the resulting index error is
    logged, not surfaced.
- **`[blocked_by::]` metadata is not searchable**, because task tokens are
  stripped before indexing. Task-attribute filters are the documented fix.
- **Linked-root destructive operations lack parity with vault ones** (no
  trash for in-place deletes). Tier 0 requires recovery or explicit
  confirmation; the UI for the richer conflict case is Tier 2.

### Not accepted

- Byte-exact parity with every third-party renderer as a design constraint.
  The durable property is "no app-private syntax in the file."
- Fixed idle-RAM and ingest-time figures as architecture drivers.

## Dissent and consensus

Three independent councillor reviews plus one independent critique were
run. On the two contested questions the result was:

- **Board is Tier 1** (3–0, with two seats changing position). Board is the
  only rapid arbitrary-status transition surface: the List's sole status
  write is a mark-done action, and other status/owner/priority changes
  require opening a task drawer. Calendar remains Tier 2.
- **AI-derived artifacts are Tier 2** (3–0, two seats changing position).
  They depend on an optionally-configured provider and therefore cannot be
  a prerequisite of the local-first core loop. Retrieval is the Tier 1
  requirement; artifacts are output, not location.
- **Latency precedes external folders** (3–0, one seat changing from
  "neither dominates"), on the grounds that latency is paid on every
  keystroke while external folders are a conditional capability.
- **Linked-root destructive safety is Tier 0** (2–1, one seat at Tier 1),
  resolved as a release gate on behavior — recoverable delete, no silent
  overwrite — without requiring conflict-resolution UI.

One factual correction was made during reconciliation: the claim that the
vector index has no readers is incorrect (it is wired through
`hybridRetrieve`). It does not change the artifact tiering, which rests on
provider optionality rather than on the index being dead code.

The strongest surviving argument *for* restructuring the block model is
maintenance: consolidating four tree derivations into one, and making
markdown parse/serialize single-sourced in Go so the frontend is a pure
tree consumer. That is a real Tier 2 improvement, and it is not justified
by bug counts alone.
