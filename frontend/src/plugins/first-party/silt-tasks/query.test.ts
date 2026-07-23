// Unit tests for the unified Tasks hub query builder (#419 phase 4).
// Ports every assertion from silt-kanban/query.test.ts against the new
// lifted builder (the base SQL must be byte-for-byte the Kanban output),
// then adds coverage for the two new levers: `groupBy` (ORDER BY changes)
// and `window` (due-date WHERE window).
import { describe, it, expect } from 'vitest'
import { buildQuery, type QueryCtxLike } from './query'
import {
  clearTaskPageRoute,
  getTaskHubQueryContext,
  type TaskFilters
} from './state.svelte'

const ctx: QueryCtxLike = {
  activeNotebook: 'Work',
  activeSection: 'Journal',
  activePage: 'Today',
  today: '2026-06-22'
}

const emptyFilters: TaskFilters = {
  owners: [],
  priorities: [],
  dueDate: '',
  tags: []
}

describe('buildQuery — scope branches (ported from silt-kanban)', () => {
  it('vault scope adds no WHERE for scope', () => {
    const { sql, params } = buildQuery('vault', emptyFilters, ctx)
    expect(sql).toContain('WHERE 1=1')
    expect(params).toEqual([])
  })

  it('notebook scope filters by activeNotebook only', () => {
    const { sql, params } = buildQuery('notebook', emptyFilters, ctx)
    expect(sql).toContain('b.notebook = ?')
    expect(sql).not.toContain('b.section = ?')
    expect(sql).not.toContain('b.page = ?')
    expect(params).toEqual(['Work'])
  })

  it('section scope filters by notebook + section', () => {
    const { sql, params } = buildQuery('section', emptyFilters, ctx)
    expect(sql).toContain('b.notebook = ?')
    expect(sql).toContain('b.section = ?')
    expect(sql).not.toContain('b.page = ?')
    expect(params).toEqual(['Work', 'Journal'])
  })

  it('page scope filters by notebook + section + page', () => {
    const { sql, params } = buildQuery('page', emptyFilters, ctx)
    expect(sql).toContain('b.notebook = ?')
    expect(sql).toContain('b.section = ?')
    expect(sql).toContain('b.page = ?')
    expect(params).toEqual(['Work', 'Journal', 'Today'])
  })
})

describe('buildQuery — source qualification', () => {
  it('adds a parameterised source predicate when a source is supplied', () => {
    const { sql, params } = buildQuery('page', emptyFilters, {
      ...ctx,
      source: 'linked:team-notes'
    })
    expect(sql).toContain('b.source = ?')
    expect(params).toEqual(['linked:team-notes', 'Work', 'Journal', 'Today'])
  })

  it('keeps the legacy pure-builder context source-free when omitted', () => {
    const { sql, params } = buildQuery('vault', emptyFilters, ctx)
    expect(sql).not.toContain('b.source = ?')
    expect(params).toEqual([])
  })

  it('preserves ambient linked scope without inventing a vault source', () => {
    clearTaskPageRoute()
    const ambient = getTaskHubQueryContext({
      activeNotebook: 'Shared Notes',
      activeSection: 'Meetings',
      activePage: 'Review',
      today: '2026-06-22'
    })
    const { sql, params } = buildQuery('page', emptyFilters, ambient)
    expect(ambient.source).toBeUndefined()
    expect(sql).not.toContain('b.source = ?')
    expect(params).toEqual(['Shared Notes', 'Meetings', 'Review'])
  })
})

