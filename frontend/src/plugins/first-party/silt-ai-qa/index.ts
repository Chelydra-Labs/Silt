// silt-ai-qa plugin entry (#224–#228).
//
// Off by default. Builds an incremental vector index in the plugin SQLite
// store (sqlite-vec), hybrid-retrieves with FTS5, and answers via
// ctx.ai.complete (streaming when available). The lifecycle provider maintains
// the index for the unified AI drawer and exposes a bespoke settings page.

import type { PluginContext, PluginManifest } from '../../sdk'
import QASettings from './QASettings.svelte'
import {
  createQAController,
  setQAController,
  getQAController
} from './state.svelte'
import { resetIndexState } from './embed_index'
import { resolveSettings } from './settings'

export const manifest: PluginManifest = {
  id: 'silt-ai-qa',
  name: 'AI Assistant',
  version: '0.1.0',
  author: 'Silt',
  description:
    'Search your vault by keyword and meaning, and get cited answers from your notes. Requires chat + search models.',
  icon: 'manage_search',
  capabilities: { ai: true, 'plugin-db': true }
}

let offSave: (() => void) | null = null
let offBlock: (() => void) | null = null
let offConfig: (() => void) | null = null

export default {
  manifest,
  settingsPageComponent: QASettings,
  onVaultOpen(ctx: PluginContext) {
    const ctl = createQAController()
    setQAController(ctl)
    ctl.loadSettings(ctx)
    // First-run / resume: rebuild when empty or embedding model changed.
    void ctl.ensureIndex(ctx)

    offSave = ctx.on('editor:save', (evt) => {
      const c = getQAController()
      if (!c) return
      c.schedulePageIndex(ctx, evt.notebook, evt.section, evt.page)
    })

    offBlock = ctx.on('block:changed', (evt) => {
      const c = getQAController()
      if (!c) return
      c.schedulePageIndex(
        ctx,
        evt.notebook ?? '',
        evt.section ?? '',
        evt.page ?? ''
      )
    })

    offConfig = ctx.on('config:changed', () => {
      const c = getQAController()
      if (!c) return
      c.loadSettings(ctx)
      void (async () => {
        try {
          const raw = (await ctx.getPluginSettings()) as Record<string, unknown>
          c.setSettings(resolveSettings(raw))
        } catch {
          /* ignore */
        }
        void c.ensureIndex(ctx)
      })()
    })
  },
  onVaultClose() {
    offSave?.()
    offBlock?.()
    offConfig?.()
    offSave = offBlock = offConfig = null
    getQAController()?.dispose()
    setQAController(null)
    resetIndexState()
  },
  onShutdown() {
    offSave?.()
    offBlock?.()
    offConfig?.()
    offSave = offBlock = offConfig = null
    getQAController()?.dispose()
    setQAController(null)
    resetIndexState()
  }
}
