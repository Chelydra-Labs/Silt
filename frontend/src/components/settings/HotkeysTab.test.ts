import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'

const mockSettings = vi.hoisted(() => ({
  config: { hotkeys: { open_search: 'Ctrl+Shift+F' } } as {
    hotkeys: Record<string, string>
  },
  dirty: false,
  pendingExternal: false,
  error: '',
  saving: false
}))
vi.mock('../../settings/store.svelte', () => ({
  settings: mockSettings,
  saveConfig: vi.fn().mockResolvedValue(true),
  reloadFromBackend: vi.fn().mockResolvedValue(undefined)
}))

import HotkeysTab from './HotkeysTab.svelte'

afterEach(cleanup)
beforeEach(() => {
  mockSettings.config = { hotkeys: { open_search: 'Ctrl+Shift+F' } }
  mockSettings.dirty = false
})

describe('HotkeysTab new global actions', () => {
  it('shows generic remap controls even before backend defaults are present', () => {
    render(HotkeysTab)
    expect(screen.getByLabelText('New page')).toHaveValue('Ctrl+N')
    expect(screen.getByLabelText('New section')).toHaveValue('Ctrl+Alt+N')
    expect(screen.getByLabelText('New notebook')).toHaveValue(
      'Ctrl+Alt+Shift+N'
    )
    expect(screen.getByLabelText('Switch page')).toHaveValue('Ctrl+P')
    expect(screen.getByLabelText('Keyboard shortcuts')).toHaveValue('Shift+?')
  })

  it('identifies conflicting bindings and blocks save', () => {
    mockSettings.config = {
      hotkeys: { new_page: 'Alt+N', open_quick_switcher: 'Alt+N' }
    }
    render(HotkeysTab)
    expect(screen.getByText('Conflicts with Switch page.')).toBeInTheDocument()
    expect(screen.getByText('Conflicts with New page.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save changes' })).toBeDisabled()
  })

  it('does not flag an editor/hub chord overlap as a conflict (scope-aware)', async () => {
    // The defaults ship format_link=Ctrl+K (editor-scoped) and
    // tasks_command_palette=Ctrl+K (hub-scoped). A scope-blind grid would flag
    // these as conflicting and permanently block Save; the fix classifies them
    // by resolver scope so the two focus contexts coexist.
    mockSettings.config = {
      hotkeys: {
        format_link: 'Ctrl+K',
        tasks_command_palette: 'Ctrl+K',
        new_page: 'Ctrl+N'
      }
    }
    render(HotkeysTab)
    expect(screen.queryByText(/Conflicts with/)).not.toBeInTheDocument()
    // An unrelated edit flips changed() true; Save must then be enabled —
    // proving the editor/hub scope overlap no longer blocks the button. A
    // scope-blind grid would keep it disabled via conflicts.size > 0.
    const newPage = screen.getByLabelText('New page')
    await fireEvent.click(newPage)
    await fireEvent.keyDown(newPage, { key: 'm', ctrlKey: true })
    expect(screen.getByRole('button', { name: 'Save changes' })).toBeEnabled()
  })
})