describe('buildQuery — filter branches (ported from silt-kanban)', () => {
  it('owners filter adds parameterised IN clause', () => {
    const filters: TaskFilters = { ...emptyFilters, owners: ['Alice', 'Bob'] }
    const { sql, params } = buildQuery('vault', filters, ctx)
    expect(sql).toContain('t.owner IN (?, ?)')
    expect(params).toEqual(['Alice', 'Bob'])
  })

  it('priorities filter adds parameterised IN clause', () => {
    const filters: TaskFilters = { ...emptyFilters, priorities: [1, 3] }
    const { sql, params } = buildQuery('vault', filters, ctx)
    expect(sql).toContain('t.priority IN (?, ?)')
    expect(params).toEqual([1, 3])
  })

  it('empty owners filter is a no-op (no IN clause)', () => {
    const { sql, params } = buildQuery('vault', emptyFilters, ctx)
    expect(sql).not.toContain('t.owner IN')
    expect(params).toEqual([])
  })

  it('dueDate=overdue uses lexicographic less-than today', () => {
    const filters: TaskFilters = { ...emptyFilters, dueDate: 'overdue' }
    const { sql, params } = buildQuery('vault', filters, ctx)
    expect(sql).toContain('t.due_date < ?')
    expect(params).toEqual(['2026-06-22'])
  })

  it('dueDate=today uses equality against today', () => {
    const filters: TaskFilters = { ...emptyFilters, dueDate: 'today' }
    const { sql, params } = buildQuery('vault', filters, ctx)
    expect(sql).toContain('t.due_date = ?')
    expect(params).toEqual(['2026-06-22'])
  })

  it('dueDate=week uses BETWEEN today and today+7', () => {
    const filters: TaskFilters = { ...emptyFilters, dueDate: 'week' }
    const { sql, params } = buildQuery('vault', filters, ctx)
    expect(sql).toContain('t.due_date BETWEEN ? AND ?')
    expect(params).toEqual(['2026-06-22', '2026-06-29'])
  })

  it('dueDate=none matches NULL or empty string', () => {
    const filters: TaskFilters = { ...emptyFilters, dueDate: 'none' }
    const { sql, params } = buildQuery('vault', filters, ctx)
    expect(sql).toContain("(t.due_date IS NULL OR t.due_date = '')")
    expect(params).toEqual([])
  })

  it('tags filter adds subquery on tags table', () => {
    const filters: TaskFilters = {
      ...emptyFilters,
      tags: ['work/project', 'personal']
    }
    const { sql, params } = buildQuery('vault', filters, ctx)
    expect(sql).toContain(
      'b.id IN (SELECT block_id FROM tags WHERE raw_path IN (?, ?))'
    )
    expect(params).toEqual(['work/project', 'personal'])
  })
})

describe('buildQuery — combined scope + filters (ported from silt-kanban)', () => {
  it('vault-scope + no filters produces WHERE 1=1 with no params', () => {
    const { sql, params } = buildQuery('vault', emptyFilters, ctx)
    expect(sql).toContain('WHERE 1=1')
    expect(params).toEqual([])
  })

  it('notebook + owners + priorities + dueToday combine via AND', () => {
    const filters: TaskFilters = {
      owners: ['Alice'],
      priorities: [2],
      dueDate: 'today',
      tags: []
    }
    const { sql, params } = buildQuery('notebook', filters, ctx)
    expect(sql).toContain('b.notebook = ?')
    expect(sql).toContain('t.owner IN (?)')
    expect(sql).toContain('t.priority IN (?)')
    expect(sql).toContain('t.due_date = ?')
    expect(params).toEqual(['Work', 'Alice', 2, '2026-06-22'])
  })

  it('always includes the priority + due_date ORDER BY by default', () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx)
    expect(sql).toContain(
      "ORDER BY t.priority ASC, COALESCE(t.due_date, '9999-12-31') ASC"
    )
  })

  it('includes the Phase 2 columns in the SELECT', () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx)
    expect(sql).toContain('t.created_at')
    expect(sql).toContain('t.completed_at')
    expect(sql).toContain('t.manual_order')
  })

  it('includes modified/estimate/subtask columns in the SELECT (#439/#440/#434)', () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx)
    expect(sql).toContain('t.modified_at')
    expect(sql).toContain('t.estimate_minutes')
    expect(sql).toContain('t.subtask_total')
    expect(sql).toContain('t.subtask_done')
  })
})

