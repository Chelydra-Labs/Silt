// Tests for the dashboard saved-views pure helpers (#863). Mirrors the
// silt-tasks settings.test.ts load/persist/coerce contract: merge + dedup,
// coerce unknown enums to defaults, drop malformed entries, strip system
// before write, and the reserved `sys-` id prefix can't shadow a built-in.
import { describe, expect, it } from 'vitest'
import {
  coerceDashboardSavedView,
  loadDashboardSavedViews,
  persistableDashboardSavedViews,
  viewMatchesDashboardState,
  type DashboardSavedView,
  type DashboardViewState
} from './dashboardSavedViews'

function state(
  overrides: Partial<DashboardViewState> = {}
): DashboardViewState {
  return {
    typeId: 'book',
    filter: {},
    sort: { property: '', desc: false },
    groupBy: '',
    viewMode: 'list',
    ...overrides
  }
}

describe('coerceDashboardSavedView', () => {
  it('keeps a well-formed entry and normalizes sort.desc to a boolean', () => {
    const v = coerceDashboardSavedView({
      id: 'u1',
      name: 'My view',
      typeId: 'book',
      filter: { status: 'read' },
      sort: { property: 'title', desc: true },
      groupBy: 'status',
      viewMode: 'board'
    })
    expect(v).toEqual({
      id: 'u1',
      name: 'My view',
      typeId: 'book',
      filter: { status: 'read' },
      sort: { property: 'title', desc: true },
      groupBy: 'status',
      viewMode: 'board'
    })
  })

  it('drops entries missing id / name / typeId', () => {
    expect(coerceDashboardSavedView({ name: 'x', typeId: 't' })).toBeNull()
    expect(coerceDashboardSavedView({ id: 'u', typeId: 't' })).toBeNull()
    expect(coerceDashboardSavedView({ id: 'u', name: 'x' })).toBeNull()
  })

  it("rejects entries carrying the reserved 'sys-' prefix", () => {
    expect(
      coerceDashboardSavedView({
        id: 'sys-impostor',
        name: 'X',
        typeId: 'book'
      })
    ).toBeNull()
  })

  it('drops non-string filter values (the IPC bag is Record<string,string>)', () => {
    const v = coerceDashboardSavedView({
      id: 'u',
      name: 'x',
      typeId: 'book',
      filter: { ok: 'read', bad: 3, alsoBad: { x: 1 } }
    })
    expect(v!.filter).toEqual({ ok: 'read' })
  })

  it('coerces an unknown viewMode by dropping it (rather than the whole view)', () => {
    const v = coerceDashboardSavedView({
      id: 'u',
      name: 'x',
      typeId: 'book',
      viewMode: 'gallery'
    })
    expect(v).not.toBeNull()
    expect(v!.viewMode).toBeUndefined()
  })

  it('treats a non-object / array entry as null', () => {
    expect(coerceDashboardSavedView(null)).toBeNull()
    expect(coerceDashboardSavedView('hi')).toBeNull()
    expect(coerceDashboardSavedView([1, 2])).toBeNull()
  })
})

describe('loadDashboardSavedViews', () => {
  it('returns [] for a non-array slice', () => {
    expect(loadDashboardSavedViews(undefined)).toEqual([])
    expect(loadDashboardSavedViews({})).toEqual([])
  })

  it('coerces valid entries and drops malformed ones', () => {
    const views = loadDashboardSavedViews([
      { id: 'u1', name: 'One', typeId: 'book' },
      { name: 'no id' }, // dropped
      { id: 'u2', name: 'Two', typeId: 'meeting' }
    ])
    expect(views.map((v) => v.id)).toEqual(['u1', 'u2'])
  })

  it('dedupes by id (first wins) so hand-edited YAML dups surface once', () => {
    const views = loadDashboardSavedViews([
      { id: 'dup', name: 'First', typeId: 'book' },
      { id: 'dup', name: 'Second', typeId: 'book' }
    ])
    expect(views).toHaveLength(1)
    expect(views[0].name).toBe('First')
  })

  it('preserves views across multiple types (the list is dashboard-wide)', () => {
    const views = loadDashboardSavedViews([
      { id: 'a', name: 'A', typeId: 'book' },
      { id: 'b', name: 'B', typeId: 'meeting' }
    ])
    expect(views.map((v) => v.typeId).sort()).toEqual(['book', 'meeting'])
  })
})

