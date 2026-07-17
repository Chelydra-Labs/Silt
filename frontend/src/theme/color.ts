// Frontend OKLCH color helper for Theme System v2 (#385). Mirrors the
// derivation math in backend/themes/derivation.go so the editor preview
// matches the values the Go side persists: hover = L+0.06 & chroma×1.04,
// active = L-0.04, disabled = chroma×0.4. Output preserves the authored
// format (hex stays hex; oklch stays oklch; rgb()/rgba() normalize to hex).
//
// `culori/fn` is the tree-shakeable entry — pulling only useMode + the rgb /
// oklch / lrgb mode definitions keeps the bundle to the OKLCH + RGB + WCAG
// pieces instead of culori's full color-space catalog. lrgb is registered
// because WCAG luminance is computed in linear-RGB.

import {
  useMode,
  modeRgb,
  modeOklch,
  modeLrgb,
  formatHex,
  parse,
  wcagContrast,
  type Color,
  type RgbColor,
  type OklchModeColor
} from 'culori/fn'

// Registering a mode wires its CSS parser and converters into culori's shared
// tables and returns a converter that parses any supported string into it.
// (Suffixed `_of` to avoid colliding with the public toHex/toOklch exports.)
const rgbOf = useMode(modeRgb)
const oklchOf = useMode(modeOklch)
useMode(modeLrgb)

// Derivation constants — must match backend/themes/derivation.go exactly.
const HOVER_DL = 0.06
const HOVER_CHROMA = 1.04
const ACTIVE_DL = -0.04
const DISABLED_CHROMA = 0.4

/** Public OKLCH components (uppercase L/C/H to distinguish from culori's lowercase form). */
export interface Oklch {
  L: number
  C: number
  H: number
  alpha?: number
}

/** Clamp lightness to the valid OKLCH range [0,1]. */
export function clampL(L: number): number {
  return L < 0 ? 0 : L > 1 ? 1 : L
}

/** Parse any accepted CSS color (hex, rgb(), rgba(), oklch()) into an RGB-mode object, or null. */
export function parseColor(s: string): RgbColor | null {
  const c = rgbOf(s)
  return c ? (c as RgbColor) : null
}

/** Any accepted color → #rrggbb (opaque; alpha dropped, mirroring the Go canonical form). Null if unparseable. */
export function toHex(s: string): string | null {
  const c = rgbOf(s)
  // Force alpha to 1 so formatHex always emits opaque #rrggbb, matching the
  // Go canonical form regardless of culori's version-specific alpha handling.
  return c ? formatHex({ ...c, alpha: 1 }) : null
}

/** Any accepted color → OKLCH components, or null if unparseable. */
export function toOklch(s: string): Oklch | null {
  const c = oklchOf(s) as OklchModeColor | undefined
  if (!c) return null
  return {
    L: c.l,
    // Greyscale / achromatic colors omit hue (and sometimes chroma) in culori.
    C: c.c ?? 0,
    H: c.h ?? 0,
    ...(c.alpha !== undefined ? { alpha: c.alpha } : {})
  }
}

/** Format OKLCH components as CSS oklch(), matching the Go precision (L,C 4dp; H 2dp) so previews match Flatten output. */
export function formatOklch({ L, C, H, alpha }: Oklch): string {
  const core = `${L.toFixed(4)} ${(C ?? 0).toFixed(4)} ${(H ?? 0).toFixed(2)}`
  return alpha === undefined ? `oklch(${core})` : `oklch(${core} / ${alpha})`
}

/**
 * Apply an OKLCH shift to a seed color, preserving its authored format:
 * oklch in → oklch out; anything else (hex/rgb/rgba) → hex out. Matches the
 * format rule in Go `derive`. Alpha is dropped on both paths, matching Go.
 */
function derive(s: string, shift: (lch: Oklch) => Oklch): string | null {
  const trimmed = s.trim()
  if (!trimmed) return null
  const input = toOklch(trimmed)
  if (!input) return null
  const out = shift(input)
  const outColor: Color = {
    mode: 'oklch',
    l: out.L,
    c: out.C,
    h: out.H
  }
  // CSS function names are case-insensitive; lowercase the check so an
  // uppercased OKLCH(...) still round-trips as oklch (a strictly-better
  // behavior than Go's case-sensitive prefix match, harmless for lowercase).
  if (trimmed.toLowerCase().startsWith('oklch(')) {
    return formatOklch(out)
  }
  const rgb = rgbOf(outColor)
  return rgb ? formatHex(rgb) : null
}

