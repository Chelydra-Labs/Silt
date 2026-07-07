import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import type { PluginContext } from '../../sdk'
import type { SummarySettings } from './types'

// vi.hoisted lifts the mock handles above the vi.mock factories (which are
// themselves hoisted above all imports) so the factories can reference them.
const { mockController, mockAppSettings } = vi.hoisted(() => {
  const defaultSettings: SummarySettings = {
    auto_on_open: true,
    on_demand_only: false,
    summary_length: 'medium',
    facets: { tasks: true, risks: true, decisions: true },
    regenerate_debounce_ms: 3000,
    max_note_chars: 12000,
    dismissed_notes: []
  }
  const mockController = {
    state: new Map<string, any>(),
    getSettings: vi.fn((): SummarySettings => ({ ...defaultSettings, facets: { ...defaultSettings.facets } })),
    generateFor: vi.fn(async () => ({ ok: true, result: {} }))
  }
  const mockAppSettings = {
    config: { ai: { chat: { model: 'qwen3:30b', provider_type: 'local' } } }
  }
  return { mockController, mockAppSettings }
})

vi.mock('./index', () => ({ getController: () => mockController }))
vi.mock('../../../settings/store.svelte', () => ({
  settings: mockAppSettings,
  loadConfig: vi.fn(),
  saveConfig: vi.fn(),
  updatePluginSetting: vi.fn()
}))

import SummaryBanner from './SummaryBanner.svelte'

function makeCtx(overrides: Partial<PluginContext> = {}): PluginContext {
  return {
    activeNotebook: 'Work',
    activeSection: 'Journal',
    activePage: 'Daily',
    updatePluginSetting: vi.fn(async () => true),
    ...overrides
  } as unknown as PluginContext
}

const PAGE_ID = 'Work/Journal/Daily'

function setPageState(state: any) {
  mockController.state.set(PAGE_ID, state)
}
function clearPageState() {
  mockController.state.clear()
}

