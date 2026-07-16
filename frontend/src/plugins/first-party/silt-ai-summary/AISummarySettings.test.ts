import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import type { PluginContext } from '../../sdk'

// Settings store is read for the unconfigured nudge only (#632: enablement
// lives under Settings → AI → Features, not plugins.disabled).
const { mockSettings } = vi.hoisted(() => ({
  mockSettings: {
    config: {
      plugins: { disabled: [], plugin_settings: {} },
      ai: {
        chat: { model: 'qwen3:30b', provider_type: 'local' },
        features: { enabled: true, summaries_enabled: true }
      }
    }
  }
}))

vi.mock('../../../settings/store.svelte', () => ({
  settings: mockSettings,
  saveConfig: vi.fn(),
  loadConfig: vi.fn()
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
    updatePluginSetting: vi.fn(async () => true),
    openSettings: vi.fn()
  } as unknown as PluginContext
}

describe('AISummarySettings', () => {
  beforeEach(() => {
    mockSettings.config.ai.chat.model = 'qwen3:30b'
  })

  it('renders the title + tuning controls once settings load', async () => {
    const ctx = makeCtx()
    const { findByLabelText, getByText } = render(AISummarySettings, {
      props: {
        ctx,
        manifest: { id: 'silt-ai-summary', name: 'AI Summary' } as any
      }
    })
    expect(getByText('AI Summary')).toBeTruthy()
    expect(await findByLabelText(/Summary length/i)).toBeTruthy()
  })

  it('points enablement at Settings → AI Features (no plugins.disabled toggle)', async () => {
    const ctx = makeCtx()
    const { findByRole, queryByRole } = render(AISummarySettings, {
      props: {
        ctx,
        manifest: { id: 'silt-ai-summary', name: 'AI Summary' } as any
      }
    })
    expect(
      await findByRole('region', { name: /Managed enablement/i })
    ).toBeTruthy()
    // No independent enable switch remains on this page.
    expect(
      queryByRole('checkbox', { name: /Enable note summaries/i })
    ).toBeNull()
    expect(queryByRole('checkbox', { name: /Generate summaries/i })).toBeNull()
  })

  it('shows the unconfigured nudge when no chat model is configured', () => {
    mockSettings.config.ai.chat.model = ''
    const { getByText, getByRole } = render(AISummarySettings, {
      props: {
        ctx: makeCtx(),
        manifest: { id: 'silt-ai-summary', name: 'AI Summary' } as any
      }
    })
    expect(getByText(/No AI provider is configured/i)).toBeTruthy()
    expect(getByRole('button', { name: /Open AI settings/i })).toBeTruthy()
  })

  it('changing summary length persists via ctx.updatePluginSetting', async () => {
    const ctx = makeCtx()
    const { findByLabelText } = render(AISummarySettings, {
      props: {
        ctx,
        manifest: { id: 'silt-ai-summary', name: 'AI Summary' } as any
      }
    })
    const select = (await findByLabelText(
      /Summary length/i
    )) as HTMLSelectElement
    await fireEvent.change(select, { target: { value: 'long' } })
    expect(ctx.updatePluginSetting).toHaveBeenCalledWith(
      'summary_length',
      'long'
    )
  })

  it('exposes the generation trigger as an accessible radiogroup', async () => {
    const ctx = makeCtx()
    const { findAllByRole, findByRole } = render(AISummarySettings, {
      props: {
        ctx,
        manifest: { id: 'silt-ai-summary', name: 'AI Summary' } as any
      }
    })
    const group = await findByRole('radiogroup', {
      name: /generation trigger/i
    })
    const radios = await findAllByRole('radio')
    expect(radios).toHaveLength(2)
    const withinGroup = radios.filter((r) => group.contains(r))
    expect(withinGroup).toHaveLength(2)
    const autoRadio = withinGroup.find((r) =>
      (r.closest('label')?.textContent ?? '').includes('Automatically on open')
    )!
    expect(autoRadio).toBeChecked()
  })

  it('toggling a facet persists the whole facets object', async () => {
    const ctx = makeCtx()
    const { findAllByRole } = render(AISummarySettings, {
      props: {
        ctx,
        manifest: { id: 'silt-ai-summary', name: 'AI Summary' } as any
      }
    })
    await findAllByRole('checkbox')
    const checkboxes = await findAllByRole('checkbox')
    const risks = checkboxes.find((c) =>
      c.closest('label')?.textContent?.includes('Risks')
    )!
    await fireEvent.click(risks)
    expect(ctx.updatePluginSetting).toHaveBeenCalledWith('facets', {
      tasks: true,
      risks: false,
      decisions: true
    })
  })
})
