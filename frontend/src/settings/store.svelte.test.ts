import { describe, it, expect, beforeEach, vi } from 'vitest'

// Mock the Wails IPC bindings. The canonical pattern (per AGENTS.md /
// AppearanceTab.test.ts) uses vi.hoisted so the mock factory can reference
// the mutable spies. Each toggle calls exactly one binding; we assert the
// right one fired with the right value and that the config mirror updated.
const mocks = vi.hoisted(() => ({
  SetOpenDevtoolsOnStartup: vi.fn().mockResolvedValue(undefined),
  SetShowFormatToolbar: vi.fn().mockResolvedValue(undefined),
  UpdatePluginSetting: vi.fn().mockResolvedValue(undefined)
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    GetSystemConfig: vi.fn(),
    GetConfigLoadError: vi.fn(),
    SaveSystemConfig: vi.fn(),
    UpdatePluginSetting: mocks.UpdatePluginSetting,
    AppendDismissedTip: vi.fn(),
    SetShowFormatToolbar: mocks.SetShowFormatToolbar,
    SetFocusMode: vi.fn(),
    SetTypewriterMode: vi.fn(),
    SetOpenDevtoolsOnStartup: mocks.SetOpenDevtoolsOnStartup
  })
)
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
    Nullable: (fn: unknown) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

import {
  settings,
  toggleDevMode,
  updatePluginSetting,
  type SystemConfig
} from './store.svelte'

function resetConfig(): void {
  settings.config = {
    ui: {
      open_devtools_on_startup: false,
      show_format_toolbar: true
    },
    editor: {}
  } as SystemConfig
  settings.saving = false
  settings.error = ''
  settings.dirty = false
}

describe('toggleDevMode (#363 — atomic Dev Mode toggle)', () => {
  beforeEach(() => {
    mocks.SetOpenDevtoolsOnStartup.mockReset()
    mocks.SetOpenDevtoolsOnStartup.mockResolvedValue(undefined)
    mocks.SetShowFormatToolbar.mockReset()
    mocks.SetShowFormatToolbar.mockResolvedValue(undefined)
    resetConfig()
  })

  it('flips false → true, calls the atomic setter, and mirrors the field', async () => {
    const result = await toggleDevMode()
    expect(result).toBe(true)
    expect(mocks.SetOpenDevtoolsOnStartup).toHaveBeenCalledWith(true)
    expect(mocks.SetShowFormatToolbar).not.toHaveBeenCalled()
    expect(settings.config?.ui?.open_devtools_on_startup).toBe(true)
    expect(settings.saving).toBe(false)
    expect(settings.error).toBe('')
  })

  it('flips true → false on the second call', async () => {
    await toggleDevMode() // → true
    const result = await toggleDevMode() // → false
    expect(result).toBe(false)
    expect(mocks.SetOpenDevtoolsOnStartup).toHaveBeenLastCalledWith(false)
    expect(settings.config?.ui?.open_devtools_on_startup).toBe(false)
  })

  it('surfaces the error and leaves the config unchanged when the IPC rejects', async () => {
    mocks.SetOpenDevtoolsOnStartup.mockRejectedValueOnce(
      new Error('vault not loaded')
    )
    const result = await toggleDevMode()
    expect(result).toBe(null)
    expect(settings.error).toBe('vault not loaded')
    // The local mirror must NOT have flipped on failure.
    expect(settings.config?.ui?.open_devtools_on_startup).toBe(false)
    expect(settings.saving).toBe(false)
  })

  it('returns null when no config is loaded yet', async () => {
    settings.config = null
    const result = await toggleDevMode()
    expect(result).toBe(null)
    expect(mocks.SetOpenDevtoolsOnStartup).not.toHaveBeenCalled()
  })
})

// updatePluginSetting is the atomic single-field write the Settings tabs
// route through (e.g. the silt-tasks local_author pref, #1125). The local
// mirror below is what makes the UI reflect the save without a
// config:changed round-trip — losing it surfaces as a stale field.
describe('updatePluginSetting (atomic single-field write + local mirror)', () => {
  beforeEach(() => {
    mocks.UpdatePluginSetting.mockReset()
    mocks.UpdatePluginSetting.mockResolvedValue(undefined)
    resetConfig()
  })

  it('writes one field via IPC and deep-creates the mirror path', async () => {
    const result = await updatePluginSetting(
      'silt-tasks',
      'local_author',
      'Dana'
    )
    expect(result).toBe(true)
    expect(mocks.UpdatePluginSetting).toHaveBeenCalledWith(
      'silt-tasks',
      'local_author',
      'Dana'
    )
    expect(settings.saving).toBe(false)
    expect(
      settings.config?.plugins?.plugin_settings?.['silt-tasks']?.[
        'local_author'
      ]
    ).toBe('Dana')
  })

  it('mirrors into an existing plugin pref without clobbering siblings', async () => {
    settings.config = {
      plugins: {
        active: ['silt-tasks'],
        disabled: [],
        plugin_settings: {
          'other-plugin': { keep: 'me' },
          'silt-tasks': { shortlist_pin: true }
        }
      }
    } as unknown as SystemConfig
    await updatePluginSetting('silt-tasks', 'local_author', '')
    expect(
      settings.config?.plugins?.plugin_settings?.['silt-tasks']?.[
        'local_author'
      ]
    ).toBe('')
    expect(
      settings.config?.plugins?.plugin_settings?.['silt-tasks']?.shortlist_pin
    ).toBe(true)
    expect(
      settings.config?.plugins?.plugin_settings?.['other-plugin']?.keep
    ).toBe('me')
  })

  it('surfaces the error and leaves the config unchanged when the IPC rejects', async () => {
    mocks.UpdatePluginSetting.mockRejectedValueOnce(new Error('disk full'))
    const result = await updatePluginSetting(
      'silt-tasks',
      'local_author',
      'Dana'
    )
    expect(result).toBe(false)
    expect(settings.error).toBe('disk full')
    expect(settings.config?.plugins?.plugin_settings).toBeUndefined()
    expect(settings.saving).toBe(false)
  })
})
