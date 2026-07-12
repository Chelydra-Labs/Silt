// silt-ai-qa plugin entry (#224–#228).
//
// Off by default. Builds an incremental vector index in the plugin SQLite
// store (sqlite-vec), hybrid-retrieves with FTS5, and answers via
// ctx.ai.complete (streaming when available). Surfaces: right drawer (AI
// Search) toggled from the status bar + bespoke settings page.

import type { PluginContext, PluginManifest } from '../../sdk'
import { registerSurface, unregisterSurface } from '../../surfaces'
import QAHub from './QAHub.svelte'
import QASettings from './QASettings.svelte'
import {
  createQAController,
  setQAController,
  getQAController
} from './state.svelte'
import { resetIndexState } from './embed_index'
import { resolveSettings } from './settings'
import { resetAISearchDrawer, toggleAISearchDrawer } from './drawer.svelte'

export const manifest: PluginManifest = {
  id: 'silt-ai-qa',
  name: 'AI Assistant',
  version: '0.1.0',
  author: 'Silt',
  description:
    'Search your vault with hybrid keyword + semantic retrieval, and get cited answers from your notes. Requires chat + embedding models.',
  icon: 'manage_search',
  capabilities: { ai: true, 'plugin-db': true }
}

export const TOGGLE_SURFACE_ID = 'silt-ai-qa:toggle'
const PLUGIN_ID = 'silt-ai-qa'

let offSave: (() => void) | null = null
let offBlock: (() => void) | null = null
let offConfig: (() => void) | null = null

function mountToggle() {
  unregisterSurface(TOGGLE_SURFACE_ID)
  registerSurface({
    id: TOGGLE_SURFACE_ID,
    pluginID: PLUGIN_ID,
    kind: 'status-bar-item',
    label: 'AI Assistant',
    icon: 'manage_search',
    onClick: () => toggleAISearchDrawer()
  })
}

function unmountToggle() {
  unregisterSurface(TOGGLE_SURFACE_ID)
  resetAISearchDrawer()
}

export default {
  manifest,
  component: QAHub,
  settingsPageComponent: QASettings,
  onVaultOpen(ctx: PluginContext) {
    const ctl = createQAController()
    setQAController(ctl)
    ctl.loadSettings(ctx)
    mountToggle()
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
    unmountToggle()
    getQAController()?.dispose()
    setQAController(null)
    resetIndexState()
  },
  onShutdown() {
    offSave?.()
    offBlock?.()
    offConfig?.()
    offSave = offBlock = offConfig = null
    unmountToggle()
    getQAController()?.dispose()
    setQAController(null)
    resetIndexState()
  }
}
