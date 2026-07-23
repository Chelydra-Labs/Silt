import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'

const mocks = vi.hoisted(() => {
  const baseConfig = {
    notebooks: { path: '/vault', default_active: 'Work' },
    editor: {
      font_family: 'Plus Jakarta Sans',
      mono_font_family: 'JetBrains Mono',
      font_size_px: 14,
      line_height: 1.6,
      tab_indent_spaces: 4,
      auto_save_delay_ms: 500,
      focus_highlight_ancestors: true
    },
    parsing: {
      auto_inject_uuid: true,
      default_task_priority: 3
    },
    hotkeys: { open_search: 'Ctrl+P' },
    plugins: { active: [], disabled: [], plugin_settings: {} }
  }
  return {
    baseConfig,
    settings: {
      config: baseConfig,
      loading: false,
      saving: false,
      error: '',
      dirty: false,
      pendingExternal: false
    },
    saveConfig: vi.fn(async () => true),
    reloadFromBackend: vi.fn(async () => {}),
    themeState: {
      id: 'cyber_forest',
      name: 'Cyber Forest',
      mode: 'dark',
      darkTokens: {
        '--color-surface-app': '#0c0c0e',
        '--font-body': "'Plus Jakarta Sans', sans-serif",
        '--font-mono': "'JetBrains Mono', monospace",
        '--font-headline': "'Hanken Grotesk', sans-serif"
      },
      lightTokens: {},
      error: null as string | null
    }
  }
})

const appMocks = vi.hoisted(() => ({
  PickVaultDestination: vi.fn(),
  MoveVault: vi.fn(),
  CopyVault: vi.fn(),
  SwitchVault: vi.fn(),
  PickVaultExportPath: vi.fn(),
  ExportVault: vi.fn(),
  PickVaultArchive: vi.fn(),
  ImportVault: vi.fn(),
  GetCloseToTray: vi.fn().mockResolvedValue(false),
  SetCloseToTray: vi.fn().mockResolvedValue(undefined),
  // Custom dictionary IPC (#196 / #338) — GeneralTab loads the list on mount.
  GetCustomDictionary: vi.fn().mockResolvedValue([]),
  AddCustomDictionaryWord: vi.fn().mockResolvedValue([]),
  RemoveCustomDictionaryWord: vi.fn().mockResolvedValue([]),
  PickCustomDictionaryExportPath: vi.fn().mockResolvedValue(''),
  PickCustomDictionaryImportFile: vi.fn().mockResolvedValue(''),
  ExportCustomDictionary: vi.fn().mockResolvedValue(undefined),
  ImportCustomDictionary: vi
    .fn()
    .mockResolvedValue({ added: 0, skipped: 0, total_read: 0 })
}))
vi.mock('../../../bindings/silt/app.js', () => appMocks)
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

vi.mock('../../settings/store.svelte', () => ({
  settings: mocks.settings,
  saveConfig: mocks.saveConfig,
  reloadFromBackend: mocks.reloadFromBackend
}))
vi.mock('../../theme/store.svelte', () => ({ themeState: mocks.themeState }))

import GeneralTab from './GeneralTab.svelte'

describe('GeneralTab vault relocate menu (#141)', () => {
  beforeEach(() => {
    mocks.settings.dirty = false
    appMocks.PickVaultDestination.mockClear()
    appMocks.MoveVault.mockClear()
    appMocks.CopyVault.mockClear()
    appMocks.SwitchVault.mockClear()
    appMocks.PickVaultExportPath.mockClear()
    appMocks.ExportVault.mockClear()
    appMocks.PickVaultArchive.mockClear()
    appMocks.ImportVault.mockClear()
  })
  afterEach(() => cleanup())

  it('renders the vault actions kebab button', async () => {
    render(GeneralTab)
    await tick()
    expect(
      screen.getByRole('button', { name: 'Vault actions' })
    ).toBeInTheDocument()
  })

  it('opening the menu reveals Move, Copy, Export, and Import actions', async () => {
    render(GeneralTab)
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Vault actions' }))
    await tick()
    expect(
      screen.getByRole('menuitem', { name: /Move vault/ })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('menuitem', { name: /Copy vault/ })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('menuitem', { name: /Export vault/ })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('menuitem', { name: /Import vault/ })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('menuitem', { name: /Switch vault/ })
    ).toBeInTheDocument()
  })

  it('Switch vault dispatches the silt:change-vault event', async () => {
    render(GeneralTab)
    await tick()
    const handler = vi.fn()
    window.addEventListener('silt:change-vault', handler)
    await fireEvent.click(screen.getByRole('button', { name: 'Vault actions' }))
    await tick()
    await fireEvent.click(
      screen.getByRole('menuitem', { name: /Switch vault/ })
    )
    expect(handler).toHaveBeenCalledTimes(1)
    window.removeEventListener('silt:change-vault', handler)
  })

  it('selecting Move opens the VaultActionModal in move mode', async () => {
    render(GeneralTab)
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Vault actions' }))
    await tick()
    await fireEvent.click(screen.getByRole('menuitem', { name: /Move vault/ }))
    await tick()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Move vault' })
    ).toBeInTheDocument()
  })

  it('selecting Export opens the VaultArchiveModal in export mode', async () => {
    render(GeneralTab)
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Vault actions' }))
    await tick()
    await fireEvent.click(
      screen.getByRole('menuitem', { name: /Export vault/ })
    )
    await tick()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Export vault' })
    ).toBeInTheDocument()
  })

  it('selecting Import opens the VaultArchiveModal in import mode', async () => {
    render(GeneralTab)
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Vault actions' }))
    await tick()
    await fireEvent.click(
      screen.getByRole('menuitem', { name: /Import vault/ })
    )
    await tick()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Import vault' })
    ).toBeInTheDocument()
  })

  it('Escape on a menu item collapses the menu', async () => {
    render(GeneralTab)
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Vault actions' }))
    await tick()
    const moveItem = screen.getByRole('menuitem', { name: /Move vault/ })
    moveItem.focus()
    await fireEvent.keyDown(moveItem, { key: 'Escape' })
    await tick()
    expect(screen.queryByRole('menuitem', { name: /Move vault/ })).toBeNull()
  })

  it('clicking outside the menu collapses it', async () => {
    render(GeneralTab)
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Vault actions' }))
    await tick()
    expect(
      screen.getByRole('menuitem', { name: /Move vault/ })
    ).toBeInTheDocument()
    await fireEvent.click(document.body)
    await tick()
    expect(screen.queryByRole('menuitem', { name: /Move vault/ })).toBeNull()
  })
})

