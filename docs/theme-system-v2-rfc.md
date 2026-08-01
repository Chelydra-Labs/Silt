# RFC: Theme System v2 — Engine & Token Architecture

> **Status:** Accepted (Phase 1 of Theme System v2, milestone #35).
> **Tracks:** #383. **Replaces:** the v1 schema documented in
> [`docs/THEMING.md`](THEMING.md) §2 and the v1 engine in
> [`ARCHITECTURE.md`](../ARCHITECTURE.md) §4.4.
> **Scope of this RFC:** the v2 token schema, surface-zone model, color-space
> strategy, unified background system, contrast guarantee, and the *contract*
> for the (separately-tracked) custom theme editor. This RFC is design-only;
> implementation is split across #384–#391, #394, #395.

This document is the single source of truth every Theme System v2 issue
references for token names, types, surface zones, and behaviors. Downstream
issues do not re-decide them.

## 0. Why v2

Silt's v1 theme schema (21 required tokens per mode against a flat 5-level `bg`
model plus an optional binary `chrome` block) cannot support named surface
zones, OKLCH derivation, expanded token coverage (radii/spacing/shadow/
typography scale/editor/error), or user-supplied background images without a
breaking change. Codebase recon confirmed eight concrete gaps:

1. `--color-surface-raised` is *consumed* (`index.css:647,962`) but never
   *declared* — an undeclared token that only resolves when a theme happens to
   emit it.
2. The **error family is not themeable.** `--color-error: #ffb4ab` is a static
   Material-3 light-mode pink hardcoded in the Tailwind `@theme` block
   (`index.css:4`) that the runtime injector never overrides, so error states
   render wrong in every dark theme. `--color-error-bg` / `--color-error-border`
   are referenced (`index.css:751-752`) but never declared at all.
3. **~40 Material-3 palette tokens** (`--color-background`, `--color-on-surface`,
   `--color-surface-container-*`, …) live in the `@theme` block as a parallel,
   untouchable namespace that bypasses the injector — contradicting the v1
   comment that calls `--color-*` "the SINGLE source of truth."
4. **No radii / spacing / shadow / typography-scale tokens** exist. Every theme
   shares identical geometry and type ramp.
5. **`schema_version` is informational only** (`validate.go:107-109`); unknown
   JSON fields are silently dropped. There is no version gate.
6. **No CI contrast gate.** The WCAG math exists (`contrast.go`) and is
   exercised in tests, but nothing in CI pins a minimum ratio for shipped themes.
7. The **flat `bg` + binary `chrome` surface model** is too coarse: only 2 of 11
   themes use `chrome`.
8. The **font registry is duplicated** between Go theme JSON and the frontend
   `fonts.ts`, and editor typography (sizes/line-heights) is entirely hardcoded.

v2 closes all eight. It is the *only* supported schema going forward.

## 1. ratified decisions

These are closed. Implementation issues implement against them; they are not
re-opened by downstream work.

