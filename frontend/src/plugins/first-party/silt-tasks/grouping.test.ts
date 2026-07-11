// Unit tests for the pure grouping helpers (#423). Each GroupBy dimension
// has a binning contract that ListView relies on, so these are the
// load-bearing assertions for the generalized list rendering.
import { describe, it, expect } from 'vitest'
import { binByDimension, type GroupSection } from './grouping'
import type { TaskDetail } from './types'

function row(overrides: Partial<TaskDetail> & { id: string }): TaskDetail {
  return {
    id: overrides.id,
    notebook: overrides.notebook ?? 'Work',
    section: overrides.section ?? 'Journal',
    page: overrides.page ?? 'Daily',
    file_date: overrides.file_date ?? '2026-07-06',
    clean_content: overrides.clean_content ?? 'task',
    status: overrides.status ?? 'TODO',
    owner: overrides.owner ?? '',
    start_date: overrides.start_date ?? '',
    due_date: overrides.due_date ?? '',
    priority: overrides.priority ?? 0,
    pinned: overrides.pinned ?? false,
    progress: overrides.progress ?? 0,
    recurrence: overrides.recurrence ?? '',
    comments_count: overrides.comments_count ?? 0,
    links_count: overrides.links_count ?? 0,
    created_at: overrides.created_at ?? '',
    completed_at: overrides.completed_at ?? '',
    manual_order: overrides.manual_order ?? 0,
    modified_at: overrides.modified_at ?? '',
    estimate_minutes: overrides.estimate_minutes ?? null,
    subtask_total: overrides.subtask_total ?? 0,
    subtask_done: overrides.subtask_done ?? 0,
    tags: overrides.tags,
    blocked_by: overrides.blocked_by,
    is_blocked: overrides.is_blocked
  }
}

const TODAY = '2026-07-06'

describe('binByDimension — none', () => {
  it('returns a single section containing every row', () => {
    const rows = [
      row({ id: 'a', due_date: '2026-07-04' }),
      row({ id: 'b', due_date: '' })
    ]
    const sections = binByDimension(rows, 'none', { today: TODAY })
    expect(sections).toHaveLength(1)
    expect(sections[0].key).toBe('all')
    expect(sections[0].label).toBe('All Tasks')
    expect(sections[0].items.map((i) => i.id)).toEqual(['a', 'b'])
  })
})

describe('binByDimension — dueDate', () => {
  it('produces the canonical 5 buckets in order with the legacy keys', () => {
    const sections = binByDimension([], 'dueDate', { today: TODAY })
    expect(sections.map((s) => s.key)).toEqual([
      'overdue',
      'today',
      'upcoming',
      'later',
      'undated'
    ])
    expect(sections.map((s) => s.label)).toEqual([
      'Overdue',
      'Today',
      'Upcoming',
      'Later',
      'No Date'
    ])
  })

  it('routes rows to the correct bucket (overdue/today/upcoming/later/undated)', () => {
    const rows = [
      row({ id: 'overdue', due_date: '2026-07-05' }), // < today
      row({ id: 'today', due_date: '2026-07-06' }), // == today
      row({ id: 'tomorrow', due_date: '2026-07-07' }), // today+1 → upcoming
      row({ id: 'inweek', due_date: '2026-07-13' }), // today+7 → upcoming
      row({ id: 'later', due_date: '2026-07-14' }), // today+8 → later
      row({ id: 'undated', due_date: '' })
    ]
    const sections = binByDimension(rows, 'dueDate', { today: TODAY })
    const byKey = Object.fromEntries(sections.map((s) => [s.key, s.items]))
    expect(byKey.overdue.map((i) => i.id)).toEqual(['overdue'])
    expect(byKey.today.map((i) => i.id)).toEqual(['today'])
    // Tomorrow AND day-7 both fall in upcoming (boundary inclusive).
    expect(byKey.upcoming.map((i) => i.id)).toEqual(['tomorrow', 'inweek'])
    expect(byKey.later.map((i) => i.id)).toEqual(['later'])
    expect(byKey.undated.map((i) => i.id)).toEqual(['undated'])
  })

  it('places each row in exactly one bucket (no double-counting)', () => {
    const rows = [
      row({ id: 'a', due_date: '2026-07-06' }),
      row({ id: 'b', due_date: '2030-01-01' })
    ]
    const sections = binByDimension(rows, 'dueDate', { today: TODAY })
    const totalAssigned = sections.reduce((n, s) => n + s.items.length, 0)
    expect(totalAssigned).toBe(rows.length)
  })
})

