// silt-ai-assistant — Writing Assistant (#229–#233).
// Off by default. Curated writing actions with accept/reject proposals.

import type { PluginContext, PluginManifest } from '../../sdk'
import { ACTION_CATALOG } from './catalog'
import { isActionEnabled } from './catalog'
import { openWritingAssistantDrawerExclusive } from '../../../lib/drawers.svelte'
import { resetWritingAssistantDrawer } from './drawer.svelte'
import WritingAssistantHub from './WritingAssistantHub.svelte'
import AssistantSettings from './AssistantSettings.svelte'
import {
  createAssistantController,
  setAssistantController,
  getAssistantController,
  syncWritingAssistantChrome
} from './state.svelte'

export const manifest: PluginManifest = {
  id: 'silt-ai-assistant',
  name: 'Writing Assistant',
  version: '0.1.0',
  author: 'Silt',
  description:
    'Curated AI writing actions — draft, rewrite, clarify, extract tasks, suggest tags and related notes. Proposals only; nothing is written until you accept. Requires an AI provider.',
  icon: 'ink_pen',
  capabilities: { ai: true, 'content-mutate': true }
}

function selectionTextFromEditor(editor: unknown): string {
  try {
    const ed = editor as {
      state?: {
        doc: { textBetween: (f: number, t: number) => string }
        selection: { from: number; to: number }
      }
    }
    const sel = ed.state?.selection
    if (!sel || sel.from === sel.to) return ''
    return ed.state!.doc.textBetween(sel.from, sel.to)
  } catch {
    return ''
  }
}

/**
 * Register slash commands for the full catalog. Enabled state is enforced at
 * invoke time so settings toggles apply without re-registering.
 */
function registerSlashCommands(ctx: PluginContext) {
  for (const action of ACTION_CATALOG) {
    ctx.registerSlashCommand({
      id: action.id,
      label: action.slashLabel,
      description: action.slashDescription,
      icon: action.icon,
      onSelect: (editor) => {
        const c = getAssistantController()
        if (!c) return
        c.loadSettings()
        if (!isActionEnabled(c.settings, action.id)) {
          openWritingAssistantDrawerExclusive()
          void c.run(ctx, action.id, {})
          return
        }
        const selectionText = selectionTextFromEditor(editor)
        openWritingAssistantDrawerExclusive()
        void c.run(ctx, action.id, { selectionText })
      }
    })
  }
}

export default {
  manifest,
  component: WritingAssistantHub,
  settingsPageComponent: AssistantSettings,
  onVaultOpen(ctx: PluginContext) {
    const ctl = createAssistantController()
    setAssistantController(ctl)
    syncWritingAssistantChrome(ctl)
    ctl.loadSettings()
    registerSlashCommands(ctx)
    ctx.on('config:changed', () => {
      getAssistantController()?.loadSettings()
    })
  },
  onVaultClose() {
    getAssistantController()?.dispose()
    setAssistantController(null)
    syncWritingAssistantChrome(null)
    resetWritingAssistantDrawer()
  },
  onShutdown() {
    getAssistantController()?.dispose()
    setAssistantController(null)
    syncWritingAssistantChrome(null)
    resetWritingAssistantDrawer()
  }
}
