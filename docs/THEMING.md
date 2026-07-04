# Theming Silt

Silt's entire visual surface — backgrounds, borders, text, accents, status colors, and optionally fonts — is driven by a single **theme**. Themes are plain JSON files. You can write one in any text editor, drop it into your vault, and select it from **Settings → Appearance**. No restart, no recompile.

> **Engineering docs.** This is the *end-user* guide. For the internal pipeline (Go loader → Wails IPC → Svelte store → `:root` injection), see [`ARCHITECTURE.md` §4.4](../ARCHITECTURE.md). For the product spec, see [`SPECS.md` §6.4](../SPECS.md). For the design-system token vision, see [`DESIGN.md` §2.1](../DESIGN.md). This document is the authoritative authoring reference; the schema table below mirrors the Go validator (`backend/themes/validate.go`) and is kept in sync by hand (see the note under the table).

---

## 1. Concepts

### Theme vs. mode

- A **theme** is a palette *family* — a complete set of colors (and optionally fonts) with a name like *Cyber Forest*. Each theme file defines **two modes** in one file:
  - **`dark`** — the colors used in dark appearance.
  - **`light`** — the colors used in light appearance.
- A **mode** is which of the two palettes is currently active: **Dark**, **Light**, or **System** (follows your OS preference). Switching mode **never** changes the active theme; it only selects which of the theme's two palettes is rendered.

Both modes are **required** in every theme file. A theme with only a dark mode is invalid and will be rejected on import.

### Surface zones (the canvas model)

Silt's UI is divided into 7 named **surface zones** — `app`, `sidebar`,
`editor`, `panel`, `card`, `modal`, `popover` — and each zone carries its own
`bg` / `border` / `text`. Only **`app`** (the root canvas behind everything) is
required; the others are optional and **inherit** from a parent zone when you
leave them out:

```
app          the root (always authored)
├─ sidebar     → app      (navigation sidebar, activity bar)
├─ editor      → app      (the TipTap writing canvas)
├─ panel       → app      (docked panels, settings, kanban/calendar)
│  ├─ card       → panel    (cards, list items, callouts)
│  └─ modal      → panel    (modal dialogs)
│     └─ popover   → modal    (menus, dropdowns, tooltips, toasts)
```

So a minimal theme authors just `app` and every other surface follows. A theme
that wants one canvas to differ — a dark **`sidebar`** against a light
`editor`/`app` (the Daybreak / Bubblegum light "readability exception") —
authors that one zone and the rest keep inheriting. Interaction state
(hover/active/focus) and accents are **not** per-zone: the same gesture reads
the same way on every surface. See §2 for the full token reference and §3 for an
annotated example.

### Semantic accents (the key idea)

Silt components never reference a concrete hue like "teal" or "indigo". Instead they reference two **semantic** accent slots:

| Semantic slot | Meaning | Used for |
| :--- | :--- | :--- |
| `accent.primary` | **"go / done"** | Active selection, completed tasks, primary buttons, focus rings, guide-rail highlights. |
| `accent.secondary` | **"in progress"** | In-progress states, the "doing" lane, secondary highlights. |

Each theme decides which concrete colors map onto `primary` and `secondary`. Cyber Forest maps teal → primary and indigo → secondary. Your theme can map *any* two hues onto them. This is what lets every theme restyle the whole app without per-theme code.

Each accent is a **triple**: `start` / `end` (a gradient pair) plus `glow` (a translucent version used for soft halos).

### First-class themes

| Theme | Status | Description |
| :--- | :--- | :--- |
| **Cyber Forest** *(the default, "Refined Cyber-Ink")* | Shipped | Ink-rich dark slate canvas, surgical teal primary, indigo secondary. Embedded in the app as the guaranteed fallback. |
| Terra Noir | Shipped | Warm dark earth palette: clay primary, moss secondary. |
| Linen | Shipped | Clean, easy-on-the-eyes paper palette: desaturated slate-blue + muted lilac. Includes a woven paper-grain `background` overlay on the editor zone (see §2 `background`). |
| Stark | Shipped | High-contrast / accessibility (WCAG AAA): pure black/white extremes, gold + cyan. |
| Graphite | Shipped | Calm monochrome dark: cool near-blacks, a single restrained blue accent. |
| Bubblegum | Shipped | Playful and vibrant: coral-pink primary, teal secondary. Light mode uses a deep raspberry sidebar + warm cream page. |
| Frost | Shipped | Clean and airy: crisp blue-tinted winter palette with sky-blue accents. |
| Synthwave | Shipped | 80s retro neon: deep indigo canvas with hot-pink and electric-cyan accents. |
| Daybreak | Shipped | Twilight-blue sidebar + warm off-white page: the readability-exception theme. |
| Aggie | Shipped | Heritage split: dark mode evokes Colorado A&M's alfalfa green + pumpkin orange; light mode shifts to CSU's modern green + gold. |
| Altgeld | Shipped | Illinois Blue canvas with Illini Orange primary — prairie-fire energy. |

