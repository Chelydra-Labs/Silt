import {
  ReadPluginSource,
  ListPlugins,
  RegisterPluginSession,
  UnregisterPluginSession,
  ClosePluginDB
} from '../../bindings/silt/app.js'
import { Events } from '@wailsio/runtime'
import { getFirstParty, firstPartyPlugins } from './registry'
import { makePluginContext } from './context'
import { setActiveLocation } from './location.svelte'
import { loadedPlugins } from './store.svelte'
import { settings } from '../settings/store.svelte'
import type { LoadedPlugins, RegisteredPlugin, SiltPlugin } from './sdk'
import { cleanupPlugin, clearAllSubscribers } from './events'
import { unregisterPluginSlashCommands } from '../lib/editor/slash-registry'
import { unregisterPluginSurfaces } from './surfaces'
import { unregisterPluginDecorations } from '../lib/editor/decorations'
import { initGrants } from './grants.svelte'
import { resetTaskHubState } from './first-party/silt-tasks/state.svelte'
import { resetTasksSettings } from './first-party/silt-tasks/settings'
import DiskPluginNotice from './DiskPluginNotice.svelte'
import {
  isFirstPartyAIPlugin,
  shouldLoadAIPlugin,
  AI_PLUGIN_AGENT
} from './shared/ai-chat/availability'
import { reconcileAgentTools } from './first-party/silt-ai-agent/tools'

// Whether the lifecycle wiring (vault:closing subscription) has been installed.
// Lives at module scope so repeated loadPlugins calls do not double-subscribe.
let lifecycleWired = false

// Per-plugin session tokens (#151). The loader registers a session when a
// plugin loads and unregisters on teardown. The token is passed to
// makePluginContext so the SDK closures can include it in every privileged
// binding call for binding-identity verification.
const sessionTokens = new Map<string, string>()

/**
 * Look up the session token registered for pluginID. Used by PluginView so
 * the context it builds for the rendered component carries the same token
 * the loader registered — without it every privileged SDK call from the
 * component fails with "missing session token" (#236).
 */
export function getSessionToken(pluginID: string): string | undefined {
  return sessionTokens.get(pluginID)
}

/**
 * Discover and initialize all active plugins:
 *   1. First-party bundled plugins (always available).
 *   2. On-disk plugins discovered under .system/plugins/ (skipping any with
 *      a .disabled sentinel), loaded from index.js as native ESM via a blob
 *      URL (so Vite does not try to resolve them at build time). Discovery is
 *      purely folder-based + the .disabled sentinel, so install "just works"
 *      with no config.yaml editing. (The legacy `plugins.active` list in
 *      config.yaml is no longer a whitelist.)
 * Each plugin's init(ctx) receives the same PluginContext. Per-plugin load
 * failures are collected rather than aborting the whole boot.
 *
 * v2 lifecycle (#106): onVaultOpen fires after init (vault is open + context
 * usable); onVaultClose / onShutdown fire via the host vault:closing event +
 * window beforeunload. Every plugin's event-bus subscriptions are cleaned up
 * on disable/uninstall/vault-close.
 *
 * `loadedPlugins.loadersReady` is flipped true at the end of a successful
 * load and false at the start of vault:closing teardown. Sidebar/PluginView
 * gate PluginContext construction on it to avoid capturing an empty session
 * token during the clear→re-register window (#326 item 5).
 */
