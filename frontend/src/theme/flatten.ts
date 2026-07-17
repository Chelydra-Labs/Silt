// Frontend Flatten mirror of backend/themes/theme.go Theme.Flatten.
// Used by the custom theme editor for same-frame live preview via injectTokens.
// Save always re-validates and flattens on the Go side; this path only needs
// fidelity for interactive preview of the keys the UI actually paints from.

import type {
  AccentTriple,
  Background,
  Mode,
  Surface,
  SurfaceZone,
  Surfaces,
  ThemeDoc,
  ThemeModeKey,
  Typography
} from './types'
import { SURFACE_PARENT, SURFACE_ZONES } from './types'
import { deriveInkOnAccent, effectiveAccentFill } from './color'

interface ZoneCss {
  name: SurfaceZone
  parent: SurfaceZone | ''
  cssBg: string
  cssBorder: string
  cssText: string
  cssTextMuted: string
  cssTextDisabled: string
}

const ZONE_CSS: ZoneCss[] = SURFACE_ZONES.map((name) => ({
  name,
  parent: SURFACE_PARENT[name],
  cssBg: `--color-surface-${name}`,
  cssBorder: `--color-surface-${name}-border`,
  cssText: `--color-surface-${name}-text`,
  cssTextMuted: `--color-surface-${name}-text-muted`,
  cssTextDisabled: `--color-surface-${name}-text-disabled`
}))

function zoneByName(name: SurfaceZone | ''): ZoneCss | undefined {
  if (!name) return undefined
  return ZONE_CSS.find((z) => z.name === name)
}

function getSurface(s: Surfaces, zone: SurfaceZone): Surface | undefined {
  if (zone === 'app') return s.app
  return s[zone]
}

function emitBackground(
  out: Record<string, string>,
  zone: SurfaceZone,
  b: Background | undefined
): void {
  if (!b) return
  out[`--silt-bg-${zone}-display`] = 'block'
  const image = b.image?.trim()
  if (image) out[`--silt-bg-${zone}-image`] = image
  const size = b.size?.trim()
  if (size) out[`--silt-bg-${zone}-size`] = size
  // Always emit opacity when a background block is present so CSS does not
  // inherit a stale value from a previous theme (default 0 when unset).
  out[`--silt-bg-${zone}-opacity`] = String(b.opacity ?? 0)
  const blend = b.blend?.trim()
  if (blend) out[`--silt-bg-${zone}-blend`] = blend
  const position = b.position?.trim()
  if (position) out[`--silt-bg-${zone}-position`] = position
  const scrim = b.scrim?.trim()
  if (scrim) out[`--silt-bg-${zone}-scrim`] = scrim
}

function emitSurface(
  out: Record<string, string>,
  z: ZoneCss,
  s: Surface | undefined
): void {
  // Incomplete shells (e.g. after resetPath deleted a leaf on an inherited
  // zone) fall through to parent var() chains instead of crashing on .trim().
  const bg = s?.bg?.trim()
  const border = s?.border?.trim()
  const text = s?.text?.trim()
  if (s && bg && border && text) {
    out[z.cssBg] = bg
    out[z.cssBorder] = border
    out[z.cssText] = text
    const muted = s.text_muted?.trim()
    out[z.cssTextMuted] = muted || 'var(--color-text-muted)'
    const disabled = s.text_disabled?.trim()
    out[z.cssTextDisabled] = disabled || 'var(--color-text-disabled)'
    emitBackground(out, z.name, s.background)
    return
  }
  const p = zoneByName(z.parent)
  if (!p) return
  out[z.cssBg] = `var(${p.cssBg})`
  out[z.cssBorder] = `var(${p.cssBorder})`
  out[z.cssText] = `var(${p.cssText})`
  out[z.cssTextMuted] = `var(${p.cssTextMuted})`
  out[z.cssTextDisabled] = `var(${p.cssTextDisabled})`
}

function flattenGeometry(out: Record<string, string>, m: Mode): void {
  if (m.radius) {
    out['--radius-sm'] = m.radius.sm.trim()
    out['--radius-md'] = m.radius.md.trim()
    out['--radius-lg'] = m.radius.lg.trim()
    out['--radius-xl'] = m.radius.xl.trim()
    out['--radius-full'] = m.radius.full.trim()
  } else {
    out['--radius-sm'] = '4px'
    out['--radius-md'] = '8px'
    out['--radius-lg'] = '12px'
    out['--radius-xl'] = '16px'
    out['--radius-full'] = '9999px'
  }
  if (m.spacing) {
    out['--spacing-sm'] = m.spacing.sm.trim()
    out['--spacing-md'] = m.spacing.md.trim()
    out['--spacing-lg'] = m.spacing.lg.trim()
    out['--spacing-xl'] = m.spacing.xl.trim()
  } else {
    out['--spacing-sm'] = '4px'
    out['--spacing-md'] = '8px'
    out['--spacing-lg'] = '16px'
    out['--spacing-xl'] = '24px'
  }
  if (m.shadow) {
    out['--shadow-sm'] = m.shadow.sm.trim()
    out['--shadow-md'] = m.shadow.md.trim()
    out['--shadow-lg'] = m.shadow.lg.trim()
  } else {
    out['--shadow-sm'] =
      '0 1px 2px color-mix(in oklch, var(--color-surface-app) 40%, transparent)'
    out['--shadow-md'] =
      '0 4px 12px color-mix(in oklch, var(--color-surface-app) 35%, transparent)'
    out['--shadow-lg'] =
      '0 12px 32px color-mix(in oklch, var(--color-surface-app) 30%, transparent)'
  }
}

