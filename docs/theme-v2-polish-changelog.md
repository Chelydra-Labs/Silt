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

---

## Zone-coverage completion pass: explicit `editor` + `popover` everywhere

A zone-coverage audit found that dark modes authored only 4 zones
(`app`/`panel`/`modal`/`card`) for 8 of 11 themes — the `editor` (the primary
writing surface of a notes app) and `popover` (menus/dropdowns) were left to
inherit. Inheritance is fine for incidental zones, but the writing canvas and
the menu layer should be deliberate. Light modes were fuller but **Stark light**
was missing `editor`. This pass closes those gaps so every theme, in both modes,
authors the full primary set: `app`/`editor`/`panel`/`card`/`modal`/`popover`
(plus `sidebar` on the dual-surface Daybreak/Bubblegum light).

### What changed

- **Dark `editor` authored on 8 themes** (cyber_forest, graphite, frost,
  daybreak, bubblegum, aggie, altgeld, stark). Each is a deliberate "page" one
  perceptible step lighter than `app` and in the same hue family, so the writing
  canvas reads as a distinct surface rather than the same wash as the chrome.
  The 3 themes that already authored a textured editor (Linen weave, Terra Noir
  grain, Synthwave grid) keep their overlays verbatim.
- **Dark `popover` authored on all 11 themes** (none authored it before). Each
  lifts one perceptible step above `modal` as the topmost tier
  (menus/tooltips/dropdowns sit above dialogs).
- **Stark light `editor` authored** — pure white (`#ffffff`) with a strong
  `#3a3a3a` border, the AAA reading surface (21:1).
- **Dark `modal` lifted off the panel plateau** on the 8 themes where
  `modal ≡ panel` (graphite, frost, synthwave, daybreak, bubblegum, aggie,
  altgeld, stark). A dialog now reads as raised one step above its panel parent,
  giving the dark ladder a clean `app → editor → panel → modal/card → popover`
  progression.
- **Light `editor` re-seated between `app` and `panel`** on 10 themes. The
  previous pass had authored light editors as the brightest tier (brighter than
  `panel`), which broke the monotonic ladder. Each light editor now sits one
  step above `app` and below `panel` — a deliberate paper surface distinguished
  by its warm/cool hue (Linen/Terra Noir warm paper, Frost/Synthwave cool,
  Cyber Forest neutral slate, etc.) rather than by brightness. Stark light is
  the deliberate exception: its AAA identity keeps `editor` at pure white.

### Result

- **Zone coverage before → after:** dark modes 4 zones → 6 (5 → 6 for the 3
  textured themes); Stark light 5 → 6. Every theme now authors
  `app`/`editor`/`panel`/`card`/`modal`/`popover` in both modes.
- **Ladders monotonic** in both modes for all 11 (`app` darkest/most-tinted →
  `popover` lightest/nearest-white; `editor` between `app` and `panel`).
  Stark light is intentionally non-monotonic by AAA design (reading surfaces
  pure white, structure border-led).
- **Gate green** for all 11 (`go test -race -count=1 ./backend/themes/...`),
  `go vet` clean. The broadened gate (each zone's text + text-muted on its own
  bg ≥ 4.5:1 / AAA-Stark; accent starts ≥ 3:1 on app) confirmed — every new
  `editor`/`popover` bg was verified to keep text-muted ≥ 4.5:1 (tightest:
  cyber_forest dark at 5.18:1 on the new popover).
- **On-point:** no identity drift — only zones were added in each theme's
  existing hue family and light-mode editor lightness adjusted; accents,
  status, error, typography, geometry, and editor-interaction blocks are
  unchanged from the prior pass.

---

## Light-mode character pass: wider ladders, confident tints, real borders

The light modes read as a flat, timid wash: zone ladders were compressed to
~0.02 luminance per tier (bubblegum's editor→panel step was **0.001** —
identical surfaces), app bgs were tinted so faintly they read as "near-white
with a hint," and borders were ~1.4-1.5:1 against their bg (invisible). This
pass gives the light modes the same presence as their dark counterparts.

