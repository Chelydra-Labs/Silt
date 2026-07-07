// silt-ai-summary plugin entry (#220–#223).
//
// The first AI-capable first-party plugin. On note open (and after a debounced
// save), it calls the user-configured LLM via ctx.ai.complete to produce a
// 2–3 sentence summary plus any new tasks/risks/decisions the note contains.
// Extractions are cached by content hash in the plugin's own SQLite store
// (#222) and re-derived only when the note or the chat model changes; "new"
// items are diffed against the prior snapshot.
//
// Off by default. Its output renders via the note-banner surface (the
// SummaryBanner component, mounted first-party by PluginNoteBanners), with a
// status-bar re-open affordance for dismissed notes. The bespoke settings
// page lands in Phase 4.
//
// This module owns the reactive lifecycle: it registers/unregisters the
// banner vs. the re-open chip per active note (respecting dismissal) and
// debounces editor:save into the controller. The pure extraction/caching
// logic lives in sibling modules and is unit-tested directly.

import type { PluginContext, PluginManifest } from '../../sdk'
import { registerSurface, unregisterSurface } from '../../surfaces'
import AISummaryPanel from './AISummaryPanel.svelte'
import SummaryBanner from './SummaryBanner.svelte'
import { createSummaryController, type SummaryController } from './state.svelte'

export const manifest: PluginManifest = {
  id: 'silt-ai-summary',
  name: 'AI Summary',
  version: '0.1.0',
  author: 'Silt',
  description:
    'A dismissible highlight at the top of each note with a 2–3 sentence summary plus any new tasks, risks, and decisions. Requires an AI provider.',
  icon: 'auto_awesome',
  // first-party ⇒ implicitly granted (no per-capability prompt). The plugin
  // needs `ai` (ctx.ai.complete) and `plugin-db` (the content-hash cache).
  capabilities: { ai: true, 'plugin-db': true }
}

// Stable surface ids. The banner host looks up data-banner-close for focus
// management; the SummaryBanner uses 'silt-ai-summary' for that attribute.
const BANNER_SURFACE_ID = 'silt-ai-summary:banner'
const REOPEN_SURFACE_ID = 'silt-ai-summary:reopen'
const PLUGIN_ID = 'silt-ai-summary'

// Module-level controller + event unsubs. One active vault at a time; reset on
// close. The banner reads the controller via getController().
let controller: SummaryController | null = null
let offSave: (() => void) | null = null
let offActiveNotebook: (() => void) | null = null
let offConfigChanged: (() => void) | null = null
let lastPageId: string | null = null
// Which surface is currently mounted for lastPageId, so a re-evaluation that
// reaches the same decision doesn't churn (unregister+register → flicker).
let mountedKind: 'banner' | 'reopen' | null = null

export function getController(): SummaryController | null {
  return controller
}

function pageIdOf(notebook: string, section: string, page: string): string {
  return `${notebook}/${section}/${page}`
}

/** Mount the banner (when the page is not dismissed) or the re-open chip
 *  (when it is). Idempotent per page: a no-op when the target kind is already
 *  mounted, so a config:changed re-evaluation that reaches the same decision
 *  doesn't flicker. */
function mountForPage(ctx: PluginContext, pageId: string, dismissed: boolean) {
  const want: 'banner' | 'reopen' = dismissed ? 'reopen' : 'banner'
  if (mountedKind === want) return
  // Swap: tear down whichever surface is up, then mount the other.
  unregisterSurface(BANNER_SURFACE_ID)
  unregisterSurface(REOPEN_SURFACE_ID)
  if (want === 'banner') {
    registerSurface({
      id: BANNER_SURFACE_ID,
      pluginID: PLUGIN_ID,
      kind: 'note-banner',
      label: 'AI summary',
      icon: 'auto_awesome',
      component: SummaryBanner
    })
  } else {
    registerSurface({
      id: REOPEN_SURFACE_ID,
      pluginID: PLUGIN_ID,
      kind: 'status-bar-item',
      label: 'Show AI summary',
      icon: 'auto_awesome',
      onClick: () => {
        // Un-dismiss the active page, then re-mount the banner. The dismissal
        // list update persists to config.yaml (config:changed will fire and the
        // handler below re-evaluates; but we also mount eagerly here for an
        // instant re-show without waiting on the round-trip).
        if (!controller) return
        const cur = controller.getSettings().dismissed_notes.filter((p) => p !== pageId)
        ctx.updatePluginSetting('dismissed_notes', cur)
          .then(() => mountForPage(ctx, pageId, false))
          .catch(() => {
            /* best-effort — config:changed will retry the re-evaluation */
          })
      }
    })
  }
  mountedKind = want
}

export default {
  manifest,
  component: AISummaryPanel,
  onVaultOpen(ctx: PluginContext) {
    controller = createSummaryController()
    lastPageId = null
    mountedKind = null

    // After a save settles, debounce-regenerate for that page (not per
    // keystroke — the cache hash makes an unchanged note a free hit). #222.
    offSave = ctx.on('editor:save', (evt) => {
      if (!controller) return
      const id = pageIdOf(evt.notebook, evt.section, evt.page)
      // Only regenerate when the banner is actually shown for this page — a
      // dismissed note shouldn't silently re-summarize in the background.
      if (id !== lastPageId || mountedKind !== 'banner') return
      const { regenerate_debounce_ms } = controller.getSettings()
      controller.scheduleGenerate(ctx, id, regenerate_debounce_ms, {
        notebook: evt.notebook,
        section: evt.section,
        page: evt.page
      })
    })

    // On note switch: cancel the prior page's pending regeneration, mount the
    // right surface (banner vs. re-open), and (when auto-on-open is on) seed.
    offActiveNotebook = ctx.on('active-notebook:changed', (evt) => {
      if (!controller) return
      if (lastPageId) controller.cancelPending(lastPageId)
      if (!evt.notebook || !evt.page) return
      const id = pageIdOf(evt.notebook, evt.section, evt.page)
      lastPageId = id
      const s = controller.getSettings()
      const dismissed = s.dismissed_notes.includes(id)
      mountForPage(ctx, id, dismissed)
      if (!dismissed && s.auto_on_open && !s.on_demand_only) {
        void controller.generateFor(ctx, id, {
          notebook: evt.notebook,
          section: evt.section,
          page: evt.page
        })
      }
    })

    // A dismissal / re-open writes dismissed_notes to config.yaml, which fires
    // config:changed. Re-evaluate the active page's surface so the banner↔chip
    // swap happens without relying on a note switch.
    offConfigChanged = ctx.on('config:changed', () => {
      if (!controller || !lastPageId) return
      const dismissed = controller.getSettings().dismissed_notes.includes(lastPageId)
      mountForPage(ctx, lastPageId, dismissed)
    })
  },
  onVaultClose() {
    offSave?.()
    offActiveNotebook?.()
    offConfigChanged?.()
    offSave = null
    offActiveNotebook = null
    offConfigChanged = null
    unregisterSurface(BANNER_SURFACE_ID)
    unregisterSurface(REOPEN_SURFACE_ID)
    controller?.dispose()
    controller = null
    lastPageId = null
    mountedKind = null
  },
  onShutdown() {
    this.onVaultClose()
  }
}
