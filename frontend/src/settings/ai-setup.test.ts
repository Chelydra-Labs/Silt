import { describe, expect, it } from 'vitest'
import { aiProviderNeedsSetup } from './ai-setup'

describe('aiProviderNeedsSetup (#450 unified setup predicate)', () => {
  it('needs setup when there is no chat config at all', () => {
    expect(aiProviderNeedsSetup(null)).toBe(true)
    expect(aiProviderNeedsSetup(undefined)).toBe(true)
    expect(aiProviderNeedsSetup({})).toBe(true)
  })

  it('needs setup when no model is chosen, regardless of provider/key', () => {
    expect(aiProviderNeedsSetup({ provider_type: 'local', model: '' })).toBe(
      true
    )
    expect(
      aiProviderNeedsSetup({
        provider_type: 'openai-compatible',
        model: '',
        has_key: true
      })
    ).toBe(true)
  })

  it('local provider with a model is ready (keyless — Ollama)', () => {
    expect(
      aiProviderNeedsSetup({ provider_type: 'local', model: 'qwen3:30b' })
    ).toBe(false)
    // Even with has_key explicitly false: local runs keyless.
    expect(
      aiProviderNeedsSetup({
        provider_type: 'local',
        model: 'qwen3:30b',
        has_key: false
      })
    ).toBe(false)
  })

  it('openai-compatible with a model + key is ready', () => {
    expect(
      aiProviderNeedsSetup({
        provider_type: 'openai-compatible',
        model: 'gpt-4o',
        has_key: true
      })
    ).toBe(false)
  })

  it('openai-compatible with a model but KNOWN-absent key needs setup', () => {
    expect(
      aiProviderNeedsSetup({
        provider_type: 'openai-compatible',
        model: 'gpt-4o',
        has_key: false
      })
    ).toBe(true)
  })

  it('treats an UNKNOWN key state (Plugins-tab view) as non-blocking', () => {
    // The Plugins tab reads SystemConfig, where keys are scrubbed. A model is
    // set so the badge must not fire; a genuinely-missing key surfaces as a
    // retryable call-time error instead.
    expect(
      aiProviderNeedsSetup({
        provider_type: 'openai-compatible',
        model: 'gpt-4o'
      })
    ).toBe(false)
  })

  it('coherence: a missing model fires on BOTH the full and the keyless view', () => {
    // This is the click-the-badge-lands-on-a-nudge guarantee: when the Plugins
    // badge shows (!model), the AI Provider tab (full view) must also show.
    const pluginsView = { provider_type: 'openai-compatible', model: '' }
    const providerTabView = {
      provider_type: 'openai-compatible',
      model: '',
      has_key: false
    }
    expect(aiProviderNeedsSetup(pluginsView)).toBe(true)
    expect(aiProviderNeedsSetup(providerTabView)).toBe(true)
  })
})
