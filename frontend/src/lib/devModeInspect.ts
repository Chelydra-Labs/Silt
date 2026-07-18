import { OpenDevTools } from '../../bindings/silt/app.js'
import { settings } from '../settings/store.svelte'

/** True when vault Dev Mode is on, or SILT_DEBUG=1 (parity with backend menu). */
export function isDevMode(): boolean {
  if (settings.config?.ui?.open_devtools_on_startup === true) return true
  try {
    // Wails may expose env via import.meta; also check common debug flags.
    const env =
      (globalThis as { SILT_DEBUG?: string }).SILT_DEBUG ??
      (typeof process !== 'undefined'
        ? (process as { env?: { SILT_DEBUG?: string } }).env?.SILT_DEBUG
        : undefined)
    if (env === '1' || env === 'true') return true
  } catch {
    /* ignore */
  }
  // Session flag set when backend reports debug (optional IPC later).
  if (
    typeof sessionStorage !== 'undefined' &&
    sessionStorage.getItem('silt_debug') === '1'
  ) {
    return true
  }
  return false
}

export type OpenInspectResult = { ok: true } | { ok: false; error: string }

/** Opens webview DevTools via the gated OpenDevTools IPC (#679/#683). */
export async function openInspect(): Promise<OpenInspectResult> {
  try {
    await OpenDevTools()
    return { ok: true }
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e)
    console.error('OpenDevTools failed:', e)
    return { ok: false, error: msg || 'Open DevTools failed' }
  }
}
