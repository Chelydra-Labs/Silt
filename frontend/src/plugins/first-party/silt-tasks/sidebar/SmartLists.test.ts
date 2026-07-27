import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

import SmartLists from './SmartLists.svelte'
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

function mockCounts(
  today: number,
  upcoming: number,
  overdue: number,
  completed: number,
  all: number
) {
  return {
    rows: [{ today, upcoming, overdue, completed, all }],
    truncated: false
  }
}

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

describe('sidebar/SmartLists (#763)', () => {
  beforeEach(() => {
    mocks.sqliteQuery.mockReset()
    resetTaskHubState()
    mocks.sqliteQuery.mockImplementation(async () => mockCounts(0, 0, 0, 0, 0))
  })
  afterEach(cleanup)

  it('renders the five smart-list rows with mocked counts', async () => {
    mocks.sqliteQuery.mockImplementation(async () =>
      mockCounts(3, 12, 1, 0, 49)
    )
    render(SmartLists, { ctx: makeCtx(), reloadSignal: 0 })
    await flush()
    expect(screen.getByTestId('today')).toBeInTheDocument()
    expect(screen.getByTestId('upcoming')).toBeInTheDocument()
    expect(screen.getByTestId('overdue')).toBeInTheDocument()
    expect(screen.getByTestId('completed')).toBeInTheDocument()
    expect(screen.getByTestId('all')).toBeInTheDocument()
    expect(screen.getByTestId('count-today').textContent?.trim()).toBe('3')
    expect(screen.getByTestId('count-all').textContent?.trim()).toBe('49')
  })

  it('clicking Today sets getTaskHubState().activeFilter === "today"', async () => {
    mocks.sqliteQuery.mockImplementation(async () =>
      mockCounts(3, 12, 1, 0, 49)
    )
    render(SmartLists, { ctx: makeCtx(), reloadSignal: 0 })
    await flush()
    await fireEvent.click(screen.getByTestId('today'))
    expect(getTaskHubState().activeFilter).toBe('today')
  })
})
