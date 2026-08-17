// ConfirmDialog: open, confirm, cancel, Esc (#531).

import { describe, expect, it, afterEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'
import ConfirmDialog from './ConfirmDialog.svelte'

describe('ConfirmDialog', () => {
  afterEach(() => {
    cleanup()
  })

  it('renders title and message with data-testid', () => {
    render(ConfirmDialog, {
      props: {
        title: 'Delete item?',
        message: 'This cannot be undone.',
        dataTestId: 'confirm-test',
        onConfirm: vi.fn(),
        onCancel: vi.fn()
      }
    })
    expect(screen.getByTestId('confirm-test')).toBeTruthy()
    expect(screen.getByRole('dialog', { name: /delete item/i })).toBeTruthy()
    expect(screen.getByText('This cannot be undone.')).toBeTruthy()
  })

  it('calls onConfirm when confirm is clicked', async () => {
    const onConfirm = vi.fn()
    const onCancel = vi.fn()
    render(ConfirmDialog, {
      props: {
        title: 'Leave?',
        message: 'Unsaved changes.',
        confirmLabel: 'Leave',
        dataTestId: 'leave',
        onConfirm,
        onCancel
      }
    })
    await fireEvent.click(screen.getByTestId('leave-confirm'))
    expect(onConfirm).toHaveBeenCalledTimes(1)
    expect(onCancel).not.toHaveBeenCalled()
  })

  it('calls onCancel when cancel is clicked', async () => {
    const onConfirm = vi.fn()
    const onCancel = vi.fn()
    render(ConfirmDialog, {
      props: {
        title: 'Leave?',
        message: 'Unsaved changes.',
        dataTestId: 'leave',
        onConfirm,
        onCancel
      }
    })
    await fireEvent.click(screen.getByTestId('leave-cancel'))
    expect(onCancel).toHaveBeenCalledTimes(1)
    expect(onConfirm).not.toHaveBeenCalled()
  })

  it('disables actions and ignores Escape while busy', async () => {
    const onConfirm = vi.fn()
    const onCancel = vi.fn()
    render(ConfirmDialog, {
      props: {
        title: 'Restore?',
        message: 'Working…',
        confirmLabel: 'Restoring…',
        busy: true,
        dataTestId: 'busy',
        onConfirm,
        onCancel
      }
    })
    expect(screen.getByTestId('busy-confirm')).toBeDisabled()
    expect(screen.getByTestId('busy-cancel')).toBeDisabled()
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onCancel).not.toHaveBeenCalled()
    await fireEvent.click(screen.getByTestId('busy-confirm'))
    expect(onConfirm).not.toHaveBeenCalled()
  })

  it('calls onCancel on Escape', async () => {
    const onCancel = vi.fn()
    render(ConfirmDialog, {
      props: {
        title: 'Leave?',
        message: 'Unsaved changes.',
        onConfirm: vi.fn(),
        onCancel
      }
    })
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onCancel).toHaveBeenCalledTimes(1)
  })
})