| # | Decision | Rationale |
|---|---|---|
| D1 | **JSON format:** keep Silt's flat JSON; do **not** adopt the W3C DTCG `$value/$type` format verbatim. `schema_version: "2.0.0"`. | DTCG 2025.10 reached stable (Oct 2025) and is the interop standard, but per-token `$type` bloats hand-authored theme JSON and the codebase/issues all assume flat. Adopt DTCG conventions (`$deprecated`) only where they add value. DTCG remains a future interop option. |
| D2 | **OKLCH derivation:** eager, in Go (`derivation.go`). | Keeps the CI contrast gate and the editor seeing deterministic, testable values. CSS `oklch(from … calc())` relative-color syntax is supported in the Wails webview, but eager derivation keeps the gate/editor/CI agreement exact. Hex-authored themes still derive the same way (parsed to OKLCH internally). |
| D3 | **Frontend color library:** `culori`. | ~10 kB gzipped tree-shaken; full OKLCH + `contrastWCAG21`; function-oriented (tree-shakes well). Over `colorjs.io` (OOP-leaning) and `better-color-tools` (no contrast). |
| D4 | **`schema_version`:** `"2.0.0"`, hard-enforced. Unsupported versions rejected with a descriptive error. **No v1→v2 migration** (single-user project; first-party themes are re-authored natively). | See ADR `docs/decisions/0002-theme-schema-v2-no-migration.md`. |
| D5 | **Surface zones:** 9 — `app, sidebar, editor, panel, modal, popover, card, titlebar, activitybar`. `toast` reuses `popover`. | Matches the recon (flat surface-zone conventions) without a sprawling ~600-token set. |
| D6 | **Inheritance:** strict tree (linear fallback via `var()` chains): `popover→modal→panel→app`, `editor→app`, `sidebar→app`, `card→panel`, `titlebar→app`, `activitybar→app`. | Chosen over a DAG for predictability under user customization: a zone an author omits falls back to its parent; an author who sets it gets exactly what they wrote. |
| D7 | **Per-zone fields:** `bg`, `border`, `text` only. Hover/active stay zone-agnostic semantic interaction tokens. | Interaction state is the same gesture everywhere; only the canvas differs. |
| D8 | **Editor tokens:** top-level `editor` block on `Mode`, not nested under the `editor` surface zone. | Caret/selection/link/highlight are interaction elements, not surfaces. |
| D9 | **`radius`/`spacing`/`shadow`:** per-mode; small fixed sets — `radius` `{sm,md,lg,xl,full}`, `spacing` `{sm,md,lg,xl}`, `shadow` `{sm,md,lg}`. | Per-mode for consistency with the existing model; fixed sets keep authoring and the editor simple. |
| D10 | **Background storage:** hybrid — base64 data URI under ~50 KB (portable in JSON), `<theme-assets>/` relative path above (so full photos don't bloat `settings.json`). Embedded-asset name references reserved for first-party themes in v2. | Round-trips cleanly; single-file themes stay single-file. |
| D11 | **Background scope:** per-zone, building on D5/D6 (the `editor` canvas is the primary use case). | Per-zone is the expressive model; global is a degenerate per-app case. |
| D12 | **Contrast gate baseline:** WCAG **AA** (4.5:1 text, 3:1 UI) for every built-in theme; **AAA** (7:1) asserted only for Stark. Runtime auto-fix (Phase 2) is **on-click only**, never silent. | AA is the gate; AAA stays opt-in per theme. Warn never blocks a save. |

## 2. Token inventory

Every v2 token, its tier, type, and v1→v2 mapping. Tiers: **P**rimitive
(authored seed), **S**emantic (resolved meaning), **C**omponent (composed).
Types: `color` / `dimension` / `fontFamily` / `fontWeight` / `image`.

### 2.1 Identity (top-level, not per-mode)

| Field | v1→v2 | Notes |
|---|---|---|
| `schema_version` | `"1.0.0"` → **`"2.0.0"`** | Now hard-enforced (D4). |
| `id`, `name`, `author`, `description` | unchanged | |
| `typography` | extended | gains a `scale` sub-struct (§2.4). |

### 2.2 Surface zones (replaces v1 `bg` + `chrome` + `border` + `text`)

The flat `bg: {void,surface,panel,hover,active}`, `border`, `text`, and optional
`chrome` blocks are **removed**. Each of the 9 zones carries `{bg, border, text}`.
Each field is a single color (S tier, `color`). A zone an author omits falls
back to its parent per D6.

| Token (CSS) | JSON path (per `mode`) | tier/type | v1→v2 |
|---|---|---|---|
| `--color-surface-app` | `surfaces.app.bg` | S / color | ← `bg.void` |
| `--color-surface-app-border` | `surfaces.app.border` | S / color | ← `border.muted` |
| `--color-surface-app-text` | `surfaces.app.text` | S / color | ← `text.primary` |
| `--color-surface-sidebar` | `surfaces.sidebar.bg` | S / color | new (was ≈ `bg.surface`) |
| `--color-surface-sidebar-border` | `surfaces.sidebar.border` | S / color | new |
| `--color-surface-sidebar-text` | `surfaces.sidebar.text` | S / color | new |
| `--color-surface-editor` | `surfaces.editor.bg` | S / color | new |
| `--color-surface-editor-border` | `surfaces.editor.border` | S / color | new |
| `--color-surface-editor-text` | `surfaces.editor.text` | S / color | new |
| `--color-surface-panel` | `surfaces.panel.bg` | S / color | ← `bg.panel` |
| `--color-surface-panel-border` | `surfaces.panel.border` | S / color | ← `border.zinc` |
| `--color-surface-panel-text` | `surfaces.panel.text` | S / color | new |
| `--color-surface-modal` | `surfaces.modal.bg` | S / color | ← `bg.surface` |
| `--color-surface-modal-border` | `surfaces.modal.border` | S / color | new |
| `--color-surface-modal-text` | `surfaces.modal.text` | S / color | new |
| `--color-surface-popover` | `surfaces.popover.bg` | S / color | new (≈ `bg.surface`) |
| `--color-surface-popover-border` | `surfaces.popover.border` | S / color | new |
| `--color-surface-popover-text` | `surfaces.popover.text` | S / color | new |
| `--color-surface-card` | `surfaces.card.bg` | S / color | ← `bg.panel` |
| `--color-surface-card-border` | `surfaces.card.border` | S / color | new |
| `--color-surface-card-text` | `surfaces.card.text` | S / color | new |

**Inheritance emission (D6).** `Flatten` emits a concrete value when the author
set a zone, and a `var(--color-surface-<parent>)` reference when they omitted it
(so a theme switch repaints both in one cycle). The full chain resolves at
`:root`. `--color-surface-raised` is **removed**; raised surfaces are the
`modal`/`popover` zones.

**Interaction tokens (unchanged semantics, retained):** `--color-hover`,
`--color-active`, `--color-border-active`, `--color-border-focus` remain
zone-agnostic semantic tokens (S / color) at `mode.hover`, `mode.active`,
`mode.border_active`, `mode.border_focus`. They are no longer nested under `bg`.
**Text-emphasis tokens:** `--color-text-primary`, `--color-text-muted`,
`--color-text-disabled` are first-class zone-agnostic semantic emphasis levels
(primary / muted / disabled), parallel to the interaction tokens. `text-primary`
resolves to the app zone's foreground by definition (it *is* "primary body
text"); `text-muted`/`text-disabled` are authored per-mode.

**No Material-3 namespace survives.** The v1 parallel M3 palette
(`--color-background`, `--color-on-surface`, the `--color-surface-container-*`
family, `--color-error: #ffb4ab`, …) is removed entirely. Every UI color flows
through the v2 tokens above; there are no aliases onto the M3 names (a grep
confirmed zero consumers of the M3 names — keeping them would be dead code).

### 2.3 Semantic colors (accent / status / error)

| Token (CSS) | JSON path (per `mode`) | tier/type | v1→v2 |
|---|---|---|---|
| `--color-accent-primary-start/-end/-glow` | `accent.primary.{start,end,glow}` | S / color | unchanged |
| `--color-accent-secondary-start/-end/-glow` | `accent.secondary.{start,end,glow}` | S / color | unchanged |
| `--color-status-warn` | `status.warn` | S / color | unchanged |
| `--color-status-danger` | `status.danger` | S / color | unchanged |
| `--color-status-success` | `status.success` | S / color | **now required** (v1 optional path retired) |
| `--color-error` | `error.fg` | S / color | **new** (replaces static M3 `#ffb4ab`) |
| `--color-error-bg` | `error.bg` | S / color | **new** (was undeclared) |
| `--color-error-border` | `error.border` | S / color | **new** (was undeclared) |

`error.{fg,bg,border}` is the canonical themeable error family; every error-
state rule consumes these tokens (#386). `--color-status-danger` (semantic
"destructive action") and `--color-error` (semantic "this is wrong / invalid
input") are deliberately distinct: status-danger is for delete/remove actions,
error is for validation/inline errors.

### 2.4 Typography (extends v1 `typography`)

| Token (CSS) | JSON path | tier/type | v1→v2 |
|---|---|---|---|
| `--font-headline` / `--font-body` / `--font-mono` | `typography.headline_font` / `font_family` / `mono_font_family` | S / fontFamily | unchanged |
| `--font-size-xs/sm/base/lg/xl/2xl` | `typography.scale.size.{xs,sm,base,lg,xl,2xl}` | S / dimension | **new** |
| `--line-height-tight/normal/relaxed` | `typography.scale.line_height.{tight,normal,relaxed}` | S / dimension | **new** |
| `--font-weight-normal/medium/semibold` | `typography.scale.weight.{normal,medium,semibold}` | S / fontWeight | **new** |

The editor canvas and headings consume these (#389). The existing config-driven
`editor-tokens.svelte.ts` pipeline (`--editor-font-*`) remains for per-user
overrides that must exist before a vault opens; theme typography is the
baseline it overrides.

### 2.5 Geometry (new)

| Token (CSS) | JSON path (per `mode`) | tier/type |
|---|---|---|
| `--radius-sm/md/lg/xl/full` | `radius.{sm,md,lg,xl,full}` | S / dimension |
| `--spacing-sm/md/lg/xl` | `spacing.{sm,md,lg,xl}` | S / dimension |
| `--shadow-sm/md/lg` | `shadow.{sm,md,lg}` | C / composed |

`shadow.*` values are full CSS box-shadows that **reference theme colors** via
`color-mix(in oklch, var(--color-surface-app) 30%, transparent)` so shadows
respect the palette in both modes (#388). `Flatten` emits sensible defaults so a
theme that omits geometry renders identically to v1 until it opts in.

### 2.6 Editor interaction tokens (new)

| Token (CSS) | JSON path (per `mode`) | tier/type |
|---|---|---|
| `--color-editor-caret` | `editor.caret` | S / color |
| `--color-editor-selection` | `editor.selection` | S / color |
| `--color-editor-selection-text` | `editor.selection_text` | S / color |
| `--color-editor-link` | `editor.link` | S / color |
| `--color-editor-link-hover` | `editor.link_hover` | S / color |
| `--color-editor-highlight` | `editor.highlight` | S / color |

The editor canvas consumes these for `caret-color`, `::selection`, link color,
and the highlight marker (#390). The hardcoded `frontend/src/lib/editor/colors.ts`
palette (the inline text-color picker) is refactored to source theme-derived
defaults while the user's pick stays authoritative.

### 2.7 Unified background block (replaces v1 `texture`)

Per-zone `surfaces.<zone>.background` (D11). The legacy `texture` block is
**removed**; Linen's paper-grain migrates to `surfaces.editor.background` with
`size: "tile"` (#391).

| Field | Type | Notes |
|---|---|---|
| `image` | image ref | Reference resolver (§3) accepts: embedded-name (`$embedded:<name>`), relative path (`assets/foo.jpg`), or `data:image/…;base64,…`. |
| `size` | enum | `tile` (background-repeat) / `cover` / `contain`. `tile` is the texture mode; `cover`/`contain` are photo modes. |
| `opacity` | number 0–1 | overlay strength |
| `blend` | mix-blend-mode keyword | e.g. `overlay`, `multiply` |
| `position` | CSS position | e.g. `center`, `top left` |
| `scrim` | color | tint layered over the image to guarantee text legibility |

## 3. Reference resolver & asset storage

The `background.image` field accepts three shapes; `Flatten` resolves them to a
CSS `background-image` value:

1. **Embedded asset name** — `$embedded:linen-paper`. Resolves to a first-party
   asset baked into the binary. Reserved for first-party themes in v2.
2. **Relative path** — `assets/my-photo.jpg`. Resolved against a per-theme
   assets directory `<theme-id>.assets/` (or co-located). Used for files above
   the base64 threshold (D10).
3. **Data URI** — `data:image/jpeg;base64,…`. Used for files under ~50 KB; keeps
   a theme a single portable JSON file.

**Asset storage on pick (Phase 1 `PickBackgroundImage` binding, #391):** read
the file → if `≤ ~50 KB`, base64-encode inline; else copy to the theme's assets
directory with a stable filename → write the reference into the theme JSON →
invalidate the cache → emit `themes:changed`. **Size gate:** images above an RFC
max (proposed 8 MP / 4 MB) are rejected or auto-resized with a warning.

**Export round-trip (#391):** base64 themes export as one JSON file; assets-dir
themes export as JSON + the assets directory; re-import resolves both with no
broken references.

## 4. OKLCH strategy

**Dual format (D2).** `validate.go` accepts both:
- hex: `#rgb`, `#rrggbb`, `#rrggbbaa` (unchanged)
- OKLCH: `oklch(L C H)` and `oklch(L C H / A)`, where `L ∈ [0,1]`, `C ≥ 0`,
  `H ∈ [0,360]` (or unbounded — hue wraps).

`Flatten` emits the color **in the format it was authored** (hex stays hex;
OKLCH stays OKLCH) — no silent conversion. Hex-authored themes flatten
identically to v1 (no behavior change).

**Eager derivation (`derivation.go`).** Pure functions compute hover/active/
disabled variants from a seed color via OKLCH lightness/chroma shifts:
- `DeriveHover(seed)`: `L + ~0.06`, `C + ~0.02` (perceptibly lighter/brighter)
- `DeriveActive(seed)`: `L − ~0.04` (perceptibly deeper)
- `DeriveDisabled(seed)`: `C × ~0.4` (desaturated)

The Oklab forward/inverse math (sRGB↔Oklab) is ported from Ottosson's reference.
Output is deterministic; hex inputs are parsed to OKLCH internally, derived, and
re-emitted in the input format. The CI contrast gate and the editor see the same
derived values.

**Frontend helper (`frontend/src/theme/color.ts`).** A thin `culori` wrapper
(D3) exposing the same derivation for the (Phase 2) editor's live preview:
`parse`, `toOklch`, `toHex`, `deriveHover/Active/Disabled`, `contrastWCAG21`.

## 5. Surface-zone model

Nine zones (D5), each `{bg, border, text}` (D7). Inheritance is a strict tree
(D6) realized as `var()` fallback chains at `:root`:

```
app ────────────── the root canvas (always authored)
├── sidebar         (→ app if omitted)
├── editor          (→ app if omitted)
├── titlebar        (→ app if omitted)
├── activitybar     (→ app if omitted)
└── panel           (→ app if omitted)
    ├── card        (→ panel)
    └── modal       (→ panel)
        └── popover (→ modal)
```

A UI region maps to exactly one zone (#387 audit):

| Zone | UI region |
|---|---|
| `app` | the root background behind everything |
| `sidebar` | folders / notebook list |
| `editor` | the TipTap writing canvas |
| `panel` | docked panels, settings panes, the silt-tasks surfaces |
| `card` | cards, list items, callouts |
| `modal` | modal dialogs |
| `popover` | menus, dropdowns, tooltips, toasts |
| `titlebar` | top title bar / header |
| `activitybar` | leftmost vertical navigation strip |

The v1 `chrome` block is **removed**; its concerns (sidebar/titlebar/activitybar palette) are now mapped to separate `sidebar`, `titlebar`, and `activitybar` zones. Daybreak, Synthwave, and Bubblegum author dark chrome zones against a light `editor`/`app`.

## 6. Contrast guarantee

Two layers (#394):

**Build-time gate (`contrast_gate_test.go`).** Enumerates the critical semantic
pairs and asserts ratios for every embedded theme, both modes:
- zone text on zone bg — every zone (4.5:1)
- status/error fg on their zones (4.5:1 text, 3:0 UI)
- `border-focus` on zone bg (3:0 UI, WCAG 2.4.11/1.4.11)
- accent starts on `surface-app` (3:0 UI)
- text-on-image: effective background = image tinted by `scrim` at `opacity`
  (4.5:1), for any theme with a `background.image`
- **Stark asserted at AAA (7:1)** for primary text as a regression guard.

Fails the build on any violation. Runs in CI on every PR touching
`backend/themes/`.

**Runtime helper (`frontend/src/theme/contrast.ts`, wrapping
`contrastRatioWCAG` in `color.ts`).** Pure WCAG ratio over OKLCH. The custom
editor consults it for an inline pass/warn/fail indicator and an on-click
auto-fix that clamps the foreground's OKLCH lightness to the nearest passing
value. Warn never blocks a save (D12).

## 7. Editor UX contract (Phase 2 target — documented, not implemented here)

Milestone #35 fences *"No custom editor yet (presets only)."* This section is
the **contract** the custom-editor milestone (#36) implements against; no editor
UI ships in Phase 1.

- **Editable scope:** semantic tokens only (surfaces, accent, status, error,
  editor, typography scale, geometry, per-zone background). Primitive tokens are
  hidden.
- **Per-token reset:** each editable token reverts to the active theme's
  authored value (and derived tokens re-derive from their seed).
- **Save-as-new-theme:** the editor's working copy persists as a new on-disk
  theme (#393); the active preset is never mutated in place.
- **Live preview:** editing the working copy re-flattens and calls
  `injectTokens` — same-frame, no reload. A working-copy model (not in-place
  mutation) keeps the preview reversible.
- **Image picker flow:** per-zone `background` picker (#401) → file picker →
  asset storage (§3) → live preview → contrast safeguard warns on text-on-image
  below AA.
- **Contrast feedback:** per-token inline indicator + aggregated non-blocking
  report; on-click auto-fix clamps OKLCH lightness (#402). Informational, never
  blocking.
- **Progressive disclosure:** a simple default view (a few high-impact controls)
  with controlled escalation to advanced per-zone/per-token editing. Defined by
  the UX spec (#398).

## 8. Non-Goals

- **The custom theme editor UI** itself (Phase 2, milestone #36). This RFC
  documents the contract (§7) only.
- **A v1→v2 migration path.** Single-user project; first-party themes are
  re-authored natively (D4, ADR 0002).
- **Theme marketplace / sharing infrastructure.** Deferred.
- **OS-aware auto-switching beyond `prefers-color-scheme`.** Deferred.
- **Per-page cover images.** Themes are user-global per the
  existing `settings.json` decision.
- **Syntax-highlighting tokens.** Silt is a notes app, not a code editor.
- **APCA.** WCAG AA is the v2 standard (D12); APCA is a possible follow-up.
- **Animated backgrounds.**

## 9. Phase split (recap)

| Issue | Phase 1 (#35, this milestone) | Phase 2 (#36) |
|---|---|---|
| #391 | `background` schema, `texture` removal, Linen migration, `PickBackgroundImage` binding, asset pipeline, export | editor image-picker UI → **#401** |
| #394 | `contrast_gate_test.go` + `contrast.ts` helper | editor indicator + auto-fix UI → **#402** |
| #387–#390 | schema + Flatten + CSS consumption | editor token-family controls → **#392** |

The engine, the contrast guarantee, and the refreshed theme set ship in Phase 1.
The editor surfaces ship in Phase 2 against this RFC and the UX spec (#398).