describe('binByDimension — status', () => {
  it('emits TODO/DOING/DONE in that canonical order, then custom lanes alphabetical', () => {
    const rows = [
      row({ id: 'd', status: 'DONE' }),
      row({ id: 't', status: 'TODO' }),
      row({ id: 'g', status: 'DOING' }),
      row({ id: 'z', status: 'WAITING' as TaskDetail['status'] }),
      row({ id: 'a', status: 'BLOCKED' as TaskDetail['status'] })
    ]
    const sections = binByDimension(rows, 'status', { today: TODAY })
    expect(sections.map((s) => s.key)).toEqual([
      'TODO',
      'DOING',
      'DONE',
      'BLOCKED',
      'WAITING'
    ])
    expect(sections[0].label).toBe('To Do')
    expect(sections[1].label).toBe('In Progress')
    expect(sections[2].label).toBe('Done')
  })

  it('omits empty standard lanes', () => {
    const sections = binByDimension(
      [row({ id: 'x', status: 'TODO' })],
      'status',
      { today: TODAY }
    )
    expect(sections.map((s) => s.key)).toEqual(['TODO'])
  })
})

describe('binByDimension — owner', () => {
  it('alphabetical, with a trailing Unassigned bucket for empty owners', () => {
    const rows = [
      row({ id: '1', owner: 'Zoe' }),
      row({ id: '2', owner: '' }),
      row({ id: '3', owner: 'Alice' }),
      row({ id: '4', owner: 'Bob' })
    ]
    const sections = binByDimension(rows, 'owner', { today: TODAY })
    expect(sections.map((s) => s.label)).toEqual([
      'Alice',
      'Bob',
      'Zoe',
      'Unassigned'
    ])
    expect(sections[3].items.map((i) => i.id)).toEqual(['2'])
    // data-group key is namespaced so it can't collide with a literal owner.
    expect(sections[0].key).toBe('owner-Alice')
    expect(sections[3].key).toBe('owner-__unassigned__')
  })
})

describe('binByDimension — priority', () => {
  it('numeric ascending; priority 0 (unset) routes to No Priority', () => {
    const rows = [
      row({ id: 'a', priority: 3 }),
      row({ id: 'b', priority: 1 }),
      row({ id: 'c', priority: 0 }),
      row({ id: 'd', priority: 2 })
    ]
    const sections = binByDimension(rows, 'priority', { today: TODAY })
    expect(sections.map((s) => s.label)).toEqual([
      'Critical',
      'Normal',
      'Low',
      'No Priority'
    ])
    expect(sections[3].items.map((i) => i.id)).toEqual(['c'])
  })
})

describe('binByDimension — tag', () => {
  it('multi-membership: a row with N tags appears once per tag', () => {
    const rows = [
      row({ id: 'a', tags: 'work/backend|team-alpha' }),
      row({ id: 'b', tags: 'work/backend' }),
      row({ id: 'c', tags: '' })
    ]
    const sections = binByDimension(rows, 'tag', { today: TODAY })
    // Alphabetical tag sections first, then trailing "No Tag".
    expect(sections.map((s) => s.label)).toEqual([
      'team-alpha',
      'work/backend',
      'No Tag'
    ])
    // Row 'a' appears under both tag sections.
    const alpha = sections.find((s) => s.label === 'team-alpha')!
    expect(alpha.items.map((i) => i.id)).toEqual(['a'])
    const backend = sections.find((s) => s.label === 'work/backend')!
    expect(backend.items.map((i) => i.id)).toEqual(['a', 'b'])
    const none = sections.find((s) => s.label === 'No Tag')!
    expect(none.items.map((i) => i.id)).toEqual(['c'])
  })
})

describe('binByDimension — notebook/section/page', () => {
  it('notebook bins by the notebook value, alphabetical + No Notebook', () => {
    const rows = [
      row({ id: 'a', notebook: 'Work', section: 'S1', page: 'P1' }),
      row({ id: 'b', notebook: 'Personal', section: 'S2', page: 'P2' }),
      row({ id: 'c', notebook: '', section: '', page: '' })
    ]
    const sections = binByDimension(rows, 'notebook', { today: TODAY })
    expect(sections.map((s) => s.label)).toEqual([
      'Personal',
      'Work',
      'No Notebook'
    ])
  })

  it('section bins by section value', () => {
    const sections = binByDimension(
      [
        row({ id: 'a', section: 'Daily' }),
        row({ id: 'b', section: 'Projects' }),
        row({ id: 'c', section: '' })
      ],
      'section',
      { today: TODAY }
    )
    expect(sections.map((s) => s.label)).toEqual([
      'Daily',
      'Projects',
      'No Section'
    ])
  })

  it('page bins by page value', () => {
    const sections = binByDimension(
      [
        row({ id: 'a', page: '2026-07-06' }),
        row({ id: 'b', page: 'Inbox' }),
        row({ id: 'c', page: '' })
      ],
      'page',
      { today: TODAY }
    )
    expect(sections.map((s) => s.label)).toEqual([
      '2026-07-06',
      'Inbox',
      'No Page'
    ])
  })
})
