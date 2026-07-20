// Color palette for text/background color pickers (#170, #408).
//
// The palette has two sections: a theme row derived from the active theme's
// color anchors (accents, status, neutral), and a fixed standard row of
// stable pigments that don't change with theme. Splitting them gives users
// both theme-tinted accents and reliably-named pigments. Theme-derived
// entries each produce a dark variant (raised OKLCH lightness, legible on
// dark surfaces) and a light variant (lowered lightness, legible on light
// surfaces); standard entries use fixed Tailwind-600/400-tier hex values
// directly. The user's stored mark color is always authoritative — only
// the initial default swatch row is theme-derived.

import { toOklch, toHex } from '../../theme/color'

export interface ColorEntry {
  id: string
  label: string
  dark: string
  light: string
}

export interface ResolvedPalette {
  // Up to 6 entries derived from the active theme's anchor tokens.
  theme: ColorEntry[]
  // Always equals FIXED_COLOR_PALETTE.
  standard: ColorEntry[]
}

// Target OKLCH lightness for the derived theme-row variants. The dark
// variant sits bright enough to read against a dark surface (~0.72,
// matching the prior Tailwind-400 tier); the light variant sits deep
// enough to read against a light surface (~0.45, matching Tailwind-600).
// These are per-seed targets — the exact value is clamped to [0,1] and
// the seed's chroma/hue are preserved so the swatch stays in the theme's
// color family.
const DARK_TARGET_L = 0.72
const LIGHT_TARGET_L = 0.45

// A seed is a named slot that pulls its color from a theme anchor token.
// If the token is absent the seed is skipped (the palette shrinks gracefully).
interface PaletteSeed {
  id: string
  label: string
  token: string // CSS custom property name on :root
}

// The canonical theme-row seeds, ordered by role prominence so the row
// reads primary-first. Every first-class theme defines these anchors (they
// are required by the v2 schema's status blocks + the accent block). Labels
// are SEMANTIC ROLES, not pigments — the actual hue is theme-derived, so
// calling a swatch "Blue" when Terra Noir's primary accent is orange
// (#c2410c) would feed a screen-reader user the wrong name. The role stays
// accurate regardless of the theme's hue choice.
const SEEDS: PaletteSeed[] = [
  { id: 'primary', label: 'Primary', token: '--color-accent-primary-start' },
  {
    id: 'secondary',
    label: 'Secondary',
    token: '--color-accent-secondary-start'
  },
  { id: 'success', label: 'Success', token: '--color-status-success' },
  { id: 'warn', label: 'Warning', token: '--color-status-warn' },
  { id: 'danger', label: 'Danger', token: '--color-status-danger' }
]

// The neutral swatch (gray) is not hue-anchored; it is derived from the
// theme's text-muted token so a "gray" highlight matches the theme's
// neutrality (warm gray on Linen, cool gray on Frost).
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

// Fixed standard pigment row: stable across themes so users have reliable
// color names available regardless of theme hue. Values use Tailwind's
// *-600 tier for light mode and *-400 tier for dark mode.
export const FIXED_COLOR_PALETTE: ColorEntry[] = [
  { id: 'red', label: 'Red', light: '#dc2626', dark: '#f87171' },
  { id: 'orange', label: 'Orange', light: '#ea580c', dark: '#fb923c' },
  { id: 'amber', label: 'Amber', light: '#d97706', dark: '#fbbf24' },
  { id: 'yellow', label: 'Yellow', light: '#ca8a04', dark: '#facc15' },
  { id: 'lime', label: 'Lime', light: '#65a30d', dark: '#a3e635' },
  { id: 'green', label: 'Green', light: '#16a34a', dark: '#4ade80' },
  { id: 'teal', label: 'Teal', light: '#0d9488', dark: '#2dd4bf' },
  { id: 'cyan', label: 'Cyan', light: '#0891b2', dark: '#22d3ee' },
  { id: 'blue', label: 'Blue', light: '#2563eb', dark: '#60a5fa' },
  { id: 'indigo', label: 'Indigo', light: '#4f46e5', dark: '#818cf8' },
  { id: 'violet', label: 'Violet', light: '#7c3aed', dark: '#a78bfa' },
  { id: 'pink', label: 'Pink', light: '#db2777', dark: '#f472b6' }
]

// FALLBACK_THEME_PALETTE backs the theme row only — before the theme has
// injected its tokens (first paint on a cold start) or if every hue seed
// fails to parse. It is a 6-entry row with reasonable role defaults
// adapted from the standard palette so the cold-start→theme-load transition
// doesn't jolt. The standard row never falls back — it is always
// FIXED_COLOR_PALETTE.
export const FALLBACK_THEME_PALETTE: ColorEntry[] = [
  { id: 'primary', label: 'Primary', dark: '#60a5fa', light: '#2563eb' },
  { id: 'secondary', label: 'Secondary', dark: '#818cf8', light: '#4f46e5' },
  { id: 'success', label: 'Success', dark: '#4ade80', light: '#16a34a' },
  { id: 'warn', label: 'Warning', dark: '#facc15', light: '#ca8a04' },
  { id: 'danger', label: 'Danger', dark: '#f87171', light: '#dc2626' },
  { id: 'gray', label: 'Gray', dark: '#a1a1aa', light: '#52525b' }
]

/**
 * Build the resolved palette from a map of CSS custom properties
 * (token → value). Theme-row seeds whose token is missing are skipped, so
 * a theme that omits an anchor simply produces a shorter row rather than a
 * gap. Falls back to FALLBACK_THEME_PALETTE if too few theme seeds resolve
 * (defensive — the editor always has a usable theme row). The standard
 * row is always FIXED_COLOR_PALETTE regardless of input.
 *
 * Pure function (no DOM access) so it is unit-testable with fixture tokens.
 */
export function deriveColorPalette(
  tokens: Record<string, string>
): ResolvedPalette {
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

  // Guarantee a minimum usable theme row even if every hue seed failed to
  // parse (should not happen on a valid theme, but fail-open beats an
  // empty row). Use the fallback row when fewer than 2 seeds resolved.
  const theme = entries.length < 2 ? FALLBACK_THEME_PALETTE : entries
  return { theme, standard: FIXED_COLOR_PALETTE }
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
