// Direct coverage for the shared comparator's pin primary key. List partitions
// its rows before sorting, so only the Board (PR-2) will feed mixed sets; pin
// the contract before two views depend on it.
import { describe, it, expect } from 'vitest'
import { compareTasks, sortTasks } from './taskOrder'
import type { SortMode } from './state.svelte'
import type { TaskDetail } from './types'

function makeTask(overrides: Partial<TaskDetail> = {}): TaskDetail {
  return {
    id: 'task-1',
    source: 'vault',
    notebook: 'Work',
    section: 'Journal',
    page: 'Daily',
    file_date: '2026-07-01',
    clean_content: 'Water plants',
    status: 'TODO',
    owner: '',
    start_date: '',
    due_date: '2026-07-15',
    priority: 3,
    pinned: false,
    progress: 0,
    recurrence: '',
    comments_count: 0,
    links_count: 0,
    created_at: '',
    completed_at: '',
    manual_order: 0,
    modified_at: '',
    estimate_minutes: null,
    shortlist_rank: null,
    subtask_total: 0,
    subtask_done: 0,
    ...overrides
  }
}

function ids(sort: SortMode, rows: TaskDetail[]): string[] {
  return sortTasks(sort, rows).map((t) => t.id)
}

describe('compareTasks pin partition', () => {
  it('sorts every pinned row ahead of every unpinned row under manual', () => {
    const rows = [
      makeTask({ id: 'u1', manual_order: 2 }),
      makeTask({ id: 'p1', pinned: true, manual_order: 5 }),
      makeTask({ id: 'u2', manual_order: 0 }),
      makeTask({ id: 'p2', pinned: true, manual_order: 1 })
    ]

    expect(ids('manual', rows)).toEqual(['p2', 'p1', 'u1', 'u2'])
  })

  it('keeps manual order and the due-date tiebreaker within each subset', () => {
    const rows = [
      // Pinned: explicit orders first, then unknown (0) rows by due date.
      makeTask({ id: 'p-late', pinned: true, due_date: '2026-02-01' }),
      makeTask({ id: 'p-order', pinned: true, manual_order: 1 }),
      makeTask({ id: 'p-early', pinned: true, due_date: '2026-01-15' }),
      // Unpinned: same rules, still behind the pinned block.
      makeTask({ id: 'u-late', due_date: '2026-02-01' }),
      makeTask({ id: 'u-order', manual_order: 1 })
    ]

    expect(ids('manual', rows)).toEqual([
      'p-order',
      'p-early',
      'p-late',
      'u-order',
      'u-late'
    ])
  })

  it('retains the non-manual sort inside both subsets', () => {
    const rows = [
      makeTask({ id: 'u-b', clean_content: 'Beta' }),
      makeTask({ id: 'p-b', pinned: true, clean_content: 'Beta' }),
      makeTask({ id: 'u-a', clean_content: 'Alpha' }),
      makeTask({ id: 'p-a', pinned: true, clean_content: 'Alpha' })
    ]

    expect(ids('title', rows)).toEqual(['p-a', 'p-b', 'u-a', 'u-b'])
  })

  it('is antisymmetric across the pin boundary', () => {
    const pinned = makeTask({ id: 'p', pinned: true })
    const unpinned = makeTask({ id: 'u' })

    expect(compareTasks('manual', pinned, unpinned)).toBeLessThan(0)
    expect(compareTasks('manual', unpinned, pinned)).toBeGreaterThan(0)
  })
})