> First-class themes are bundled and always selectable. The schema and everything in this guide applies equally to first-class and user-authored themes.

---

## 2. Token schema reference

Every theme is a JSON object. The tables below list **every token the validator requires** plus the optional blocks. The "JSON path" is relative to a `mode` object (e.g. `modes.dark.surfaces.app.bg`) unless the table says otherwise.

> This table mirrors the validator in `backend/themes/validate.go`. The two are kept in sync by hand: when you add a token to the schema, add a row here **and** an entry there. (There is no automated coupling today — the doc is the author-facing reference, the Go validator is the enforcement.)
>
> **These are the ONLY `--color-*` custom properties the theme engine emits.** If you are authoring a Svelte component or CSS rule, use only the CSS variables listed below. A reference to a token not in this table will silently fall back to its hardcoded default — which is always dark-mode-tuned and will render invisible in light themes. The complete emitted color set: the surface-zone tokens (`--color-surface-<zone>`, `--color-surface-<zone>-border`, `--color-surface-<zone>-text` for each of the 7 zones), `--color-hover`, `--color-active`, `--color-border-active`, `--color-border-focus`, `--color-text-primary`, `--color-text-muted`, `--color-text-disabled`, the accents (`--color-accent-primary/-secondary-{start,end,glow}`), the status colors (`--color-status-{warn,danger,success}`), the themeable error family (`--color-error`, `--color-error-bg`, `--color-error-border`), and the editor interaction tokens (`--color-editor-{caret,selection,selection-text,link,link-hover,highlight}`). Per-zone background overlays emit `--silt-bg-<zone>-*` (non-color) tokens.

### Identity (top-level, not per-mode)

| Field | Required | Meaning |
| :--- | :--- | :--- |
| `schema_version` | yes | **`"2.0.0"` (required).** Hard-enforced — any other value is rejected on import. v1 themes are not migrated; re-author them as v2 (see ADR [`0002`](../docs/decisions/0002-theme-schema-v2-no-migration.md)). |
| `id` | yes | Unique identifier, lowercase `[a-z0-9_-]`. Used as the filename on disk and the picker key. |
| `name` | yes | Human-readable display name. |
| `author` | optional | Author credit. |
| `description` | optional | One-line description shown in the picker. |

### `surfaces` — the 7 named zones (per-mode)

Each zone is `{bg, border, text}`. Only **`surfaces.app`** is required; every other zone is optional and inherits from its parent when you omit it (see §1 for the inheritance tree).

| JSON path | CSS variable | Required | Meaning |
| :--- | :--- | :--- | :--- |
| `surfaces.app.{bg,border,text}` | `--color-surface-app` / `-app-border` / `-app-text` | yes | The root canvas behind everything. `bg` also seeds the native window background (the pre-CSS paint color). |
| `surfaces.sidebar.*` | `--color-surface-sidebar{,-border,-text}` | no | Navigation sidebar, activity bar. Inherits from `app`. |
| `surfaces.editor.*` | `--color-surface-editor{,-border,-text}` | no | The TipTap writing canvas. Inherits from `app`. |
| `surfaces.panel.*` | `--color-surface-panel{,-border,-text}` | no | Docked panels, settings panes, kanban/calendar. Inherits from `app`. |
| `surfaces.card.*` | `--color-surface-card{,-border,-text}` | no | Cards, list items, callouts. Inherits from `panel`. |
| `surfaces.modal.*` | `--color-surface-modal{,-border,-text}` | no | Modal dialogs. Inherits from `panel`. |
| `surfaces.popover.*` | `--color-surface-popover{,-border,-text}` | no | Menus, dropdowns, tooltips, toasts. Inherits from `modal`. |

