// NamePromptDialog: open, validate, confirm, cancel (#531).

import { describe, expect, it, afterEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'
import NamePromptDialog from './NamePromptDialog.svelte'

describe('NamePromptDialog', () => {
  afterEach(() => {
    cleanup()
  })

  it('renders with initial value and data-testid', () => {
    render(NamePromptDialog, {
      props: {
        title: 'Rename theme',
        initialValue: 'My Theme',
        dataTestId: 'name-test',
        onConfirm: vi.fn(),
        onCancel: vi.fn()
      }
    })
    expect(screen.getByTestId('name-test')).toBeTruthy()
    const input = screen.getByTestId('name-test-input') as HTMLInputElement
    expect(input.value).toBe('My Theme')
  })

  it('confirms trimmed name', async () => {
    const onConfirm = vi.fn()
    render(NamePromptDialog, {
      props: {
        title: 'Save as',
        initialValue: 'Draft',
        dataTestId: 'save-name',
        onConfirm,
        onCancel: vi.fn()
      }
    })
    const input = screen.getByTestId('save-name-input') as HTMLInputElement
    await fireEvent.input(input, { target: { value: '  Custom Theme  ' } })
    await fireEvent.click(screen.getByTestId('save-name-confirm'))
    expect(onConfirm).toHaveBeenCalledWith('Custom Theme')
  })

  it('rejects empty name without calling onConfirm', async () => {
    const onConfirm = vi.fn()
    render(NamePromptDialog, {
      props: {
        title: 'Save as',
        initialValue: '',
        dataTestId: 'save-name',
        onConfirm,
        onCancel: vi.fn()
      }
    })
    await fireEvent.click(screen.getByTestId('save-name-confirm'))
    expect(onConfirm).not.toHaveBeenCalled()
    expect(screen.getByText(/name is required/i)).toBeTruthy()
  })

  it('calls onCancel on Escape', async () => {
    const onCancel = vi.fn()
    render(NamePromptDialog, {
      props: {
        title: 'Rename',
        initialValue: 'x',
        onConfirm: vi.fn(),
        onCancel
      }
    })
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('confirms on Enter in the name field', async () => {
    const onConfirm = vi.fn()
    render(NamePromptDialog, {
      props: {
        title: 'Save as',
        initialValue: 'Draft',
        dataTestId: 'save-name',
        onConfirm,
        onCancel: vi.fn()
      }
    })
    const input = screen.getByTestId('save-name-input') as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'Enter Theme' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    expect(onConfirm).toHaveBeenCalledWith('Enter Theme')
  })
})