export async function loadPlugins(
  activeNotebook: string,
  activeSection: string,
  activePage: string
): Promise<LoadedPlugins> {
  // Keep the reactive location state in sync (#69). Plugins that read
  // ctx.activeNotebook at query time see the live value.
  setActiveLocation(activeNotebook, activeSection, activePage)
  // Initialize the granted-capabilities cache BEFORE plugins load, so the
  // registry-internal gates (#158) see the correct grants when plugins call
  // registerSlashCommand / registerSurface / provideDecorations during init.
  initGrants()
  const plugins = new Map<string, RegisteredPlugin>()
  const errors: { id: string; message: string }[] = []

  // Discover on-disk plugins by folder. The installed list carries the
  // on-disk manifest's contentSha256 (#161) so the loader can verify runtime
  // integrity before importing the JS.
  let installed: {
    id: string
    disabled: boolean
    has_index: boolean
    contentSha256?: string
  }[]
  try {
    installed = (await ListPlugins()) ?? []
  } catch {
    installed = []
  }

  for (const p of installed) {
    const id = p.id
    if (getFirstParty(id)) continue // first-party wins; handled below
    if (p.disabled) continue // .disabled sentinel → skip
    try {
      if (!p.has_index) {
        errors.push({ id, message: 'missing index.js' })
        continue
      }
      const src = await ReadPluginSource(id)

      // Runtime integrity verification (#161): compute sha256 of the source
      // and compare against the on-disk manifest's contentSha256 (set at
      // install time). A mismatch means the file was tampered with post-
      // install → refuse to load.
      if (p.contentSha256) {
        const computed = await sha256Hex(src)
        if (computed !== p.contentSha256) {
          errors.push({
            id,
            message: `integrity check failed (expected ${p.contentSha256.slice(0, 12)}…, got ${computed.slice(0, 12)}…)`
          })
          continue
        }
      }

      const blob = new Blob([src], { type: 'text/javascript' })
      const url = URL.createObjectURL(blob)
      let mod: { default?: SiltPlugin } & Partial<SiltPlugin>
      try {
        mod = await import(/* @vite-ignore */ url)
      } finally {
        URL.revokeObjectURL(url)
      }
      const def = (mod?.default ?? mod) as SiltPlugin | undefined
      const manifest = def?.manifest ?? { id, name: id, version: '0.0.0' }
      // Register a session token for binding-identity verification (#151).
      let token = sessionTokens.get(id)
      if (!token) {
        token = await RegisterPluginSession(id)
        sessionTokens.set(id, token)
      }
      // Per-plugin context so getPluginSettings knows which plugin is
      // resolving its settings (#133). The location getters stay reactive.
      const ctx = makePluginContext(id, token)
      def?.init?.(ctx)
      def?.onVaultOpen?.(ctx)
      const diskMod = mod as {
        default?: SiltPlugin & { component?: unknown }
      } & Partial<SiltPlugin & { component?: unknown }>
      const reg: RegisteredPlugin = {
        manifest,
        component: (diskMod.default?.component ??
          diskMod.component ??
          DiskPluginNotice) as RegisteredPlugin['component'],
        init: def?.init,
        onVaultOpen: def?.onVaultOpen,
        onVaultClose: def?.onVaultClose,
        onShutdown: def?.onShutdown,
        source: 'disk'
      }
      plugins.set(id, reg)
    } catch (e) {
      errors.push({
        id,
        message: e instanceof Error ? e.message : String(e)
      })
    }
  }

  // First-party plugins: user-disableable via Settings → Plugins
  // (plugins.disabled). First-party AI modules are gated by ai.features
  // instead (#632) so the Plugins tab is not the product switch.
  const disabledIds = new Set(settings.config?.plugins?.disabled ?? [])
  for (const fp of firstPartyPlugins()) {
    const id = fp.manifest.id
    if (isFirstPartyAIPlugin(id)) {
      if (!shouldLoadAIPlugin(id)) continue
    } else if (disabledIds.has(id)) {
      continue
    }
    if (plugins.has(id)) continue
    // Already live from a prior loadPlugins under the same desired set —
    // carry over without re-running init/onVaultOpen (avoids double-register
    // of slash commands / surfaces on feature-flag toggles).
    const existing = loadedPlugins.plugins.get(id)
    if (existing) {
      plugins.set(id, existing)
      // Agent tools are filtered by RAG at register time; re-apply when the
      // agent is reused across a RAG-only feature flip (#632).
      if (id === AI_PLUGIN_AGENT) {
        reconcileAgentTools()
      }
      continue
    }
    // Register a session token for first-party plugins too (#151).
    let fpToken = sessionTokens.get(id)
    if (!fpToken) {
      try {
        fpToken = await RegisterPluginSession(id)
        sessionTokens.set(id, fpToken)
      } catch {
        // Best-effort: if session registration fails (e.g. vault not
        // loaded yet), continue with no token — the SDK passes '' which
        // the Go side rejects for session-gated bindings.
      }
    }
    const ctx = makePluginContext(id, fpToken)
    fp.init?.(ctx)
    fp.onVaultOpen?.(ctx)
    plugins.set(id, fp)
  }

  // Reconcile: tear down anything that was loaded but is not in the desired
  // set (AI feature off, plugin disabled, disk plugin removed). Without this,
  // loadPlugins only builds a new map and swaps it — slash commands, surfaces,
  // sessions, and event-bus subs would linger after Enable AI / RAG / summaries
  // flip off (#632).
  for (const id of [...loadedPlugins.plugins.keys()]) {
    if (!plugins.has(id)) {
      teardownPlugin(id)
    }
  }

  loadedPlugins.plugins = plugins
  loadedPlugins.errors = errors
  // Session tokens are live; consumers (Sidebar/PluginView) can safely build
  // a PluginContext now. Flipping this last re-triggers any $derived that
  // suspended during the clear→re-register window (#326 item 5).
  loadedPlugins.loadersReady = true

  wireLifecycleOnce()

  return { plugins, errors, loadersReady: true }
}

/**
 * Install the host lifecycle wiring exactly once. Subscribes to:
 *   - vault:closing (Go emits before teardown) → run every plugin's
 *     onVaultClose, then clear all event-bus subscriptions, then run
 *     onShutdown. The vault is about to go away.
 *   - window beforeunload → run onShutdown (the reliable frontend signal that
 *     the app is exiting; the Go OnShutdown may fire after IPC is gone).
 * Idempotent across repeated loadPlugins calls (lifecycleWired guard).
 *
 * IMPORTANT: the closures read loadedPlugins.plugins (the reactive store) at
 * fire time, NOT the plugins parameter captured at first call. A plugin
 * installed after the first loadPlugins call still receives onVaultClose /
 * onShutdown on the next vault close.
 */
