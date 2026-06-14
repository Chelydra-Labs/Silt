// Theme store (#46): holds the active theme/mode and the dark/light token
// maps, subscribes to the backend GetActiveTheme / ApplyTheme IPC methods,
// re-resolves "system" mode locally via prefers-color-scheme, and drives
// the runtime injector. Svelte 5 $state runes in a .svelte.ts module
// (matches plugins/store.svelte.ts).
import { ApplyTheme, GetActiveTheme } from '../../wailsjs/go/main/App.js'
import { EventsOn } from '../../wailsjs/runtime/runtime.js'
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

let darkMedia: MediaQueryList | null = null
let started = false

/** Returns true when the OS prefers light mode (used to resolve "system"). */
function osPrefersLight(): boolean {
  if (typeof window === 'undefined' || !window.matchMedia) return false
  return window.matchMedia('(prefers-color-scheme: light)').matches
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
  // no second IPC round-trip (both token maps are already in hand).
  if (typeof window !== 'undefined' && window.matchMedia) {
    darkMedia = window.matchMedia('(prefers-color-scheme: dark)')
    darkMedia.addEventListener('change', () => {
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
