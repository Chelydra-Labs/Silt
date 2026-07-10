import { describe, it, expect, beforeEach, vi } from 'vitest'

// Mock the Wails IPC bindings. The canonical pattern (per AGENTS.md /
// AppearanceTab.test.ts) uses vi.hoisted so the mock factory can reference
// the mutable spies. Each toggle calls exactly one binding; we assert the
// right one fired with the right value and that the config mirror updated.
const mocks = vi.hoisted(() => ({
  SetOpenDevtoolsOnStartup: vi.fn().mockResolvedValue(undefined),
  SetShowFormatToolbar: vi.fn().mockResolvedValue(undefined)
}))

vi.mock('../../bindings/silt/app.js', () => ({
  GetSystemConfig: vi.fn(),
  GetConfigLoadError: vi.fn(),
  SaveSystemConfig: vi.fn(),
  UpdatePluginSetting: vi.fn(),
  AppendDismissedTip: vi.fn(),
  SetShowFormatToolbar: mocks.SetShowFormatToolbar,
  SetFocusMode: vi.fn(),
  SetTypewriterMode: vi.fn(),
  SetOpenDevtoolsOnStartup: mocks.SetOpenDevtoolsOnStartup
}))
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
    Nullable: (fn: any) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

import { settings, toggleDevMode, toggleFormatToolbar } from './store.svelte'

function resetConfig(): void {
  settings.config = {
    ui: {
      open_devtools_on_startup: false,
      show_format_toolbar: true
    },
    editor: {}
  } as any
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