// ── New: groupBy lever (ORDER BY changes) ─────────────────────────────

describe('buildQuery — groupBy lever (new in #419)', () => {
  it("groupBy='none' keeps the legacy priority-first ORDER BY", () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx, {
      groupBy: 'none'
    })
    expect(sql).toContain(
      "ORDER BY t.priority ASC, COALESCE(t.due_date, '9999-12-31') ASC"
    )
  })

  it("groupBy omitted behaves identically to 'none'", () => {
    const withExplicit = buildQuery('vault', emptyFilters, ctx, {
      groupBy: 'none'
    }).sql
    const without = buildQuery('vault', emptyFilters, ctx).sql
    expect(without).toBe(withExplicit)
  })

  it("groupBy='priority' keeps the legacy priority-first ORDER BY", () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx, {
      groupBy: 'priority'
    })
    expect(sql).toContain(
      "ORDER BY t.priority ASC, COALESCE(t.due_date, '9999-12-31') ASC"
    )
  })

  it("groupBy='status' sorts by status first", () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx, {
      groupBy: 'status'
    })
    expect(sql).toContain('ORDER BY t.status ASC,')
    // Tiebreaker still present.
    expect(sql).toContain('COALESCE(t.due_date')
  })

  it("groupBy='owner' sorts by owner first", () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx, {
      groupBy: 'owner'
    })
    expect(sql).toContain('ORDER BY t.owner ASC,')
  })

  it("groupBy='dueDate' promotes due date to the leading sort key", () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx, {
      groupBy: 'dueDate'
    })
    expect(sql).toContain('ORDER BY COALESCE(t.due_date')
    // The legacy priority-first order is NOT used.
    expect(sql).not.toContain('ORDER BY t.priority ASC,')
  })

  it('groupBy does not add a WHERE clause (sort-only concern)', () => {
    const { sql, params } = buildQuery('vault', emptyFilters, ctx, {
      groupBy: 'status'
    })
    expect(sql).toContain('WHERE 1=1')
    expect(params).toEqual([])
  })
})

// ── New: window lever (due-date WHERE window) ─────────────────────────

describe('buildQuery — window lever (new in #419)', () => {
  it('adds a parameterised due_date >= ? AND due_date <= ? clause', () => {
    const { sql, params } = buildQuery('vault', emptyFilters, ctx, {
      window: { start: '2026-06-01', end: '2026-06-30' }
    })
    expect(sql).toContain('t.due_date >= ?')
    expect(sql).toContain('t.due_date <= ?')
    expect(params).toEqual(['2026-06-01', '2026-06-30'])
  })

  it('window combines with scope + filters via AND', () => {
    const filters: TaskFilters = {
      owners: ['Alice'],
      priorities: [],
      dueDate: '',
      tags: []
    }
    const { sql, params } = buildQuery('notebook', filters, ctx, {
      window: { start: '2026-06-01', end: '2026-06-30' }
    })
    expect(sql).toContain('b.notebook = ?')
    expect(sql).toContain('t.owner IN (?)')
    expect(sql).toContain('t.due_date >= ?')
    expect(sql).toContain('t.due_date <= ?')
    expect(params).toEqual(['Work', 'Alice', '2026-06-01', '2026-06-30'])
  })

  it('window AND groupBy compose on the same query', () => {
    const { sql, params } = buildQuery('vault', emptyFilters, ctx, {
      groupBy: 'status',
      window: { start: '2026-06-01', end: '2026-06-30' }
    })
    expect(sql).toContain('t.due_date >= ?')
    expect(sql).toContain('t.due_date <= ?')
    expect(sql).toContain('ORDER BY t.status ASC,')
    expect(params).toEqual(['2026-06-01', '2026-06-30'])
  })

  it('no window leaves the WHERE clause untouched', () => {
    const { sql, params } = buildQuery('vault', emptyFilters, ctx)
    expect(sql).not.toContain('t.due_date >= ?')
    expect(params).toEqual([])
  })
})