/** Perceptibly-brighter hover variant: L+0.06 (clamped), chroma×1.04. Output keeps the input's format. */
export function deriveHover(s: string): string | null {
  return derive(s, (lch) => ({
    L: clampL(lch.L + HOVER_DL),
    C: lch.C * HOVER_CHROMA,
    H: lch.H
  }))
}

/** Perceptibly-deeper pressed variant: L-0.04 (clamped). Output keeps the input's format. */
export function deriveActive(s: string): string | null {
  return derive(s, (lch) => ({
    L: clampL(lch.L + ACTIVE_DL),
    C: lch.C,
    H: lch.H
  }))
}

/** Desaturated disabled variant: chroma×0.4. Output keeps the input's format. */
export function deriveDisabled(s: string): string | null {
  return derive(s, (lch) => ({
    L: lch.L,
    C: lch.C * DISABLED_CHROMA,
    H: lch.H
  }))
}

/**
 * WCAG 2.x contrast ratio (1..21) of `fg` against opaque `bg`. Returns null
 * if either color is unparseable — culori's wcagContrast throws on undefined
 * luminance, so the inputs are parsed first.
 */
export function contrastRatioWCAG(fg: string, bg: string): number | null {
  if (!parse(fg) || !parse(bg)) return null
  return wcagContrast(fg, bg)
}

/**
 * Opaque fill representing how `start` paints over `surfaceBG` (mirrors Go
 * effectiveAccentFill). Translucent starts are source-over composited so
 * on-accent ink matches the rendered CTA, not uncomposited RGB channels.
 */
export function effectiveAccentFill(start: string, surfaceBG: string): string {
  const src = parse(start.trim()) as RgbColor | undefined
  if (!src) return start
  const a = src.alpha === undefined ? 1 : src.alpha
  if (a >= 0.999) return start
  const dst = parse(surfaceBG.trim()) as RgbColor | undefined
  // Unparseable surface: do not bias dark (#000). Let deriveInkOnAccent use start.
  if (!dst) return start
  const r = Math.round(((src.r ?? 0) * a + (dst.r ?? 0) * (1 - a)) * 255)
  const g = Math.round(((src.g ?? 0) * a + (dst.g ?? 0) * (1 - a)) * 255)
  const b = Math.round(((src.b ?? 0) * a + (dst.b ?? 0) * (1 - a)) * 255)
  return formatHex({
    mode: 'rgb',
    r: r / 255,
    g: g / 255,
    b: b / 255
  })
}

/**
 * Label ink for solid accent fills (mirrors Go DeriveInkOnAccent). Prefers
 * near-black / white that meet 4.5:1; pure black when near-black falls short
 * (medium indigos). Used when AccentTriple.on is omitted at flatten time.
 * Callers with possibly-translucent start should pass effectiveAccentFill first.
 */
export function deriveInkOnAccent(start: string): string {
  const nearBlack = '#0a0a0a'
  const pureBlack = '#000000'
  const white = '#ffffff'
  const cands = [nearBlack, pureBlack, white].map((ink) => ({
    ink,
    ratio: contrastRatioWCAG(ink, start)
  }))
  const passing = cands.filter((c) => c.ratio !== null && c.ratio >= 4.5) as {
    ink: string
    ratio: number
  }[]
  if (passing.length > 0) {
    const best = passing.reduce((a, b) => (b.ratio > a.ratio ? b : a))
    if (best.ink === pureBlack) {
      const near = passing.find((c) => c.ink === nearBlack)
      if (near) return nearBlack
    }
    return best.ink
  }
  const ranked = cands.filter((c) => c.ratio !== null) as {
    ink: string
    ratio: number
  }[]
  if (ranked.length === 0) return white
  return ranked.reduce((a, b) => (b.ratio > a.ratio ? b : a)).ink
}