describe('persistableDashboardSavedViews', () => {
  it('strips the system marker from every emitted record', () => {
    const out = persistableDashboardSavedViews([
      { id: 'u1', name: 'U', typeId: 'book', system: false },
      { id: 'u2', name: 'U2', typeId: 'book' }
    ])
    expect(out).toEqual([
      { id: 'u1', name: 'U', typeId: 'book' },
      { id: 'u2', name: 'U2', typeId: 'book' }
    ])
    for (const r of out) expect('system' in r).toBe(false)
  })

  it('drops system views before writing (they re-derive from code on load)', () => {
    const out = persistableDashboardSavedViews([
      { id: 'sys-x', name: 'S', typeId: 'book', system: true },
      { id: 'u1', name: 'U', typeId: 'book' }
    ])
    expect(out).toEqual([{ id: 'u1', name: 'U', typeId: 'book' }])
  })

  it('emits [] when only system views are present', () => {
    expect(
      persistableDashboardSavedViews([
        { id: 'sys-x', name: 'S', typeId: 'book', system: true }
      ])
    ).toEqual([])
  })
})

describe('viewMatchesDashboardState', () => {
  it('matches when every defined dim equals the live state', () => {
    const view: DashboardSavedView = {
      id: 'u',
      name: 'mine',
      typeId: 'book',
      groupBy: 'status',
      viewMode: 'board'
    }
    expect(
      viewMatchesDashboardState(
        view,
        state({ groupBy: 'status', viewMode: 'board' })
      )
    ).toBe(true)
  })

  it('mismatches when a defined scalar differs', () => {
    const view: DashboardSavedView = {
      id: 'u',
      name: 'mine',
      typeId: 'meeting',
      groupBy: 'status'
    }
    expect(viewMatchesDashboardState(view, state({ typeId: 'book' }))).toBe(
      false
    )
    expect(viewMatchesDashboardState(view, state({ groupBy: 'owner' }))).toBe(
      false
    )
  })

  it('undefined-by-view dims do not disqualify (partial template)', () => {
    const view: DashboardSavedView = { id: 'u', name: 'mine', typeId: 'book' }
    // Defines only typeId; any filter/sort/groupBy/viewMode still matches.
    expect(
      viewMatchesDashboardState(
        view,
        state({ filter: { a: 'b' }, groupBy: 'x', viewMode: 'board' })
      )
    ).toBe(true)
  })

  it('filter dim compares the bag structurally', () => {
    const view: DashboardSavedView = {
      id: 'u',
      name: 'mine',
      typeId: 'book',
      filter: { status: 'read' }
    }
    expect(
      viewMatchesDashboardState(view, state({ filter: { status: 'read' } }))
    ).toBe(true)
    expect(
      viewMatchesDashboardState(view, state({ filter: { status: 'unread' } }))
    ).toBe(false)
    expect(
      viewMatchesDashboardState(
        view,
        state({ filter: { status: 'read', extra: 'x' } })
      )
    ).toBe(false)
  })

  it('sort dim compares property + desc structurally', () => {
    const view: DashboardSavedView = {
      id: 'u',
      name: 'mine',
      typeId: 'book',
      sort: { property: 'title', desc: false }
    }
    expect(
      viewMatchesDashboardState(
        view,
        state({ sort: { property: 'title', desc: false } })
      )
    ).toBe(true)
    expect(
      viewMatchesDashboardState(
        view,
        state({ sort: { property: 'title', desc: true } })
      )
    ).toBe(false)
  })

  it('a complete-snapshot view reduces to the strict check', () => {
    const view: DashboardSavedView = {
      id: 'u',
      name: 'mine',
      typeId: 'book',
      filter: { status: 'read' },
      sort: { property: 'title', desc: true },
      groupBy: 'status',
      viewMode: 'board'
    }
    const s = state({
      filter: { status: 'read' },
      sort: { property: 'title', desc: true },
      groupBy: 'status',
      viewMode: 'board'
    })
    expect(viewMatchesDashboardState(view, s)).toBe(true)
    expect(viewMatchesDashboardState(view, { ...s, viewMode: 'list' })).toBe(
      false
    )
  })
})
