// silt-ai-agent plugin entry (#596).
//
// A first-party AI agent that uses tools to search, read, create, and
// organize notes. Drives ctx.ai.complete with the tool catalog; each turn
// dispatches registered tools in parallel and feeds results back for the
// next iteration. This entry wires the registry, loop, and per-plugin DB
// schema; P0 tools are registered on vault open via tools.ts.

import type { PluginContext, PluginManifest } from '../../sdk'
import { migrateSchema } from './db'
import { clearTools } from './tool-registry'
import { registerP0Tools, registerP1Tools, registerP2Tools } from './tools'
import { cleanupExpired } from './staging'
import { resetAIChatDrawer } from '../../shared/ai-chat/drawer.svelte'

export const manifest: PluginManifest = {
  id: 'silt-ai-agent',
  name: 'AI Agent',
  version: '0.1.0',
  author: 'Silt',
  description:
    'AI agent that uses tools to search, read, create, and organize notes in your vault.',
  icon: 'psychology',
  capabilities: {
    ai: true,
    'content-mutate': true,
    'plugin-db': true
  }
}

export default {
  manifest,
  onVaultOpen(ctx: PluginContext) {
    // Register P0 + P1 + P2 tools so the agent loop has a catalog before it runs.
    // Drawer availability is gated by getAIAvailability().drawerAvailable
    // (ai.features + session), not a separate chrome flag.
    registerP0Tools()
    registerP1Tools()
    registerP2Tools()
    // Stamp the staging_tokens schema so the DB is ready for Phase 5. Runs
    // once per process (guarded by a module flag); safe on every vault open.
    void migrateSchema(ctx)
      .then(() => cleanupExpired(ctx))
      .catch((e) => {
        console.warn('silt-ai-agent: schema migration failed:', e)
      })
  },
  onVaultClose() {
    resetAIChatDrawer()
    clearTools()
  },
  onShutdown() {
    resetAIChatDrawer()
    clearTools()
  }
}
