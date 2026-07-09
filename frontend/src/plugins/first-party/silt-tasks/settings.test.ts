// Tests for the saved-views persistence layer (#427 Phase 6). Covers:
//   - loadSavedViews merges system + user views with dedup by id
//   - persistSavedViews strips system views before writing (system views
//     are re-derived from code on every load)
//   - loadColumns defaults + cap behavior
//
// Settings are routed through the PluginContext SDK: each test seeds the
// module's config slice via initTasksSettings(ctx) with a stub ctx whose
// getPluginSettings resolves to the desired slice. persist* calls land on
// mocks.updatePluginSetting (the same binding the SDK wraps).
import { describe, expect, it, beforeEach, vi } from 'vitest'
import type { PluginContext } from '../../sdk'

const mocks = vi.hoisted(() => ({
  updatePluginSetting: vi.fn().mockResolvedValue(true)
}))

import {
  loadSavedViews,
  persistSavedViews,
  loadColumns,
  initTasksSettings
} from './settings'
import { SYSTEM_VIEWS } from './savedViews'

// Seed the module-scoped config slice (and wire saveFn to
// mocks.updatePluginSetting) by constructing a throwaway ctx whose
// getPluginSettings resolves to `slice`. Mirrors how TasksHub's onMount
// calls initTasksSettings(ctx) before any load* read.
async function setTasksSettings(slice: Record<string, unknown>) {
  const ctx = {
    getPluginSettings: vi.fn().mockResolvedValue(slice),
    updatePluginSetting: mocks.updatePluginSetting
  } as unknown as PluginContext
  await initTasksSettings(ctx)
}

describe('loadSavedViews (#427)', () => {
  beforeEach(async () => {
    mocks.updatePluginSetting.mockReset().mockResolvedValue(true)
    await setTasksSettings({})
  })

  it('returns the 3 system views when no user + no legacy configured', () => {
    const views = loadSavedViews()
    expect(views).toHaveLength(SYSTEM_VIEWS.length)
    expect(views.map((v) => v.id).sort()).toEqual(
      SYSTEM_VIEWS.map((v) => v.id).sort()
    )
  })

  it('merges user views from saved_views[] alongside the system views', async () => {
    await setTasksSettings({
      saved_views: [
        {
          id: 'u1',
          name: 'My Sprint',
          displayMode: 'board',
          groupBy: 'status',
          sort: 'manual',
          scope: 'vault',
          filters: { owners: [], priorities: [], dueDate: '', tags: [] }
        }
      ]
    })
    const views = loadSavedViews()
    expect(views).toHaveLength(SYSTEM_VIEWS.length + 1)
    expect(views.find((v) => v.id === 'u1')?.name).toBe('My Sprint')
  })

  it('coerces unknown enum values to defaults rather than dropping the view', async () => {
    await setTasksSettings({
      saved_views: [
        {
          id: 'u1',
          name: 'Bad Enums',
          // All four enums are unknown garbage; coercion drops just them.
          displayMode: 'bogus',
          groupBy: 'bogus',
          sort: 'bogus',
          calendarSubMode: 'bogus',
          scope: 'vault',
          filters: { owners: [], priorities: [], dueDate: '', tags: [] }
        }
      ]
    })
    const views = loadSavedViews()
    const u = views.find((v) => v.id === 'u1')
    expect(u).toBeDefined()
    expect(u!.displayMode).toBeUndefined()
    expect(u!.groupBy).toBeUndefined()
    expect(u!.sort).toBeUndefined()
    expect(u!.calendarSubMode).toBeUndefined()
  })

  it('drops entries missing id or name (cannot be activated / displayed)', async () => {
    await setTasksSettings({
      saved_views: [
        { name: 'No Id' },
        { id: 'x' },
        { id: 'y', name: 'OK', scope: 'vault' }
      ]
    })
    const views = loadSavedViews()
    const ids = views.map((v) => v.id)
    expect(ids).not.toContain('x')
    expect(ids).toContain('y')
  })

  it("rejects user views carrying the reserved 'sys-' prefix", async () => {
    await setTasksSettings({
      saved_views: [
        {
          id: 'sys-today-board',
          name: 'Attempted Hijack',
          scope: 'vault',
          filters: { owners: [], priorities: [], dueDate: '', tags: [] }
        }
      ]
    })
    const views = loadSavedViews()
    // The system view is present (system:true), the user impostor is gone.
    const sys = views.find((v) => v.id === 'sys-today-board')
    expect(sys?.system).toBe(true)
    expect(sys?.name).toBe("Today's Board")
  })

  it('ignores legacy silt-kanban.boards[] (migration is Go-side, not a frontend forward-read)', async () => {
    // The Go migrator (#431) lifts legacy boards into saved_views[] once
    // at startup. loadSavedViews() must NOT forward-read the legacy key,
    // or user-deleted views would resurrect from the uncleared boards[]
    // entry on every load.
    await setTasksSettings({
      saved_views: [
        {
          id: 'u1',
          name: 'User',
          scope: 'vault',
          filters: { owners: [], priorities: [], dueDate: '', tags: [] }
        }
      ]
    })
    const views = loadSavedViews()
    const ids = views.map((v) => v.id).sort()
    // Legacy board b1 is absent — loadSavedViews() reads only saved_views[].
    expect(ids).toEqual([...SYSTEM_VIEWS.map((v) => v.id), 'u1'].sort())
    expect(ids).not.toContain('b1')
  })
})

