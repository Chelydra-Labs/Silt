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
  initTasksSettings,
  preloadTasksSettings,
  reloadTasksSettings,
  loadWeekStart,
  persistWeekStart,
  loadInspectorPaneWidth,
  persistInspectorPaneWidth,
  canFitInspectorSplit,
  maxInspectorPaneWidthForHost,
  onTasksSettingsChanged,
  DEFAULT_INSPECTOR_PANE_WIDTH,
  MIN_INSPECTOR_PANE_WIDTH,
  MAX_INSPECTOR_PANE_WIDTH,
  MIN_LIST_MASTER_WIDTH
} from './settings'
import { SYSTEM_VIEWS } from './savedViews'
import { getTaskWeekStart } from '../../../lib/taskWeekStart.svelte'

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

describe('loadColumns (#421/#437)', () => {
  beforeEach(async () => {
    await setTasksSettings({})
  })

  it('returns the default TODO/DOING/DONE when columns is unset', () => {
    expect(loadColumns()).toEqual([
      { name: 'TODO' },
      { name: 'DOING' },
      { name: 'DONE' }
    ])
  })

  it('returns defaults when columns is not an array', async () => {
    await setTasksSettings({
      columns: 'TODO,DOING'
    })
    expect(loadColumns()).toEqual([
      { name: 'TODO' },
      { name: 'DOING' },
      { name: 'DONE' }
    ])
  })

  it('trims to 50 entries when the persisted array exceeds the cap', async () => {
    const many = Array.from({ length: 60 }, (_, i) => `COL${i}`)
    await setTasksSettings({
      columns: many
    })
    const cols = loadColumns()
    expect(cols).toHaveLength(50)
    expect(cols[0]).toEqual({ name: 'COL0' })
    expect(cols[49]).toEqual({ name: 'COL49' })
  })

  it('accepts legacy string[] and structured columns with wipLimit', async () => {
    await setTasksSettings({
      columns: ['Backlog', { name: 'TODO', wipLimit: 3 }, 'DONE']
    })
    expect(loadColumns()).toEqual([
      { name: 'Backlog' },
      { name: 'TODO', wipLimit: 3 },
      { name: 'DONE' }
    ])
  })

  it('returns a copy of a valid array (caller can mutate without touching the snapshot)', async () => {
    await setTasksSettings({
      columns: ['Backlog', 'TODO', 'DOING', 'DONE']
    })
    const cols = loadColumns()
    expect(cols).toEqual([
      { name: 'Backlog' },
      { name: 'TODO' },
      { name: 'DOING' },
      { name: 'DONE' }
    ])
    cols.push({ name: 'Extra' })
    // A second read returns the original persisted set, not the mutated copy.
    expect(loadColumns()).toEqual([
      { name: 'Backlog' },
      { name: 'TODO' },
      { name: 'DOING' },
      { name: 'DONE' }
    ])
  })
})

