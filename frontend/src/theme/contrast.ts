// Contrast guidance for the custom theme editor. Warns never block save
// (RFC D12). Math reuses color.ts WCAG ratio; classification bands and
// OKLCH lightness auto-fix live here so the UI stays presentation-only.

import {
  clampL,
  contrastRatioWCAG,
  formatOklch,
  toOklch,
  type Oklch
} from './color'

export { contrastRatioWCAG } from './color'

export type ContrastLevel = 'pass' | 'warn' | 'fail'

/**
 * Classify a contrast ratio for text (default) or UI chrome pairs.
 * Text AA: pass ≥ 4.5, warn 4.0–4.5, fail < 4.0.
 * UI AA: pass ≥ 3.0, warn 2.5–3.0, fail < 2.5.
 */
export function classifyContrast(
  ratio: number | null,
  text = true
): ContrastLevel {
  if (ratio === null || !Number.isFinite(ratio)) return 'fail'
  if (text) {
    if (ratio >= 4.5) return 'pass'
    if (ratio >= 4.0) return 'warn'
    return 'fail'
  }
  if (ratio >= 3.0) return 'pass'
  if (ratio >= 2.5) return 'warn'
  return 'fail'
}

/**
 * Clamp foreground OKLCH lightness toward a target WCAG ratio against bg.
 * Preserves polarity: if fg is lighter than bg, L only increases; if darker,
 * L only decreases. Returns a new color string in the seed's format preference
 * (oklch stays oklch; otherwise hex-like seeds still format as oklch for
 * precision), or null if unparseable / already at or above target / no fix.
 */
export function autoFixLightness(
  fg: string,
  bg: string,
  targetRatio = 4.5
): string | null {
  const fgLch = toOklch(fg)
  const bgLch = toOklch(bg)
  if (!fgLch || !bgLch) return null

  const current = contrastRatioWCAG(fg, bg)
  if (current !== null && current >= targetRatio) return null

  // Polarity from OKLCH L (perceptually ordered; matches "fg lighter than bg").
  const fgLighter = fgLch.L >= bgLch.L
  const lo = fgLighter ? fgLch.L : 0
  const hi = fgLighter ? 1 : fgLch.L

  // Binary search the L that just meets targetRatio along the allowed half.
  let best: Oklch | null = null
  let a = lo
  let b = hi
  for (let i = 0; i < 24; i++) {
    const mid = (a + b) / 2
    const candidate: Oklch = {
      L: clampL(mid),
      C: fgLch.C,
      H: fgLch.H,
      ...(fgLch.alpha !== undefined ? { alpha: fgLch.alpha } : {})
    }
    const formatted = formatOklch(candidate)
    const ratio = contrastRatioWCAG(formatted, bg)
    if (ratio !== null && ratio >= targetRatio) {
      best = candidate
      // Tighten toward original L (smallest change that still passes).
      if (fgLighter) b = mid
      else a = mid
    } else {
      if (fgLighter) a = mid
      else b = mid
    }
  }

  if (!best) {
    // Push to the extreme of the allowed direction as a last resort.
    const extreme: Oklch = {
      L: fgLighter ? 1 : 0,
      C: fgLch.C,
      H: fgLch.H,
      ...(fgLch.alpha !== undefined ? { alpha: fgLch.alpha } : {})
    }
    const formatted = formatOklch(extreme)
    const ratio = contrastRatioWCAG(formatted, bg)
    if (ratio === null || ratio < targetRatio) return null
    best = extreme
  }

  // Prefer oklch when the seed was oklch; otherwise still emit oklch so the
  // fixed L is exact (hex quantization would re-break borderline pairs).
  return formatOklch(best)
}

export interface ContrastPair {
  id: string
  label: string
  fg: string
  bg: string
  ratio: number | null
  level: ContrastLevel
}

/**
 * Effective background for text-on-image contrast.
 *
 * v1 does not sample image luminance. When a scrim is set it is the authoring
 * control for readability and is used as the effective background; otherwise
 * the solid zone fallback is used. `imageRef` / `opacity` are reserved for a
 * future sampling path.
 */
export function effectiveBackgroundWithScrim(
  imageRef: string | undefined,
  scrim: string | undefined,
  opacity: number | undefined,
  solidFallback: string
): string {
  void imageRef
  void opacity
  const s = scrim?.trim()
  if (s) return s
  return solidFallback
}

/**
 * Core readability pairs for the editor summary strip. Resolves only concrete
 * (non-var()) token values; missing keys yield fail with null ratio.
 * App/editor pairs prefer `--silt-bg-*-scrim` as the effective bg when present
 * (text-on-image authoring control; image luminance not sampled yet).
 */
export function coreContrastPairs(
  tokens: Record<string, string>
): ContrastPair[] {
  const pairs: {
    id: string
    label: string
    fgKey: string
    bgKey: string
    scrimKey?: string
    imageKey?: string
  }[] = [
    {
      id: 'app-text',
      label: 'App text on app background',
      fgKey: '--color-surface-app-text',
      bgKey: '--color-surface-app',
      scrimKey: '--silt-bg-app-scrim',
      imageKey: '--silt-bg-app-image'
    },
    {
      id: 'muted-text',
      label: 'Muted text on app background',
      fgKey: '--color-text-muted',
      bgKey: '--color-surface-app',
      scrimKey: '--silt-bg-app-scrim',
      imageKey: '--silt-bg-app-image'
    },
    {
      id: 'accent',
      label: 'Accent on app background',
      fgKey: '--color-accent-primary-start',
      bgKey: '--color-surface-app',
      scrimKey: '--silt-bg-app-scrim',
      imageKey: '--silt-bg-app-image'
    },
    {
      id: 'error',
      label: 'Error text on error background',
      fgKey: '--color-error',
      bgKey: '--color-error-bg'
    },
    {
      id: 'editor-text',
      label: 'Editor text on editor background',
      fgKey: '--color-surface-editor-text',
      bgKey: '--color-surface-editor',
      scrimKey: '--silt-bg-editor-scrim',
      imageKey: '--silt-bg-editor-image'
    }
  ]

  return pairs.map(({ id, label, fgKey, bgKey, scrimKey, imageKey }) => {
    const fg = resolveConcrete(tokens, fgKey)
    const solidBg = resolveConcrete(tokens, bgKey)
    const scrim = scrimKey ? resolveConcrete(tokens, scrimKey) : undefined
    const image = imageKey ? resolveConcrete(tokens, imageKey) : undefined
    const bg =
      solidBg != null
        ? effectiveBackgroundWithScrim(image, scrim, undefined, solidBg)
        : (scrim ?? undefined)
    const ratio =
      fg && bg && !fg.startsWith('var(') && !bg.startsWith('var(')
        ? contrastRatioWCAG(fg, bg)
        : null
    return {
      id,
      label,
      fg: fg ?? '',
      bg: bg ?? '',
      ratio,
      level: classifyContrast(ratio, true)
    }
  })
}

/** Follow one level of var(--name) so inherited editor/app text still measures. */
function resolveConcrete(
  tokens: Record<string, string>,
  key: string,
  depth = 0
): string | undefined {
  if (depth > 6) return undefined
  const v = tokens[key]
  if (!v) return undefined
  const m = v.match(/^var\(\s*(--[\w-]+)\s*\)$/)
  if (m) return resolveConcrete(tokens, m[1], depth + 1)
  return v
}