When you omit a zone, the engine emits `var(--color-surface-<parent>)` fallbacks, so the zone always resolves and a theme switch repaints every surface in one cycle. Author a zone only when you want it to differ from its parent. `--color-text-primary` (body copy, highest-contrast text) is not authored directly — it is an alias for `surfaces.app.text`.

### Interaction & emphasis tokens (per-mode, zone-agnostic)

These apply on **every** surface — the same gesture reads the same way everywhere.

| JSON path | CSS variable | Required | Meaning |
| :--- | :--- | :--- | :--- |
| `hover` | `--color-hover` | yes | Hovered-row background. |
| `active` | `--color-active` | yes | Pressed / active-row background. |
| `border_active` | `--color-border-active` | yes | Emphasized border (hovered). |
| `border_focus` | `--color-border-focus` | yes | Focus-trace border. |
| `text_muted` | `--color-text-muted` | yes | Metadata, labels, secondary text. |
| `text_disabled` | `--color-text-disabled` | yes | Disabled / struck-through text. |

### `accent` — semantic accents (×2 triples, per-mode)

| JSON path | CSS variable | Meaning |
| :--- | :--- | :--- |
| `accent.primary.start` | `--color-accent-primary-start` | "go/done" gradient start. |
| `accent.primary.end` | `--color-accent-primary-end` | "go/done" gradient end. |
| `accent.primary.glow` | `--color-accent-primary-glow` | "go/done" soft halo (usually `rgba(...)`). |
| `accent.secondary.start` | `--color-accent-secondary-start` | "in-progress" gradient start. |
| `accent.secondary.end` | `--color-accent-secondary-end` | "in-progress" gradient end. |
| `accent.secondary.glow` | `--color-accent-secondary-glow` | "in-progress" soft halo. |

### `status` — warn / danger / success (per-mode)

| JSON path | CSS variable | Required | Meaning |
| :--- | :--- | :--- | :--- |
| `status.warn` | `--color-status-warn` | yes | Warnings. |
| `status.danger` | `--color-status-danger` | yes | Destructive actions (delete, remove). |
| `status.success` | `--color-status-success` | yes | Success / confirmed. (Required in v2 — the v1 optional fallback is retired.) |

### `error` — themeable error family (per-mode)

The themeable replacement for the static Material-3 error pink that used to render wrong in every dark theme. `status.danger` (destructive actions) and `error.fg` (validation / invalid input) are deliberately distinct.

| JSON path | CSS variable | Meaning |
| :--- | :--- | :--- |
| `error.fg` | `--color-error` | Inline / validation error text. |
| `error.bg` | `--color-error-bg` | Error-state background fill. |
| `error.border` | `--color-error-border` | Error-state border. |

### `radius` / `spacing` / `shadow` (optional, per-mode)

Optional geometry ramps. Omit them entirely and the engine emits v1-equivalent defaults. `shadow.*` values are full CSS box-shadows — they typically reference theme colors via `color-mix(in oklch, var(--color-surface-app) 30%, transparent)` so they read correctly in both modes.

| JSON path | CSS variable | Keys |
| :--- | :--- | :--- |
| `radius.{sm,md,lg,xl,full}` | `--radius-{sm,md,lg,xl,full}` | corner-radius ramp |
| `spacing.{sm,md,lg,xl}` | `--spacing-{sm,md,lg,xl}` | spatial-rhythm ramp |
| `shadow.{sm,md,lg}` | `--shadow-{sm,md,lg}` | elevation ramp (full box-shadow) |

### `editor` — editor-canvas interaction (optional, per-mode)

Caret, selection, link, and highlight colors for the writing canvas. These are interaction elements, not surfaces, so the block is top-level on the mode (not nested under the `editor` zone). Omit it and the engine derives sensible defaults from your accents.

| JSON path | CSS variable | Meaning |
| :--- | :--- | :--- |
| `editor.caret` | `--color-editor-caret` | The text caret. |
| `editor.selection` | `--color-editor-selection` | `::selection` fill. |
| `editor.selection_text` | `--color-editor-selection-text` | `::selection` text. |
| `editor.link` | `--color-editor-link` | Link color. |
| `editor.link_hover` | `--color-editor-link-hover` | Hovered link. |
| `editor.highlight` | `--color-editor-highlight` | The highlight marker. |

### `background` — per-zone surface overlay (optional, per-zone)

