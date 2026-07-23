import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import { tick } from 'svelte'
import type { PluginContext } from '../../sdk'
import type { SummarySettings } from './types'
import type { PageState } from './state.svelte'

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
    state: {} as Record<string, PageState>,
    getSettings: vi.fn((): SummarySettings => ({
      ...defaultSettings,
      facets: { ...defaultSettings.facets }
    })),
    generateFor: vi.fn(async () => ({ ok: true, result: {} }))
  }
  const mockAppSettings = {
    config: { ai: { chat: { model: 'qwen3:30b', provider_type: 'local' } } }
  }
  return { mockController, mockAppSettings }
})

vi.mock('./index', () => ({
  getController: () => mockController,
  // The contract under test: SummaryBanner must use THIS id (the one the host
  // registers the banner under) for its data-banner-close attribute. Providing
  // it via the mock verifies the component reads the imported binding rather
  // than a hardcoded literal.
  BANNER_SURFACE_ID: 'silt-ai-summary:banner'
}))
vi.mock('../../../settings/store.svelte', () => ({
  settings: mockAppSettings,
  loadConfig: vi.fn(),
  saveConfig: vi.fn(),
  updatePluginSetting: vi.fn()
}))

import SummaryBanner from './SummaryBanner.svelte'
import { createSummaryController } from './state.svelte'

function makeCtx(overrides: Partial<PluginContext> = {}): PluginContext {
  return {
    activeNotebook: 'Work',
    activeSection: 'Journal',
    activePage: 'Daily',
    updatePluginSetting: vi.fn(async () => true),
    openSettings: vi.fn(),
    ...overrides
  } as unknown as PluginContext
}

const PAGE_ID = 'Work/Journal/Daily'

function setPageState(state: unknown) {
  mockController.state[PAGE_ID] = state as never
}
function clearPageState() {
  for (const k of Object.keys(mockController.state)) {
    delete mockController.state[k]
  }
}

