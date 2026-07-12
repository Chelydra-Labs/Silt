# Theme System v2 — Editor UX & Workflow Spec

> **Status:** Accepted for milestone #36.  
> **Tracks:** #398. **Gates:** #392, #401, #402, #393.  
> **Companion:** [theme-system-v2-rfc.md](theme-system-v2-rfc.md) (token contract), [THEMING.md](THEMING.md) (authoring).

This document is the interaction design for the custom theme editor. Implementation PRs must conform to it. Visual polish targets a **world-class** bar: progressive disclosure, live app preview, OKLCH-first color controls, and non-blocking contrast guidance — not a flat dump of ~70 tokens.

---

## 1. Product principles

1. **Presets first.** Customization starts from a built-in or saved theme. Blank-canvas is a secondary power action.
2. **Simple by default.** ~5 high-impact controls cover most writers; Advanced is one disclosure away.
3. **Working copy.** Edits never mutate embedded presets. Disk writes happen only on Save.
4. **Live, not strobe.** Every edit re-injects CSS variables into the real app chrome in the same frame. Hover never injects.
5. **Warn, never block.** Contrast failures inform; Save always succeeds (RFC D12).
6. **Keyboard complete.** Same a11y bar as AppearanceTab: labels, roving tabindex where grids exist, `aria-live` for status/contrast.

---

## 2. User journeys

### A — Browse & apply a preset (existing)

| | |
|---|---|
| **Entry** | Settings → Appearance |
| **Flow** | Scan card grid → click to stage preview → Apply / double-click to commit |
| **Stuck** | Preview left active after leaving tab → unmount restores active theme |
| **Unstick** | Revert banner / Esc |

### B — Light customization (primary path)

| | |
|---|---|
| **Entry** | Appearance → active theme → **Customize** |
| **Flow** | Editor opens on Simple strip → tweak accent / app bg / text / font / radius → live app updates → **Save as new theme** (if seed was built-in) or **Save** (if disk custom) → name dialog → theme appears in grid and becomes active |
| **Stuck** | Unsure what changed → Reset all / per-token reset |
| **Unstick** | Dirty bar + Revert discards working copy |

### C — Deep customization

| | |
|---|---|
| **Entry** | Editor → **Advanced** |
| **Flow** | Left nav by intent (Surfaces, Color & accent, …) → edit zones / status / geometry / editor tokens / backgrounds → contrast summary → Save |
| **Stuck** | Broken zone palette → “inherits from parent” + Reset group |
| **Unstick** | Coherence: default edits hit app zone; per-zone override is explicit |

### D — Reopen a saved custom theme

| | |
|---|---|
| **Entry** | Appearance → custom card → Customize |
| **Flow** | Working copy seeded from disk JSON → edit → Save overwrites same id |
| **Stuck** | Want a variant → Save as new |

### E — Export / import (with assets)

| | |
|---|---|
| **Entry** | Appearance export / import / drop |
| **Flow** | Existing import/export; custom themes with images round-trip via JSON + `.assets/` |
| **Stuck** | Validation errors → status live region with field messages |

### F — Contrast warning

| | |
|---|---|
| **Entry** | Any color edit that drops a pair below AA |
| **Flow** | Inline badge on token + aggregate “N pairs below AA” → jump to pair → optional one-click auto-fix (OKLCH lightness) → Save still allowed |
| **Stuck** | Decorative low contrast intentional → ignore and Save |

---

## 3. Information architecture

### Layout (immersive full-width — not a modal, not nested under Settings nav)

While the editor is open, **Settings section nav is hidden** so the chrome is only:

```
[Activity bar] | full-width Theme Editor
```

```
┌──────────────────────────────────────────────────────────────────┐
│ ← Appearance   Theme Editor · {name}   [Dark|Light]  Revert Save │
├──────────────────────────────────────────────────────────────────┤
│ [ Simple | Surfaces | Color | Type | Geometry | Editor | Bg ]    │  ← top tabs
├──────────────────────────────────────────────────────────────────┤
│  Controls for selected group (full width)                        │
│  Sticky: Unsaved changes · Esc reverts                           │
│  Contrast: “2 pairs below AA — review”                           │
└──────────────────────────────────────────────────────────────────┘
```

- **Do not** stack Settings sections + editor groups as two left rails.
- Editor groups are **top tabs**, not a third vertical nav.
- Live preview is **workspace-wide** via `injectTokens` (same contract as Appearance two-stage preview).

### Simple controls (default)

