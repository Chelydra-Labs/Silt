import { OpenDevTools } from '../../bindings/silt/app.js'
import { settings } from '../settings/store.svelte'

/**
 * True when vault Dev Mode is on. Backend OpenDevTools still re-checks
 * SILT_DEBUG / vault flag at call time — UI must not be spoofable via
 * sessionStorage or other page-script state.
 */
export function isDevMode(): boolean {
  return settings.config?.ui?.open_devtools_on_startup === true
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
