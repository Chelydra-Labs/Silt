// silt-ai-summary plugin entry (#220–#223).
//
// The first AI-capable first-party plugin. On note open (and after a debounced
// save), it calls the user-configured LLM via ctx.ai.complete to produce a
// 2–3 sentence summary plus any new tasks/risks/decisions the note contains.
// Extractions are cached by content hash in the plugin's own SQLite store
// (#222) and re-derived only when the note or the chat model changes; "new"
// items are diffed against the prior snapshot.
//
// Off by default. Its output renders via the note-banner surface (Phase 3's
// SummaryBanner component), NOT in this module's main view (AISummaryPanel is
// an informational placeholder). The bespoke settings page lands in Phase 4.
//
// This module owns the reactive lifecycle (onVaultOpen wires editor:save +
// note-switch subscriptions to the controller); the pure extraction/caching
// logic lives in sibling modules and is unit-tested directly.

import type { PluginContext, PluginManifest } from '../../sdk'
import AISummaryPanel from './AISummaryPanel.svelte'
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

// Module-level controller + event unsubs. One active vault at a time; reset on
// close. The banner (Phase 3) reads the controller via getController().
let controller: SummaryController | null = null
let offSave: (() => void) | null = null
let offActiveNotebook: (() => void) | null = null
let lastPageId: string | null = null

export function getController(): SummaryController | null {
  return controller
}

function pageIdOf(notebook: string, section: string, page: string): string {
  return `${notebook}/${section}/${page}`
}

export default {
  manifest,
  component: AISummaryPanel,
  onVaultOpen(ctx: PluginContext) {
    controller = createSummaryController()
    lastPageId = null

    // After a save settles, debounce-regenerate for that page (not per
    // keystroke — the cache hash makes an unchanged note a free hit). #222.
    offSave = ctx.on('editor:save', (evt) => {
      if (!controller) return
      const id = pageIdOf(evt.notebook, evt.section, evt.page)
      const { regenerate_debounce_ms } = controller.getSettings()
      controller.scheduleGenerate(ctx, id, regenerate_debounce_ms, {
        notebook: evt.notebook,
        section: evt.section,
        page: evt.page
      })
    })

    // On note switch, cancel the prior page's pending regeneration and (when
    // auto-on-open is on) seed the new page. on_demand_only skips the auto
    // seed so the banner waits for an explicit Regenerate.
    offActiveNotebook = ctx.on('active-notebook:changed', (evt) => {
      if (!controller) return
      if (lastPageId) controller.cancelPending(lastPageId)
      const id = pageIdOf(evt.notebook, evt.section, evt.page)
      lastPageId = id
      const s = controller.getSettings()
      if (!evt.notebook || !evt.page) return
      if (s.auto_on_open && !s.on_demand_only) {
        void controller.generateFor(ctx, id, {
          notebook: evt.notebook,
          section: evt.section,
          page: evt.page
        })
      }
    })
  },
  onVaultClose() {
    offSave?.()
    offActiveNotebook?.()
    offSave = null
    offActiveNotebook = null
    controller?.dispose()
    controller = null
    lastPageId = null
  },
  onShutdown() {
    this.onVaultClose()
  }
}
