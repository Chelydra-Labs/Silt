import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// Mock the Wails-bound functions before importing the store.
vi.mock('../../bindings/silt/app.js', () => ({
  ListTemplates: vi.fn(),
  GetTemplate: vi.fn(),
  RenderTemplate: vi.fn(),
  SaveUserTemplate: vi.fn(),
  DeleteUserTemplate: vi.fn(),
  ReloadTemplates: vi.fn(),
  CreatePageFromTemplate: vi.fn(),
  RenderTemplateBlocks: vi.fn()
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn()
  },
  Call: { ByID: vi.fn(), ByName: vi.fn() },
  CancellablePromise: class {
    then() {
      return this
    }
    catch() {
      return this
    }
    finally() {
      return this
    }
  },
  Create: {
    Nullable: (fn: any) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

import {
  templatesState,
  loadTemplates,
  initTemplates,
  _resetForTests
} from './store.svelte'

// Import the mocked modules to configure their behavior.
import { ListTemplates } from '../../bindings/silt/app.js'
import { Events } from '@wailsio/runtime'

const mockListTemplates = vi.mocked(ListTemplates)
const mockEventsOn = vi.mocked(Events.On)

describe('templates store', () => {
  beforeEach(() => {
    _resetForTests()
    vi.clearAllMocks()
  })

  afterEach(() => {
    _resetForTests()
  })

  it('loadTemplates populates templatesState.items', async () => {
    mockListTemplates.mockResolvedValue({
      templates: [
        {
          id: 'daily-note',
          title: 'Daily Note',
          category: 'daily',
          source: 'builtin'
        },
        {
          id: 'meeting-notes',
          title: 'Meeting Notes',
          category: 'meetings',
          source: 'builtin'
        }
      ],
      errors: [],
      warnings: []
    } as any)

    await loadTemplates()

    expect(templatesState.items.length).toBe(2)
    expect(templatesState.items[0].id).toBe('daily-note')
    expect(templatesState.loadError).toBeNull()
    expect(templatesState.loading).toBe(false)
  })

  it('loadTemplates surfaces errors', async () => {
    mockListTemplates.mockRejectedValue(new Error('IPC failed'))

    await loadTemplates()

    expect(templatesState.items.length).toBe(0)
    expect(templatesState.loadError).toBe('IPC failed')
    expect(templatesState.loading).toBe(false)
  })

  it('initTemplates is idempotent', () => {
    const dispose1 = initTemplates()
    const dispose2 = initTemplates()

    // Second call should be a no-op (returns a no-op disposer).
    expect(dispose2()).toBeUndefined()

    dispose1()
  })

  it('initTemplates subscribes to templates:changed', () => {
    const dispose = initTemplates()

    expect(mockEventsOn).toHaveBeenCalledWith(
      'templates:changed',
      expect.any(Function)
    )

    dispose()
  })

  it('loadTemplates preserves plugin_id on plugin templates (#96)', async () => {
    mockListTemplates.mockResolvedValue({
      templates: [
        {
          id: 'plugin-tpl',
          title: 'Plugin Tpl',
          category: 'projects',
          source: 'plugin',
          plugin_id: 'silt-tasks'
        },
        {
          id: 'daily-note',
          title: 'Daily Note',
          category: 'daily',
          source: 'builtin'
        }
      ],
      errors: [],
      warnings: []
    } as any)

    await loadTemplates()

    expect(templatesState.items.length).toBe(2)
    const pluginTpl = templatesState.items.find((t) => t.id === 'plugin-tpl')
    expect(pluginTpl).toBeDefined()
    expect(pluginTpl?.source).toBe('plugin')
    expect(pluginTpl?.plugin_id).toBe('silt-tasks')
  })
})
