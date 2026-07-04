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
