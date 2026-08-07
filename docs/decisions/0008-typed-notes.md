# ADR 0008: Typed notes — schema-driven property system

Date: 2026-08-02
Status: Accepted

## Context

Silt's note model is a graph of untyped Markdown pages (Obsidian/Logseq
lineage). Users asked for structured, "databased" notes (Notion/Capacities
lineage): a page declares a `type:` in YAML frontmatter, inherits that type's
property schema from a `.system/types/<type>.yaml` file, renders and edits
its properties through a dedicated UI, and appears in a per-type dashboard
queryable by property — while Markdown frontmatter remains the source of
truth and the SQLite index stays reproducible from it (cardinal rule #4,
ARCHITECTURE §0).

Four design questions needed resolution before implementation:

**(a) Direction: commit to databased notes?** The feature is a product-line
shift, not an incremental add-on. It introduces a schema layer, a projection,
a new UI surface, and MCP read/write tools. Once pages carry `type:` and
property values, removing the feature would orphan frontmatter fields.

**(b) Propagation model: rewrite every file on schema change, or derive
virtually?** When a user adds a property to a type's schema, every page of
that type should show the new field. Two approaches: (1) batch-rewrite every
instance file to insert the new frontmatter key, or (2) treat the schema as
the source of truth and let the UI render schema-declared fields even when
the frontmatter value is absent (virtual propagation).

**(c) UI placement: where does the properties editor live?** The app's chrome
edges are all claimed: top = titlebar + breadcrumb, right = AI + task
drawers, left = nav rail. A properties surface needs a home that doesn't
displace reading context.

**(d) Validation: hand-rolled validator or a JSON-Schema engine?** The
property-type taxonomy is closed (9 types), but relation-target checks
(page must exist + match the declared target type) cannot be expressed in
JSON Schema. A JSON-Schema engine would handle the scalar types but leave
relation validation as a separate concern, splitting the validation logic
across two systems.

## Decision

**(a) Commit to databased notes.** Ship the full typed-notes epic as Silt
0.4.0. The `type:` frontmatter field, the `.system/types/` schema directory,
the SQLite projection, the properties UI, the per-type dashboard, and the
MCP read/write tools all land together. This is a minor-version bump, not a
major: existing frontmatter is unchanged (the `type:` field is purely
additive), and untyped pages continue to work exactly as before.

**(b) Schema is the source of truth; frontmatter holds only set values
(virtual propagation).** Adding a property to a type's schema does NOT
rewrite any instance file. The field appears (empty) because the schema
declares it — the UI merges schema-declared fields with set frontmatter
values at render time. This satisfies the "propagates to all instances"
requirement with zero file churn, preserves byte-exactness of unrelated
frontmatter keys, and means the SQLite projection only stores rows for
properties that actually have values (sparse, like `block_meta`). The
projection is re-derived from frontmatter + schema on every index rebuild.

This follows the Capacities/Tana consensus (confirmed via research): a
schema change is a schema-level operation, not a per-file migration. The
cost is a slightly more complex read path (the UI must merge two sources);
the benefit is that schema evolution never risks corrupting user content.

**(c) Bottom docked panel for the properties editor.** The panel rises on
demand (type chip click or `Mod+;`), pushes the editor up via `flex-1`
shrink so reading context is preserved, and uses `aria-modal="false"` (like
the existing `TaskEditDrawer`) so focus can leave the panel without closing
it. The bottom edge is the only unclaimed chrome surface. An inline meta
strip in the breadcrumb row shows the type chip + hero field value for
glanceability; untyped pages stay clean (no "+ Type" noise).

**(d) Hand-rolled validator over a JSON-Schema engine.** The 9-type taxonomy
(`text | number | date | datetime | checkbox | select | multiselect | page |
pages`) is closed and small. A hand-rolled `switch` validator (~100 lines)
handles all scalar types with `strconv`/`time.Parse`/membership checks AND
handles relation validation (target existence + declared target type via an
index lookup) in the same code path. A JSON-Schema engine would require a
dependency, a schema-to-JSON-Schema translation layer, and a separate
relation validator — three moving parts instead of one. The closed taxonomy
means JSON Schema's extensibility buys nothing.

## Consequences

- **`.system/types/*.yaml`** is a new per-vault YAML asset (tier 2), sibling
  to `.system/templates/`. Shipped defaults scaffold the directory on vault
  init: `book`, `meeting`, `person`, `project`, `decision`, `one_on_one`,
  `standup`, `retrospective` (entities + structured rituals; not Daily/Weekly).
  A hot-reload watcher (extending the templates watcher pattern) bumps an
  in-memory schema generation and emits a `types:changed` event on external
  edits.
- **`page_types` + `page_properties`** are new SQLite projection tables
  (tier 5, working memory). They are reproducible: deleting the index and
  relaunching rebuilds them from frontmatter + schema. Source-aware
  (`source` column discriminates vault vs linked notebooks).
- **MCP write safety:** the schema gates every write. `set_page_property`
  validates the value against the type's property schema AND checks relation
  targets BEFORE any file I/O. Invalid values return a structured error and
  leave the file byte-identical. This is the core safety argument for
  exposing typed-property writes to AI clients.
- **Virtual propagation means the read path merges two sources.** This is an
  acceptable cost — the merge is a simple schema-walk that fills in empty
  values for schema-declared fields that have no frontmatter value.
- **No two-way inverse write for relations.** A `page`/`pages` value stores
  the target path; deleting the target leaves a dangling reference (silently
  inert, like `block_references`' source-only FK). Backlinks already derive
  the reverse via `page_links`. A two-way inverse write is a documented
  follow-up.

## Alternatives considered

- **Batch-rewrite on schema change.** Rejected: risks corrupting unrelated
  frontmatter keys, breaks byte-exactness guarantees, and churns the file
  system on every schema edit. The schema-as-source-of-truth model is strictly
  better for a local-first tool where the user's files are the product.
- **JSON-Schema engine.** Rejected: adds a dependency and a translation layer
  for a closed 9-type taxonomy; cannot express relation-target validation;
  splits validation logic across two systems.
- **Properties in a side drawer (right edge).** Rejected: the right edge is
  already claimed by the AI panel and task drawers. A bottom panel is the
  only unclaimed chrome edge and preserves reading context better than a
  side drawer (which narrows the editor width).
- **Per-file SQLite for type schemas.** Rejected: type schemas are per-vault
  YAML assets (tier 2), not working memory. They are user-authored (or
  shipped defaults), durable, and portable. Storing them in SQLite would
  violate cardinal rule #4 (SQLite is working memory, not a system of record).