| Control | Maps to |
|---|---|
| App background | `modes.{mode}.surfaces.app.bg` |
| App text | `modes.{mode}.surfaces.app.text` |
| Accent primary | `accent.primary.start` (end/glow derived or linked) |
| Body font | `typography.font_family` |
| Corner radius | `radius.md` (scales others proportionally optional; v1 edits md only) |

### Advanced groups (by intent)

1. **Surfaces** — 9 zones × bg/border/text (+ muted/disabled when needed); inheritance labels  
2. **Color & accent** — primary/secondary triples, status, error, hover/active/focus  
3. **Typography** — families + type scale  
4. **Geometry** — radius / spacing / shadow ramps  
5. **Editor** — caret, selection, links, highlight  
6. **Background** — per-zone image, size, opacity, blend, position, scrim  

Primitive tokens stay hidden.

---

## 4. Interaction model

### Working copy

- Seed from `GetThemeJSON(id)` (embed or disk).
- Deep-clone seed for reset.
- Dirty when structured JSON differs from seed (or dirty flags).
- On edit: FE flatten → `injectTokens` (rAF-coalesced while dragging).
- On discard / leave confirm / unmount: `restoreActiveTheme()`.
- Built-in seed → Save always **Save as new** (fork). Disk custom → Save overwrites; Save as new available.

### Reset granularity

| Level | Behavior |
|---|---|
| Per-token | Revert field to seed |
| Per-group | Revert all fields in Advanced group |
| Reset all | Full seed restore + re-inject |

### Color picker (OKLCH-first)

- Swatch button opens popover on **activate** (not focus).
- Primary: hue + lightness (+ chroma “vibrance”).
- Advanced disclosure: numeric L / C / H + paste hex/`oklch()`.
- Keyboard: separate sliders with `aria-valuetext`.
- Library: existing `culori` helpers in `color.ts`.

### Background images

- Editor uses **PickImageFile + PrepareBackgroundAsset** (no disk theme write).
- Staging reference lands in working copy; materialize on Save only.
- Controls: size (tile/cover/contain), opacity, blend, position, scrim.
- Readability: contrast against scrim-tinted effective background.

### Contrast

| Class | Ratio band (text) |
|---|---|
| Pass | ≥ 4.5:1 |
| Warn | 4.0–4.5:1 |
| Fail | < 4.0:1 |

UI chrome pairs use 3:1. Indicators: icon + text (not color alone). Aggregate polite `aria-live`. Auto-fix: clamp foreground OKLCH L toward AA, preserve polarity; on-click only.

### Unsaved guard

- Sticky dirty bar while dirty.
- Navigate away / Close → confirm “Leave without saving?”
- Esc in clean state closes; Esc with dirty may confirm (or Revert then close — prefer confirm).

---

## 5. Accessibility contract

- Full keyboard operability for every control.
- Color popover: focus trap; Esc closes popover only.
- Contrast summary: `aria-live="polite"`, debounced (not per slider tick).
- Status messages reuse Appearance `themeStatus` patterns (`role="status"` / `alert`).
- No Playwright; Vitest component tests cover a11y contracts.
- Address all reasonable Svelte `a11y_*` warnings.

---

## 6. Coherence safeguards

- Default Simple edits affect **app** zone and global accent — child zones inherit via Flatten `var()` chains.
- Per-zone override is Advanced and labeled “Overrides inherited {parent}”.
- Derived interaction tokens (hover/active/disabled) stay collapsed under seeds unless Advanced unlocks them.

---

## 7. Non-goals

- Theme marketplace, first-run tour, mobile layout.
- Blocking save on contrast.
- Modal full editor.
- Primitive token editing.

---

## 8. Implementation mapping

| Spec area | Issues |
|---|---|
| This document | #398 |
| Editor shell + Simple/Advanced + picker | #392 |
| Background surface | #401 |
| Contrast surface | #402 |
| Save / rename / delete | #393 |
| User + architecture docs | #396 |

---

## 9. Design references (behavior, not branding)

- Progressive disclosure: NN/g — two levels max; no wizard for interdependent colors.
- Seed-driven systems: few seeds → full semantic set (Linear/Raycast-style simplicity).
- OKLCH: perceptual lightness primary; L/C/H as keyboard truth (Evil Martians / oklch.com).
- Unsaved changes: explicit dirty + confirm on leave (Cloudscape pattern).
- Silt AppearanceTab: two-stage preview, no hover-strobe, restore on unmount — **extend this DNA**.
