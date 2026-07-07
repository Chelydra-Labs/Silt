import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import type { PluginContext } from '../../sdk'

// The settings store is read for enabled-state + the unconfigured nudge, and
// saveConfig is called on the enable/disable toggle. Mock both.
const { mockSettings, mockSaveConfig, mockLoadPlugins, mockTeardown } = vi.hoisted(() => ({
  mockSettings: {
    config: {
      plugins: { disabled: ['silt-ai-summary'], plugin_settings: {} },
      ai: { chat: { model: 'qwen3:30b', provider_type: 'local' } }
    }
  },
  mockSaveConfig: vi.fn(async () => true),
  mockLoadPlugins: vi.fn(async () => ({ plugins: new Map(), loadersReady: true })),
  mockTeardown: vi.fn()
}))

vi.mock('../../../settings/store.svelte', () => ({
  settings: mockSettings,
  saveConfig: mockSaveConfig,
  loadConfig: vi.fn()
}))
vi.mock('../../loader', () => ({
  loadPlugins: mockLoadPlugins,
  teardownPlugin: mockTeardown
}))

import AISummarySettings from './AISummarySettings.svelte'

function makeCtx(): PluginContext {
  return {
    getPluginSettings: vi.fn(async () => ({
      auto_on_open: true,
      on_demand_only: false,
      summary_length: 'medium',
      facets: { tasks: true, risks: true, decisions: true },
      regenerate_debounce_ms: 3000,
      max_note_chars: 12000,
      dismissed_notes: []
    })),
    updatePluginSetting: vi.fn(async () => true)
  } as unknown as PluginContext
}

describe('AISummarySettings', () => {
  beforeEach(() => {
    mockSaveConfig.mockReset()
    mockLoadPlugins.mockReset()
    mockTeardown.mockReset()
    mockSaveConfig.mockResolvedValue(true)
    mockLoadPlugins.mockResolvedValue({ plugins: new Map(), loadersReady: true })
    // Start each test with the plugin disabled (enabled=false) so the toggle
    // exercises the enable path.
    mockSettings.config.plugins.disabled = ['silt-ai-summary']
    mockSettings.config.ai.chat.model = 'qwen3:30b'
  })

  it('renders the title + tuning controls once settings load', async () => {
    const ctx = makeCtx()
    const { findByLabelText, getByText } = render(AISummarySettings, {
      props: { ctx, manifest: { id: 'silt-ai-summary', name: 'AI Summary' } as any }
    })
    expect(getByText('AI Summary')).toBeTruthy()
    // The summary-length select loads from ctx.getPluginSettings.
    expect(await findByLabelText(/Summary length/i)).toBeTruthy()
  })

  it('shows the unconfigured nudge when no chat model is configured', () => {
    mockSettings.config.ai.chat.model = ''
    const { getByText } = render(AISummarySettings, {
      props: { ctx: makeCtx(), manifest: { id: 'silt-ai-summary', name: 'AI Summary' } as any }
    })
    expect(getByText(/No AI provider is configured/i)).toBeTruthy()
  })

  it('toggling enabled writes plugins.disabled + reloads the plugin', async () => {
    const ctx = makeCtx()
    const { findByLabelText } = render(AISummarySettings, {
      props: { ctx, manifest: { id: 'silt-ai-summary', name: 'AI Summary' } as any }
    })
    const toggle = (await findByLabelText(/Generate summaries/i)) as HTMLInputElement
    // Currently disabled → toggle is unchecked; enabling removes it from the list.
    await fireEvent.click(toggle)
    expect(mockSaveConfig).toHaveBeenCalledTimes(1)
    const savedCfg = (mockSaveConfig.mock.calls as any[])[0][0]
    expect(savedCfg.plugins.disabled).not.toContain('silt-ai-summary')
    expect(mockLoadPlugins).toHaveBeenCalled()
  })

  it('changing summary length persists via ctx.updatePluginSetting', async () => {
    const ctx = makeCtx()
    const { findByLabelText } = render(AISummarySettings, {
      props: { ctx, manifest: { id: 'silt-ai-summary', name: 'AI Summary' } as any }
    })
    const select = (await findByLabelText(/Summary length/i)) as HTMLSelectElement
    await fireEvent.change(select, { target: { value: 'long' } })
    expect(ctx.updatePluginSetting).toHaveBeenCalledWith('summary_length', 'long')
  })

  it('toggling a facet persists the whole facets object', async () => {
    const ctx = makeCtx()
    const { findAllByRole } = render(AISummarySettings, {
      props: { ctx, manifest: { id: 'silt-ai-summary', name: 'AI Summary' } as any }
    })
    // Wait for load, then click the Risks checkbox.
    await findAllByRole('checkbox')
    const checkboxes = await findAllByRole('checkbox')
    // The first checkbox is the Enabled toggle; facets follow.
    const risks = checkboxes.find((c) => c.closest('label')?.textContent?.includes('Risks'))!
    await fireEvent.click(risks)
    expect(ctx.updatePluginSetting).toHaveBeenCalledWith('facets', {
      tasks: true,
      risks: false,
      decisions: true
    })
  })
})
