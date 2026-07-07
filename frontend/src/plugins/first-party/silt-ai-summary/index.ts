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
// status-bar re-open affordance for dismissed / on-demand notes. The bespoke
// settings page lands in Phase 4.
//
// This module owns the reactive lifecycle: it registers/unregisters the
// banner vs. the re-open chip per active note (respecting dismissal AND
// on-demand mode) and debounces editor:save into the controller. The pure
// extraction/caching logic lives in sibling modules and is unit-tested directly.

import type { PluginContext, PluginManifest } from '../../sdk'
import { registerSurface, unregisterSurface } from '../../surfaces'
import AISummaryPanel from './AISummaryPanel.svelte'
import AISummarySettings from './AISummarySettings.svelte'
import SummaryBanner from './SummaryBanner.svelte'
import { createSummaryController, type SummaryController } from './state.svelte'
import { resetCacheState } from './cache'
import { decideMountKind } from './mountKind'

// Re-exported so callers that already import the entry can reach the decision
// helper; the pure implementation lives in mountKind.ts (testable in isolation).
export { decideMountKind }

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

// Stable surface ids. The banner host's cross-banner focus management queries
// `[data-banner-close="<registered id>"]` after a dismiss, so the SummaryBanner
// MUST set its close button's data-banner-close to the SAME id it is registered
// under — exported here so the component imports the single source of truth
// rather than re-hardcoding the literal (which drifted once and broke focus).
export const BANNER_SURFACE_ID = 'silt-ai-summary:banner'
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

/** Mount the banner or the re-open chip per {@link decideMountKind}. Idempotent
 *  per page: a no-op when the target kind is already mounted, so a
 *  config:changed re-evaluation that reaches the same decision doesn't flicker.
 *  The chip's onClick clears dismissal + shows the banner + generates (cache
 *  hit serves instantly, miss generates) — the unified re-show path for both
 *  dismissed and on-demand. */
function mountForPage(
  ctx: PluginContext,
  pageId: string,
  opts: { dismissed: boolean; onDemandOnly: boolean }
) {
  const want = decideMountKind(opts)
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
      // Label reflects WHY the chip is showing so the affordance is honest.
      label: opts.onDemandOnly ? 'Generate AI summary' : 'Show AI summary',
      icon: 'auto_awesome',
      onClick: () => {
        const ctl = controller
        if (!ctl) return
        const cur = ctl.getSettings()
        // Clear dismissal first (no-op in the on-demand-only case) so a
        // future note switch re-evaluates cleanly; persist before showing.
        const unDismiss = cur.dismissed_notes.filter((p) => p !== pageId)
        const persist =
          unDismiss.length !== cur.dismissed_notes.length
            ? ctx.updatePluginSetting('dismissed_notes', unDismiss)
            : Promise.resolve(true)
        persist
          .then(() => {
            // The vault may close/switch between the click and the IPC
            // resolving — re-check controller identity so we never dereference
            // a nulled (onVaultClose) controller or mount into a torn-down
            // vault.
            if (controller !== ctl) return
            // Show the banner for this session (onDemandOnly=false forces it
            // even in on-demand mode — the user explicitly requested).
            mountForPage(ctx, pageId, { dismissed: false, onDemandOnly: false })
            // Serve the cache (instant hit) or generate on a miss. Non-force
            // so a still-valid cached summary isn't needlessly regenerated.
            void ctl.generateFor(ctx, pageId, {
              notebook: ctx.activeNotebook,
              section: ctx.activeSection,
              page: ctx.activePage
            })
          })
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
  settingsPageComponent: AISummarySettings,
  onVaultOpen(ctx: PluginContext) {
    controller = createSummaryController()
    lastPageId = null
    mountedKind = null

    // After a save settles, debounce-regenerate for that page (not per
    // keystroke — the cache hash makes an unchanged note a free hit). #222.
    offSave = ctx.on('editor:save', (evt) => {
      if (!controller) return
      const id = pageIdOf(evt.notebook, evt.section, evt.page)
      const s = controller.getSettings()
      // Only regenerate when the banner is shown AND the user hasn't chosen
      // on-demand (on-demand = generate only on explicit click). A dismissed
      // note (mountedKind === 'reopen') is also skipped.
      if (id !== lastPageId || mountedKind !== 'banner' || s.on_demand_only) return
      controller.scheduleGenerate(ctx, id, s.regenerate_debounce_ms, {
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
      mountForPage(ctx, id, { dismissed, onDemandOnly: s.on_demand_only })
      if (!dismissed && !s.on_demand_only && s.auto_on_open) {
        void controller.generateFor(ctx, id, {
          notebook: evt.notebook,
          section: evt.section,
          page: evt.page
        })
      }
    })

    // A dismissal / re-open / settings tweak writes config.yaml, which fires
    // config:changed. Re-evaluate the active page's surface so the banner↔chip
    // swap (incl. an on-demand toggle) happens without a note switch.
    offConfigChanged = ctx.on('config:changed', () => {
      if (!controller || !lastPageId) return
      const s = controller.getSettings()
      mountForPage(ctx, lastPageId, {
        dismissed: s.dismissed_notes.includes(lastPageId),
        onDemandOnly: s.on_demand_only
      })
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
    // Forget the per-vault cache-migration flag so the next vault re-creates
    // its own summaries table. Without this, a vault switch skips migration
    // (the flag is still true from the closing vault) and every cache query
    // against the new vault's plugin.db fails (#222 cross-vault regression).
    resetCacheState()
  },
  onShutdown() {
    this.onVaultClose()
  }
}
