// Product-level AI availability (#632).
//
// Single derivation for loader gating, titlebar chrome, and the drawer empty
// state. Feature flags live on settings.config.ai.features; chat/embedding
// readiness uses the shared ai-setup predicates so Plugins badges and the AI
// settings page stay coherent.

import {
  aiProviderNeedsSetup,
  embeddingProviderNeedsSetup
} from '../../../settings/ai-setup'
import { settings } from '../../../settings/store.svelte'

export const AI_PLUGIN_AGENT = 'silt-ai-agent'
export const AI_PLUGIN_QA = 'silt-ai-qa'
export const AI_PLUGIN_ASSISTANT = 'silt-ai-assistant'
export const AI_PLUGIN_SUMMARY = 'silt-ai-summary'

const FIRST_PARTY_AI = new Set([
  AI_PLUGIN_AGENT,
  AI_PLUGIN_QA,
  AI_PLUGIN_ASSISTANT,
  AI_PLUGIN_SUMMARY
])

export function isFirstPartyAIPlugin(pluginId: string): boolean {
  return FIRST_PARTY_AI.has(pluginId)
}

export type AgentWritesMode = 'read_only' | 'confirm' | 'auto'

export interface AIFeaturesSnapshot {
  enabled: boolean
  rag_enabled: boolean
  summaries_enabled: boolean
  /** Agent vault mutation policy (#924). Missing/invalid → confirm. */
  agent_writes: AgentWritesMode
}

export function normalizeAgentWritesMode(value: unknown): AgentWritesMode {
  if (value === 'read_only' || value === 'confirm' || value === 'auto') {
    return value
  }
  return 'confirm'
}

/** Read feature flags from the reactive settings store (defaults all off). */
export function readAIFeatures(): AIFeaturesSnapshot {
  const f = (
    settings.config?.ai as
      | {
          features?: {
            enabled?: boolean
            rag_enabled?: boolean
            summaries_enabled?: boolean
            agent_writes?: string
          }
        }
      | undefined
  )?.features
  return {
    enabled: f?.enabled === true,
    rag_enabled: f?.rag_enabled === true,
    summaries_enabled: f?.summaries_enabled === true,
    agent_writes: normalizeAgentWritesMode(f?.agent_writes)
  }
}

/**
 * Whether a first-party AI plugin should register a session under the current
 * feature flags. Mirrors backend config.AIPluginLoadEnabled.
 */
export function shouldLoadAIPlugin(pluginId: string): boolean {
  const f = readAIFeatures()
  switch (pluginId) {
    case AI_PLUGIN_AGENT:
    case AI_PLUGIN_ASSISTANT:
      return f.enabled
    case AI_PLUGIN_QA:
      return f.enabled && f.rag_enabled
    case AI_PLUGIN_SUMMARY:
      return f.enabled && f.summaries_enabled
    default:
      return false
  }
}

export function getAIAvailability() {
  const features = readAIFeatures()
  const chat = settings.config?.ai?.chat as
    { provider_type?: string; model?: string; has_key?: boolean } | undefined
  const embedding = settings.config?.ai?.embedding as
    { provider_type?: string; model?: string; has_key?: boolean } | undefined
  const chatReady = !aiProviderNeedsSetup(chat)
  const embedReady = !embeddingProviderNeedsSetup(embedding)
  const aiEnabled = features.enabled
  const ragEnabled = features.enabled && features.rag_enabled
  const summariesEnabled = features.enabled && features.summaries_enabled
  // Show the titlebar control whenever master AI is on so the user can open
  // the drawer and see the chat-setup empty-state when the model is missing.
  const drawerAvailable = aiEnabled

  return {
    aiEnabled,
    ragEnabled,
    summariesEnabled,
    chatReady,
    embedReady,
    drawerAvailable,
    features
  }
}