A unified per-zone overlay that replaces the v1 `texture` block. A "texture" is a `background` with `size: "tile"` + low opacity; a "background photo" is `size: "cover"` with a scrim. Declare it nested under any zone: `surfaces.<zone>.background`. Zones without a `background` block pay zero compositing cost.

| JSON path | CSS variable | Required | Meaning |
| :--- | :--- | :--- | :--- |
| `background.image` | `--silt-bg-<zone>-image` | yes | CSS `background-image`: one or more `repeating-linear-gradient(...)` / `url(...)` layers (inline SVG data URLs for noise), an embedded asset name, a relative path, or a `data:` URI. |
| `background.size` | `--silt-bg-<zone>-size` | no | `tile` (repeat) / `cover` / `contain`. |
| `background.opacity` | `--silt-bg-<zone>-opacity` | no | Overlay strength, `0`–`1`. |
| `background.blend` | `--silt-bg-<zone>-blend` | no | CSS `mix-blend-mode` keyword. |
| `background.position` | `--silt-bg-<zone>-position` | no | CSS `background-position`. |
| `background.scrim` | `--silt-bg-<zone>-scrim` | no | Tint color layered over the image to guarantee text legibility. |

**Sandbox:** the `image` and `position` values flow verbatim into CSS, so the validator rejects any value containing `;`, `{`, `}`, `<`, `>`, or `\` — preventing breakout from the `:root{--name:value;}` injection context. Use URL-encoded inline SVG data URLs (`url("data:image/svg+xml,%3Csvg...")`) for noise patterns.

**Authoring tips:** keep `opacity` low (`0.04`–`0.12`) for a subtle grain; use `multiply`/`overlay` for dark grain on a light canvas, `screen`/`soft-light` for light grain on a dark canvas. The SVG `feTurbulence` filter generates fractal noise — pair it with `feColorMatrix type="saturate" values="0"` for a desaturated grain that adapts to any palette (see Linen's definition for a reference weave).

### `typography` (optional, theme-level — not per-mode)

When absent, the theme inherits fonts from your [`config.yaml`](../SPECS.md) `editor.*` settings. When present, each non-empty field overrides the config font via a CSS fallback chain. The optional `scale` sub-object sets the type ramp.

| JSON path | CSS variable | Meaning |
| :--- | :--- | :--- |
| `typography.font_family` | `--font-body` | Body / proportional font stack. |
| `typography.mono_font_family` | `--font-mono` | Monospace font stack. |
| `typography.headline_font` | `--font-headline` | Heading font stack. |
| `typography.scale.size.{xs,sm,base,lg,xl,2xl}` | `--font-size-*` | Type sizes (CSS dimensions). |
| `typography.scale.line_height.{tight,normal,relaxed}` | `--line-height-*` | Line heights (unitless). |
| `typography.scale.weight.{normal,medium,semibold}` | `--font-weight-*` | Weights (unitless). |

### Accepted value formats

- **Colors** (every surface / interaction / accent / status / error / editor slot): `#hex` (`#rgb`, `#rrggbb`, `#rrggbbaa`), `rgb(r, g, b)`, `rgba(r, g, b, a)`, or **`oklch(L C H)` / `oklch(L C H / A)`**. Anything else — named colors (`red`), `hsl()`, `url()` at a color slot, `expression()`, `<script>` — is **rejected at validation time**. This is the security sandbox: a user-imported theme can never smuggle executable CSS.
- **Font-family** (`typography.*`): any CSS font-family string (e.g. `'Inter', sans-serif`). Rejected if it contains `;`, `{`, `}`, `<`, `>`, or `\`.
- **Geometry** (`radius` / `spacing` / `shadow`): CSS dimensions (`4px`, `0.5rem`) for radius/spacing; full box-shadow strings for `shadow`.

#### OKLCH (new in v2)

You may author any color as `oklch(L C H)` — lightness `L ∈ [0,1]`, chroma `C ≥ 0`, hue `H` in degrees — or `oklch(L C H / A)` with alpha. OKLCH is **perceptually uniform**: the same lightness step reads as the same brightness shift on every hue, which is why derived hover/active/disabled variants stay balanced. The engine emits an OKLCH value **verbatim** (it does not silently convert it to hex) and parses it internally for the contrast gate. Hex-authored themes keep working exactly as before — reach for OKLCH when you want perceptual uniformity.

```
"accent": {
  "primary": { "start": "oklch(0.7 0.15 180)", "end": "oklch(0.55 0.13 185)", "glow": "oklch(0.7 0.15 180 / 0.15)" }
}
```

---

## 3. Authoring a theme

### A minimal valid theme

The smallest theme that passes validation — both modes, only the required tokens. It authors just the `app` zone; every other surface inherits from it. Copy this, change the `id`/`name`, and swap colors:

```json
{
  "schema_version": "2.0.0",
  "id": "my-theme",
  "name": "My Theme",
  "modes": {
    "dark": {
      "surfaces": {
        "app": { "bg": "#0c0c0e", "border": "#1e1e23", "text": "#dee3e6" }
      },
      "hover": "#1c1c21", "active": "#222226",
      "border_active": "#3f3f46", "border_focus": "#52525b",
      "text_muted": "#8b8b94", "text_disabled": "#4b5563",
      "accent": {
        "primary":   { "start": "#2dd4bf", "end": "#0d9488", "glow": "rgba(20,184,166,0.15)" },
        "secondary": { "start": "#6366f1", "end": "#a855f7", "glow": "rgba(168,85,247,0.12)" }
      },
      "status": { "warn": "#fbbf24", "danger": "#f43f5e", "success": "#22c55e" },
      "error":  { "fg": "#f43f5e", "bg": "#121215", "border": "#3f3f46" }
    },
    "light": {
      "surfaces": {
        "app": { "bg": "#f8fafc", "border": "#e2e8f0", "text": "#0f172a" }
      },
      "hover": "#e2e8f0", "active": "#cbd5e1",
      "border_active": "#94a3b8", "border_focus": "#64748b",
      "text_muted": "#4d5667", "text_disabled": "#94a3b8",
      "accent": {
        "primary":   { "start": "#0d9488", "end": "#115e59", "glow": "rgba(13,148,136,0.10)" },
        "secondary": { "start": "#4f46e5", "end": "#7c3aed", "glow": "rgba(79,70,229,0.08)" }
      },
      "status": { "warn": "#d97706", "danger": "#e11d48", "success": "#16a34a" },
      "error":  { "fg": "#e11d48", "bg": "#ffffff", "border": "#94a3b8" }
    }
  }
}
```

### A full annotated theme (typography, zones, editor, background, geometry)

```json
{
  "schema_version": "2.0.0",                  // required; must be exactly "2.0.0"
  "id": "ocean-dusk",                         // required; lowercase [a-z0-9_-]; becomes the filename
  "name": "Ocean Dusk",                       // required; shown in the picker
  "author": "Your Name",                      // optional
  "description": "Deep blue with a coral primary.",  // optional
  "typography": {                             // optional; theme-level (not per-mode)
    "font_family": "'Inter', sans-serif",
    "mono_font_family": "'JetBrains Mono', monospace",
    "headline_font": "'Hanken Grotesk', sans-serif",
    "scale": {                                // optional type ramp
      "size": { "base": "15px", "lg": "18px", "xl": "24px" },
      "line_height": { "normal": "1.6" },
      "weight": { "medium": "500", "semibold": "600" }
    }
  },
  "modes": {
    "dark": {
      "surfaces": {
        "app":     { "bg": "#0a0f1a", "border": "#161f30", "text": "#e6ecf5" },
        "sidebar": { "bg": "#0f1626", "border": "#1f2b40", "text": "#e6ecf5" },
        "editor":  { "bg": "#0a0f1a", "border": "#161f30", "text": "#e6ecf5",
                     "background": {                     // optional per-zone overlay
                       "image": "url(\"data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='140' height='140'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='2' stitchTiles='stitch'/%3E%3CfeColorMatrix type='saturate' values='0'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E\")",
                       "size": "tile", "opacity": 0.05, "blend": "screen"
                     } },
        "panel":   { "bg": "#141d30", "border": "#1f2b40", "text": "#e6ecf5" }
      },
      "hover": "#1a2438", "active": "#222d44",
      "border_active": "#2f3f5a", "border_focus": "#3f5275",
      "text_muted": "#7e8aa0", "text_disabled": "#4a5468",
      "accent": {
        "primary":   { "start": "#fb7185", "end": "#e11d48", "glow": "rgba(244,63,94,0.15)" },
        "secondary": { "start": "#38bdf8", "end": "#0ea5e9", "glow": "rgba(56,189,248,0.12)" }
      },
      "status": { "warn": "#fbbf24", "danger": "#f43f5e", "success": "#22c55e" },
      "error":  { "fg": "#f43f5e", "bg": "#0f1626", "border": "#2f3f5a" },
      "radius": { "sm": "4px", "md": "8px", "lg": "12px", "xl": "16px", "full": "9999px" },
      "shadow": { "sm": "0 1px 2px color-mix(in oklch, #0a0f1a 40%, transparent)" },
      "editor": { "caret": "#fb7185", "selection": "rgba(251,113,133,0.25)", "selection_text": "#e6ecf5",
                  "link": "#38bdf8", "link_hover": "#0ea5e9", "highlight": "rgba(251,191,36,0.35)" }
    },
    "light": {
      "surfaces": {
        "app":   { "bg": "#f1f5f9", "border": "#dde6f0", "text": "#0f172a" },
        "panel": { "bg": "#ffffff", "border": "#cbd5e1", "text": "#0f172a" }
      },
      "hover": "#dde6f0", "active": "#cbd5e1",
      "border_active": "#94a3b8", "border_focus": "#64748b",
      "text_muted": "#475569", "text_disabled": "#94a3b8",
      "accent": {
        "primary":   { "start": "#e11d48", "end": "#9f1239", "glow": "rgba(225,29,72,0.10)" },
        "secondary": { "start": "#0284c7", "end": "#075985", "glow": "rgba(2,132,199,0.08)" }
      },
      "status": { "warn": "#d97706", "danger": "#be123c", "success": "#16a34a" },
      "error":  { "fg": "#e11d48", "bg": "#ffffff", "border": "#94a3b8" }
    }
  }
}
```

The dark mode authors four zones (the `editor` carries a `background` overlay); the light mode authors only `app` and `panel`, so `sidebar`/`editor`/`card`/`modal`/`popover` all inherit.

---

## 4. Choosing & mapping accents

1. **Pick two hues.** One for the "go/done" primary, one for the "in-progress" secondary. They should be visually distinct so the two states never blur together.
2. **Give each a gradient pair (`start` → `end`).** `start` is the brighter/lighter end; `end` is the deeper end. Components draw `linear-gradient(to bottom right, start, end)`.
3. **Make a matching `glow`.** The glow is the same hue at low alpha (≈0.08–0.15), used for soft halos behind active elements. Use `rgba(...)` (or `oklch(... / 0.12)`) so you can control transparency.
4. **Mind the mode.** In light mode, accent `start`s are usually *deeper* (so they stay readable on white); in dark mode, *brighter* (so they glow on dark). Compare Cyber Forest's dark `#2dd4bf` vs light `#0d9488` for the same primary.
5. **`primary` should pass AA (4.5:1) against `surfaces.app.bg`.** See accessibility below. Stark is the exception — it targets AAA (7:1) as a theme-specific design requirement.

