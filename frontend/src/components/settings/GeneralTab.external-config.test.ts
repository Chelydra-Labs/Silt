// External-config behaviour of the comment author field (#1125): the
// config:changed hot-reload replaces the whole settings snapshot for ANY
// external config.yaml write (settings/store.svelte.ts), and the field
// never sets settings.dirty, so the store's pendingExternal machinery
// cannot protect it. These tests run against the REAL settings store —
// GeneralTab.test.ts mocks it with a plain object, which cannot invalidate
// the component's deriveds after render. Assigning settings.config from
// here is the same write the hot-reload handler performs.
import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'

const appMocks = vi.hoisted(() =>
  createAppIpcMocks({
    GetCloseToTray: vi.fn().mockResolvedValue(false),
    SetCloseToTray: vi.fn().mockResolvedValue(undefined),
    GetLocalAuthor: vi.fn().mockResolvedValue(''),
    UpdatePluginSetting: vi.fn().mockResolvedValue(undefined),
    GetCustomDictionary: vi.fn().mockResolvedValue([])
  })
)
vi.mock('$silt-app', () => appMocks)
vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn(() => () => {})
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
    Nullable: <T>(fn: T) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

import { settings } from '../../settings/store.svelte'
import type { SystemConfig } from '../../settings/store.svelte'
import GeneralTab from './GeneralTab.svelte'

// Fresh snapshot per call: the hot-reload path swaps in a brand-new object,
// and reusing one would let an updatePluginSetting mirror leak between
// snapshots.
function snapshot(localAuthor: string | undefined): SystemConfig {
  return {
    notebooks: { path: '/vault', default_active: 'Work' },
    editor: {},
    hotkeys: { open_search: 'Ctrl+P' },
    plugins: {
      active: [],
      disabled: [],
      plugin_settings:
        localAuthor === undefined
          ? {}
          : { 'silt-tasks': { local_author: localAuthor } }
    }
  } as unknown as SystemConfig
}

describe('GeneralTab comment author vs external config reload (#1125)', () => {
  const authorInput = () =>
    screen.getByLabelText('Name on task comments') as HTMLInputElement

  beforeEach(() => {
    settings.config = null
    settings.loading = false
    settings.saving = false
    settings.error = ''
    settings.dirty = false
    settings.pendingExternal = false
    appMocks.GetLocalAuthor.mockClear()
    appMocks.UpdatePluginSetting.mockClear()
  })
  afterEach(() => cleanup())

  it('keeps the typed name when an external write replaces the snapshot mid-edit', async () => {
    settings.config = snapshot('Dana')
    render(GeneralTab)
    await waitFor(() => {
      expect(authorInput().value).toBe('Dana')
    })
    await fireEvent.input(authorInput(), { target: { value: 'Typed Name' } })
    // External config.yaml write that does not even touch local_author —
    // the hot-reload still swaps the whole snapshot object.
    settings.config = snapshot('Dana')
    await tick()
    expect(authorInput().value).toBe('Typed Name')
    // Even an external local_author change loses to the in-progress draft
    // (same rule as the store's dirty-draft preservation: the user's edit
    // wins until they save or reload).
    settings.config = snapshot('External Name')
    await tick()
    expect(authorInput().value).toBe('Typed Name')
  })

  it('adopts an externally changed author while the field is untouched', async () => {
    settings.config = snapshot('Dana')
    render(GeneralTab)
    await waitFor(() => {
      expect(authorInput().value).toBe('Dana')
    })
    settings.config = snapshot('External Name')
    await tick()
    expect(authorInput().value).toBe('External Name')
  })

  it('adopts the trimmed persisted value after a save, then still follows external changes', async () => {
    settings.config = snapshot('Dana')
    render(GeneralTab)
    await waitFor(() => {
      expect(authorInput().value).toBe('Dana')
    })
    await fireEvent.input(authorInput(), { target: { value: '  Renamed  ' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => {
      expect(appMocks.UpdatePluginSetting).toHaveBeenCalledWith(
        'silt-tasks',
        'local_author',
        'Renamed'
      )
    })
    // The real store mirrors the trimmed write into settings.config;
    // retiring the touched flag must re-run the sync so the field drops
    // the padding it was typed with.
    await waitFor(() => {
      expect(authorInput().value).toBe('Renamed')
    })
    // The sync must stay alive afterwards: an external snapshot swap still
    // reaches the now-untouched field instead of leaving it detached.
    settings.config = snapshot('External Name')
    await tick()
    expect(authorInput().value).toBe('External Name')
  })

  it('keeps the field empty after a cleared save despite a non-empty OS prefill', async () => {
    // Start absent with a live prefill: this is the configuration the
    // presence rule must survive. Seeding a stored value here (as an
    // earlier version did) skipped the prefill IPC entirely, so the final
    // empty-field assertion also passed under a truthiness regression.
    appMocks.GetLocalAuthor.mockResolvedValue('OS Name')
    settings.config = snapshot(undefined)
    render(GeneralTab)
    await waitFor(() => {
      expect(authorInput().value).toBe('OS Name')
    })
    await fireEvent.input(authorInput(), { target: { value: '' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => {
      expect(appMocks.UpdatePluginSetting).toHaveBeenCalledWith(
        'silt-tasks',
        'local_author',
        ''
      )
    })
    // The mirrored "" is a real stored value: presence keeps the field
    // empty rather than snapping back to the non-empty prefill.
    await waitFor(() => {
      expect(authorInput().value).toBe('')
    })
    // The prefill IPC ran once at mount and stays retired for the session.
    expect(appMocks.GetLocalAuthor).toHaveBeenCalledTimes(1)
  })

  it('re-arms the OS prefill when an external snapshot drops the stored author', async () => {
    appMocks.GetLocalAuthor.mockResolvedValue('OS Name')
    settings.config = snapshot('Dana')
    render(GeneralTab)
    await waitFor(() => {
      expect(authorInput().value).toBe('Dana')
    })
    // Stored at mount: the prefill round-trip is skipped entirely.
    expect(appMocks.GetLocalAuthor).not.toHaveBeenCalled()
    // An external edit (hand-edit of config.yaml, or the backend's legacy
    // seed strip) removes the key: the field falls back to the OS prefill
    // instead of going blank.
    settings.config = snapshot(undefined)
    await waitFor(() => {
      expect(appMocks.GetLocalAuthor).toHaveBeenCalledTimes(1)
    })
    await waitFor(() => {
      expect(authorInput().value).toBe('OS Name')
    })
  })
})
