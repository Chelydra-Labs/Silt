// Color palette for text/background color pickers (#170, #408).
//
// The default palette is derived from the ACTIVE THEME's color anchors
// (accents, status, error) rather than a fixed Tailwind table, so a colored
// highlight reads cohesively against the theme's surface temperature. Each
// seed produces a dark variant (raised OKLCH lightness, legible on dark
// surfaces) and a light variant (lowered lightness, legible on light
// surfaces). The user's stored mark color is always authoritative — only the
// initial default swatch row is theme-derived.

import { toOklch, toHex, contrastRatioWCAG } from '../../theme/color'

export interface ColorEntry {
  id: string
  label: string
  dark: string
  light: string
}

// Target OKLCH lightness for the derived variants. The dark variant sits
// bright enough to read against a dark surface (~0.72, matching the prior
// Tailwind-400 tier); the light variant sits deep enough to read against a
// light surface (~0.45, matching Tailwind-600). These are per-seed targets —
// the exact value is clamped to [0,1] and the seed's chroma/hue are preserved
// so the swatch stays in the theme's color family.
const DARK_TARGET_L = 0.72
const LIGHT_TARGET_L = 0.45

// A seed is a named slot that pulls its color from a theme anchor token.
// If the token is absent the seed is skipped (the palette shrinks gracefully).
interface PaletteSeed {
  id: string
  label: string
  token: string // CSS custom property name on :root
}

// The canonical seed set, ordered warm → cool so the swatch row reads
// naturally. Every first-class theme defines these anchors (they are required
// by the v2 schema's status/error blocks + the accent block).
const SEEDS: PaletteSeed[] = [
  { id: 'danger', label: 'Red', token: '--color-status-danger' },
  { id: 'error', label: 'Crimson', token: '--color-error' },
  { id: 'warn', label: 'Amber', token: '--color-status-warn' },
  { id: 'success', label: 'Green', token: '--color-status-success' },
  { id: 'secondary', label: 'Teal', token: '--color-accent-secondary-start' },
  { id: 'primary', label: 'Blue', token: '--color-accent-primary-start' }
]

// The neutral swatches (gray, black/white) are not hue-anchored; they are
// derived from the theme's text-muted token so a "gray" highlight matches the
// theme's neutrality (warm gray on Linen, cool gray on Frost).
const NEUTRAL_SEEDS: PaletteSeed[] = [
  { id: 'gray', label: 'Gray', token: '--color-text-muted' }
]

/**
 * Adjust an OKLCH seed color to a target lightness, preserving chroma and hue.
 * Returns an opaque #rrggbb hex string, or null if the seed is unparseable.
 */
function adjustLightness(seed: string, targetL: number): string | null {
  const lch = toOklch(seed)
  if (!lch) return null
  const adjusted = `oklch(${targetL.toFixed(4)} ${lch.C.toFixed(4)} ${lch.H.toFixed(2)})`
  return toHex(adjusted)
}

/**
 * Derive a ColorEntry (dark + light variant) from a seed color string.
 * Each variant targets a fixed OKLCH lightness for legibility; the seed's
 * chroma and hue are preserved so the swatch stays in the theme's family.
 */
function deriveEntry(
  id: string,
  label: string,
  seedColor: string
): ColorEntry | null {
  const dark = adjustLightness(seedColor, DARK_TARGET_L)
  const light = adjustLightness(seedColor, LIGHT_TARGET_L)
  if (!dark || !light) return null
  return { id, label, dark, light }
}

/**
 * Build a theme-derived default palette from a map of CSS custom properties
 * (token → value). Seeds whose token is missing are skipped, so a theme that
 * omits an anchor simply produces a shorter palette rather than a gap. A
 * neutral gray (from text-muted) and a contrast black/white pair round out
 * the row. Returns at least the neutral + B/W entries even if all hue seeds
 * are absent (defensive — the editor always has a usable swatch row).
 *
 * Pure function (no DOM access) so it is unit-testable with fixture tokens.
 */
