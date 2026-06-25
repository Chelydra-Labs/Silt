import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

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
      mode: 'dark' as 'dark' | 'light' | 'system',
      darkTokens: {
        '--color-void': '#0c0c0e',
        '--font-body': "'Plus Jakarta Sans', sans-serif",
        '--font-mono': "'JetBrains Mono', monospace",
        '--font-headline': "'Hanken Grotesk', sans-serif"
      } as Record<string, string>,
      lightTokens: {} as Record<string, string>,
      error: null as string | null
    }
  }
})

vi.mock('../../../wailsjs/runtime/runtime.js', () => ({
  EventsOn: vi.fn(),
  EventsOff: vi.fn(),
  EventsEmit: vi.fn()
}))

const appMocks = vi.hoisted(() => ({
  PickVaultDestination: vi.fn(),
  MoveVault: vi.fn(),
  CopyVault: vi.fn(),
  SwitchVault: vi.fn(),
  PickVaultExportPath: vi.fn(),
  ExportVault: vi.fn(),
  PickVaultArchive: vi.fn(),
  ImportVault: vi.fn()
}))
vi.mock('../../../wailsjs/go/main/App.js', () => appMocks)
vi.mock('../../../wailsjs/runtime/runtime.js', () => ({
  EventsOn: vi.fn(() => () => {}),
  EventsOff: vi.fn(),
  EventsEmit: vi.fn()
}))

vi.mock('../../settings/store.svelte', () => ({
  settings: mocks.settings,
  saveConfig: mocks.saveConfig,
  reloadFromBackend: mocks.reloadFromBackend
}))
vi.mock('../../theme/store.svelte', () => ({ themeState: mocks.themeState }))

import WorkspaceTab from './WorkspaceTab.svelte'

describe('WorkspaceTab vault relocate menu (#141)', () => {
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
    render(WorkspaceTab)
    await tick()
    expect(
      screen.getByRole('button', { name: 'Vault actions' })
    ).toBeInTheDocument()
  })

  it('opening the menu reveals Move, Copy, Export, and Import actions', async () => {
    render(WorkspaceTab)
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
    render(WorkspaceTab)
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
    render(WorkspaceTab)
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
    render(WorkspaceTab)
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
    render(WorkspaceTab)
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
    render(WorkspaceTab)
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
    render(WorkspaceTab)
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
