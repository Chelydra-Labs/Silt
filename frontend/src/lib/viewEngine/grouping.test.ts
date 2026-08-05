// Generic contracts for the shared view-engine binning primitive (#863).
// These mirror the algorithm extracted from silt-tasks/grouping.test.ts and
// dashboards, expressed against a plain test row type so the assertions
// verify the GENERIC behaviour (not the task-specific projection).
import { describe, it, expect } from 'vitest'
import {
  binByKey,
  singleSection,
  UNASSIGNED_VALUE,
  type GroupSection
} from './grouping'

interface Row {
  id: string
  tag?: string
  cats?: string[]
  n?: number
  owner?: string
}

describe('binByKey — single-membership (scalar)', () => {
  it('alphabetical buckets + trailing Unassigned for empty values', () => {
    const rows: Row[] = [
      { id: '1', owner: 'Zoe' },
      { id: '2', owner: '' },
      { id: '3', owner: 'Alice' },
      { id: '4', owner: 'Bob' }
    ]
    const sections = binByKey(rows, {
      keyOf: (r) => r.owner ?? '',
      sectionKey: (v) => `owner-${v}`
    })
    expect(sections.map((s) => s.label)).toEqual([
      'Alice',
      'Bob',
      'Zoe',
      'Unassigned'
    ])
    expect(sections[3].items.map((r) => r.id)).toEqual(['2'])
    // Namespaced key can't collide with a literal owner value.
    expect(sections[0].key).toBe('owner-Alice')
    expect(sections[3].key).toBe(`owner-${UNASSIGNED_VALUE}`)
  })

  it('omits the unassigned bucket when no row is empty', () => {
    const sections = binByKey([{ id: 'a', owner: 'X' }], {
      keyOf: (r) => r.owner ?? ''
    })
    expect(sections).toHaveLength(1)
    expect(sections[0].key).toBe('X')
  })

  it('honours a custom unassignedLabel', () => {
    const sections = binByKey([{ id: 'a', owner: '' }], {
      keyOf: (r) => r.owner ?? '',
      unassignedLabel: 'Nobody'
    })
    expect(sections[0].label).toBe('Nobody')
  })
})

describe('binByKey — numeric ordering (priority-style)', () => {
  it('numeric ascending with custom compareKeys', () => {
    const rows: Row[] = [
      { id: 'a', n: 3 },
      { id: 'b', n: 1 },
      { id: 'c', n: 2 }
    ]
    const sections = binByKey(rows, {
      keyOf: (r) => (r.n ? String(r.n) : ''),
      compareKeys: (a, b) => Number(a) - Number(b),
      sectionLabel: (v) => `P${v}`
    })
    expect(sections.map((s) => s.label)).toEqual(['P1', 'P2', 'P3'])
  })
})

describe('binByKey — multi-membership (tags/multiselect)', () => {
  it('a row with N values appears once per value', () => {
    const rows: Row[] = [
      { id: 'a', cats: ['work/backend', 'team-alpha'] },
      { id: 'b', cats: ['work/backend'] },
      { id: 'c', cats: [] }
    ]
    const sections = binByKey(rows, {
      keyOf: (r) => r.cats ?? [],
      sectionKey: (v) => `tag-${v}`
    })
    expect(sections.map((s) => s.label)).toEqual([
      'team-alpha',
      'work/backend',
      'Unassigned'
    ])
    const alpha = sections.find((s) => s.label === 'team-alpha')!
    expect(alpha.items.map((r) => r.id)).toEqual(['a'])
    const backend = sections.find((s) => s.label === 'work/backend')!
    expect(backend.items.map((r) => r.id)).toEqual(['a', 'b'])
    const none = sections.find((s) => s.label === 'Unassigned')!
    expect(none.items.map((r) => r.id)).toEqual(['c'])
  })

  it('drops empty strings inside an array (no phantom buckets)', () => {
    const sections = binByKey([{ id: 'a', cats: ['x', '', '  ', 'y'] }], {
      keyOf: (r) => r.cats ?? []
    })
    expect(sections.map((s) => s.label).sort()).toEqual(['x', 'y'])
  })

  it('routes a row whose values are all empty to unassigned', () => {
    const sections = binByKey(
      [
        { id: 'a', cats: ['', '  '] },
        { id: 'b', cats: ['real'] }
      ],
      { keyOf: (r) => r.cats ?? [] }
    )
    expect(sections).toHaveLength(2)
    expect(sections[1].items.map((r) => r.id)).toEqual(['a'])
  })
})

describe('binByKey — ordering modes', () => {
  it("order 'desc' reverses the comparator", () => {
    const sections = binByKey(
      [
        { id: 'a', owner: 'A' },
        { id: 'b', owner: 'C' },
        { id: 'c', owner: 'B' }
      ],
      { keyOf: (r) => r.owner ?? '', order: 'desc' }
    )
    expect(sections.map((s) => s.label)).toEqual(['C', 'B', 'A'])
  })

  it("order 'none' keeps first-seen insertion order", () => {
    const sections = binByKey(
      [
        { id: 'a', owner: 'Z' },
        { id: 'b', owner: 'A' },
        { id: 'c', owner: 'M' }
      ],
      { keyOf: (r) => r.owner ?? '', order: 'none' }
    )
    expect(sections.map((s) => s.label)).toEqual(['Z', 'A', 'M'])
  })
})

describe('binByKey — within-section sort', () => {
  it('applies sortBy inside each bucket without reordering buckets', () => {
    const sections = binByKey(
      [
        { id: 'a', owner: 'A', n: 3 },
        { id: 'b', owner: 'A', n: 1 },
        { id: 'c', owner: 'B', n: 9 }
      ],
      {
        keyOf: (r) => r.owner ?? '',
        sortBy: (x, y) => (x.n ?? 0) - (y.n ?? 0)
      }
    )
    expect(sections[0].items.map((r) => r.id)).toEqual(['b', 'a'])
    expect(sections[1].items.map((r) => r.id)).toEqual(['c'])
  })
})

describe('binByKey — empty input', () => {
  it('returns no sections (and no unassigned) for an empty row list', () => {
    const sections = binByKey([], { keyOf: () => '' })
    expect(sections).toEqual([])
  })
})

describe('singleSection', () => {
  it('wraps every row in one bucket with the given key/label', () => {
    const rows: Row[] = [{ id: 'a' }, { id: 'b' }]
    const sections: GroupSection<Row>[] = singleSection(rows, 'all', 'All')
    expect(sections).toEqual([
      { key: 'all', label: 'All', items: [{ id: 'a' }, { id: 'b' }] }
    ])
    // Returns a copy so the caller can't mutate the input array via the section.
    sections[0].items.push({ id: 'c' })
    expect(rows).toHaveLength(2)
  })
})