export function deriveColorPalette(
  tokens: Record<string, string>
): ColorEntry[] {
  const entries: ColorEntry[] = []
  const seen = new Set<string>()

  for (const seed of [...SEEDS, ...NEUTRAL_SEEDS]) {
    const raw = tokens[seed.token]
    if (!raw) continue
    const entry = deriveEntry(seed.id, seed.label, raw)
    if (!entry) continue
    // Avoid duplicate ids (e.g. two themes where danger === error hue).
    if (seen.has(entry.id)) continue
    seen.add(entry.id)
    entries.push(entry)
  }

  // Contrast pair: black-for-light-mode / white-for-dark-mode. These invert
  // so "Black" on a light theme is truly dark and on a dark theme is truly
  // light — the user's intent is "max contrast text", not a specific pigment.
  entries.push({
    id: 'black',
    label: 'Black',
    dark: '#fafafa',
    light: '#18181b'
  })

  // Guarantee a minimum usable palette even if every hue seed failed to parse
  // (should not happen on a valid theme, but fail-open beats an empty row).
  // A single hue + black is still usable; only fall back when NO hue resolved.
  if (entries.length < 2) {
    return FALLBACK_COLOR_PALETTE
  }
  return entries
}

/**
 * Read the active theme's color-anchor tokens from the :root CSS custom
 * properties the theme injector wrote. Called by the picker consumers at
 * render/menu-open time so the palette always reflects the current theme.
 * Returns an empty object if the DOM is unavailable (ssr / pre-mount).
 */
export function readActiveThemeColorTokens(): Record<string, string> {
  if (typeof document === 'undefined') return {}
  const styles = getComputedStyle(document.documentElement)
  const tokenNames = [...SEEDS, ...NEUTRAL_SEEDS].map((s) => s.token)
  const out: Record<string, string> = {}
  for (const name of tokenNames) {
    const v = styles.getPropertyValue(name).trim()
    if (v) out[name] = v
  }
  return out
}

/**
 * Resolve the hex value for the current theme mode.
 * @param entry palette entry
 * @param isDark true if dark mode is active
 */
export function resolveColor(entry: ColorEntry, isDark: boolean): string {
  return isDark ? entry.dark : entry.light
}

/**
 * Derive the legibility-checked variant for a given mode. If the derived
 * variant fails WCAG AA (4.5:1) against the background, the raw entry value
 * is still returned — the L-targeting is a best-effort heuristic, not a hard
 * gate, and the user's explicit pick always wins. Exported for testability.
 */
export function resolveColorChecked(
  entry: ColorEntry,
  isDark: boolean,
  bg: string
): string {
  const hex = resolveColor(entry, isDark)
  const ratio = contrastRatioWCAG(hex, bg)
  if (ratio !== null && ratio < 4.5) {
    // Log-worthy in dev but not blocking — the L targets are tuned for the
    // common case; an edge-case theme may miss AA on one swatch.
  }
  return hex
}

// FALLBACK_COLOR_PALETTE is used only before the theme has injected its tokens
// (first paint on a cold start) or if every hue seed fails to parse. It mirrors
// the prior fixed Tailwind set so the editor is never without a swatch row.
// Once the theme resolves, deriveColorPalette takes over.
export const FALLBACK_COLOR_PALETTE: ColorEntry[] = [
  { id: 'red', label: 'Red', dark: '#f87171', light: '#dc2626' },
  { id: 'orange', label: 'Orange', dark: '#fb923c', light: '#ea580c' },
  { id: 'yellow', label: 'Yellow', dark: '#facc15', light: '#ca8a04' },
  { id: 'green', label: 'Green', dark: '#4ade80', light: '#16a34a' },
  { id: 'teal', label: 'Teal', dark: '#2dd4bf', light: '#0d9488' },
  { id: 'blue', label: 'Blue', dark: '#60a5fa', light: '#2563eb' },
  { id: 'indigo', label: 'Indigo', dark: '#818cf8', light: '#4f46e5' },
  { id: 'purple', label: 'Purple', dark: '#c084fc', light: '#9333ea' },
  { id: 'pink', label: 'Pink', dark: '#f472b6', light: '#db2777' },
  { id: 'gray', label: 'Gray', dark: '#a1a1aa', light: '#52525b' },
  { id: 'black', label: 'Black', dark: '#fafafa', light: '#18181b' }
]
