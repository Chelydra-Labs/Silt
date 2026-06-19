// Component coverage for the vault relocation modal (#141). The IPC layer is
// mocked via vi.hoisted + vi.mock over the wailsjs binding module (the
// canonical pattern — see AppearanceTab.test.ts). No real IPC in a test.

import { describe, expect, it, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'
import VaultActionModal from './VaultActionModal.svelte'

const mocks = vi.hoisted(() => ({
  PickVaultDestination: vi.fn(),
  MoveVault: vi.fn(),
  CopyVault: vi.fn(),
  SwitchVault: vi.fn()
}))

vi.mock('../../../wailsjs/go/main/App.js', () => mocks)
vi.mock('../../../wailsjs/runtime/runtime.js', () => ({
  EventsOn: vi.fn(),
  EventsOff: vi.fn(),
  EventsEmit: vi.fn()
}))

describe('VaultActionModal', () => {
  afterEach(() => {
    cleanup()
    vi.clearAllMocks()
  })

  it('move mode: renders a dialog with the primary action disabled until a destination is chosen', async () => {
    render(VaultActionModal, {
      mode: 'move',
      currentPath: '/old/vault',
      onClose: () => {}
    })
    await tick()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    const primary = screen.getByRole('button', { name: 'Move vault' })
    expect(primary).toBeDisabled()
  })

  it('move mode: Cancel invokes onClose', async () => {
    const onClose = vi.fn()
    render(VaultActionModal, {
      mode: 'move',
      currentPath: '/old/vault',
      onClose
    })
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('move mode: choosing a destination then committing calls MoveVault and closes', async () => {
    mocks.PickVaultDestination.mockResolvedValue('/new/vault')
    mocks.MoveVault.mockResolvedValue({})
    const onClose = vi.fn()
    render(VaultActionModal, {
      mode: 'move',
      currentPath: '/old/vault',
      onClose
    })
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Choose…' }))
    await tick()
    // Destination set → primary enabled.
    const primary = await screen.findByRole('button', { name: 'Move vault' })
    await waitFor(() => expect(primary).not.toBeDisabled())
    await fireEvent.click(primary)
    await waitFor(() =>
      expect(mocks.MoveVault).toHaveBeenCalledWith('/new/vault', false)
    )
    expect(onClose).toHaveBeenCalled()
  })

  it('move mode: delete-original requires the nested confirmation', async () => {
    mocks.PickVaultDestination.mockResolvedValue('/new/vault')
    mocks.MoveVault.mockResolvedValue({})
    render(VaultActionModal, {
      mode: 'move',
      currentPath: '/old/vault',
      onClose: () => {}
    })
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Choose…' }))
    await tick()
    // Toggle "delete original" ON.
    const deleteCheckbox = screen.getByRole('checkbox', {
      name: /Delete the original vault/
    })
    await fireEvent.click(deleteCheckbox)
    await tick()
    // Primary stays disabled until the nested confirm is checked.
    const primary = screen.getByRole('button', { name: 'Move vault' })
    expect(primary).toBeDisabled()
    // Confirm.
    const confirmCheckbox = screen.getByRole('checkbox', {
      name: /permanently deleted/
    })
    await fireEvent.click(confirmCheckbox)
    await tick()
    await waitFor(() => expect(primary).not.toBeDisabled())
  })

  it('copy mode: success shows the Switch affordance and switching calls SwitchVault', async () => {
    mocks.PickVaultDestination.mockResolvedValue('/copy/of/vault')
    mocks.CopyVault.mockResolvedValue({
      files_copied: 7,
      bytes_copied: 2048,
      skipped_index: true
    })
    mocks.SwitchVault.mockResolvedValue(undefined)
    const onClose = vi.fn()
    render(VaultActionModal, {
      mode: 'copy',
      currentPath: '/old/vault',
      onClose
    })
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Choose…' }))
    await tick()
    const primary = await screen.findByRole('button', { name: 'Copy vault' })
    await waitFor(() => expect(primary).not.toBeDisabled())
    await fireEvent.click(primary)
    // Success state renders a status region + the Switch affordance.
    await waitFor(() => expect(screen.getByRole('status')).toBeInTheDocument())
    expect(screen.getByText(/Copied 7 files/)).toBeInTheDocument()
    await fireEvent.click(
      screen.getByRole('button', { name: 'Switch to this vault' })
    )
    await waitFor(() =>
      expect(mocks.SwitchVault).toHaveBeenCalledWith('/copy/of/vault')
    )
    expect(onClose).toHaveBeenCalled()
  })

  it('renders an accessible alert when the operation errors', async () => {
    mocks.PickVaultDestination.mockResolvedValue('/bad/dest')
    mocks.CopyVault.mockRejectedValue(new Error('already a Silt vault'))
    render(VaultActionModal, {
      mode: 'copy',
      currentPath: '/old/vault',
      onClose: () => {}
    })
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Choose…' }))
    await tick()
    const primary = await screen.findByRole('button', { name: 'Copy vault' })
    await waitFor(() => expect(primary).not.toBeDisabled())
    await fireEvent.click(primary)
    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())
    expect(screen.getByRole('alert').textContent).toContain(
      'already a Silt vault'
    )
  })
})