function wireLifecycleOnce() {
  if (lifecycleWired) return
  lifecycleWired = true

  Events.On('vault:closing', () => {
    // Mark loaders not-ready BEFORE any teardown so Sidebar/PluginView
    // suspend context construction while sessionTokens is being cleared
    // and before the next loadPlugins re-registers them. Without this,
    // a remount in the window captures a context with an empty token and
    // every privileged SDK call fails until the component remounts again
    // (#326 item 5).
    loadedPlugins.loadersReady = false
    for (const reg of loadedPlugins.plugins.values()) {
      try {
        reg.onVaultClose?.()
      } catch (err) {
        console.error(`[silt] onVaultClose for ${reg.manifest.id} threw:`, err)
      }
    }
    // Per-plugin event-bus cleanup before the global clear, so every
    // plugin's subscriptions get deterministic teardown even if a plugin
    // was removed by a path that bypassed teardownPlugin.
    for (const reg of loadedPlugins.plugins.values()) {
      cleanupPlugin(reg.manifest.id)
    }
    // Drop any remaining event-bus subscriptions so a stale listener cannot
    // fire against the next vault (#106).
    clearAllSubscribers()
    // Unregister every plugin's editor contributions (slash commands,
    // surfaces, decorations) before onShutdown and before the plugins map is
    // cleared — mirroring teardownPlugin's ordering so a plugin's onShutdown
    // cannot re-read its own now-removed contributions. Without this, a vault
    // switch leaves the previous vault's plugin commands/surfaces/decorations
    // in the registries as ghosts (#580).
    for (const reg of loadedPlugins.plugins.values()) {
      const id = reg.manifest.id
      try {
        unregisterPluginSlashCommands(id)
        unregisterPluginSurfaces(id)
        unregisterPluginDecorations(id)
      } catch (err) {
        console.error(`[silt] contribution unregister for ${id} threw:`, err)
      }
    }
    for (const reg of loadedPlugins.plugins.values()) {
      try {
        reg.onShutdown?.()
      } catch (err) {
        console.error(`[silt] onShutdown for ${reg.manifest.id} threw:`, err)
      }
    }
    // The plugins map is stale after teardown; clear the reactive store.
    loadedPlugins.plugins = new Map()
    loadedPlugins.errors = []
    // Reset the unified hub's module-global state so scope/filters/focusDate
    // from the previous vault don't linger into the next (#326 item 1). The
    // settings store is reset by the next loadPlugins, but this reactive
    // module is not — without this a switched vault opens with the previous
    // vault's Tasks scope/filter/focus.
    resetTaskHubState()
    // Clear the settings module's cached configSlice + saveFn so the next
    // vault doesn't inherit the previous vault's plugin settings or a stale
    // session-bound persist function (#474 review finding).
    resetTasksSettings()
    // Clear all session tokens so the next vault starts fresh (#151).
    for (const [, token] of sessionTokens) {
      UnregisterPluginSession(token).catch(() => {})
    }
    sessionTokens.clear()
  })

  window.addEventListener('beforeunload', () => {
    for (const reg of loadedPlugins.plugins.values()) {
      try {
        reg.onShutdown?.()
      } catch {
        // Swallow during page teardown — logging here is unreliable.
      }
    }
  })
}

/**
 * Tear down a SINGLE plugin's host surface: run its onVaultClose + onShutdown
 * and remove its event-bus subscriptions. Used by the manager when a plugin is
 * disabled or uninstalled at runtime (the full vault:closing path handles the
 * bulk case).
 */
export function teardownPlugin(pluginID: string): void {
  const reg = loadedPlugins.plugins.get(pluginID)
  if (!reg) return
  try {
    reg.onVaultClose?.()
  } catch {
    // best-effort
  }
  cleanupPlugin(pluginID)
  unregisterPluginSlashCommands(pluginID)
  unregisterPluginSurfaces(pluginID)
  unregisterPluginDecorations(pluginID)
  // Unregister the session token (#151).
  const token = sessionTokens.get(pluginID)
  if (token) {
    UnregisterPluginSession(token).catch(() => {})
    sessionTokens.delete(pluginID)
  }
  // Close the per-plugin DB pool (#213) — before onShutdown and before any
  // folder removal on uninstall (Windows file lock).
  ClosePluginDB(pluginID).catch(() => {})
  try {
    reg.onShutdown?.()
  } catch {
    // best-effort
  }
  loadedPlugins.plugins.delete(pluginID)
}

/**
 * Compute the hex-encoded sha256 of a string (#161 runtime integrity check).
 * Uses the Web Crypto API (crypto.subtle.digest), available in the Wails
 * webview and in jsdom test environments.
 */
async function sha256Hex(text: string): Promise<string> {
  const encoder = new TextEncoder()
  const data = encoder.encode(text)
  const hashBuffer = await crypto.subtle.digest('SHA-256', data)
  const hashArray = Array.from(new Uint8Array(hashBuffer))
  return hashArray.map((b) => b.toString(16).padStart(2, '0')).join('')
}