function flattenEditor(out: Record<string, string>, m: Mode): void {
  if (m.editor) {
    out['--color-editor-caret'] = m.editor.caret.trim()
    out['--color-editor-selection'] = m.editor.selection.trim()
    out['--color-editor-selection-text'] = m.editor.selection_text.trim()
    out['--color-editor-link'] = m.editor.link.trim()
    out['--color-editor-link-hover'] = m.editor.link_hover.trim()
    out['--color-editor-highlight'] = m.editor.highlight.trim()
    return
  }
  out['--color-editor-caret'] = 'var(--color-accent-primary-start)'
  out['--color-editor-selection'] =
    'color-mix(in oklch, var(--color-accent-primary-start) 30%, transparent)'
  out['--color-editor-selection-text'] = 'var(--color-surface-editor-text)'
  out['--color-editor-link'] = 'var(--color-accent-secondary-start)'
  out['--color-editor-link-hover'] = 'var(--color-accent-secondary-end)'
  out['--color-editor-highlight'] =
    'color-mix(in oklch, var(--color-status-warn) 40%, transparent)'
}

function flattenEffects(out: Record<string, string>, m: Mode): void {
  const focus = m.focus_glow?.trim()
  out['--focus-glow'] = focus || 'none'
  const border = m.border_glow?.trim()
  out['--border-glow'] = border || 'none'
}

function flattenNavIcons(out: Record<string, string>, m: Mode): void {
  const canonical = ['notes', 'tags', 'calendar', 'tasks', 'kanban', 'settings']
  for (const id of canonical) {
    out[`--color-nav-icon-${id}`] =
      'var(--color-surface-activitybar-text-muted)'
  }
  if (m.nav_icons) {
    for (const [k, v] of Object.entries(m.nav_icons)) {
      const val = v?.trim()
      if (val) out[`--color-nav-icon-${k}`] = val
    }
  }
}

function flattenTypography(
  out: Record<string, string>,
  ty: Typography | undefined
): void {
  if (!ty) return
  const headline = ty.headline_font?.trim()
  if (headline) out['--font-headline'] = headline
  const body = ty.font_family?.trim()
  if (body) out['--font-body'] = body
  const mono = ty.mono_font_family?.trim()
  if (mono) out['--font-mono'] = mono
  const sc = ty.scale
  if (!sc) return
  if (sc.size) {
    for (const [k, v] of Object.entries(sc.size)) {
      const vv = v?.trim()
      if (vv) out[`--font-size-${k}`] = vv
    }
  }
  if (sc.line_height) {
    for (const [k, v] of Object.entries(sc.line_height)) {
      const vv = v?.trim()
      if (vv) out[`--line-height-${k}`] = vv
    }
  }
  if (sc.weight) {
    for (const [k, v] of Object.entries(sc.weight)) {
      const vv = v?.trim()
      if (vv) out[`--font-weight-${k}`] = vv
    }
  }
}

/**
 * Flatten a ThemeDoc for the given mode into CSS custom-property tokens,
 * matching backend/themes Theme.Flatten key set for surfaces, interaction,
 * accents, status, error, geometry, editor, effects, nav icons, typography,
 * and background overlays.
 */
export function flattenTheme(
  doc: ThemeDoc,
  mode: ThemeModeKey
): Record<string, string> {
  const m = mode === 'light' ? doc.modes.light : doc.modes.dark
  const out: Record<string, string> = {}

  for (const z of ZONE_CSS) {
    emitSurface(out, z, getSurface(m.surfaces, z.name))
  }

  out['--color-hover'] = m.hover.trim()
  out['--color-active'] = m.active.trim()
  out['--color-border-active'] = m.border_active.trim()
  out['--color-border-focus'] = m.border_focus.trim()

  out['--color-text-primary'] = 'var(--color-surface-app-text)'
  out['--color-text-muted'] = m.text_muted.trim()
  out['--color-text-disabled'] = m.text_disabled.trim()

  // App surface is the CTA backdrop for compositing translucent starts.
  const appBG = m.surfaces.app.bg?.trim() ?? ''
  out['--color-accent-primary-start'] = m.accent.primary.start
  out['--color-accent-primary-end'] = m.accent.primary.end
  out['--color-accent-primary-glow'] = m.accent.primary.glow
  out['--color-accent-primary-on'] = resolveAccentOn(m.accent.primary, appBG)
  out['--color-accent-secondary-start'] = m.accent.secondary.start
  out['--color-accent-secondary-end'] = m.accent.secondary.end
  out['--color-accent-secondary-glow'] = m.accent.secondary.glow
  out['--color-accent-secondary-on'] = resolveAccentOn(
    m.accent.secondary,
    appBG
  )
  out['--color-text-on-accent'] = out['--color-accent-primary-on']

  out['--color-status-warn'] = m.status.warn
  out['--color-status-danger'] = m.status.danger
  out['--color-status-success'] = m.status.success

  out['--color-error'] = m.error.fg
  out['--color-error-bg'] = m.error.bg
  out['--color-error-border'] = m.error.border

  flattenGeometry(out, m)
  flattenEditor(out, m)
  flattenEffects(out, m)
  flattenNavIcons(out, m)
  flattenTypography(out, doc.typography)

  return out
}

function resolveAccentOn(t: AccentTriple, surfaceBG: string): string {
  const on = t.on?.trim()
  if (on) return on
  return deriveInkOnAccent(effectiveAccentFill(t.start, surfaceBG))
}
