# Theme System v2 — Polish Pass (#395)

Per-theme changelog for the v2 polish pass over Silt's 11 built-in themes. The
earlier phase migrated them faithfully to v2 CORE (surface zones, error block,
`schema_version 2.0.0`); this pass populates the NEW optional token categories
(radius / spacing / shadow / editor / `typography.scale`) with intentional
per-theme values, adopts OKLCH notation where it earns its keep, extends the
unified background block beyond Linen, and tunes each error block so error
states read clearly instead of blending into the panel.

Every change here is additive — the established surface/accent/status palettes
are preserved (OKLCH conversions are at equal visual appearance, verified to
round-trip within 1–2 sRGB units). All 11 themes still pass the v2 contrast
gate (AAA 7:1 for Stark, AA 4.5:1 elsewhere) and `go vet`.

## Geometry personalities

- **Linen** — soft/rounded (md 12px, lg 16px), airy spacing (xl 32px).
- **Graphite** — sharp/dense (md 4px, lg 6px), tight spacing (md 6px).
- **Stark** — crisp (md 2px), heavier weights for AAA legibility.
- **Synthwave** — tight (md 6px), dense spacing (xl 16px), neon-glow shadows.
- **Bubblegum** — soft/rounded playful (md 10px, xl 18px).
- Others — coherent mid-scale (md 8px, lg 12px).

## OKLCH adoption

Accents converted to OKLCH at equal appearance (hover/active now derive
perceptually instead of muddying through hex round-trips):

- **Frost** — sky-blue + indigo winter accents.
- **Terra Noir** — clay + moss earth tones.
- **Synthwave** — hot-pink + electric-cyan neon.

## Backgrounds (beyond Linen)

- **Terra Noir** — warm fractal-noise grain on the editor canvas in both modes
  (soft-light in dark, multiply in light), opacity 0.05–0.06.
- **Synthwave** — retro Tron-style grid (cyan + pink hairlines at 40px) on the
  dark editor canvas, overlay blend, opacity 0.08.

---

## Per-theme notes

### Cyber Forest (default)
Mid-scale geometry and a coherent teal/indigo editor block. Error bg now a
dark desaturated red (`#1a1012`) in dark mode and a near-white pink tint
(`#fdf2f4`) in light, so validation errors stop reading as "just another
panel." Standard 16px type ramp.

### Terra Noir
OKLCH-derived clay + moss accents (the earth tones looked muddy when v1
derived hover/active from hex). Warm fractal-noise grain on the editor canvas
extends the paper-earth metaphor. Slightly organic geometry (lg 14px, xl
20px). 17px serif base with relaxed line-height — a reading-first theme.

### Linen
Soft/rounded geometry (md 12px) and airier spacing (xl 32px) lean into the
paper feel; warm-taupe shadows are kept very light. Larger 17px base with the
most relaxed line-height in the set (1.65 / 1.85). Woven paper-grain texture
preserved on the editor background. Error bg now a warm rose tint.

### Stark
Crisp 2px corners and a heavier weight ramp (medium 600, semibold 700) for
AAA legibility. Error block tuned hard: pure-white border in dark, pure-black
border in light, so an invalid field is unmistakable. Selection text inverts
(black on gold dark, white on amber light) for maximum readability.

### Graphite
Sharp/dense geometry (md 4px) and tight spacing (md 6px) deliver the calm
monochrome discipline the identity calls for. 17px base with relaxed
line-height. Single restrained blue accent drives the caret; the warm-gray
shadow tint is kept very subtle. Error bg now a dark desaturated clay.

### Frost
OKLCH-derived sky-blue + indigo accents — winter blues benefit from
perceptual hover/active derivation. Cool, light shadows. Airy spacing. The
editor caret and link both render in OKLCH so the derivation pipeline is
end-to-end consistent.

### Synthwave
OKLCH-derived hot-pink + electric-cyan neon. Tight retro geometry (md 6px)
and dense spacing (xl 16px). The md/lg shadows are dual-layer neon halos that
reference the accent tokens via `color-mix`, so elevated surfaces glow. A
Tron-style cyan/pink grid (40px) overlays the dark editor canvas — subtle at
opacity 0.08, overlay-blended so it never competes with text. Light mode
stays clean (sun-bleached poster feel).

### Daybreak
Warm dusk-tinted shadows and a coherent amber/indigo editor block. Mid-scale
geometry. Error bg tuned to a soft rose. The twilight-blue sidebar (kept
from migration) still frames the warm cream page in light mode. Standard 16px
type ramp.

### Bubblegum
Soft/rounded playful geometry (md 10px, xl 18px). Coral caret + teal link
mirror the accent pair. Error bg now a soft raspberry tint. Slightly larger
display sizes (xl 1.3125rem, 2xl 1.625rem) for the playful vibe. Light mode
keeps its deep raspberry sidebar + warm cream page (from migration).

### Aggie
Sturdy mid-scale geometry (md 6px, lg 10px) with earthy pumpkin caret and
alfalfa-green link. Error bg tuned per mode — dark clay in dark, soft rose in
light. Standard 16px type ramp honours the heritage/legibility identity.

