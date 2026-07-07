import { beforeEach, describe, expect, it, vi } from 'vitest'

// Mock the settings store so readProviderInfo (which reads settings.config.ai)
// is controllable. ai-setup runs real (it's the pure predicate under test).
const { mockSettings } = vi.hoisted(() => ({
  mockSettings: {
    config: { ai: { chat: { model: '', provider_type: 'local' } as Record<string, unknown> } }
  }
}))
vi.mock('../../../settings/store.svelte', () => ({
  settings: mockSettings,
  loadConfig: vi.fn(),
  saveConfig: vi.fn(),
  updatePluginSetting: vi.fn()
}))

import { decideMountKind } from './mountKind'
import { readProviderInfo } from './state.svelte'
import { aiProviderNeedsSetup } from '../../../settings/ai-setup'

describe('decideMountKind (on-demand + dismissal)', () => {
  it('mounts the banner when not dismissed and not on-demand', () => {
    expect(decideMountKind({ dismissed: false, onDemandOnly: false })).toBe('banner')
  })
  it('mounts the re-open chip when the note is dismissed', () => {
    expect(decideMountKind({ dismissed: true, onDemandOnly: false })).toBe('reopen')
  })
  it('mounts the re-open chip in on-demand mode even when not dismissed', () => {
    // The #220/#221 fix: on-demand suppresses the auto banner so the user
    // drives every generation. Previously the banner mounted and rendered a
    // perpetual skeleton because no generation fired.
    expect(decideMountKind({ dismissed: false, onDemandOnly: true })).toBe('reopen')
  })
  it('dismissed + on-demand still mounts the chip (dismissal wins either way)', () => {
    expect(decideMountKind({ dismissed: true, onDemandOnly: true })).toBe('reopen')
  })
})

describe('readProviderInfo coherence with aiProviderNeedsSetup (#450)', () => {
  beforeEach(() => {
    mockSettings.config.ai.chat = { model: '', provider_type: 'local' }
  })

  it('treats a local provider with a model as configured (matches the helper)', () => {
    mockSettings.config.ai.chat = { model: 'qwen3:30b', provider_type: 'local' }
    const info = readProviderInfo()
    expect(info.isConfigured).toBe(!aiProviderNeedsSetup(mockSettings.config.ai.chat))
    expect(info.isConfigured).toBe(true)
    expect(info.configuredModel).toBe('qwen3:30b')
  })

  it('treats no model as unconfigured (matches the helper)', () => {
    mockSettings.config.ai.chat = { model: '', provider_type: 'openai-compatible', has_key: true }
    const info = readProviderInfo()
    expect(info.isConfigured).toBe(!aiProviderNeedsSetup(mockSettings.config.ai.chat))
    expect(info.isConfigured).toBe(false)
  })

  it('agrees with the helper across the local/openai × model/key matrix', () => {
    const cases: Record<string, unknown>[] = [
      { model: 'm', provider_type: 'local' },
      { model: '', provider_type: 'local' },
      { model: 'm', provider_type: 'openai-compatible' }, // unknown key → helper says ready
      { model: 'm', provider_type: 'openai-compatible', has_key: false } // helper says needs setup
    ]
    for (const chat of cases) {
      mockSettings.config.ai.chat = chat
      const info = readProviderInfo()
      expect(info.isConfigured, `for ${JSON.stringify(chat)}`).toBe(!aiProviderNeedsSetup(chat as any))
    }
  })
})