// ── New: high-cardinality groupBy dimensions (#423) ──────────────────

describe('buildQuery — tag/notebook/section/page groupBy shares the legacy ORDER BY', () => {
  it.each(['tag', 'notebook', 'section', 'page'] as const)(
    "groupBy='%s' falls through to the priority-first ORDER BY (client-side binning)",
    (g) => {
      const { sql } = buildQuery('vault', emptyFilters, ctx, { groupBy: g })
      expect(sql).toContain(
        "ORDER BY t.priority ASC, COALESCE(t.due_date, '9999-12-31') ASC"
      )
    }
  )
})

// ── New: sort lever (#423) ────────────────────────────────────────────

describe('buildQuery — sort lever (new in #423)', () => {
  it("sort='manual' emits a NULLS-LAST manual_order ORDER BY", () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx, {
      sort: 'manual'
    })
    expect(sql).toContain('CASE WHEN t.manual_order IS NULL THEN 1 ELSE 0 END')
    expect(sql).toContain('t.manual_order ASC')
  })

  it("sort='priority' promotes priority to the leading key", () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx, {
      sort: 'priority'
    })
    expect(sql).toContain('ORDER BY t.priority ASC,')
  })

  it("sort='title' orders by clean_content", () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx, {
      sort: 'title'
    })
    expect(sql).toContain('ORDER BY b.clean_content ASC')
  })

  it("sort='created' pushes empty created_at to the bottom via CASE WHEN", () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx, {
      sort: 'created'
    })
    expect(sql).toContain(
      "CASE WHEN t.created_at IS NULL OR t.created_at = '' THEN '9999' ELSE t.created_at END"
    )
  })

  it("sort='owner' uses COALESCE(NULLIF(...)) so empty owners sort last", () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx, {
      sort: 'owner'
    })
    expect(sql).toContain("COALESCE(NULLIF(t.owner, ''), '~')")
  })

  it("sort='dueDate' is the canonical tiebreaker-only ORDER BY", () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx, {
      sort: 'dueDate'
    })
    expect(sql).toContain('ORDER BY COALESCE(t.due_date')
    expect(sql).not.toContain('ORDER BY t.priority ASC,')
  })

  it('sort+groupBy compose: status leads so groups stay contiguous', () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx, {
      groupBy: 'status',
      sort: 'title'
    })
    expect(sql).toContain('ORDER BY t.status ASC,')
    expect(sql).toContain('b.clean_content ASC')
  })

  it('sort+groupBy=owner leads with owner grouping, then the sort', () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx, {
      groupBy: 'owner',
      sort: 'priority'
    })
    expect(sql).toContain("ORDER BY COALESCE(NULLIF(t.owner, ''), '~') ASC,")
    expect(sql).toContain('t.priority ASC')
  })

  it('sort absent falls back to the groupBy-driven ORDER BY (backward compatible)', () => {
    const withSort = buildQuery('vault', emptyFilters, ctx, {
      groupBy: 'status'
    }).sql
    expect(withSort).toContain('ORDER BY t.status ASC,')
    // The ORDER BY is just the status grouping + due-date tiebreaker;
    // no manual_order CASE WHEN or clean_content sort leaked in.
    const orderBy = withSort.slice(withSort.indexOf('ORDER BY'))
    expect(orderBy).not.toContain('b.clean_content ASC')
    expect(orderBy).not.toContain('CASE WHEN t.manual_order')
  })

  it("sort='modified' puts recent first and null/empty as oldest", () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx, {
      sort: 'modified'
    })
    expect(sql).toContain(
      "CASE WHEN t.modified_at IS NULL OR t.modified_at = '' THEN '0000' ELSE t.modified_at END DESC"
    )
  })

  it("sort='estimate' nulls last then ascending minutes", () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx, {
      sort: 'estimate'
    })
    expect(sql).toContain(
      'CASE WHEN t.estimate_minutes IS NULL THEN 1 ELSE 0 END'
    )
    expect(sql).toContain('t.estimate_minutes ASC')
  })
})

