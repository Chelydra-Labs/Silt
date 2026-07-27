import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, cleanup, fireEvent } from '@testing-library/svelte'

import MiniCalendar from './MiniCalendar.svelte'
import type { PluginContext } from '../../../sdk'
import { v2CtxStubs } from '../../../test-helpers'
import { getTaskHubState, resetTaskHubState } from '../state.svelte'

const mocks = vi.hoisted(() => ({
  sqliteQuery: vi.fn()
}))

function makeCtx(overrides: Partial<PluginContext> = {}): PluginContext {
  return {
    ...v2CtxStubs,
    today: '2026-07-06',
    sqliteQuery: mocks.sqliteQuery,
    ...overrides
  } as PluginContext
}

function mockDayCounts(entries: Array<{ d: string; c: number }>) {
  return {
    rows: entries.map((e) => ({ d: e.d, c: e.c })),
    truncated: false
  }
}

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

describe('sidebar/MiniCalendar (#763)', () => {
  beforeEach(() => {
    mocks.sqliteQuery.mockReset()
    resetTaskHubState()
    mocks.sqliteQuery.mockImplementation(async () => mockDayCounts([]))
  })
  afterEach(cleanup)

  it('renders the grid with mocked day-dots', async () => {
    mocks.sqliteQuery.mockImplementation(async () =>
      mockDayCounts([
        { d: '2026-07-06', c: 2 },
        { d: '2026-07-10', c: 1 }
      ])
    )
    render(MiniCalendar, { ctx: makeCtx(), reloadSignal: 0 })
    await flush()
    const dayWithDots = document.querySelector(
      '[data-testid="mini-day-2026-07-06"] [aria-hidden="true"]'
    )
    expect(dayWithDots).toBeTruthy()
    const dayWithoutDots = document.querySelector(
      '[data-testid="mini-day-2026-07-07"] [aria-hidden="true"]'
    )
    expect(dayWithoutDots).toBeNull()
  })

  it('clicking a day sets getTaskHubState().focusDate', async () => {
    render(MiniCalendar, { ctx: makeCtx(), reloadSignal: 0 })
    await flush()
    const cell = document.querySelector<HTMLElement>(
      '[data-testid="mini-day-2026-07-06"]'
    )
    expect(cell).toBeTruthy()
    await fireEvent.click(cell!)
    expect(getTaskHubState().focusDate).toBe('2026-07-06')
  })

  it('today marker is present on the ctx.today cell', async () => {
    render(MiniCalendar, { ctx: makeCtx(), reloadSignal: 0 })
    await flush()
    const todayCell = document.querySelector<HTMLElement>(
      '[data-testid="mini-day-2026-07-06"]'
    )
    expect(todayCell).toBeTruthy()
    expect(todayCell!.getAttribute('aria-current')).toBe('date')
  })

  it('reloadSignal prop change triggers a re-query', async () => {
    const ctx = makeCtx()
    mocks.sqliteQuery.mockImplementation(async () => mockDayCounts([]))
    const { rerender } = render(MiniCalendar, { ctx, reloadSignal: 0 })
    await flush()
    const afterMount = mocks.sqliteQuery.mock.calls.length
    // Initial $effect fired exactly one query on mount.
    expect(afterMount).toBeGreaterThanOrEqual(1)
    // Bumping reloadSignal must drive a fresh query.
    await rerender({ ctx, reloadSignal: 1 })
    await flush()
    expect(mocks.sqliteQuery.mock.calls.length).toBeGreaterThan(afterMount)
  })
})