---

## 5. Accessibility & contrast

Silt targets **WCAG 2.2**. Your theme is checked against the shipped palette; aim for:

| Element | Minimum ratio | Level |
| :--- | :--- | :--- |
| Each zone's `text` on its own `bg` (every authored zone) | **≥ 4.5:1** | AA |
| `text_muted` / `text_disabled` on zone backgrounds | **≥ 4.5:1** | AA |
| `accent.primary.start` / `accent.secondary.start` on `surfaces.app.bg` (non-text UI) | **≥ 3:1** | AA (non-text) |

### How to verify

- **In-repo harness:** `backend/themes/contrast.go` computes WCAG contrast ratios (`ContrastRatio(a, b)`); `backend/themes/contrast_test.go` is the CI gate that asserts the thresholds above for every shipped first-class theme, both modes. Drop your theme's colors into a quick ratio check using the same formula.
- **Browser devtools:** inspect any text element → computed color → the accessibility panel reports the contrast ratio against its background.
- **Online tools:** [WebAIM Contrast Checker](https://webaim.org/resources/contrastchecker/), [APCA Contrast Calculator](https://www.myndex.com/APCA/).

> A theme that fails contrast will still *import* (validation checks structure/format, not perceptual contrast) but will be hard to read. The contrast tests guard the **shipped** first-class themes; treat the thresholds above as your authoring target.

---

## 6. Importing a theme

You have a valid `my-theme.json`. There are three ways to add it; all end with the theme appearing in **Settings → Appearance** immediately (no restart):

### Option A — the picker Import button
1. Open **Settings → Appearance**.
2. Click **Import .json**.
3. Select your `.json` in the native file dialog.

### Option B — drag & drop
Drag the `.json` file from your file manager onto the **Appearance** tab. It imports through the same validated path.

### Option C — drop it into the vault
Copy the file directly into `<your-vault>/.system/themes/`. It is enumerated the next time the picker loads (the listing re-fetches on the `themes:changed` event).

### What the importer does for you
- **Validates** the file against the canonical v2 schema. `schema_version` must be exactly `"2.0.0"` — a v1 file (or any other version) is rejected with a clear error naming the field. If a token is missing or a color is malformed, **nothing is written** and the error names the offending field and the expected format.
- **Sanitizes the `id`** to lowercase `[a-z0-9_-]` so it is filename-safe on every platform (underscores preserved).
- **Namespaces collisions:**
  - If your `id` collides with a built-in (e.g. `cyber_forest`), it is renamed to `user-cyber_forest` so the bundled default is never overwritten.
  - If `user-cyber_forest` already exists too, a counter is appended (`user-cyber_forest-2`, …).
  - If your `id` already exists as a *different* on-disk theme, the import is **rejected** (rename the `id` in your JSON and try again) — Silt never silently overwrites a different theme.
- **Sandbox:** because the schema only accepts `#hex` / `rgb()` / `rgba()` / `oklch()` at color slots and font-family / background-image strings are stripped of CSS-breaking characters, a hostile theme cannot smuggle `<script>`, `url()` at a color slot, or `expression()` past validation.

### Exporting (for round-trip editing)
Click **Export active** in **Settings → Appearance** to save the currently-active theme (or the embedded default) to a `.json` you can edit and re-import. This is the fastest way to start a new theme: export the default, tweak the colors, change the `id`, and import.

---

## 7. Selecting a theme & mode

1. Open **Settings → Appearance** (the gear icon in the titlebar, or Settings in the sidebar footer).
2. **Theme list:** click a row (or focus it with the keyboard and press Enter/Space) to make it active. The whole shell repaints in one frame — no reload.
3. **Mode toggle:** Dark / Light / System. System follows your OS appearance preference live. Changing the mode **does not** change the active theme.
4. **Live preview:** hover (or keyboard-focus) a non-active row to preview it without committing; move away or press Esc to restore the active theme.
5. **Persistence:** your selection and mode are saved to your user-global `settings.json` and restored on the next launch.

### Keyboard shortcuts (picker)
- **Tab** into the picker; **Arrow ↑/↓/←/→** move focus between rows.
- **Home / End** jump to the first / last row.
- **Enter / Space** select the focused row.
- **Esc** cancel any live preview.

---

## 8. Troubleshooting

| Symptom | Cause & fix |
| :--- | :--- |
| **"schema_version … is not supported"** | The file's `schema_version` is not exactly `"2.0.0"` (often a v1 theme). v2 is the only supported schema and there is no migration — re-author the file as v2 (see §2 / §9). |
| **"token is missing"** on import | A required token (see §2) is empty or absent in one of the modes. The error names the field, e.g. `modes.light.error.fg`. Fill it in both `dark` and `light`. |
| **"not a valid color: …"** | A color slot holds a value that isn't `#hex`/`rgb()`/`rgba()`/`oklch()` — e.g. a named color (`red`), `hsl()`, or a typo. Use hex or oklch. |
| **"theme id already exists"** | A theme with the same `id` is already on disk (a *different* theme, not a built-in). Change the `id` in your JSON and re-import. |
| **Imported as `user-<id>` (renamed)** | Your `id` collided with a built-in (`cyber_forest`). The importer namespaced it for you; the status line says "Imported as user-cyber_forest (renamed from cyber_forest)". |
| **"id … is invalid after sanitization"** | Your `id` consisted entirely of invalid characters. Use lowercase letters, digits, hyphens, and underscores. |
| **Theme not appearing in the list** | (a) The file isn't a `.json` in `<vault>/.system/themes/`. (b) It failed validation — check the load errors surfaced in the picker. (c) You're looking before the `themes:changed` event fired — reopen Settings. |
| **Typography fonts not applying** | The `typography` section is optional and theme-level. If you omitted it, the config `editor.*` fonts remain in effect. If you set a field but see no change, confirm the font is installed on your system (themes reference fonts by name; they don't bundle them). |
| **Background overlay not visible** | The `background` block is optional and per-zone. Check that `opacity` is non-zero and `size` is right (`tile` repeats; `cover`/`contain` are for photos). The overlay renders only on the zone that declares it. Confirm `blend` is appropriate for your palette (`multiply`/`overlay` for light modes, `screen`/`soft-light` for dark). |
| **"not a safe background.image value"** | The `background.image` (or `position`) field must not contain `;`, `{`, `}`, `<`, `>`, or `\`. Use URL-encoded inline SVG data URLs (`url("data:image/svg+xml,%3Csvg...")`) for noise patterns. |
| **"background.opacity must be a number in [0,1]"** | `opacity` is a decimal between `0` and `1` (e.g. `0.06`). Values outside that range are rejected. |
| **"background.blend … is not a recognized mix-blend-mode"** | `blend` must be one of the standard CSS `mix-blend-mode` keywords. Custom strings are rejected. |
| **First-paint flash of the wrong color on restart** | The native window background is seeded from the active theme's `surfaces.app.bg` at launch via an mtime-aware cache. If you hand-edited the on-disk file, touch its mtime or re-import so the cache refreshes. |

---

## 9. Appendix: blank theme template

Copy-paste this and fill in the `…` placeholders. Both modes are required; everything under `surfaces` beyond `app`, and the `radius` / `shadow` / `editor` / `background` / `typography.scale` blocks, is optional (delete what you don't need).

```json
{
  "schema_version": "2.0.0",
  "id": "your-theme-id",
  "name": "Your Theme Name",
  "author": "Your Name",
  "description": "A short description.",
  "typography": {
    "font_family": "'Inter', sans-serif",
    "mono_font_family": "'JetBrains Mono', monospace",
    "headline_font": "'Hanken Grotesk', sans-serif"
  },
  "modes": {
    "dark": {
      "surfaces": {
        "app":     { "bg": "#………", "border": "#………", "text": "#………" },
        "sidebar": { "bg": "#………", "border": "#………", "text": "#………" },
        "editor":  { "bg": "#………", "border": "#………", "text": "#………",
                     "background": { "image": "…", "size": "tile", "opacity": 0.06, "blend": "overlay" } },
        "panel":   { "bg": "#………", "border": "#………", "text": "#………" }
      },
      "hover": "#………", "active": "#………",
      "border_active": "#………", "border_focus": "#………",
      "text_muted": "#………", "text_disabled": "#………",
      "accent": {
        "primary":   { "start": "#………", "end": "#………", "glow": "rgba(…,…,…,0.15)" },
        "secondary": { "start": "#………", "end": "#………", "glow": "rgba(…,…,…,0.12)" }
      },
      "status": { "warn": "#………", "danger": "#………", "success": "#………" },
      "error":  { "fg": "#………", "bg": "#………", "border": "#………" },
      "radius": { "sm": "4px", "md": "8px", "lg": "12px", "xl": "16px", "full": "9999px" },
      "editor": { "caret": "#………", "selection": "rgba(…,…,…,0.25)", "selection_text": "#………",
                  "link": "#………", "link_hover": "#………", "highlight": "rgba(…,…,…,0.35)" }
    },
    "light": {
      "surfaces": {
        "app": { "bg": "#………", "border": "#………", "text": "#………" }
      },
      "hover": "#………", "active": "#………",
      "border_active": "#………", "border_focus": "#………",
      "text_muted": "#………", "text_disabled": "#………",
      "accent": {
        "primary":   { "start": "#………", "end": "#………", "glow": "rgba(…,…,…,0.10)" },
        "secondary": { "start": "#………", "end": "#………", "glow": "rgba(…,…,…,0.08)" }
      },
      "status": { "warn": "#………", "danger": "#………", "success": "#………" },
      "error":  { "fg": "#………", "bg": "#………", "border": "#………" }
    }
  }
}
```

> **Delete `background` entirely** from a zone if you don't want a surface overlay — a zone without a `background` block pays zero compositing cost. Author only `surfaces.app` and omit the rest if you want every surface to share the root canvas. Omit `radius` / `shadow` / `editor` / `typography.scale` to fall through to the engine's sensible defaults.