describe('SummaryBanner', () => {
  beforeEach(() => {
    clearPageState()
    mockController.generateFor.mockReset()
    mockController.generateFor.mockResolvedValue({ ok: true, result: {} })
    mockAppSettings.config.ai.chat.model = 'qwen3:30b'
    mockAppSettings.config.ai.chat.provider_type = 'local'
  })

  it('renders the unconfigured nudge when no chat model is configured', () => {
    mockAppSettings.config.ai.chat.model = ''
    const { getByText } = render(SummaryBanner, {
      props: { ctx: makeCtx(), onDismiss: () => {} }
    })
    expect(getByText(/Configure an AI provider/i)).toBeTruthy()
  })

  it('renders the loading skeleton while a generation is in flight', () => {
    setPageState({ status: 'loading' })
    const { container } = render(SummaryBanner, {
      props: { ctx: makeCtx(), onDismiss: () => {} }
    })
    expect(container.querySelector('.skeleton')).toBeTruthy()
  })

  it('renders the summary + facet items with New badges in the ready state', () => {
    setPageState({
      status: 'ready',
      stale: false,
      result: {
        ok: true,
        result: {
          summary: 'The team agreed to ship the API next week.',
          tasks: ['Ship the API', 'Write the brief'],
          risks: [],
          decisions: ['Go with option B'],
          newItems: { tasks: ['Ship the API'], risks: [], decisions: [] },
          fromCache: false,
          model: 'qwen3:30b',
          generatedAt: '2026-07-06T10:00:00Z'
        }
      }
    })
    const { getByText, container } = render(SummaryBanner, {
      props: { ctx: makeCtx(), onDismiss: () => {} }
    })
    expect(getByText(/ship the API next week/i)).toBeTruthy()
    expect(getByText('Ship the API')).toBeTruthy()
    // "Ship the API" is a new item → a New pill is rendered.
    expect(container.querySelectorAll('.new-pill').length).toBeGreaterThan(0)
    // "Write the brief" is not new → no New pill on it.
    expect(container.querySelectorAll('.new-pill').length).toBe(1)
  })

  it('renders the muted empty state when the summary is blank', () => {
    setPageState({
      status: 'ready',
      stale: false,
      result: {
        ok: true,
        result: {
          summary: '',
          tasks: [],
          risks: [],
          decisions: [],
          newItems: { tasks: [], risks: [], decisions: [] },
          fromCache: true,
          model: '',
          generatedAt: '2026-07-06T10:00:00Z'
        }
      }
    })
    const { getByText, container } = render(SummaryBanner, {
      props: { ctx: makeCtx(), onDismiss: () => {} }
    })
    expect(getByText(/Nothing to highlight/i)).toBeTruthy()
    expect(container.querySelector('.facets')).toBeNull()
  })

  it('renders an inline error + Retry in the error state', () => {
    setPageState({
      status: 'error',
      result: {
        ok: false,
        error: { code: 'provider-error', message: 'endpoint unreachable' }
      }
    })
    const { getByText, getByRole } = render(SummaryBanner, {
      props: { ctx: makeCtx(), onDismiss: () => {} }
    })
    expect(getByText(/Couldn't generate a summary/i)).toBeTruthy()
    expect(getByRole('button', { name: /Retry/i })).toBeTruthy()
  })

  it('persists dismissal then calls onDismiss when the close button is clicked', async () => {
    setPageState({
      status: 'ready',
      stale: false,
      result: {
        ok: true,
        result: {
          summary: 's',
          tasks: [],
          risks: [],
          decisions: [],
          newItems: { tasks: [], risks: [], decisions: [] },
          fromCache: false,
          model: 'm',
          generatedAt: '2026-07-06T10:00:00Z'
        }
      }
    })
    let dismissed = false
    const ctx = makeCtx()
    const { getByRole } = render(SummaryBanner, {
      props: { ctx, onDismiss: () => { dismissed = true } }
    })
    await fireEvent.click(getByRole('button', { name: /Dismiss AI summary/i }))
    expect(ctx.updatePluginSetting).toHaveBeenCalledWith('dismissed_notes', [PAGE_ID])
    expect(dismissed).toBe(true)
  })

  it('triggers a forced regeneration when Regenerate is clicked', async () => {
    setPageState({
      status: 'ready',
      stale: false,
      result: {
        ok: true,
        result: {
          summary: 's',
          tasks: [],
          risks: [],
          decisions: [],
          newItems: { tasks: [], risks: [], decisions: [] },
          fromCache: true,
          model: 'm',
          generatedAt: '2026-07-06T10:00:00Z'
        }
      }
    })
    const ctx = makeCtx()
    const { getByRole } = render(SummaryBanner, {
      props: { ctx, onDismiss: () => {} }
    })
    await fireEvent.click(getByRole('button', { name: /Regenerate summary/i }))
    expect(mockController.generateFor).toHaveBeenCalledWith(ctx, PAGE_ID, { force: true })
  })

  it('hides a facet the user has toggled off in settings', () => {
    mockController.getSettings = vi.fn((): SummarySettings => ({
      auto_on_open: true,
      on_demand_only: false,
      summary_length: 'medium',
      facets: { tasks: true, risks: false, decisions: true },
      regenerate_debounce_ms: 3000,
      max_note_chars: 12000,
      dismissed_notes: []
    }))
    setPageState({
      status: 'ready',
      stale: false,
      result: {
        ok: true,
        result: {
          summary: 's',
          tasks: ['t'],
          risks: ['r'],
          decisions: ['d'],
          newItems: { tasks: [], risks: [], decisions: [] },
          fromCache: false,
          model: 'm',
          generatedAt: '2026-07-06T10:00:00Z'
        }
      }
    })
    const { queryByText, getByText } = render(SummaryBanner, {
      props: { ctx: makeCtx(), onDismiss: () => {} }
    })
    expect(getByText('Tasks')).toBeTruthy()
    expect(getByText('Decisions')).toBeTruthy()
    expect(queryByText('Risks')).toBeNull() // toggled off
  })
})
