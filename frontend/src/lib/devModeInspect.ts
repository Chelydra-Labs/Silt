import { OpenDevTools } from '../../bindings/silt/app.js'
import { settings } from '../settings/store.svelte'

/** True when vault Dev Mode (open DevTools on startup) is on. */
export function isDevMode(): boolean {
  return settings.config?.ui?.open_devtools_on_startup === true
}

/** Opens webview DevTools via the gated OpenDevTools IPC (#679/#683). */
export async function openInspect(): Promise<void> {
  try {
    await OpenDevTools()
  } catch (e) {
    console.error('OpenDevTools failed:', e)
  }
}