// Close-to-tray is a user-global window preference (#501). Its state lives on
// the tab, not in the vault-scoped config store. These tests pin the
// hydration-from-disk, optimistic-toggle-persist, failure-revert-with-alert,
// and inflight rapid-click guard contracts.
describe('GeneralTab close-to-tray toggle (#501)', () => {
  beforeEach(() => {
    mocks.settings.config = mocks.baseConfig
    mocks.settings.error = ''
    mocks.settings.loading = false
    appMocks.GetCloseToTray.mockReset()
    appMocks.SetCloseToTray.mockReset()
    appMocks.GetCloseToTray.mockResolvedValue(false)
    appMocks.SetCloseToTray.mockResolvedValue(undefined)
  })
  afterEach(() => cleanup())

  it('hydrates the toggle from the persisted preference (on)', async () => {
    appMocks.GetCloseToTray.mockResolvedValue(true)
    render(GeneralTab)
    const sw = screen.getByRole('switch', { name: 'Close to tray' })
    // onMount awaits GetCloseToTray then flips the state. waitFor rides
    // the microtask boundary between the resolved mock and the $state write.
    await waitFor(() => {
      expect(sw).toHaveAttribute('aria-checked', 'true')
    })
    expect(appMocks.GetCloseToTray).toHaveBeenCalledTimes(1)
  })

  it('persists a successful toggle and stays flipped', async () => {
    render(GeneralTab)
    const sw = screen.getByRole('switch', { name: 'Close to tray' })
    expect(sw).toHaveAttribute('aria-checked', 'false')
    await fireEvent.click(sw)
    await tick()
    expect(appMocks.SetCloseToTray).toHaveBeenCalledWith(true)
    expect(sw).toHaveAttribute('aria-checked', 'true')
    // No error surfaced on success.
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('reverts the toggle on failure and surfaces role=alert', async () => {
    appMocks.SetCloseToTray.mockRejectedValue(new Error('disk full'))
    render(GeneralTab)
    const sw = screen.getByRole('switch', { name: 'Close to tray' })
    await fireEvent.click(sw)
    await tick()
    expect(appMocks.SetCloseToTray).toHaveBeenCalledWith(true)
    // Reverted to the pre-toggle state.
    expect(sw).toHaveAttribute('aria-checked', 'false')
    const alert = screen.getByRole('alert')
    expect(alert.textContent).toMatch(/could not save/i)
  })

  it('ignores a rapid second click while a write is inflight', async () => {
    // Hold the write open so the toggle stays inflight across the
    // second click — exercises the `if (closeToTrayInflight) return` guard.
    let resolveWrite!: () => void
    appMocks.SetCloseToTray.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          resolveWrite = resolve
        })
    )
    render(GeneralTab)
    const sw = screen.getByRole('switch', { name: 'Close to tray' })
    await fireEvent.click(sw)
    await tick()
    expect(appMocks.SetCloseToTray).toHaveBeenCalledTimes(1)
    // The toggle is disabled mid-write (the visual half of the guard).
    expect(sw).toBeDisabled()
    // Rapid second click — the inflight guard returns early.
    await fireEvent.click(sw)
    await tick()
    expect(appMocks.SetCloseToTray).toHaveBeenCalledTimes(1)
    resolveWrite()
    await tick()
  })
})

// When config is absent but the backend reported a load error, the
// Workspace section must surface the actual error (so the user can act
// on a broken config.yaml) instead of the generic no-workspace copy.
// The Window section stays available regardless — it's user-global.
describe('GeneralTab Workspace config-error surface', () => {
  beforeEach(() => {
    mocks.settings.config = null
    mocks.settings.loading = false
    mocks.settings.error = ''
  })
  afterEach(() => {
    mocks.settings.config = mocks.baseConfig
    mocks.settings.error = ''
    mocks.settings.loading = false
    cleanup()
  })

  it('shows the actual config error when config is absent but settings.error is set', () => {
    mocks.settings.error = 'yaml: did not find expected key at line 5'
    render(GeneralTab)
    const alert = screen.getByRole('alert')
    expect(alert.textContent).toContain(
      'yaml: did not find expected key at line 5'
    )
    // The generic no-workspace message is not shown.
    expect(screen.queryByText(/No workspace configuration loaded/i)).toBeNull()
  })

  it('keeps the Window section available when the workspace errors', () => {
    mocks.settings.error = 'config load failed'
    render(GeneralTab)
    // Close-to-tray is user-global — always renders, independent of config.
    expect(
      screen.getByRole('switch', { name: 'Close to tray' })
    ).toBeInTheDocument()
  })
})
