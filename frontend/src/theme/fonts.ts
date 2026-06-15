// Preloaded font registry (#82) — the single source of truth for the fonts
// the font picker (Settings → General) offers and for the theme-typography
// override indicator (Settings → Appearance).
//
// Fonts are bundled offline via @fontsource (woff2 self-hosted by Vite — no
// Google Fonts CDN at runtime, preserving Silt's local-first guarantee). We
// import a bounded weight set (400/500/600/700, the weights the UI actually
// uses) per family rather than every weight, to keep the bundle small. Space
// Mono ships only Regular + Bold by the type designer's intent, so it imports
// just those.
//
// Each entry's `cssFamily` is the value stored in `config.editor.font_family`
// / `mono_font_family` and the value injected as `--editor-font-family`. It is
// the bare family name (e.g. "Plus Jakarta Sans"), matching the config.yaml
// default convention; CSS accepts an unquoted multi-word family name as a
// sequence of identifiers. The picker renders each option in its own font by
// applying `style="font-family: <cssFamily>"`.

// --- Body / sans-serif ----------------------------------------------------
import '@fontsource/plus-jakarta-sans/400.css'
import '@fontsource/plus-jakarta-sans/500.css'
import '@fontsource/plus-jakarta-sans/600.css'
import '@fontsource/plus-jakarta-sans/700.css'
import '@fontsource/inter/400.css'
import '@fontsource/inter/500.css'
import '@fontsource/inter/600.css'
import '@fontsource/inter/700.css'
import '@fontsource/lexend/400.css'
import '@fontsource/lexend/500.css'
import '@fontsource/lexend/600.css'
import '@fontsource/lexend/700.css'
import '@fontsource/work-sans/400.css'
import '@fontsource/work-sans/500.css'
import '@fontsource/work-sans/600.css'
import '@fontsource/work-sans/700.css'
import '@fontsource/manrope/400.css'
import '@fontsource/manrope/500.css'
import '@fontsource/manrope/600.css'
import '@fontsource/manrope/700.css'

// --- Monospace ------------------------------------------------------------
import '@fontsource/jetbrains-mono/400.css'
import '@fontsource/jetbrains-mono/500.css'
import '@fontsource/jetbrains-mono/600.css'
import '@fontsource/jetbrains-mono/700.css'
import '@fontsource/fira-code/400.css'
import '@fontsource/fira-code/500.css'
import '@fontsource/fira-code/600.css'
import '@fontsource/fira-code/700.css'
import '@fontsource/ibm-plex-mono/400.css'
import '@fontsource/ibm-plex-mono/500.css'
import '@fontsource/ibm-plex-mono/600.css'
import '@fontsource/ibm-plex-mono/700.css'
import '@fontsource/space-mono/400.css'
import '@fontsource/space-mono/700.css'

// --- Display / headline ---------------------------------------------------
import '@fontsource/hanken-grotesk/400.css'
import '@fontsource/hanken-grotesk/500.css'
import '@fontsource/hanken-grotesk/600.css'
import '@fontsource/hanken-grotesk/700.css'
import '@fontsource/sora/400.css'
import '@fontsource/sora/500.css'
import '@fontsource/sora/600.css'
import '@fontsource/sora/700.css'
import '@fontsource/bricolage-grotesque/400.css'
import '@fontsource/bricolage-grotesque/500.css'
import '@fontsource/bricolage-grotesque/600.css'
import '@fontsource/bricolage-grotesque/700.css'

export type FontCategory = 'sans' | 'mono' | 'display'
export type FontSource = 'bundled' | 'system'

export interface FontEntry {
  /** Stable registry key (also the theme-typography reference id). */
  id: string
  /** Human-readable name shown in the dropdown. */
  displayName: string
  /** Value stored in config and injected as the CSS font-family. */
  cssFamily: string
  category: FontCategory
  source: FontSource
}