describe('week_start preference (#888)', () => {
  beforeEach(async () => {
    mocks.updatePluginSetting.mockReset().mockResolvedValue(true)
    await setTasksSettings({})
  })

  it('defaults malformed or missing values to Sunday', async () => {
    expect(loadWeekStart()).toBe('sunday')
    await setTasksSettings({ week_start: 'friday' })
    expect(loadWeekStart()).toBe('sunday')
  })

  it('loads a persisted Monday value', async () => {
    await setTasksSettings({ week_start: 'monday' })
    expect(loadWeekStart()).toBe('monday')
  })

  it('preloads the active-vault value before the Tasks hub consumes settings', async () => {
    const preloadCtx = {
      getPluginSettings: vi.fn().mockResolvedValue({ week_start: 'monday' }),
      updatePluginSetting: mocks.updatePluginSetting
    } as unknown as PluginContext
    const hubCtx = {
      getPluginSettings: vi.fn(),
      updatePluginSetting: mocks.updatePluginSetting
    } as unknown as PluginContext

    await expect(preloadTasksSettings(preloadCtx)).resolves.toBe(true)
    expect(getTaskWeekStart()).toBe('monday')
    await expect(initTasksSettings(hubCtx)).resolves.toBe(true)
    expect(hubCtx.getPluginSettings).not.toHaveBeenCalled()
  })

  it('persists only validated values through the SDK', async () => {
    await expect(persistWeekStart('monday')).resolves.toBe(true)
    expect(getTaskWeekStart()).toBe('monday')
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'week_start',
      'monday'
    )
    mocks.updatePluginSetting.mockClear()
    await expect(persistWeekStart('saturday' as never)).resolves.toBe(false)
    expect(mocks.updatePluginSetting).not.toHaveBeenCalled()
  })

  it('refreshes the reactive source for an active-notebook settings reload', async () => {
    const ctx = {
      getPluginSettings: vi.fn().mockResolvedValue({ week_start: 'monday' }),
      updatePluginSetting: mocks.updatePluginSetting
    } as unknown as PluginContext
    await reloadTasksSettings(ctx)
    expect(getTaskWeekStart()).toBe('monday')
    vi.mocked(ctx.getPluginSettings).mockResolvedValue({ week_start: 'sunday' })
    await reloadTasksSettings(ctx)
    expect(getTaskWeekStart()).toBe('sunday')
  })

  it('ignores stale init and reload completions after a vault switch', async () => {
    let resolveOldInit!: (slice: Record<string, unknown>) => void
    let resolveNewInit!: (slice: Record<string, unknown>) => void
    const oldInit = new Promise<Record<string, unknown>>(
      (resolve) => (resolveOldInit = resolve)
    )
    const newInit = new Promise<Record<string, unknown>>(
      (resolve) => (resolveNewInit = resolve)
    )
    const oldCtx = {
      getPluginSettings: vi.fn(() => oldInit),
      updatePluginSetting: mocks.updatePluginSetting
    } as unknown as PluginContext
    const newCtx = {
      getPluginSettings: vi.fn(() => newInit),
      updatePluginSetting: mocks.updatePluginSetting
    } as unknown as PluginContext

    const oldInitResult = initTasksSettings(oldCtx)
    const newInitResult = initTasksSettings(newCtx)
    resolveNewInit({ week_start: 'monday' })
    await expect(newInitResult).resolves.toBe(true)
    resolveOldInit({ week_start: 'sunday' })
    await expect(oldInitResult).resolves.toBe(false)
    expect(loadWeekStart()).toBe('monday')

    let resolveOldReload!: (slice: Record<string, unknown>) => void
    let resolveNewReload!: (slice: Record<string, unknown>) => void
    const oldReload = new Promise<Record<string, unknown>>(
      (resolve) => (resolveOldReload = resolve)
    )
    const newReload = new Promise<Record<string, unknown>>(
      (resolve) => (resolveNewReload = resolve)
    )
    const oldReloadCtx = {
      getPluginSettings: vi.fn(() => oldReload),
      updatePluginSetting: mocks.updatePluginSetting
    } as unknown as PluginContext
    const newReloadCtx = {
      getPluginSettings: vi.fn(() => newReload),
      updatePluginSetting: mocks.updatePluginSetting
    } as unknown as PluginContext

    const oldReloadResult = reloadTasksSettings(oldReloadCtx)
    const newReloadResult = reloadTasksSettings(newReloadCtx)
    resolveNewReload({ week_start: 'sunday' })
    await expect(newReloadResult).resolves.toBe(true)
    resolveOldReload({ week_start: 'monday' })
    await expect(oldReloadResult).resolves.toBe(false)
    expect(loadWeekStart()).toBe('sunday')
  })
})

describe('inspector pane width (#910)', () => {
  beforeEach(async () => {
    mocks.updatePluginSetting.mockReset().mockResolvedValue(true)
    await setTasksSettings({})
  })

  it('defaults when unset and clamps on load', async () => {
    expect(loadInspectorPaneWidth()).toBe(DEFAULT_INSPECTOR_PANE_WIDTH)
    await setTasksSettings({ inspector_pane_width: 200 })
    expect(loadInspectorPaneWidth()).toBe(MIN_INSPECTOR_PANE_WIDTH)
    await setTasksSettings({ inspector_pane_width: 9999 })
    expect(loadInspectorPaneWidth()).toBe(MAX_INSPECTOR_PANE_WIDTH)
    await setTasksSettings({ inspector_pane_width: 560.7 })
    expect(loadInspectorPaneWidth()).toBe(561)
  })

  it('persist clamps and writes via updatePluginSetting', async () => {
    await persistInspectorPaneWidth(100)
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'inspector_pane_width',
      MIN_INSPECTOR_PANE_WIDTH
    )
    mocks.updatePluginSetting.mockClear()
    await persistInspectorPaneWidth(900)
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'inspector_pane_width',
      MAX_INSPECTOR_PANE_WIDTH
    )
  })

  it('canFitInspectorSplit uses master + handle + pane min', () => {
    const need = MIN_LIST_MASTER_WIDTH + 4 + MIN_INSPECTOR_PANE_WIDTH
    expect(canFitInspectorSplit(need)).toBe(true)
    expect(canFitInspectorSplit(need - 1)).toBe(false)
  })

  it('maxInspectorPaneWidthForHost leaves room for the master list', () => {
    expect(maxInspectorPaneWidthForHost(0)).toBe(MAX_INSPECTOR_PANE_WIDTH)
    // host 900 → budget 900 - 420 - 4 = 476
    expect(maxInspectorPaneWidthForHost(900)).toBe(476)
    // wide host still caps at absolute max
    expect(maxInspectorPaneWidthForHost(2000)).toBe(MAX_INSPECTOR_PANE_WIDTH)
  })

  it('onTasksSettingsChanged fires after init/reload', async () => {
    const seen: number[] = []
    const unsub = onTasksSettingsChanged(() => seen.push(1))
    await setTasksSettings({ inspector_pane_width: 500 })
    expect(seen.length).toBeGreaterThanOrEqual(1)
    unsub()
  })
})
