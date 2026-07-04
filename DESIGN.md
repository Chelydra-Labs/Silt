Design Specification: Silt

Core Design System, Component Tokens, & Interaction Specification

**How to use this document.** The visual design system — the Cyber-Ink
language, the token schema, component visual specs, and interaction models.

- **Authoritative for:** design tokens (color/typography/spacing/shape),
  component visual specifications, motion & interaction specs, the visual
  accessibility targets.

**Principles**
- The token tables here are the contract shared by the Go theme loader, the
  runtime CSS injector, and the theme JSON — all three follow them.
- Read on-disk theme files as the editable source; this doc defines the
  schema and the canonical defaults, not individual user themes.

**Rules**
- When a token changes, update the loader, the injector, and the theme JSON
  in the same change as this doc.
- In a component spec, reference semantic accent tokens
  (`--accent-primary-*`), not a concrete hue.

**Best practices**
- Prefer a cross-reference to ARCHITECTURE §4.4 / SPECS §6.4 over restating
  the theme-engine plumbing here.

**Not for**
- The theme-engine plumbing (ARCHITECTURE.md §4.4), the theme file-format
  spec (SPECS.md §6.4), or implementation detail.

1. Design Vision: Refined Cyber-Ink

Most digital workspace applications fall into one of two visual extremes: flat, sterile minimalism that feels clinical (e.g., default note-taking apps) or over-saturated, high-contrast neon layouts that induce cognitive fatigue during multi-hour reading/writing sessions.

Silt implements "Refined Cyber-Ink"—a design framework engineered for deep, distraction-free focus:

Ink-Rich Canvas: The interface relies on an ultra-dark slate base (#0c0c0e) and dark charcoal panels (#121215). This mimics high-grade dark paper, absorbing light emission to protect eyes on OLED, mini-LED, and high-brightness displays.

Surgical Accents: Highly saturated color gradients are constrained to less than 3% of the active viewport area. They act as glowing signposts (for checkboxes, keyboard navigation path markers, and active selection guides). The teal accent sits in the teal-400 → teal-600 range (rather than a fully-saturated sky/cyan) so it stays readable across long sessions without inducing visual fatigue; the indigo "in-progress" gradient remains one notch more vivid so the active state still draws the eye.

Hairline Isolation: Visual boundaries use absolute $1\text{px}$ lines with dark metallic borders instead of heavy box-shadow offsets, maintaining a clean, structured appearance.

2. Design System Tokens (Semantic & Raw)

This token set maps directly to our Go configuration runtime and Svelte theme-injection components. These variables translate to dark/light-mode variables dynamically.

2.1 Color Tokens Schema (Cyber Forest — the default / primary theme)

The canonical theme schema is **Theme System v2** (RFC `docs/theme-system-v2-rfc.md`; `schema_version: "2.0.0"`, the only supported version): modes-based (`modes.dark` / `modes.light`) with hue-agnostic **semantic accent tokens**, a **7-zone surface model**, a themeable **error family**, optional **geometry / typography-scale / editor** sub-trees, and a unified per-zone **background** block. Components reference only the semantic accents (`--accent-primary-*` = the "go / done" hue, `--accent-secondary-*` = the "in progress" hue); each theme maps its concrete hues onto them. This is the single source of truth shared by the theme loader, the runtime CSS injector, and `cyber_forest.json` — all three follow the token tables here. **Cyber Forest is the default and primary theme** (embedded as the guaranteed fallback); the additional first-class palettes in §2.2 are alternates.

**The surface model (v2).** A mode authors 7 named zones — `app, sidebar, editor, panel, card, modal, popover` — each `{bg, border, text}`. Only `app` is required; the rest inherit from a parent zone (`popover→modal→panel→app`; `sidebar`/`editor`→`app`; `card→panel`) via `var()` fallback chains, so a theme that cares about one canvas (the editor, say) authors just that zone and the rest follow the app root. "Dark chrome + light page" patterns (Daybreak, Bubblegum light) are now expressed as a dark `sidebar` zone against a light `editor`/`app` — the v1 `chrome` block is removed. Color slots accept `#hex`, `rgb()`/`rgba()`, and `oklch(L C H[/ A])`; OKLCH is what lets a theme derive perceptually-uniform hover/active/disabled variants and what lets the CI contrast gate (AA; AAA for Stark) reason exactly.

```
{
  "schema_version": "2.0.0",
  "id": "cyber_forest",
  "name": "Cyber Forest",
  "author": "Chelydra Labs",
  "description": "The default Silt dark theme: ink-rich slate canvas with surgical teal primary and indigo secondary accents.",
  "typography": {
    "font_family": "'Plus Jakarta Sans', sans-serif",
    "mono_font_family": "'JetBrains Mono', monospace",
    "headline_font": "'Hanken Grotesk', sans-serif"
  },
  "modes": {
    "dark": {
      "surfaces": {
        "app":   { "bg": "#0c0c0e", "border": "#1e1e23", "text": "#dee3e6" },
        "panel": { "bg": "#121215", "border": "#27272a", "text": "#dee3e6" },
        "modal": { "bg": "#121215", "border": "#3f3f46", "text": "#dee3e6" },
        "card":  { "bg": "#161619", "border": "#27272a", "text": "#dee3e6" }
      },
      "hover": "#1c1c21", "active": "#222226",
      "border_active": "#3f3f46", "border_focus": "#52525b",
      "text_muted": "#8b8b94", "text_disabled": "#4b5563",
      "accent": {
        "primary":   { "start": "#2dd4bf", "end": "#0d9488", "glow": "rgba(20, 184, 166, 0.15)" },
        "secondary": { "start": "#6366f1", "end": "#a855f7", "glow": "rgba(168, 85, 247, 0.12)" }
      },
      "status": { "warn": "#fbbf24", "danger": "#f43f5e", "success": "#22c55e" },
      "error":  { "fg": "#f43f5e", "bg": "#121215", "border": "#3f3f46" }
    },
    "light": {
      "surfaces": {
        "app":   { "bg": "#f8fafc", "border": "#e2e8f0", "text": "#0f172a" },
        "panel": { "bg": "#ffffff", "border": "#cbd5e1", "text": "#0f172a" },
        "modal": { "bg": "#ffffff", "border": "#94a3b8", "text": "#0f172a" },
        "card":  { "bg": "#f1f5f9", "border": "#cbd5e1", "text": "#0f172a" }
      },
      "hover": "#e2e8f0", "active": "#cbd5e1",
      "border_active": "#94a3b8", "border_focus": "#64748b",
      "text_muted": "#4d5667", "text_disabled": "#94a3b8",
      "accent": {
        "primary":   { "start": "#0d9488", "end": "#115e59", "glow": "rgba(13, 148, 136, 0.10)" },
        "secondary": { "start": "#4f46e5", "end": "#7c3aed", "glow": "rgba(79, 70, 229, 0.08)" }
      },
      "status": { "warn": "#d97706", "danger": "#e11d48", "success": "#16a34a" },
      "error":  { "fg": "#e11d48", "bg": "#ffffff", "border": "#94a3b8" }
    }
  }
}
```

Cyber Forest authors only the `app`, `panel`, `modal`, and `card` zones; `sidebar`, `editor`, and `popover` inherit from `app`/`panel`/`modal`. The optional blocks a theme may add on top: `radius` / `spacing` / `shadow` (geometry ramps), `editor` (caret / selection / link / highlight), `typography.scale` (sizes / line-heights / weights), and a per-zone `background` (`image` / `size` / `opacity` / `blend` / `position` / `scrim`) that subsumes the v1 `texture` overlay and powers per-zone background photos. `Flatten` emits sensible defaults for every omitted block, so a minimal theme renders with v1-equivalent geometry and type. The themeable `error` family replaces the static Material-3 error pink; `status.danger` (destructive actions) and `error.fg` (validation / invalid input) are deliberately distinct.


**Token usage convention (when to reach for `--color-text-primary` vs `--accent-primary-*`).**
The "Surgical Accents" doctrine above (accents are *signposts*, < 3% of the
viewport) decides which token a given element binds to:

- **`--color-text-primary`** → plain text chrome: wordmarks, headings, static labels,
  and body copy. These must follow each theme's body-text hue so a theme switch
  is visibly perceptible everywhere text appears.
- **`--accent-primary-*`** → signposts and interactive/selected state only:
  focusable icons, active-tab indicators, selected listbox rows, CTAs, links,
  focus rings, and breadcrumb "you are here" markers.

The split matters because three first-class themes share **cool** accents
(Cyber Forest teal, Graphite blue, Linen slate-blue) — if a prominent label
like the wordmark or the active-notebook header is bound to the accent token,
switching between those themes barely shifts its hue and the theme change reads
as inert even though the palette swapped correctly. Binding such
elements to `--color-text-primary` surfaces each theme's distinct body-text color
(neutral white / warm oatmeal / cool blue-gray) in the most eye-catching chrome.
The brand `<img>` logo (not the wordmark text) carries the brand identity; the
accent token is never used as a decorative text color.


2.2 First-Class Theme Palettes

Silt ships a curated set of first-class themes alongside the default. Each is a plain JSON file embedded in the binary (so it is always selectable, even before a vault exists or when the themes directory is wiped) and written to `<vault>/.system/themes/` by `ScaffoldVault` so it is editable on disk. All consume the same canonical schema and semantic accents from §2.1 — **no per-theme component code exists**; switching themes only changes the injected CSS custom-property values.

| Theme | `id` | Character |
| :--- | :--- | :--- |
| Cyber Forest *(default / primary)* | `cyber_forest` | Ink-rich dark slate, surgical teal primary, indigo secondary. |
| Terra Noir | `silt-terra-noir` | Warm dark earth: clay primary, moss secondary. |
| Linen | `silt-linen` | Woven linen paper: warm grey-taupe canvas + woven-grain editor background, slate-blue + muted lilac. |
| Stark | `silt-stark` | High-contrast / accessibility (WCAG AAA): pure black/white extremes, gold + cyan. |
| Graphite | `silt-graphite` | Calm true-neutral monochrome: pure gray canvas, single restrained blue accent, neutral-steel secondary. |
| Bubblegum | `silt-bubblegum` | Playful and vibrant: coral-pink primary, teal secondary. Light mode uses dual surfaces (deep raspberry sidebar + cream page). |
| Frost | `silt-frost` | Clean and airy: crisp blue-tinted winter palette with sky-blue accents. |
| Synthwave | `silt-synthwave` | 80s retro neon: deep indigo canvas, hot-pink + electric-cyan accents. |
| Daybreak | `silt-daybreak` | Twilight-blue sidebar + warm off-white page: the readability-exception dual-surface theme. |
| Aggie | `silt-aggie` | Heritage split: dark mode = alfalfa green + pumpkin orange; light mode = CSU green + gold. |
| Altgeld | `silt-altgeld` | Dark navy canvas with bright orange + electric blue — prairie-fire energy. |

Every first-class theme ships both dark and light variants and its own `typography` pairing. The palettes below document the color design intent for all eleven first-class themes.

2.2.1 Terra Noir — warm dark earth

A dark earth palette: warm near-black canvas with **clay/terracotta** primary (selection guides, active focus, completed checks) and **moss** secondary (in-progress / DOING indicator, metadata chips). Intent: a warmer, organic counterpart to Cyber Forest's cool slate, for users who prefer earth tones over cyber neons.

- Dark: `surfaces.app.bg #100b07` (warm near-black); `surfaces.app.text #ece3d5` (warm white); `accent.primary #e07a3c → #b4421a` (clay); `accent.secondary #84a04a → #5e7d2f` (moss).
- Light: `surfaces.app.bg #f6efe4` (warm paper); `surfaces.app.text #2a2014`; `accent.primary #c2511f → #9a3a14`; `accent.secondary #5a7d2a → #44611d`.
- Tuning: dark `text-muted #8a7860 → #a89478` to clear WCAG AA (4.5:1) on `active` — the binding constraint in dark mode is muted text on the lightest interaction surface.

2.2.2 Linen — woven linen paper

A soft, low-chroma palette modeled on natural linen: a warm grey-taupe canvas in dark mode (the authentic flax/oatmeal tone — grey-dominant with a whisper of warmth, never brown) and warm paper in light, both carrying a subtle **woven-thread + paper-grain background** on the editor surface (Linen is the canonical first-class theme that declares a `surfaces.editor.background` block; see §2.1). `primary` = muted **slate-blue** (reads as faded fountain-pen ink on paper), `secondary` = muted **lilac**. Intent: long-session comfort — a calm, tactile "paper" surface distinct from Cyber Forest's cool slate and Graphite's flat monochrome.

- Dark: `surfaces.app.bg #242220` (warm grey-taupe); `surfaces.app.text #e8e3d8` (oatmeal-white); `accent.primary #7fb3c4 → #5d97ab`; `accent.secondary #a8a3d4 → #847cb0`; `surfaces.editor.background` = light-thread linen weave + grayscale grain, `overlay` blend, opacity 0.08, `size: tile`.
- Light: `surfaces.app.bg #faf6ef` (warm paper, not pure white); `surfaces.app.text #2b2a27`; `accent.primary #4a8a9c → #3a7383`; `accent.secondary #686da3 → #565b8e`; `surfaces.editor.background` = dark-thread weave + grain, `multiply` blend, opacity 0.10, `size: tile`.
- Tuning: dark `text-muted → #b9b0a1` (warm grey) to clear AA on Linen's surfaces.

2.2.3 Stark — high-contrast / accessibility (WCAG AAA)

A first-class accessibility theme targeting **WCAG 2.2 AAA** (≥7:1 body text). Pure black/white extremes (21:1), **border-led structure** (because the near-uniform background can't separate panels by fill alone), and maximum-visibility accents: **gold/amber** primary and **cyan** secondary. Intent: an out-of-box option for low-vision and bright-environment users, rather than relying on them authoring a custom theme.

- Dark: `surfaces.app.bg #000000`; `surfaces.app.text #ffffff` (21:1); `border.active #ffffff` / `border.focus #ffd400` (vivid gold focus rings); `accent.primary #ffd400 → #ffb800`; `accent.secondary #00e5ff → #00b8d4`.
- Light: `surfaces.app.bg #ffffff`; `surfaces.app.text #000000` (21:1); `border.active #000000` / `border.focus #0000cc`; `accent.primary #8a5a00 → #6f4800`; `accent.secondary #005f70 → #00475a`.
- Exempt / decorative tokens (not WCAG-essential): the `*-glow` halos and `text-disabled`. Focus states are unmistakable in both modes (≥3:1 against adjacent colors per WCAG 2.4.11 / 1.4.11), asserted in the contrast harness.

2.2.4 Graphite — calm monochrome / true-dark

For users who find Cyber Forest *too colorful*. Graphite is a **true neutral monochrome**: pure neutral-gray surfaces (zero blue tint, unlike Cyber Forest's blue slate) with a **single restrained blue** accent as the only color and a **neutral steel** secondary. Neutral-white text (`#ebebeb`) reads distinctly cleaner/warmer than Cyber Forest's cool `#dee3e6`. Intent: the "developer dark" / "dimmed" aesthetic — a calm, flat, low-chroma surface. Comfortable AAA contrast, **not** the extreme contrast of Stark.

- Dark: `surfaces.app.bg #0a0a0a` (true near-black, neutral); `surfaces.app.text #ebebeb`; `accent.primary #6f9ad8 → #4d72a0` (restrained blue); `accent.secondary #9aa3ad → #6f7882` (neutral steel).
- Light: `surfaces.app.bg #f8f8f8`; `surfaces.app.text #1a1a1a`; `accent.primary #4a6fa0 → #374f78`; `accent.secondary #6a737d → #525a63`.
- Distinctness: primary (blue) and secondary (neutral steel) differ in both hue and chroma so go/done and in-progress never blur, while the overall surface stays a calm flat monochrome.

2.2.5 Bubblegum — playful & vibrant

A fun, energetic theme that breaks from the muted/professional incumbents.
**Coral-pink** primary and **teal** secondary on a warm magenta-tinged
canvas. Typography pairs Outfit (body — geometric, rounded), Sora (headline),
and Fira Code (mono). Intent: the theme for users who want their workspace to
feel creative and joyful rather than clinical — a decorated notebook, not a
spreadsheet.

- Dark: `surfaces.app.bg #1a0e1e` (warm magenta-tinted near-black); `surfaces.app.text #f5ede8` (warm cream-white); `accent.primary #ff6b8a → #e84371` (coral-pink); `accent.secondary #4fd1c5 → #38b2ac` (teal).
- Light (dual-surface): page `surfaces.app.bg #fdf6f0` (warm cream), `surfaces.app.text #2d1f1a` (warm dark brown); the `sidebar` zone is authored dark — `surfaces.sidebar.bg #2a1322` (deep raspberry-black), `surfaces.sidebar.text #f5ede8` (cream-white) — so the app skeleton is a deep raspberry candy shell framing a warm cream page. Accents deepen to `#e84371` / `#2c7a7b` for readability on white.
- Tuning: dark `accent.primary.start #ff6b8a` deepened to `#e84371` in light mode for AA non-text (3:1) on the warm cream page; the dark-mode coral glows on the magenta canvas while the light-mode deeper coral reads cleanly on cream.

2.2.6 Frost — clean & airy

A crisp, blue-tinted palette that fills the "bright and cool" gap — the
existing themes lean warm (Terra Noir), neutral (Graphite), or purely dark
(Cyber Forest). Typography pairs DM Sans (body), Lexend (headline), and Geist
Mono (mono). Dark mode is a moonlit frozen lake (cold steel-blue with icy
accents); light mode is a brilliant winter morning. Intent: a workspace that
feels clean, cold, and modern — the visual equivalent of fresh air.

- Dark: `surfaces.app.bg #0a0e14` (cold blue-black); `surfaces.app.text #e2eaf2` (cool ice-white); `accent.primary #38bdf8 → #0ea5e9` (sky-blue — frost on glass); `accent.secondary #818cf8 → #6366f1` (periwinkle).
- Light: `surfaces.app.bg #f0f4fa` (pale ice-blue); `surfaces.app.text #061020` (deep cold navy); `accent.primary #0284c7 → #0369a1` (deeper sky); `accent.secondary #4f46e5 → #3730a3` (deeper indigo).
- Tuning: dark `text-muted #8a9aae → #94a4b8` to clear AA on the lightest interaction surface (`active #263348`).

2.2.7 Synthwave — 80s retro neon

Deep indigo/near-black canvas with neon-hot accents — the visual language of
Miami Vice, Tron, and synthwave album art. Typography pairs Figtree (body),
Bricolage Grotesque (headline — expressive display), and Space Mono (mono —
retro terminal). Intent: unmistakably distinct from every other theme in the
roster; the theme for users who want their workspace to feel like a night
drive.

- Dark: `surfaces.app.bg #0d0b1a` (deep indigo-black); `surfaces.app.text #e8e6f0` (cool lavender-white); `accent.primary #ff2d95 → #e91e63` (hot pink/magenta — the neon sign); `accent.secondary #00f0ff → #00b8d4` (electric cyan — the grid line). Glow values intentionally stronger (0.15–0.18) for a neon-halo effect.
- Light: `surfaces.app.bg #f0eef5` (pale lavender — sun-bleached poster); `surfaces.app.text #1c1c34` (deep indigo); `accent.primary #c2185b → #880e4f` (deeper magenta); `accent.secondary #00838f → #006064` (deeper cyan).

2.2.8 Daybreak — dark sidebar + light page (dual surfaces)

The **"readability exception"** theme: a twilight-blue app skeleton
(sidebar, titlebar, activity bar) framing a warm off-white editor surface.
Text in the sidebar is light; text on the page is dark. Typography pairs Inter
(body — neutral, highly readable), Hanken Grotesk (headline), and JetBrains
Mono (mono). Intent: the theme for users who want dark navigation (low glare,
clear structure) with a bright reading/writing surface — the pattern Notion,
Readwise, and several code editors use for sustained reading sessions.

- Dark: `surfaces.app.bg #0c0e14` (dark charcoal-blue); `surfaces.app.text #eaeef5` (cool off-white); `accent.primary #f59e0b → #d97706` (warm amber); `accent.secondary #818cf8 → #6366f1` (indigo). Standard single-surface dark mode (no `sidebar` override needed — everything is dark).
- Light (dual-surface): page `surfaces.app.bg #faf8f5` (warm off-white — the paper), `surfaces.app.text #1c1917` (warm near-black — ink on paper); the `sidebar` zone is authored dark — `surfaces.sidebar.bg #1c2842` (twilight blue — the pre-dawn sky), `surfaces.sidebar.border #2c3c5e` (anchor blue), `surfaces.sidebar.text #eaeef5` (cool off-white). Shared accents: `accent.primary #cc7408 → #92400e` (amber — tuned to clear AA non-text on both sidebar and page), `accent.secondary #6366f1 → #4f46e5` (indigo — brightened from #4338ca for sidebar-surface visibility).

2.2.9 Aggie — heritage split: alfalfa/pumpkin (dark) → green/gold (light)

A **dual-identity heritage easter egg**: dark mode evokes Colorado A&M's
original colors (alfalfa green canvas + pumpkin orange accents — the harvest
field at dusk), while light mode shifts to CSU's modern palette (green canvas
+ CSU Green text + gold/Oval Green accents — the modern campus on a sunny
day). The dark→light switch mirrors the school's evolution from agricultural
college to state university. Typography pairs Work Sans (body — humanist,
warm), Manrope (headline), and IBM Plex Mono (mono).

- Dark (historical — alfalfa green + pumpkin orange): `surfaces.app.bg #0a1810` (dark alfalfa-green); `surfaces.app.text #e8efe0` (sage-white); `accent.primary #e07a30 → #b45a18` (pumpkin orange — derived from CSU Aggie Orange #D9782D); `accent.secondary #6aaa4a → #3a7a2a` (alfalfa green — the living plant color).
- Light (modern — CSU green + gold): `surfaces.app.bg #f0f5ee` (pale green meadow); `surfaces.app.text #1E4D2B` (CSU Green — the actual school color); `accent.primary #7a6408 → #5f4a06` (deep gold — CSU Gold #C8C372 deepened for readability); `accent.secondary #006144 → #004d36` (CSU Oval Green — the actual school color).

2.2.10 Altgeld — Illinois Blue + Illini Orange (prairie fire)

Dark Illinois Blue canvas with bright Illini Orange primary and electric blue
secondary — the energy of a prairie fire at dusk. Light mode uses actual
Illinois Blue (#13294B) as the text color and deep Illini Orange for accents.
Typography pairs Public Sans (body — clean, institutional), Schibsted Grotesk
(headline — bold, confident), and Martian Mono (mono). Intent: the boldest,
most color-assertive palette in the roster — unmistakable school identity.

- Dark: `surfaces.app.bg #0d1525` (Illinois Blue #13294B-tinted near-black — the prairie night); `surfaces.app.text #e6ecf5` (cool blue-white — moonlight); `accent.primary #FF5F05 → #cc4a00` (Illini Orange — the actual school color); `accent.secondary #4080e0 → #2050a0` (bright blue — Illinois Blue family, brightened for dark-mode pop).
- Light: `surfaces.app.bg #eaf0f6` (pale blue-grey — the dawn sky); `surfaces.app.text #13294B` (Illinois Blue — the actual school color); `accent.primary #c44a00 → #9a3800` (deep Illini Orange for readability on white); `accent.secondary #1a3677 → #0d1f4d` (Illinois Blue family).


3. Typography & Spacing Rhythm

3.1 Proportional Scaling & Hierarchy

To preserve natural visual hierarchy across deeply indented outliner structures, text elements use the following proportional sizes:

Primary Body Copy: 14px (0.875rem) — optimized for code and technical note-taking readability.

Heading 1 (#): 24px (1.5rem) | Line-Height: 1.3 | Weight: Bold (700)

Heading 2 (##): 18px (1.125rem) | Line-Height: 1.4 | Weight: Semi-Bold (600)

Default Bullet Block: 14px (0.875rem) | Line-Height: 1.6 | Weight: Regular (400)

Monospace Metadata / Shortcuts: 12px (0.75rem) | Line-Height: 1.0 | Weight: Regular (400)

3.2 Indentation Grid Scales

The indent spacing scale matches the indentation depths of the hierarchy blocks:

$$\text{Padding-Left} = L \times 24\text{px}$$

Where $L$ represents the absolute nesting depth level (e.g., Level 0 = 0px, Level 1 = 24px, Level 2 = 48px, Level 3 = 72px).

Line Height Constraint: Every block features a native py-1 ($4\text{px}$ top/bottom) padding window, giving a total block-to-block baseline vertical distance of $28\text{px}$ at $14\text{px}$ text sizes.

4. UI Component Specifications

4.1 The Task Checkpoint Component

Custom checkbox rendering mimics the structural rounded-corner boundaries (rx="16") of the Silt logo.

       [ ] TODO                    [/] DOING                   [x] DONE
   ┌───────────────┐           ┌───────────────┐           ┌───────────────┐
   │               │           │    ┌─────┐    │           │    \     /    │
   │               │           │    │  /  │    │           │     \   /     │
   │               │           │    └─────┘    │           │      \ /      │
   └───────────────┘           └───────────────┘           └───────────────┘
       Border: zinc-400            Border: indigo-500          Border: teal-500
   BG: --color-surface            BG: --color-surface            BG: --color-accent-primary-glow
                               Inside: secondary-grad      Inside: primary-check SVG


Token Rules

Inactive State (TODO):

Border: var(--color-border-zinc)

Background: var(--color-surface)

Hover Transition: border-color 150ms ease, box-shadow 150ms ease

Hover Style: Border: var(--color-accent-primary-start), Glow: 0 0 8px var(--color-accent-primary-glow)

In-Progress State (DOING):

Border: linear-gradient(to bottom right, var(--color-accent-secondary-start), var(--color-accent-secondary-end))

Content: Inner indicator square rotated $12^\circ$ to match the logo slant (M 28,14 L 20,50).

Completed State (DONE):

Border: var(--color-accent-primary-end)

Background: var(--color-accent-primary-glow)

Content: SVG checkmark colored in var(--color-accent-primary-start). Text within the block is struck through and shifted to color var(--color-text-disabled).

4.2 Dynamic Guideline Guide Rails

To prevent visual disorientation in deeply nested lists, the vertical guidelines highlight active parent-child hierarchies.

 - [ ] Root Task Element
 |   - [ ] Sub-Task Level 1
 |   |   - [/ ] Active Focused Block Node  <-- Highlight active guide rails
 |   - [ ] Unfocused Block Node            <-- Fallback guide rail


Standard Guide Rail: Width: $1\text{px}$ solid, offset by $-12\text{px}$ to the left of the child text node. Color: var(--color-border-muted).

Active Ancestral Path Guide Rail: Width: $1.5\text{px}$ solid. Undergoes color-blend shift to linear-gradient(to bottom, var(--color-accent-primary-start), var(--color-accent-primary-end)) when a child node receives active keyboard or mouse focus.

Path-Trace Duration: 250ms cubic-bezier(0.16, 1, 0.3, 1).

4.3 Inline Tag & Metadata Chips

Metadata tags are styled as low-contrast, highly readable pills to prevent cluttering block logs:

Owner Chip ([Chris]):

Typography: Monospace font stack, 0.75rem.

Style: Background: rgba(99, 102, 241, 0.08), Border: 1px solid rgba(99, 102, 241, 0.20), Color: #a5b4fc.

Priority Chip (Critical / #1):

Style: Background: rgba(244, 63, 94, 0.08), Border: 1px solid rgba(244, 63, 94, 0.30), Color: #fca5a5, Font-Weight: 700.

Priority Chip (Low / #3):

Style: Background: var(--color-panel), Border: 1px solid var(--color-border-zinc), Color: var(--color-text-muted).

4.4 Glassmorphism Contextual Menu

The slash command menu uses clear, frosted glass visual styling, maintaining background spatial context when triggered inline:

.command-palette {
  background-color: rgba(22, 22, 25, 0.75);
  border: 1px solid var(--color-border-active);
  border-radius: 8px;
  backdrop-filter: blur(12px) saturate(140%);
  -webkit-backdrop-filter: blur(12px) saturate(140%);
  box-shadow: 
    0 10px 25px -5px rgba(0, 0, 0, 0.50),
    0 0 15px rgba(99, 102, 241, 0.04);
}


5. Interaction States & Dynamic Feedback

Every component in Silt implements distinct states to provide clear feedback during mouse, keyboard, or touch-screen interaction:

Component

Default State

Hover State

Focus State

Active / Clicked State

Document Block Line

Transparent background, standard guide rails

Light background highlight (var(--color-hover)), shows line grab icon

var(--color-surface), guideline color transitions to the primary accent

N/A

Checklist Toggle

var(--color-border-zinc) border

var(--color-accent-primary-start) border, subtle glow

Standard glow ring

Transitions status to next cycle

Kanban Task Card

var(--color-panel) base, no offset

var(--color-hover) base, $1\text{px}$ upward translate

Highlighted outer border

Rotate $2^\circ$, add shadow layer on drag

6. Motion Specification & Micro-Animations

The UI avoids heavy or slow animations, keeping all transitions under $220\text{ms}$ to ensure the app feels fast and highly responsive.

Transitions Easing Curve: cubic-bezier(0.16, 1, 0.3, 1) (Ultra-smooth Exponential Out).

Hover Interaction Transitions: Duration: 120ms for color changes and layout shifts.

Command Menu Initialization: Scale transition from 0.97 to 1.0 combined with opacity fade-in. Duration: 100ms.

Kanban Card Drag-Reorder: Uses compile-time svelte/animate (using Svelte's native flip transition mechanics). Duration: 200ms with linear-out motion.

7. Picker & Selection UX

**Theme selection.** Settings → Appearance is the single surface. Mode is a `role="radiogroup"` of Dark / Light / System; changing mode never changes the active theme. Themes are a `role="listbox"` of `role="option"` rows with roving tabindex, Arrow/Home/End navigation, Enter/Space commit, and Esc to cancel any live preview. Swatches are data-driven from theme metadata (no per-theme code branches). The picker renders a live preview on hover/focus; the active theme is restored on `mouseleave`/`blur`/`Esc`. Errors and status updates flow through a `role="status" aria-live="polite"` region (escalating to `role="alert" aria-live="assertive"` for errors). The active id and mode persist across restarts via user-global settings.

**Page Template Picker.** The template picker reuses the same modal chrome, Refined Cyber-Ink token system, and iconography rules as the theme picker. Iconography follows the Material Symbols convention; the `icon` frontmatter field is a Material Symbols name rendered at 18–20px. No emojis are used in first-class template icons — they are abstract, CSS-friendly glyphs. The picker is a centered overlay (`role="dialog"`, `aria-modal="true"`) with a category-grouped `role="listbox"`, roving tabindex (Arrow/Home/End/Enter), a live preview pane, a dynamic placeholder form, and a Tab focus trap. Entry points: the sidebar `content_copy` button + `Ctrl+Shift+T` (new page mode) and the `/template` slash command (insert mode).


8. Accessibility (A11Y) & Keyboard Navigation Compliance

Silt is built for complete hands-on-keyboard efficiency, complying with WCAG 2.2 AA guidelines (the legal compliance standard). Stark is the designated AAA theme and retains its 7:1 body-text requirement as a theme-specific design invariant.

Contrast Ratios: Text-to-background contrast ratios are maintained at or above WCAG AA (4.5:1 for normal text). Stark additionally maintains AAA (7:1) as its theme-specific requirement.

Dual-surface themes: a theme that wants a dark app skeleton against a light reading surface (Daybreak, Bubblegum light) authors the `sidebar` zone dark against a light `editor` / `app` — the v1 `chrome` block is replaced by the `sidebar` surface zone. The WCAG contrast gate tests each zone's text against its own resolved background, so the dark sidebar and the light editor each independently meet the AA (4.5:1) threshold. This is the "readability exception" pattern — dark navigation framing a bright writing surface.

Focus States: Every interactive element features an explicit :focus-visible outline ring of $2\text{px}$ var(--color-border-focus) offset by $1\text{px}$ to prevent overlapping with components.

Keyboard Navigation Paths: Users can navigate the entire interface using standard shortcut triggers:

Tab and Shift+Tab to shift indentation levels.

Up / Down Arrow keys to navigate blocks.

Enter to create a new parallel block.

/ to trigger the contextual palette list, with keyboard arrows used to select options and Enter to confirm.

ARIA Label Mapping: Task check elements feature explicit ARIA attributes updating in real-time based on state values:

TODO state features: aria-role="checkbox" aria-checked="false" aria-label="Task Toggle: Not Started".

DOING state features: aria-role="checkbox" aria-checked="mixed" aria-label="Task Toggle: In Progress".

DONE state features: aria-role="checkbox" aria-checked="true" aria-label="Task Toggle: Completed".