/**
 * The curated, bundled font set. The three defaults (Plus Jakarta Sans /
 * JetBrains Mono / Hanken Grotesk) match the canonical default theme's
 * typography block and the config.yaml defaults, so a fresh install renders
 * in those families without any user action.
 */
export const FONT_REGISTRY: FontEntry[] = [
  // Sans-serif body fonts
  { id: 'plus-jakarta-sans', displayName: 'Plus Jakarta Sans', cssFamily: 'Plus Jakarta Sans', category: 'sans', source: 'bundled' },
  { id: 'inter', displayName: 'Inter', cssFamily: 'Inter', category: 'sans', source: 'bundled' },
  { id: 'lexend', displayName: 'Lexend', cssFamily: 'Lexend', category: 'sans', source: 'bundled' },
  { id: 'work-sans', displayName: 'Work Sans', cssFamily: 'Work Sans', category: 'sans', source: 'bundled' },
  { id: 'manrope', displayName: 'Manrope', cssFamily: 'Manrope', category: 'sans', source: 'bundled' },
  // Monospace fonts
  { id: 'jetbrains-mono', displayName: 'JetBrains Mono', cssFamily: 'JetBrains Mono', category: 'mono', source: 'bundled' },
  { id: 'fira-code', displayName: 'Fira Code', cssFamily: 'Fira Code', category: 'mono', source: 'bundled' },
  { id: 'ibm-plex-mono', displayName: 'IBM Plex Mono', cssFamily: 'IBM Plex Mono', category: 'mono', source: 'bundled' },
  { id: 'space-mono', displayName: 'Space Mono', cssFamily: 'Space Mono', category: 'mono', source: 'bundled' },
  // Display / headline fonts
  { id: 'hanken-grotesk', displayName: 'Hanken Grotesk', cssFamily: 'Hanken Grotesk', category: 'display', source: 'bundled' },
  { id: 'sora', displayName: 'Sora', cssFamily: 'Sora', category: 'display', source: 'bundled' },
  { id: 'bricolage-grotesque', displayName: 'Bricolage Grotesque', cssFamily: 'Bricolage Grotesque', category: 'display', source: 'bundled' },
  // System fallbacks (always available offline; no bundled files)
  { id: 'system-ui', displayName: 'System UI', cssFamily: 'system-ui', category: 'sans', source: 'system' },
  { id: 'sans-serif', displayName: 'Sans Serif (generic)', cssFamily: 'sans-serif', category: 'sans', source: 'system' },
  { id: 'monospace', displayName: 'Monospace (generic)', cssFamily: 'monospace', category: 'mono', source: 'system' },
]

/** Registry ids of the three default families (theme + config defaults). */
export const DEFAULT_BODY_ID = 'plus-jakarta-sans'
export const DEFAULT_MONO_ID = 'jetbrains-mono'
export const DEFAULT_HEADLINE_ID = 'hanken-grotesk'

/** The bundled (non-system) entries of a given category, in registry order. */
export function bundledByCategory(category: FontCategory): FontEntry[] {
  return FONT_REGISTRY.filter((f) => f.source === 'bundled' && f.category === category)
}

/** The system-fallback entries (rendered in their own optgroup). */
export function systemFonts(): FontEntry[] {
  return FONT_REGISTRY.filter((f) => f.source === 'system')
}

/** Look up an entry by its cssFamily (the value stored in config). */
export function findByCssFamily(cssFamily: string): FontEntry | undefined {
  return FONT_REGISTRY.find((f) => f.cssFamily === cssFamily)
}

/**
 * Resolve a config font-family value to a human-readable display name. Falls
 * back to the raw value when it isn't in the registry (e.g. a hand-edited
 * config.yaml from before the picker existed) so the picker never shows a
 * blank for a value it doesn't curate.
 */
export function displayNameForCssFamily(cssFamily: string): string {
  return findByCssFamily(cssFamily)?.displayName ?? cssFamily
}