describe('SummaryBanner', () => {
  beforeEach(() => {
    clearPageState()
    mockController.generateFor.mockReset()
    mockController.generateFor.mockResolvedValue({ ok: true, result: {} })
    mockAppSettings.config.ai.chat.model = 'qwen3:30b'
    mockAppSettings.config.ai.chat.provider_type = 'local'
  })

  it('sets data-banner-close to the registered surface id (cross-banner focus contract)', () => {
    // The host's PluginNoteBanners queries `[data-banner-close="<registered id>"]`
    // to forward focus after a dismiss. A mismatch silently drops focus. This
    // pins the contract: the close button carries the imported BANNER_SURFACE_ID.
    const { container } = render(SummaryBanner, {
      props: { ctx: makeCtx(), onDismiss: () => {} }
    })
    const closeBtn = container.querySelector<HTMLButtonElement>(
      '[data-banner-close="silt-ai-summary:banner"]'
    )
    expect(closeBtn).toBeTruthy()
    expect(closeBtn?.getAttribute('aria-label')).toBe('Dismiss AI summary')
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
      props: {
        ctx,
        onDismiss: () => {
          dismissed = true
        }
      }
    })
    await fireEvent.click(getByRole('button', { name: /Dismiss AI summary/i }))
    // No contentHash on pageState → falls back to bare pageId (#455 fallback
    // for error / pre-summary states).
    expect(ctx.updatePluginSetting).toHaveBeenCalledWith('dismissed_notes', [
      PAGE_ID
    ])
    expect(dismissed).toBe(true)
  })

  // --- #455 keyed dismissal: `${pageId}:${contentHash}` ----------------------
  it('keys dismissal by `${pageId}:${contentHash}` when the hash is available', async () => {
    // The crux of #455: dismissal is bound to the dismissed content version,
    // so editing the note (changing the hash) re-shows the banner.
    setPageState({
      status: 'ready',
      stale: false,
      contentHash: 'abc123def',
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
          generatedAt: '2026-07-06T10:00:00Z',
          contentHash: 'abc123def'
        }
      }
    })
    const ctx = makeCtx()
    const { getByRole } = render(SummaryBanner, {
      props: { ctx, onDismiss: () => {} }
    })
    await fireEvent.click(getByRole('button', { name: /Dismiss AI summary/i }))
    expect(ctx.updatePluginSetting).toHaveBeenCalledWith('dismissed_notes', [
      `${PAGE_ID}:abc123def`
    ])
  })

  it('does not duplicate the dismiss key when the same hash is already dismissed', async () => {
    // Re-dismissing the same content version is a no-op on the persisted list
    // (the bounded-list guard) — the banner still dismisses visually.
    mockController.getSettings = vi.fn((): SummarySettings => ({
      auto_on_open: true,
      on_demand_only: false,
      summary_length: 'medium',
      facets: { tasks: true, risks: true, decisions: true },
      regenerate_debounce_ms: 3000,
      max_note_chars: 12000,
      dismissed_notes: [`${PAGE_ID}:abc123def`]
    }))
    setPageState({
      status: 'ready',
      stale: false,
      contentHash: 'abc123def',
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
          generatedAt: '2026-07-06T10:00:00Z',
          contentHash: 'abc123def'
        }
      }
    })
    let dismissed = false
    const ctx = makeCtx()
    const { getByRole } = render(SummaryBanner, {
      props: {
        ctx,
        onDismiss: () => {
          dismissed = true
        }
      }
    })
    await fireEvent.click(getByRole('button', { name: /Dismiss AI summary/i }))
    // Already-present → no IPC write (the bounded-list guard).
    expect(ctx.updatePluginSetting).not.toHaveBeenCalled()
    expect(dismissed).toBe(true)
  })

  it('bounds dismissed_notes to MAX_DISMISSED, dropping the oldest entry', async () => {
    // Pre-fill the dismissed list past the cap so the new dismiss must trim.
    // pageId for this render is Work/Journal/Daily; the list holds 500 prior
    // ids, so appending one more (501) drops the oldest.
    const stale: string[] = Array.from({ length: 500 }, (_, i) => `NB/S/p${i}`)
    mockController.getSettings = vi.fn((): SummarySettings => ({
      auto_on_open: true,
      on_demand_only: false,
      summary_length: 'medium',
      facets: { tasks: true, risks: true, decisions: true },
      regenerate_debounce_ms: 3000,
      max_note_chars: 12000,
      dismissed_notes: stale
    }))
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
    const ctx = makeCtx()
    const { getByRole } = render(SummaryBanner, {
      props: { ctx, onDismiss: () => {} }
    })
    await fireEvent.click(getByRole('button', { name: /Dismiss AI summary/i }))
    const saved = (ctx.updatePluginSetting as ReturnType<typeof vi.fn>).mock
      .calls[0][1] as string[]
    expect(saved).toHaveLength(500)
    expect(saved[saved.length - 1]).toBe(PAGE_ID)
    // Oldest entry (p0) was trimmed to make room.
    expect(saved[0]).toBe('NB/S/p1')
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
    expect(mockController.generateFor).toHaveBeenCalledWith(ctx, PAGE_ID, {
      force: true
    })
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

  // --- Stale state (finding 3: dimming + freshness hidden) -----------------
  it('applies the is-stale class and hides the freshness line during a regen', () => {
    setPageState({
      status: 'ready',
      stale: true,
      result: {
        ok: true,
        result: {
          summary: 'Prior summary still readable.',
          tasks: ['Old task'],
          risks: [],
          decisions: [],
          newItems: { tasks: [], risks: [], decisions: [] },
          fromCache: true,
          model: 'm',
          generatedAt: '2026-07-06T10:00:00Z'
        }
      }
    })
    const { container } = render(SummaryBanner, {
      props: { ctx: makeCtx(), onDismiss: () => {} }
    })
    // The stale class drives the opacity dimming (CSS-scoped to .is-stale).
    expect(container.querySelector('.summary-banner.is-stale')).toBeTruthy()
    // The "Updating…" affordance replaces the freshness line during stale.
    expect(container.querySelector('.updating-line')).toBeTruthy()
    expect(container.querySelector('.freshness-line')).toBeNull()
  })

  // --- Live-region announcements (finding 1: SR completion signal) ---------
  it('announces "Summary ready." when the result has no new diff items', () => {
    setPageState({
      status: 'ready',
      stale: false,
      result: {
        ok: true,
        result: {
          summary: 'A summary with no new items.',
          tasks: ['Existing task'],
          risks: [],
          decisions: [],
          newItems: { tasks: [], risks: [], decisions: [] },
          fromCache: true,
          model: 'm',
          generatedAt: '2026-07-06T10:00:00Z'
        }
      }
    })
    const { container } = render(SummaryBanner, {
      props: { ctx: makeCtx(), onDismiss: () => {} }
    })
    const liveRegion = container.querySelector('[aria-live="polite"]')
    expect(liveRegion?.textContent).toMatch(/Summary ready/i)
  })

  it('announces "Summarizing…" during a fresh load', () => {
    setPageState({ status: 'loading' })
    const { container } = render(SummaryBanner, {
      props: { ctx: makeCtx(), onDismiss: () => {} }
    })
    const liveRegion = container.querySelector('[aria-live="polite"]')
    expect(liveRegion?.textContent).toMatch(/Summarizing/i)
  })

  // --- Error branches (finding 4: oversized dead-end + fetch-failed) -------
  it('hides Retry and points at the setting for oversized errors', () => {
    setPageState({
      status: 'error',
      result: {
        ok: false,
        error: { code: 'oversized', message: 'too long' }
      }
    })
    const { queryByRole, getByText } = render(SummaryBanner, {
      props: { ctx: makeCtx(), onDismiss: () => {} }
    })
    // Retry is hidden — the note hasn't shrunk, so it would deterministically
    // fail again.
    expect(queryByRole('button', { name: 'Retry' })).toBeNull()
    expect(getByText(/Max note size/i)).toBeTruthy()
  })

  // --- #472 settings deep-link CTAs ----------------------------------------
  it('renders an "Open AI settings" CTA in the unconfigured state', () => {
    mockAppSettings.config.ai.chat.model = ''
    const ctx = makeCtx()
    const { getByRole } = render(SummaryBanner, {
      props: { ctx, onDismiss: () => {} }
    })
    const cta = getByRole('button', { name: /Open AI settings/i })
    expect(cta).toBeTruthy()
    void fireEvent.click(cta)
    expect(ctx.openSettings).toHaveBeenCalledWith('ai')
  })

  it('renders an "Open AI settings" CTA for oversized errors', () => {
    // The oversized error can't be retried into success — point at the limit
    // setting via a deep-link instead of leaving the user with text alone.
    setPageState({
      status: 'error',
      result: {
        ok: false,
        error: { code: 'oversized', message: 'too long' }
      }
    })
    const ctx = makeCtx()
    const { getByRole } = render(SummaryBanner, {
      props: { ctx, onDismiss: () => {} }
    })
    const cta = getByRole('button', { name: /Open AI settings/i })
    expect(cta).toBeTruthy()
    void fireEvent.click(cta)
    expect(ctx.openSettings).toHaveBeenCalledWith('ai')
  })

  it('shows Retry for fetch-failed errors (transient/retryable)', () => {
    setPageState({
      status: 'error',
      result: {
        ok: false,
        error: { code: 'fetch-failed', message: 'vault busy' }
      }
    })
    const { getByRole, getByText } = render(SummaryBanner, {
      props: { ctx: makeCtx(), onDismiss: () => {} }
    })
    expect(getByRole('button', { name: 'Retry' })).toBeTruthy()
    expect(getByText(/read this note's content/i)).toBeTruthy()
  })

  // --- Show-more collapse (aria-expanded + count contract) -----------------
  it('expands a facet list past the preview limit on Show-more click', async () => {
    setPageState({
      status: 'ready',
      stale: false,
      result: {
        ok: true,
        result: {
          summary: 's',
          tasks: ['t1', 't2', 't3', 't4', 't5'],
          risks: [],
          decisions: [],
          newItems: { tasks: [], risks: [], decisions: [] },
          fromCache: false,
          model: 'm',
          generatedAt: '2026-07-06T10:00:00Z'
        }
      }
    })
    const { getByRole, container } = render(SummaryBanner, {
      props: { ctx: makeCtx(), onDismiss: () => {} }
    })
    // FACET_PREVIEW_LIMIT is 3 → 2 hidden, only 3 .facet-item rendered.
    expect(container.querySelectorAll('.facet-item')).toHaveLength(3)
    const moreBtn = getByRole('button', { name: /Show 2 more/i })
    expect(moreBtn.getAttribute('aria-expanded')).toBe('false')
    await fireEvent.click(moreBtn)
    expect(container.querySelectorAll('.facet-item')).toHaveLength(5)
    expect(moreBtn.getAttribute('aria-expanded')).toBe('true')
  })

  // --- Finding 2: unconfigured nudge is not dismissible per-note -----------
  it('hides the close button when unconfigured (nudge is global, not per-note)', () => {
    mockAppSettings.config.ai.chat.model = ''
    const { queryByRole } = render(SummaryBanner, {
      props: { ctx: makeCtx(), onDismiss: () => {} }
    })
    // No dismiss — the nudge must persist until a provider is configured so it
    // can't poison dismissed_notes with a nudge dismissal.
    expect(queryByRole('button', { name: /Dismiss AI summary/i })).toBeNull()
  })

  // --- Regression: $state(new Map()) was non-reactive (#silt-ai-summary) ---
  it('clears the stale spinner when the controller resolves stale=false', async () => {
    // Regression: $state(new Map()) was non-reactive, so the banner rendered
    // once at mount and never observed the controller's stale=false write.
    //
    // The hand-rolled mock above uses a plain object for `state`, which
    // $derived can't track — so for THIS test we swap in the real controller's
    // $state-wrapped Record. setPageState still writes through mockController,
    // so the spec's exact mutation path (mockController.state[PAGE_ID].stale)
    // exercises a genuinely reactive store.
    const reactiveController = createSummaryController(() => ({
      isConfigured: true,
      configuredModel: 'qwen3:30b'
    }))
    const originalState = mockController.state
    mockController.state = reactiveController.state

    try {
      setPageState({
        status: 'ready',
        stale: true,
        result: {
          ok: true,
          result: {
            summary: 'Prior summary still readable.',
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
      const { container } = render(SummaryBanner, {
        props: { ctx: makeCtx(), onDismiss: () => {} }
      })
      expect(container.querySelector('.updating-line')).toBeTruthy() // baseline: spinner shown

      // Controller's run() wins its generation guard and clears stale.
      const ps = mockController.state[PAGE_ID]
      ps.stale = false
      await tick()

      expect(container.querySelector('.updating-line')).toBeNull()
    } finally {
      mockController.state = originalState
    }
  })
})