// ── New: stale filter (#440) ──────────────────────────────────────────

describe('buildQuery — stale filter (new in #440)', () => {
  it('adds open + modified_at older-than-30d clause with cutoff param', () => {
    const filters: TaskFilters = { ...emptyFilters, stale: true }
    const { sql, params } = buildQuery('vault', filters, ctx)
    expect(sql).toContain("t.status != 'DONE'")
    expect(sql).toContain('substr(t.modified_at, 1, 10) < ?')
    // today is 2026-06-22 → cutoff is 2026-05-23
    expect(params).toEqual(['2026-05-23'])
  })

  it('stale=false/undefined is a no-op', () => {
    const { sql, params } = buildQuery('vault', emptyFilters, ctx)
    expect(sql).not.toContain('substr(t.modified_at')
    expect(params).toEqual([])
  })
})

// ── New: activeFilter lever (sidebar smart-list, #432) ───────────────

describe('buildQuery — activeFilter lever (new in #432)', () => {
  it("activeFilter='today' adds status!=DONE AND due_date=today", () => {
    const { sql, params } = buildQuery('vault', emptyFilters, ctx, {
      activeFilter: 'today'
    })
    expect(sql).toContain("t.status != 'DONE'")
    expect(sql).toContain('t.due_date = ?')
    expect(params).toEqual(['2026-06-22'])
  })

  it("activeFilter='overdue' adds status!=DONE AND due_date<today", () => {
    const { sql, params } = buildQuery('vault', emptyFilters, ctx, {
      activeFilter: 'overdue'
    })
    expect(sql).toContain("t.status != 'DONE'")
    expect(sql).toContain('t.due_date < ?')
    expect(params).toEqual(['2026-06-22'])
  })

  it("activeFilter='upcoming' adds status!=DONE AND due_date>today AND due_date<=today+7", () => {
    const { sql, params } = buildQuery('vault', emptyFilters, ctx, {
      activeFilter: 'upcoming'
    })
    expect(sql).toContain("t.status != 'DONE'")
    expect(sql).toContain('t.due_date > ?')
    expect(sql).toContain('t.due_date <= ?')
    expect(params).toEqual(['2026-06-22', '2026-06-29'])
  })

  it("activeFilter='completed' adds status=DONE", () => {
    const { sql, params } = buildQuery('vault', emptyFilters, ctx, {
      activeFilter: 'completed'
    })
    expect(sql).toContain("t.status = 'DONE'")
    expect(params).toEqual([])
  })

  it("activeFilter='today' overrides filters.dueDate='overdue' (no conflicting clause)", () => {
    const filters: TaskFilters = { ...emptyFilters, dueDate: 'overdue' }
    const { sql, params } = buildQuery('vault', filters, ctx, {
      activeFilter: 'today'
    })
    // Smart-list wins: only the today equality, no overdue less-than.
    expect(sql).toContain('t.due_date = ?')
    expect(sql).not.toContain('t.due_date < ?')
    expect(params).toEqual(['2026-06-22'])
  })

  it("activeFilter='all' is a no-op (no smart-list WHERE added)", () => {
    const { sql, params } = buildQuery('vault', emptyFilters, ctx, {
      activeFilter: 'all'
    })
    // No smart-list clause is appended to the WHERE; the body ends with
    // 'WHERE 1=1' followed directly by the ORDER BY (the EXISTS subquery
    // for is_blocked references `bt.status != 'DONE'`, so a substring
    // check on the full SQL would false-positive).
    expect(sql).toContain('WHERE 1=1 ORDER BY')
    expect(params).toEqual([])
  })
})

