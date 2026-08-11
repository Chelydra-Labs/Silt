import { beforeEach, describe, expect, it, vi } from 'vitest'

const { settingsMock } = vi.hoisted(() => ({
  settingsMock: {
    config: null as null | {
      ai?: {
        features?: {
          enabled?: boolean
          rag_enabled?: boolean
          summaries_enabled?: boolean
        }
        chat?: { provider_type?: string; model?: string; has_key?: boolean }
        embedding?: {
          provider_type?: string
          model?: string
          has_key?: boolean
        }
      }
    }
  }
}))

vi.mock('../../../settings/store.svelte', () => ({
  settings: settingsMock
}))

import {
  getAIAvailability,
  isFirstPartyAIPlugin,
  shouldLoadAIPlugin
} from './availability'

function setFeatures(
  features: {
    enabled?: boolean
    rag_enabled?: boolean
    summaries_enabled?: boolean
  },
  providers?: {
    chat?: { provider_type?: string; model?: string; has_key?: boolean }
    embedding?: {
      provider_type?: string
      model?: string
      has_key?: boolean
    }
  }
) {
  settingsMock.config = {
    ai: {
      features: {
        enabled: false,
        rag_enabled: false,
        summaries_enabled: false,
        ...features
      },
      chat: providers?.chat ?? { provider_type: 'local', model: 'qwen' },
      embedding: providers?.embedding ?? {
        provider_type: 'local',
        model: 'nomic'
      }
    }
  }
}

describe('AI availability matrix', () => {
  beforeEach(() => {
    settingsMock.config = null
  })

  it('defaults all flags off when config is missing', () => {
    const a = getAIAvailability()
    expect(a.aiEnabled).toBe(false)
    expect(a.ragEnabled).toBe(false)
    expect(a.summariesEnabled).toBe(false)
    expect(a.drawerAvailable).toBe(false)
    expect(a.chatReady).toBe(false)
    expect(a.features.agent_writes).toBe('confirm')
  })

  it('master AI enables drawer even when chat is not ready', () => {
    setFeatures(
      { enabled: true },
      {
        chat: { provider_type: 'openai-compatible', model: '', has_key: false }
      }
    )
    const a = getAIAvailability()
    expect(a.aiEnabled).toBe(true)
    expect(a.drawerAvailable).toBe(true)
    expect(a.chatReady).toBe(false)
  })

  it('clamps rag/summaries to master off', () => {
    setFeatures({
      enabled: false,
      rag_enabled: true,
      summaries_enabled: true
    })
    const a = getAIAvailability()
    expect(a.ragEnabled).toBe(false)
    expect(a.summariesEnabled).toBe(false)
  })

  it('shouldLoadAIPlugin matches Go AIPluginLoadEnabled mapping', () => {
    setFeatures({ enabled: false })
    expect(shouldLoadAIPlugin('silt-ai-agent')).toBe(false)
    expect(shouldLoadAIPlugin('silt-ai-assistant')).toBe(false)
    expect(shouldLoadAIPlugin('silt-ai-qa')).toBe(false)
    expect(shouldLoadAIPlugin('silt-ai-summary')).toBe(false)

    setFeatures({ enabled: true })
    expect(shouldLoadAIPlugin('silt-ai-agent')).toBe(true)
    expect(shouldLoadAIPlugin('silt-ai-assistant')).toBe(true)
    expect(shouldLoadAIPlugin('silt-ai-qa')).toBe(false)
    expect(shouldLoadAIPlugin('silt-ai-summary')).toBe(false)

    setFeatures({ enabled: true, rag_enabled: true })
    expect(shouldLoadAIPlugin('silt-ai-qa')).toBe(true)

    setFeatures({ enabled: true, summaries_enabled: true })
    expect(shouldLoadAIPlugin('silt-ai-summary')).toBe(true)

    expect(shouldLoadAIPlugin('silt-tasks')).toBe(false)
  })

  it('identifies first-party AI plugin ids', () => {
    expect(isFirstPartyAIPlugin('silt-ai-agent')).toBe(true)
    expect(isFirstPartyAIPlugin('silt-tasks')).toBe(false)
  })

  it('reports embedReady from embedding provider setup', () => {
    setFeatures(
      { enabled: true, rag_enabled: true },
      {
        chat: { provider_type: 'local', model: 'qwen' },
        embedding: {
          provider_type: 'openai-compatible',
          model: 'text-embedding',
          has_key: false
        }
      }
    )
    const a = getAIAvailability()
    expect(a.embedReady).toBe(false)
    expect(a.ragEnabled).toBe(true)
  })
})