The shape, applied to 10 of 11 themes (Stark is the AAA value-led exception):

1. **Widened ladders** to ~0.030-0.050 luminance per step. App bgs dropped from
   the timid ~0.84-0.92 band to a confident **0.72-0.81** (clearly tinted
   canvases), climbing in perceptible steps to a near-white popover (~0.98).
   Min step per theme went from ~0.001-0.024 → **0.026-0.038**.
2. **Pushed app/editor chroma** so each app reads as a confident colored
   canvas, not "white with a hint."
3. **Strengthened borders** to clearly-darker shades of each bg — container
   edges (card/modal/popover) now land at **3.5-8.4:1** against their bg
   (was 1.4-1.5:1), so cards/modals/menus visibly lift off their parent.
4. **Hover/active** re-tuned darker to sit below the new (darker) app.

A key constraint surfaced: darkening the app *reduces* contrast for dark
accents (they converge in luminance), so each theme's app luminance has a
floor set by its accent's 3:1 gate. Two themes needed their accent deepened
to free the app to be characterful (standard for light mode, where accents
read deeper):

- **Daybreak** amber primary `#cc7408` → `#bd6a08` (deeper amber; the original
  forced app ≥ L0.86, which was too timid for a warm dawn-cream). Caret,
  selection, and glow track the new hue.

### Per-theme light-mode character

- **Cyber Forest** — cool slate ladder deepened (`#e1e9f2` app → `#fcfdfe`
  popover); the canvas now reads as clearly cool slate, not pale blue-white.
  Teal accent clears 3.06:1 on the darker app.
- **Terra Noir** — warm clay-parchment app `#efe0c6`; the editor grain overlay
  is preserved. Clay accent 3.60:1. Warmest ladder in the set.
- **Linen** — warm cream-paper app `#efe8d6`; woven-grain editor overlay
  preserved. Slate-blue accent 3.18:1.
- **Graphite** — neutral grey ladder `#e4e4e4` → `#fdfdfd`; restrained
  monochrome identity kept, but the value ladder now has visible steps (the
  single blue accent pops harder against the confident grey).
- **Frost** — crisp icy-blue app `#dde8f4`; reads as a winter morning, not a
  white hint of blue. Sky-blue OKLCH accent 3.30:1.
- **Synthwave** — soft lilac app `#e2deec`; the light mode carries a hint of
  the neon world (sun-bleached poster). Magenta OKLCH accent 4.46:1.
- **Daybreak** — warm dawn-cream app `#f1e8d0` framing the dark twilight
  sidebar; the dual-surface contrast is now sharper (deeper amber 3.29:1).
- **Bubblegum** — warm blush app `#f4e3d8` framing the dark raspberry sidebar;
  coral accent 3.07:1. The editor/panel plateau (was 0.001 step) is gone.
- **Aggie** — soft alfalfa-green app `#dce8d2`; reads as a green meadow. CSU
  Green text + gold accent (3.07:1) land against the confident green tint.
- **Altgeld** — confident Illinois-blue app `#d2deec`; Illini Orange (3.56:1)
  lands hard against the blue-tinted base — prairie-fire identity intact.
- **Stark** — untouched. Its AAA identity is pure black/white extremes with
  border-led structure; forcing tint would betray it. The value ladder
  (white reading surfaces, grey structural tiers) is its character.

### Verification

`go test -race -count=1 ./backend/themes/...` green for all 11; `go vet` clean.
Every light-mode accent clears ≥3:1 on its (now darker) app — including the
OKLCH accents (terra-noir 3.60, frost 3.30, synthwave 4.46) verified via the Go
harness. Dark modes were not touched in this pass. Representative before→after
ladders:

- **Bubblegum**: app 0.853→0.791, min step **0.001→0.030**.
- **Cyber Forest**: app 0.884→0.807, min step **0.010→0.027**.
- **Daybreak**: app 0.917→0.810, min step **0.009→0.026**.
