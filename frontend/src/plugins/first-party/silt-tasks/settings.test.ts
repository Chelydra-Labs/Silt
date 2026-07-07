// Tests for the saved-views persistence layer (#427 Phase 6). Covers:
//   - loadSavedViews merges system + user + legacy with dedup by id
//   - persistSavedViews strips system views before writing (system views
//     are re-derived from code on every load)
//   - loadLegacyKanbanBoardsAsViews maps SavedBoard → SavedView with
//     displayMode='board' / groupBy='status' / sort='manual'
//
// Mocks mirror TasksHub.test.ts / KanbanSidebar.test.ts: hoisted config
// snapshot + updatePluginSetting stub.
import { describe, expect, it, beforeEach, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  updatePluginSetting: vi.fn().mockResolvedValue(true),
  settings: {
    config: {
      plugins: {
        active: [],
        disabled: [],
        plugin_settings: {} as Record<string, Record<string, unknown>>
      }
    },
    loading: false,
    saving: false,
    error: '',
    dirty: false,
    pendingExternal: false
  }
}))

vi.mock('../../../settings/store.svelte', () => ({
  settings: mocks.settings,
  updatePluginSetting: mocks.updatePluginSetting
}))

import {
  loadSavedViews,
  loadLegacyKanbanBoardsAsViews,
  persistSavedViews,
  loadColumns,
  TASKS_PLUGIN_ID
} from './settings'
import { SYSTEM_VIEWS } from './savedViews'

describe('loadSavedViews (#427)', () => {
  beforeEach(() => {
    mocks.settings.config.plugins.plugin_settings = {}
    mocks.updatePluginSetting.mockReset().mockResolvedValue(true)
  })

  it('returns the 3 system views when no user + no legacy configured', () => {
    const views = loadSavedViews()
    expect(views).toHaveLength(SYSTEM_VIEWS.length)
    expect(views.map((v) => v.id).sort()).toEqual(
      SYSTEM_VIEWS.map((v) => v.id).sort()
    )
  })

  it('merges user views from saved_views[] alongside the system views', () => {
    mocks.settings.config.plugins.plugin_settings['silt-tasks'] = {
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
    }
    const views = loadSavedViews()
    expect(views).toHaveLength(SYSTEM_VIEWS.length + 1)
    expect(views.find((v) => v.id === 'u1')?.name).toBe('My Sprint')
  })

  it('coerces unknown enum values to defaults rather than dropping the view', () => {
    mocks.settings.config.plugins.plugin_settings['silt-tasks'] = {
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
    }
    const views = loadSavedViews()
    const u = views.find((v) => v.id === 'u1')
    expect(u).toBeDefined()
    expect(u!.displayMode).toBeUndefined()
    expect(u!.groupBy).toBeUndefined()
    expect(u!.sort).toBeUndefined()
    expect(u!.calendarSubMode).toBeUndefined()
  })

  it('drops entries missing id or name (cannot be activated / displayed)', () => {
    mocks.settings.config.plugins.plugin_settings['silt-tasks'] = {
      saved_views: [
        { name: 'No Id' },
        { id: 'x' },
        { id: 'y', name: 'OK', scope: 'vault' }
      ]
    }
    const views = loadSavedViews()
    const ids = views.map((v) => v.id)
    expect(ids).not.toContain('x')
    expect(ids).toContain('y')
  })

  it("rejects user views carrying the reserved 'sys-' prefix", () => {
    mocks.settings.config.plugins.plugin_settings['silt-tasks'] = {
      saved_views: [
        {
          id: 'sys-today-board',
          name: 'Attempted Hijack',
          scope: 'vault',
          filters: { owners: [], priorities: [], dueDate: '', tags: [] }
        }
      ]
    }
    const views = loadSavedViews()
    // The system view is present (system:true), the user impostor is gone.
    const sys = views.find((v) => v.id === 'sys-today-board')
    expect(sys?.system).toBe(true)
    expect(sys?.name).toBe("Today's Board")
  })

  it('ignores legacy silt-kanban.boards[] (migration is Go-side, not a frontend forward-read)', () => {
    // The Go migrator (#431) lifts legacy boards into saved_views[] once
    // at startup. loadSavedViews() must NOT forward-read the legacy key,
    // or user-deleted views would resurrect from the uncleared boards[]
    // entry on every load.
    mocks.settings.config.plugins.plugin_settings['silt-tasks'] = {
      saved_views: [
        {
          id: 'u1',
          name: 'User',
          scope: 'vault',
          filters: { owners: [], priorities: [], dueDate: '', tags: [] }
        }
      ]
    }
    mocks.settings.config.plugins.plugin_settings['silt-kanban'] = {
      boards: [
        {
          id: 'b1',
          name: 'Legacy Board',
          scope: 'notebook',
          filters: {
            owners: ['a'],
            priorities: [1],
            dueDate: 'today',
            tags: ['x']
          }
        }
      ]
    }
    const views = loadSavedViews()
    const ids = views.map((v) => v.id).sort()
    // Legacy board b1 is absent — loadSavedViews() reads only saved_views[].
    expect(ids).toEqual([...SYSTEM_VIEWS.map((v) => v.id), 'u1'].sort())
    expect(ids).not.toContain('b1')
  })
})