describe('persistSavedViews (#427)', () => {
  beforeEach(async () => {
    mocks.updatePluginSetting.mockReset().mockResolvedValue(true)
    await setTasksSettings({})
  })

  it('strips system views before writing (they re-derive from code on load)', async () => {
    const ok = await persistSavedViews([
      ...SYSTEM_VIEWS,
      {
        id: 'u1',
        name: 'User',
        displayMode: 'list',
        system: false
      }
    ])
    expect(ok).toBe(true)
    expect(mocks.updatePluginSetting).toHaveBeenCalledTimes(1)
    // initTasksSettings wires saveFn to ctx.updatePluginSetting(key, value) —
    // the SDK knows the plugin id from its session, so only the key + value
    // are passed.
    const [key, value] = mocks.updatePluginSetting.mock.calls[0]
    expect(key).toBe('saved_views')
    const written = value as Array<{ id: string; system?: boolean }>
    expect(written).toHaveLength(1)
    expect(written[0].id).toBe('u1')
    // The system flag is stripped on write so a stale `system: true` in
    // YAML can't lock a user view out of deletion next load.
    expect(written[0].system).toBeUndefined()
  })

  it('writes an empty array when only system views are present', async () => {
    await persistSavedViews(SYSTEM_VIEWS)
    const [, value] = mocks.updatePluginSetting.mock.calls[0]
    expect(value).toEqual([])
  })
})

describe('loadColumns (#421)', () => {
  beforeEach(async () => {
    await setTasksSettings({})
  })

  it('returns the default TODO/DOING/DONE when columns is unset', () => {
    expect(loadColumns()).toEqual(['TODO', 'DOING', 'DONE'])
  })

  it('returns defaults when columns is not an array', async () => {
    await setTasksSettings({
      columns: 'TODO,DOING'
    })
    expect(loadColumns()).toEqual(['TODO', 'DOING', 'DONE'])
  })

  it('trims to 50 entries when the persisted array exceeds the cap', async () => {
    const many = Array.from({ length: 60 }, (_, i) => `COL${i}`)
    await setTasksSettings({
      columns: many
    })
    const cols = loadColumns()
    expect(cols).toHaveLength(50)
    expect(cols[0]).toBe('COL0')
    expect(cols[49]).toBe('COL49')
  })

  it('returns a copy of a valid array (caller can mutate without touching the snapshot)', async () => {
    await setTasksSettings({
      columns: ['Backlog', 'TODO', 'DOING', 'DONE']
    })
    const cols = loadColumns()
    expect(cols).toEqual(['Backlog', 'TODO', 'DOING', 'DONE'])
    cols.push('Extra')
    // A second read returns the original persisted set, not the mutated copy.
    expect(loadColumns()).toEqual(['Backlog', 'TODO', 'DOING', 'DONE'])
  })
})
