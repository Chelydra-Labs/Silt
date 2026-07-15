// silt-ai-agent plugin entry (#596).
//
// A first-party AI agent that uses tools to search, read, create, and
// organize notes. Drives ctx.ai.complete with the tool catalog; each turn
// dispatches registered tools in parallel and feeds results back for the
// next iteration. This entry wires the registry, loop, chat UX, and per-plugin
// DB schema; P0 tools are registered on vault open via tools.ts.

import type { PluginContext, PluginManifest } from '../../sdk'
import AgentHub from './AgentHub.svelte'
import {
  createAgentController,
  getAgentController,
  setAgentController
} from './state.svelte'
import { migrateSchema, resetMigrationState } from './db'
import { clearTools } from './tool-registry'
import { registerP0Tools } from './tools'
import { cleanupExpired } from './staging'

export const manifest: PluginManifest = {
  id: 'silt-ai-agent',
  name: 'AI Agent',
  version: '0.1.0',
  author: 'Silt',
  description:
    'AI agent that uses tools to search, read, create, and organize notes in your vault.',
  icon: 'smart_toy',
  capabilities: {
    ai: true,
    'content-mutate': true,
    'plugin-db': true,
    'read-files': true
  }
}

export default {
  manifest,
  component: AgentHub,
  onVaultOpen(ctx: PluginContext) {
    const ctl = createAgentController()
    setAgentController(ctl)
    ctl.attach(ctx)
    // Register P0 tools so the agent loop has a catalog before it runs.
    registerP0Tools()
    // Stamp the staging_tokens schema so the DB is ready for Phase 5. Runs
    // once per process (guarded by a module flag); safe on every vault open.
    void migrateSchema(ctx)
      .then(() => cleanupExpired(ctx))
      .catch((e) => {
        console.warn('silt-ai-agent: schema migration failed:', e)
      })
  },
  onVaultClose() {
    getAgentController()?.dispose()
    setAgentController(null)
    clearTools()
    resetMigrationState()
  },
  onShutdown() {
    getAgentController()?.dispose()
    setAgentController(null)
    clearTools()
    resetMigrationState()
  }
}