describe('persistSavedViews (#427)', () => {
  beforeEach(() => {
    mocks.updatePluginSetting.mockReset().mockResolvedValue(true)
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
    const [pluginId, key, value] = mocks.updatePluginSetting.mock.calls[0]
    expect(pluginId).toBe(TASKS_PLUGIN_ID)
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
    const [, , value] = mocks.updatePluginSetting.mock.calls[0]
    expect(value).toEqual([])
  })
})

describe('loadLegacyKanbanBoardsAsViews (#427 forward-read)', () => {
  beforeEach(() => {
    mocks.settings.config.plugins.plugin_settings = {}
  })

  it('returns [] when no silt-kanban.boards are configured', () => {
    expect(loadLegacyKanbanBoardsAsViews()).toEqual([])
  })

  it('maps each SavedBoard to a SavedView with the board-mode dimensions', () => {
    mocks.settings.config.plugins.plugin_settings['silt-kanban'] = {
      boards: [
        {
          id: 'b1',
          name: 'My Board',
          scope: 'notebook',
          filters: {
            owners: ['alice', 'bob'],
            priorities: [1, 2],
            dueDate: 'today',
            tags: ['backend']
          }
        }
      ]
    }
    const views = loadLegacyKanbanBoardsAsViews()
    expect(views).toHaveLength(1)
    const v = views[0]
    expect(v.id).toBe('b1')
    expect(v.name).toBe('My Board')
    // A saved board is just a saved view with displayMode='board'.
    expect(v.displayMode).toBe('board')
    expect(v.groupBy).toBe('status')
    expect(v.sort).toBe('manual')
    expect(v.columns).toBeUndefined()
    expect(v.scope).toBe('notebook')
    expect(v.filters?.owners).toEqual(['alice', 'bob'])
    expect(v.filters?.priorities).toEqual([1, 2])
    expect(v.filters?.dueDate).toBe('today')
    expect(v.filters?.tags).toEqual(['backend'])
    // System flag is never set on legacy-mapped views (they're user-owned).
    expect(v.system).toBeUndefined()
  })

  it('skips malformed entries (missing id/name or invalid scope)', () => {
    mocks.settings.config.plugins.plugin_settings['silt-kanban'] = {
      boards: [
        { name: 'No Id' }, // missing id
        { id: 'x' }, // missing name
        {
          id: 'y',
          name: 'Bad Scope',
          scope: 'galaxy', // invalid scope
          filters: {}
        },
        {
          id: 'z',
          name: 'OK',
          scope: 'vault',
          filters: {}
        }
      ]
    }
    const views = loadLegacyKanbanBoardsAsViews()
    expect(views).toHaveLength(1)
    expect(views[0].id).toBe('z')
  })
})

describe('loadColumns (#421)', () => {
  beforeEach(() => {
    mocks.settings.config.plugins.plugin_settings = {}
  })

  it('returns the default TODO/DOING/DONE when columns is unset', () => {
    expect(loadColumns()).toEqual(['TODO', 'DOING', 'DONE'])
  })

  it('returns defaults when columns is not an array', () => {
    mocks.settings.config.plugins.plugin_settings['silt-tasks'] = {
      columns: 'TODO,DOING'
    }
    expect(loadColumns()).toEqual(['TODO', 'DOING', 'DONE'])
  })

  it('trims to 50 entries when the persisted array exceeds the cap', () => {
    const many = Array.from({ length: 60 }, (_, i) => `COL${i}`)
    mocks.settings.config.plugins.plugin_settings['silt-tasks'] = {
      columns: many
    }
    const cols = loadColumns()
    expect(cols).toHaveLength(50)
    expect(cols[0]).toBe('COL0')
    expect(cols[49]).toBe('COL49')
  })

  it('returns a copy of a valid array (caller can mutate without touching the snapshot)', () => {
    mocks.settings.config.plugins.plugin_settings['silt-tasks'] = {
      columns: ['Backlog', 'TODO', 'DOING', 'DONE']
    }
    const cols = loadColumns()
    expect(cols).toEqual(['Backlog', 'TODO', 'DOING', 'DONE'])
    cols.push('Extra')
    // A second read returns the original persisted set, not the mutated copy.
    expect(loadColumns()).toEqual(['Backlog', 'TODO', 'DOING', 'DONE'])
  })
})