// ── status / limit / orderBy levers (ListView open/done paths) ────────

describe('buildQuery — status lever', () => {
  it("status='open' adds t.status != 'DONE'", () => {
    const { sql, params } = buildQuery('vault', emptyFilters, ctx, {
      status: 'open'
    })
    expect(sql).toContain("t.status != 'DONE'")
    // WHERE body is only the status clause (not WHERE 1=1).
    expect(sql).toMatch(/WHERE t\.status != 'DONE' ORDER BY/)
    expect(params).toEqual([])
  })

  it("status='done' adds t.status = 'DONE'", () => {
    const { sql, params } = buildQuery('vault', emptyFilters, ctx, {
      status: 'done'
    })
    expect(sql).toContain("t.status = 'DONE'")
    expect(sql).toMatch(/WHERE t\.status = 'DONE' ORDER BY/)
    expect(params).toEqual([])
  })

  it("status='all' / omitted is a no-op", () => {
    const omitted = buildQuery('vault', emptyFilters, ctx).sql
    const all = buildQuery('vault', emptyFilters, ctx, { status: 'all' }).sql
    expect(omitted).toContain('WHERE 1=1 ORDER BY')
    expect(all).toBe(omitted)
  })

  it("status='open' with activeFilter='today' may duplicate open clause (AND is fine)", () => {
    const { sql, params } = buildQuery('vault', emptyFilters, ctx, {
      activeFilter: 'today',
      status: 'open'
    })
    // Both levers contribute; count occurrences of the open predicate.
    const matches = sql.match(/t\.status != 'DONE'/g) ?? []
    expect(matches.length).toBeGreaterThanOrEqual(2)
    expect(sql).toContain('t.due_date = ?')
    expect(params).toEqual(['2026-06-22'])
  })

  it("status='done' with activeFilter='completed' may duplicate DONE clause", () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx, {
      activeFilter: 'completed',
      status: 'done'
    })
    const matches = sql.match(/t\.status = 'DONE'/g) ?? []
    expect(matches.length).toBeGreaterThanOrEqual(2)
  })
})

describe('buildQuery — limit lever', () => {
  it('appends LIMIT N for a positive integer', () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx, { limit: 500 })
    expect(sql).toMatch(/LIMIT 500$/)
  })

  it('ignores missing, non-integer, zero, and negative limits', () => {
    expect(buildQuery('vault', emptyFilters, ctx).sql).not.toContain('LIMIT')
    expect(
      buildQuery('vault', emptyFilters, ctx, { limit: 0 }).sql
    ).not.toContain('LIMIT')
    expect(
      buildQuery('vault', emptyFilters, ctx, { limit: -1 }).sql
    ).not.toContain('LIMIT')
    expect(
      buildQuery('vault', emptyFilters, ctx, { limit: 1.5 }).sql
    ).not.toContain('LIMIT')
  })
})

describe('buildQuery — orderBy override', () => {
  it('replaces the composed ORDER BY body when orderBy is set', () => {
    const { sql } = buildQuery('vault', emptyFilters, ctx, {
      orderBy: 'b.file_date DESC',
      groupBy: 'status',
      sort: 'title'
    })
    expect(sql).toContain('ORDER BY b.file_date DESC')
    expect(sql).not.toContain('ORDER BY t.status ASC')
    expect(sql).not.toContain('b.clean_content ASC')
  })

  it('composes with status + limit for the completed-list shape', () => {
    const { sql, params } = buildQuery('vault', emptyFilters, ctx, {
      status: 'done',
      orderBy: 'b.file_date DESC',
      limit: 200
    })
    expect(sql).toContain("t.status = 'DONE'")
    expect(sql).toContain('ORDER BY b.file_date DESC')
    expect(sql).toMatch(/LIMIT 200$/)
    expect(params).toEqual([])
  })
})
