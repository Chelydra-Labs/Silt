# ADR 0002: Theme schema v2 — no v1 migration

Date: 2026-07-04
Status: Accepted

## Context

Silt's v1 theme schema — 21 required tokens per mode against a flat 5-level
`bg` model plus an optional binary `chrome` block — cannot support named surface
zones, OKLCH color derivation, expanded token coverage (radii / spacing / shadow
/ typography scale / editor / themeable error family), or user-supplied
background images without a breaking change. The eight concrete gaps are
enumerated in [`docs/theme-system-v2-rfc.md`](../theme-system-v2-rfc.md) §0.

Two facts about the project make a compatibility shim unwelcome here:

- **Single-user project.** There is no installed base of third-party themes to
  keep working. The cardinal rule "delete obsolete compatibility shims rather
  than layer new ones" (`AGENTS.md`) applies: a migration layer would be permanent
  dead weight carried by an engine whose value is simplicity.
- **First-party themes are the only themes in the wild.** Every shipped theme is
  authored in-tree under `backend/themes/themes/`, so they can be re-authored
  natively in v2 at the same time the engine switches, with no external
  coordination.

## Decision

**v2 is the only supported theme schema.** `schema_version` is hard-enforced at
`"2.0.0"` (`backend/themes/validate.go` rejects any other value with a
descriptive error, and `DisallowUnknownFields` makes typos fail loudly). There
is **no v1→v2 migration code path**:

- A theme file carrying any other `schema_version` (including a v1 `"1.0.0"`
  file) is rejected on import with a clear, field-level error.
- The first-party theme set is re-authored natively as v2 JSON (the canonical
  example is `backend/themes/themes/cyber_forest.json`).
- The engine carries zero v1 parsing, translation, or fallback logic.

## Consequences

- A hand-edited or previously-exported v1 theme file will be **rejected on
  import**, not silently translated. The error names `schema_version` so the
  cause is obvious; the fix is to re-author the file as v2
  ([`docs/THEMING.md`](../THEMING.md) §2 is the authoring reference).
- The theme engine stays free of compatibility shims, in line with the repo's
  no-shim rule — one schema, one code path, one source of truth for token names.
- `ARCHITECTURE.md` §4.4, `SPECS.md` §6.4, `DESIGN.md` §2.1, and
  `docs/THEMING.md` document v2 as the shipping schema; this ADR records *why*
  there is no bridge from v1.
- Forward-only from here: a future v3 would face the same decision on its own
  merits, not inherit a v1 compatibility obligation.
