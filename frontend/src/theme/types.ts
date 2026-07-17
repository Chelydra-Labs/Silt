// Theme System v2 document types — mirror backend/themes/theme.go and
// docs/theme-system-v2-rfc.md. Used by the custom theme editor working copy
// and FE flatten for live preview.

export const SURFACE_ZONES = [
  'app',
  'sidebar',
  'editor',
  'panel',
  'card',
  'modal',
  'popover',
  'titlebar',
  'activitybar'
] as const

export type SurfaceZone = (typeof SURFACE_ZONES)[number]

/** Parent zone for inheritance (RFC §5). Empty string = root (app). */
export const SURFACE_PARENT: Record<SurfaceZone, SurfaceZone | ''> = {
  app: '',
  sidebar: 'app',
  editor: 'app',
  panel: 'app',
  card: 'panel',
  modal: 'panel',
  popover: 'modal',
  titlebar: 'app',
  activitybar: 'app'
}

export interface ThemeDoc {
  schema_version: string
  id: string
  name: string
  author?: string
  description?: string
  typography?: Typography
  modes: { dark: Mode; light: Mode }
}

export interface Typography {
  font_family?: string
  mono_font_family?: string
  headline_font?: string
  scale?: TypeScale
}

export interface TypeScale {
  size?: Record<string, string>
  line_height?: Record<string, string>
  weight?: Record<string, string>
}

export interface Mode {
  surfaces: Surfaces
  hover: string
  active: string
  border_active: string
  border_focus: string
  text_muted: string
  text_disabled: string
  accent: {
    primary: AccentTriple
    secondary: AccentTriple
  }
  status: { warn: string; danger: string; success: string }
  error: { fg: string; bg: string; border: string }
  radius?: Radius
  spacing?: Spacing
  shadow?: Shadow
  editor?: EditorTokens
  focus_glow?: string
  border_glow?: string
  nav_icons?: Record<string, string>
}

export interface Surfaces {
  app: Surface
  sidebar?: Surface
  editor?: Surface
  panel?: Surface
  card?: Surface
  modal?: Surface
  popover?: Surface
  titlebar?: Surface
  activitybar?: Surface
}

export interface Surface {
  bg: string
  border: string
  text: string
  text_muted?: string
  text_disabled?: string
  background?: Background
}

export interface Background {
  image?: string
  size?: string
  opacity?: number
  blend?: string
  position?: string
  scrim?: string
}

export interface AccentTriple {
  start: string
  end: string
  glow: string
  /** Label ink for solid fills using start. Omitted → derived at flatten. */
  on?: string
}

export interface Radius {
  sm: string
  md: string
  lg: string
  xl: string
  full: string
}

export interface Spacing {
  sm: string
  md: string
  lg: string
  xl: string
}

export interface Shadow {
  sm: string
  md: string
  lg: string
}

export interface EditorTokens {
  caret: string
  selection: string
  selection_text: string
  link: string
  link_hover: string
  highlight: string
}

export type ThemeModeKey = 'dark' | 'light'

export type AdvancedGroup =
  'surfaces' | 'color' | 'typography' | 'geometry' | 'editor' | 'background'