### Altgeld
Bold mid-scale geometry with prairie-fire orange caret + Illinois-blue link.
Slightly larger display sizes (xl 1.3125rem, 2xl 1.625rem). Error bg tuned
to read clearly against the Illinois Blue canvas. Standard 16px base.

---

## Light-mode surface refresh

The light modes had collapsed toward a single flat near-white: `panel` and
`modal` were both `#ffffff`, `card` sat *below* `app` (recessed rather than
raised), borders were 2–3% from their bg (invisible), and `app` started so
close to white there was no headroom for a ladder. This pass re-authored every
light mode so each zone reads as a distinct, intentional surface.

The shape is consistent across the set: `app` is pushed down to a clearly
tinted base (the theme's identity hue at ~88–92% L), then the ladder climbs in
visible 2–3% steps — panel → card → modal → popover — toward the lightest tier
(white or near-white). A dedicated `editor` "paper" zone is authored for every
theme (slightly warmer/cooler than the app) so the writing canvas feels like a
page rather than the same wash. `popover` is now explicitly authored (the
lightest tier) on all themes instead of inheriting modal. Borders are deepened
to ~8–12% darker than their bg so containers actually delineate; `border_focus`
is pulled darker for clearer focus rings; `text_muted`/`text_disabled` are
tuned to stay ≥4.5:1 (and far above for Stark). Error blocks get a clearer
soft-tint bg with a visibly chromatic border so validation errors read as
errors. Daybreak/Bubblegum keep their dark `sidebar` chrome verbatim. Dark
modes, accents, OKLCH-adopted colors (Frost/Terra Noir/Synthwave), Linen/Terra
Noir editor grain, the Synthwave dark grid, geometry, and typography are all
untouched. Contrast gate green for all 11 (AAA ≥7:1 for Stark).

- **Cyber Forest light**: cool slate ladder (`#eef2f7` → `#ffffff`); added
  cool-paper editor; borders pulled to slate-gray; crisper focus ring.
- **Linen light**: warm paper progression (`#f6f0e4` → `#fffefb`); editor
  grain preserved on a warm-white sheet; visible taupe borders.
- **Stark light**: intentionally restrained AAA ladder — white app/modal/
  popover, `#f2f2f2` panel, `#e4e4e4` inset card (all ≥14:1); hierarchy is
  border-led (descending to pure black on modal/popover). Identity preserved.
- **Graphite light**: true-neutral gray ladder (`#ececec` → `#ffffff`); the
  single blue accent now pops against the calm gray base.
- **Frost light**: icy blue ladder (`#e6eef8` → `#ffffff`) climbing to
  frost-white; OKLCH sky-blue/indigo accents preserved; clear blue-gray
  borders.
- **Synthwave light**: sun-bleached lavender poster ladder (`#eae6f0` →
  `#ffffff`); OKLCH pink/cyan accents preserved; stays clean (no grid, per the
  poster identity).
- **Terra Noir light**: warm earth ladder (`#efe4d0` → `#fffcf5`); OKLCH
  clay/moss accents and the warm editor grain both preserved.
- **Daybreak light**: warm cream ladder (`#faf5ec` → `#ffffff`) tuned so the
  amber accent still clears 3:1; dark twilight sidebar kept verbatim.
- **Bubblegum light**: warm peach ladder (`#f7ece2` → `#fffcf7`); deep
  raspberry sidebar kept verbatim; coral accent pops against the peach base.
- **Aggie light**: alfalfa-green-tinted ladder (`#e6efe0` → `#fcfef8`); gold
  + green heritage accents read cleanly.
- **Altgeld light**: Illinois-Blue-tinted ladder (`#dde7f2` → `#fbfdfe`);
  Illini Orange primary lands hard against the blue-tinted base.

Biggest refreshes (most-flat → most-laddered): **Graphite, Frost, Altgeld,
Aggie, Synthwave** — these had the most severe panel≡modal≡white collapse and
the faintest borders. **Stark** got the lightest touch (its AAA identity is
austere by design).

---

## Design re-author pass: per-theme palette cohesion

A follow-up pass that retunes each theme's `status` / `error` family (and a few
accents, glows, and surface ladders) so every palette reads as deliberate and
self-coherent. The driver: six themes (cyber_forest, frost, synthwave,
daybreak, bubblegum, altgeld) had shipped with an identical copy-pasted
`status {#fbbf24 / #f43f5e / #22c55e}` + `error {#f43f5e …}` block — verbatim
Tailwind amber-400 / rose-500 / green-500. That single shared block was why
"they all look the same." Every status/error family is now hand-tuned to belong
to its theme's hue family, and every accent `glow` is re-aligned to its
gradient's actual start hue. The broadened contrast gate (which now also tests
each zone's resolved `text-muted` on its own bg) is green for all 11 themes,
including the two previously-failing dual-surface light sidebars.

