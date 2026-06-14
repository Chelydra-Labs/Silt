// Theme store (#46, #47): holds the active theme/mode and the dark/light
// token maps, the listing of available themes, subscribes to the backend
// GetActiveTheme / ApplyTheme / ListThemes IPC methods, re-resolves "system"
// mode locally via prefers-color-scheme, and drives the runtime injector.
// Svelte 5 $state runes in a .svelte.ts module (matches
// plugins/store.svelte.ts and settings/store.svelte.ts).
import {
  ApplyTheme,
  GetActiveTheme,
  ListThemes
} from '../../wailsjs/go/main/App.js'
import { EventsOn } from '../../wailsjs/runtime/runtime.js'
import type { themes } from '../../wailsjs/go/models'
import { injectTokens } from './inject'

export type ThemeMode = 'dark' | 'light' | 'system'

export interface ThemeState {
  id: string
  name: string
  mode: ThemeMode
  darkTokens: Record<string, string>
  lightTokens: Record<string, string>
  /** Last error from a theme IPC call (surfaced so the UI can show it). */
  error: string | null
}

export const themeState: ThemeState = $state({
  id: '',
  name: '',
  mode: 'dark',
  darkTokens: {},
  lightTokens: {},
  error: null
})

/**
 * Listing store (#47): every theme currently selectable, populated from
 * `ListThemes()`. Decoupled from the active theme (themeState): the
 * picker renders `themesState.items` and the highlighted row matches
 * `themeState.id`. Re-fetched on the backend's `themes:changed` event
 * (emitted by the importer — see backend/themes/importer.go) so an
 * imported theme appears in the picker immediately, no restart.
 */
export interface ThemesListingState {
  items: themes.ThemeInfo[]
  loadError: string | null
  loading: boolean
}

export const themesState: ThemesListingState = $state({
  items: [],
  loadError: null,
  loading: false
})

let schemeMedia: MediaQueryList | null = null
let started = false
let themesStarted = false
let offThemesChanged: (() => void) | null = null

/** Returns true when the OS prefers light mode (used to resolve "system").
 * Reads the cached MQL rather than allocating one per repaint; null pre-init
 * or with no window → default to dark. The query is explicitly for "light"
 * so the (rare) "no preference" state stays dark, matching the prior
 * per-call semantics. */
function osPrefersLight(): boolean {
  return schemeMedia ? schemeMedia.matches : false
}

/** Pick the concrete token map for the active mode, resolving "system". */
function effectiveTokens(s: ThemeState): Record<string, string> {
  if (s.mode === 'light') return s.lightTokens
  if (s.mode === 'dark') return s.darkTokens
  return osPrefersLight() ? s.lightTokens : s.darkTokens
}

/** Re-inject the effective tokens for the current state (same-tick). */
function repaint(): void {
  injectTokens(effectiveTokens(themeState))
}

/**
 * Initialize the theme engine on startup. Loads the active theme over IPC,
 * injects it before/with the first meaningful paint, and wires up the
 * "system" mode listener + theme:changed event. Safe to call once.
 */
export async function initTheme(): Promise<void> {
  if (started) return
  started = true

  // Watch prefers-color-scheme so "system" mode follows the OS live, with
  // no second IPC round-trip (both token maps are already in hand). The
  // cached MQL is the "light" query so osPrefersLight can read .matches
  // directly; its change listener fires on any dark↔light transition.
  if (typeof window !== 'undefined' && window.matchMedia) {
    schemeMedia = window.matchMedia('(prefers-color-scheme: light)')
    schemeMedia.addEventListener('change', () => {
      if (themeState.mode === 'system') repaint()
    })
  }

  // Re-paint when the backend reports a theme change. The event carries the
  // resolved {id, mode}; if it matches what this window already applied
  // (the common case -- our own applyTheme call), skip the redundant
  // GetActiveTheme round-trip + re-inject. Falls through to a re-fetch when
  // the change is external or the local state hasn't caught up yet.
  EventsOn(
    'theme:changed',
    async (payload: { id?: string; mode?: string } | null) => {
      if (
        payload &&
        payload.id === themeState.id &&
        payload.mode === themeState.mode
      ) {
        return
      }
      try {
        const res = await GetActiveTheme()
        applyResult(res)
      } catch (err) {
        console.error('theme: failed to apply theme:changed event:', err)
        themeState.error = err instanceof Error ? err.message : String(err)
      }
    }
  )

  try {
    const res = await GetActiveTheme()
    applyResult(res)
  } catch (err) {
    console.error('theme: failed to load active theme on startup:', err)
    themeState.error = err instanceof Error ? err.message : String(err)
    // On error the shell still renders from the index.css :root fallbacks;
    // initTheme is fire-and-forget so nothing blocks on a loader.
  }
}

/** Apply an IPC result to the store and inject the effective tokens. */
function applyResult(res: {
  id: string
  name: string
  mode: string
  dark_tokens: Record<string, string>
  light_tokens: Record<string, string>
}): void {
  themeState.id = res.id
  themeState.name = res.name
  themeState.mode = (res.mode as ThemeMode) || 'dark'
  themeState.darkTokens = res.dark_tokens || {}
  themeState.lightTokens = res.light_tokens || {}
  themeState.error = null
  repaint()
}

/**
 * Switch to a theme/mode, persist it via the backend, and inject the result.
 * Returns true on success.
 */
export async function applyTheme(
  id: string,
  mode: ThemeMode
): Promise<boolean> {
  try {
    const res = await ApplyTheme(id, mode)
    applyResult(res)
    return true
  } catch (err) {
    console.error('theme: ApplyTheme failed:', err)
    themeState.error = err instanceof Error ? err.message : String(err)
    return false
  }
}

/**
 * Load the listing of selectable themes. Safe to call repeatedly; subsequent
 * calls overwrite the previous result (used as the `themes:changed`
 * event handler in `initThemes()`).
 */
export async function loadThemes(): Promise<void> {
  themesState.loading = true
  themesState.loadError = null
  try {
    const res = await ListThemes()
    themesState.items = res?.themes ?? []
  } catch (err) {
    console.error('theme: ListThemes failed:', err)
    themesState.loadError = err instanceof Error ? err.message : String(err)
  } finally {
    themesState.loading = false
  }
}

/**
 * Wire the theme-listing store: one initial load plus a subscription to
 * the backend's `themes:changed` event so an imported theme appears
 * immediately. Idempotent; safe to call once from `App.svelte onMount`.
 * Returns a disposer that unsubscribes from the event — call it on
 * unmount to prevent duplicate listeners during dev hot-reload
 * (mirrors the initConfigHotReload pattern in settings/store.svelte.ts).
 */
export function initThemes(): () => void {
  if (themesStarted) return () => {}
  themesStarted = true
  void loadThemes()
  offThemesChanged = EventsOn('themes:changed', () => {
    void loadThemes()
  })
  return () => {
    offThemesChanged?.()
    offThemesChanged = null
    themesStarted = false
  }
}