### Accent gradient values kept where DESIGN.md canonizes them
`#2dd4bf`/`#0d9488`/`#6366f1`/`#a855f7` (Cyber Forest) and
`#f59e0b`/`#d97706`/`#6366f1`/`#4f46e5` (Daybreak) are the documented identity
hues from DESIGN.md §2.1 / §2.2.8 — each appears only in its own theme, so they
are deliberate and per-theme, not copy-paste. They are preserved; everything
around them (status, error, highlight, glow) is re-authored.

### Per-theme notes

- **Cyber Forest (default)** — exemplary template. Dark `modal` lifts off the
  panel plateau (`#121215` → `#151519`). Status retuned to the cool palette:
  warn a muted gold `#e0b04a`, danger a cool rose `#dd5a72`, success a teal-green
  `#37b594` (harmonizes with the primary). Error block is now slate-plum.
  Accents/glows aligned to start hues. Light status mirrors the cool family.
- **Terra Noir** — dark `modal` lifts off the editor/panel plateau
  (`#17110b` → `#1c140d`). Status joins the earth palette: warn ochre, danger
  brick, success a fresh leaf-green distinct from the moss "in-progress"
  secondary. Error stays the warm clay block. Light status deepened (ochre /
  brick / moss).
- **Linen** — dark `modal` lifts (`#2b2825` → `#302c29`). The lone stock value
  (success `#22c55e`) replaced with a low-chroma sage `#7a9a6a` / `#4a7a4a` to
  match Linen's muted identity. Warn/danger already custom — preserved.
- **Stark** — light `accent.primary.start` corrected from olive-bronze `#8a5a00`
  to true gold `#b88a00` (clears the 3:1 non-text gate at 3.15:1; AAA text gate
  untouched at 21:1). `end` + `glow` track the new gold. Success retuned vivid
  for AAA legibility on both extremes (`#34d36b` dark, `#0e7a36` light).
- **Graphite** — the warm status/error were the only warm colors in a
  cool-neutral monochrome, so they are cooled and desaturated: warn
  low-chroma `#c8b074`, danger dusty `#c25555`, success muted sage `#5a9678`.
  Error bg/border neutralized. Light mirrors.
- **Frost** — status retuned frosty: warn `#e8c45a`, danger a cool berry-rose
  `#e85a7a`, success icy mint `#4ddbb0`. Light ladder re-authored so `panel`
  sits clearly below `card` (no card/panel inversion) and `editor` reads as the
  bright cool paper. Light status deepened.
- **Synthwave** — status retuned neon: warn `#ffc857`, danger hot-magenta
  `#ff3d6e`, success neon-mint `#4ddbb0`. The `error` block is now internally
  hue-consistent (the bg was plum while fg/border were red — now all magenta
  family). Light `editor`/`card` plateau split (`#f4f1fa` / `#f8f5fc`).
- **Daybreak** — **gate fix**: light `sidebar` now authors its own `text_muted`
  (`#a8b4cc`, 7.0:1 on the twilight bg) and `text_disabled` so muted sidebar
  text stops rendering at 1.4:1. Light `border_active` neutralized (`#9c8060`
  → `#8a8499`) so the warm-brown no longer leaks onto the cool sidebar. Status
  retuned warm (amber / rose / green).
- **Bubblegum** — **gate fix**: light `sidebar` authors its own `text_muted`
  (`#c8a8b8`, 8.0:1 on the raspberry bg) and `text_disabled`. Light
  `border_focus` realigned to the raspberry family (`#6c4838` → `#6a4458`).
  Status retuned to the coral/teal palette — success moves to the teal side
  (`#1f7a78`), danger to deep coral matching the primary. Error is dark
  raspberry. Light status deepened.
- **Aggie** — light `accent.primary.start` corrected from olive `#7a6408` (H93)
  to true gold `#a07c00`; the cited `#b08800` would have failed the 3:1 gate on
  the green app (2.81:1), so the brightest passing gold was chosen (3.32:1).
  `end` + `glow` track the new gold. Light ladder re-authored (panel below card,
  editor as warm paper). Success becomes CSU green (`#2a7a22`, matching the
  text identity). Dark success is alfalfa-green.
- **Altgeld** — light primary + secondary `glow` re-aligned to their gradient
  hues (primary was the dark-mode orange, secondary was the text-color blue —
  both now match their light-mode starts). Light ladder re-authored (panel below
  card, editor as paper). Status retuned to the orange/blue family (warn
  prairie-fire amber `#ffa840`, danger `#e85060`, success warm green).

### Audit: zero stock-Tailwind status/error values remain
A repo-wide grep for the offender set (`#fbbf24`, `#d97706`, `#f43f5e`,
`#e11d48`, `#22c55e`, `#16a34a`, `#ef4444`, `rgba(251,191,36,…)`,
`rgba(217,119,6,…)`, etc.) returns matches only in the DESIGN.md-canonical
accent gradients of Cyber Forest and Daybreak — the documented identity hues
that are unique to each theme. Every `status`, every `error`, and every
`editor.highlight` across all 11 themes is now a hand-tuned, palette-coherent
value. Contrast gate green for all 11 (AAA ≥7:1 for Stark; AA ≥4.5:1 elsewhere,
including Daybreak + Bubblegum light sidebars).
