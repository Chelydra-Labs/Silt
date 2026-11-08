import { createRawSnippet, tick } from 'svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import Button from './Button.svelte'
import Dialog from './Dialog.svelte'
import IconButton from './IconButton.svelte'
import Input from './Input.svelte'

function textSnippet(text: string) {
  return createRawSnippet(() => ({
    render: () => `<span>${text}</span>`
  }))
}

describe('Button primitive', () => {
  afterEach(() => {
    cleanup()
  })

  it('renders variants and sizes using named token classes', () => {
    const { rerender } = render(Button, {
      props: {
        variant: 'primary',
        size: 'md',
        dataTestId: 'btn',
        children: textSnippet('Save')
      }
    })

    const btn = screen.getByTestId('btn')
    expect(btn).toHaveTextContent('Save')
    expect(btn.className).toContain('bg-accent-primary-start/20')
    expect(btn.className).toContain('px-4 py-2 text-type-sm rounded-lg')

    rerender({
      variant: 'danger',
      size: 'sm',
      dataTestId: 'btn',
      children: textSnippet('Delete')
    })
    expect(btn.className).toContain('bg-status-danger/20')
    expect(btn.className).toContain('text-status-danger')
    expect(btn.className).toContain('px-2.5 py-1.5 text-type-xs rounded-md')

    rerender({
      variant: 'ghost',
      size: 'md',
      dataTestId: 'btn',
      children: textSnippet('Cancel')
    })
    expect(btn.className).toContain('bg-transparent')
    expect(btn.className).toContain('text-text-muted')
  })

  it('invokes onclick when enabled and suppresses click when disabled', async () => {
    const onclick = vi.fn()
    const { rerender } = render(Button, {
      props: {
        dataTestId: 'action-btn',
        onclick,
        children: textSnippet('Run')
      }
    })

    const btn = screen.getByTestId('action-btn') as HTMLButtonElement
    await fireEvent.click(btn)
    expect(onclick).toHaveBeenCalledTimes(1)

    rerender({
      dataTestId: 'action-btn',
      disabled: true,
      onclick,
      children: textSnippet('Run')
    })
    expect(btn.disabled).toBe(true)
  })
})

describe('IconButton primitive', () => {
  afterEach(() => {
    cleanup()
  })

  it('renders accessible aria-label, title fallback, Material Symbols glyph, and active state', () => {
    const { rerender } = render(IconButton, {
      props: {
        ariaLabel: 'Toggle sidebar',
        icon: 'dock_to_right',
        size: 'sm',
        dataTestId: 'icon-btn'
      }
    })

    const btn = screen.getByTestId('icon-btn')
    expect(btn).toHaveAttribute('aria-label', 'Toggle sidebar')
    expect(btn).toHaveAttribute('title', 'Toggle sidebar')
    expect(btn).not.toHaveAttribute('aria-pressed')
    expect(btn.className).toContain('w-7 h-7 rounded-md')

    const glyph = btn.querySelector('.material-symbols-outlined')
    expect(glyph).toBeTruthy()
    expect(glyph).toHaveTextContent('dock_to_right')
    expect(glyph?.className).toContain('text-icon-md')

    rerender({
      ariaLabel: 'Toggle sidebar',
      icon: 'dock_to_right',
      size: 'md',
      active: true,
      dataTestId: 'icon-btn'
    })
    expect(btn).toHaveAttribute('aria-pressed', 'true')
    expect(btn.className).toContain('bg-active')
    expect(btn.className).toContain('w-8 h-8 rounded-lg')
  })
})

describe('Input primitive', () => {
  afterEach(() => {
    cleanup()
  })

  it('wires aria-invalid and aria-describedby to the rendered inline error message', async () => {
    const oninput = vi.fn()
    const { rerender } = render(Input, {
      props: {
        id: 'font-size',
        value: '14',
        describedBy: 'font-size-hint',
        dataTestId: 'font-input',
        oninput
      }
    })

    const input = screen.getByTestId('font-input') as HTMLInputElement
    expect(input.value).toBe('14')
    expect(input).not.toHaveAttribute('aria-invalid')
    expect(input).toHaveAttribute('aria-describedby', 'font-size-hint')

    await fireEvent.input(input, { target: { value: '99' } })
    expect(oninput).toHaveBeenCalledTimes(1)

    rerender({
      id: 'font-size',
      value: '99',
      describedBy: 'font-size-hint',
      error: 'Must be between 10 and 32',
      dataTestId: 'font-input',
      oninput
    })

    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(input).toHaveAttribute(
      'aria-describedby',
      'font-size-hint font-size-error'
    )
    const errorEl = document.getElementById('font-size-error')
    expect(errorEl).toBeTruthy()
    expect(errorEl).toHaveTextContent('Must be between 10 and 32')
    expect(errorEl).toHaveAttribute('role', 'alert')
  })
})

describe('Dialog primitive', () => {
  afterEach(() => {
    cleanup()
  })

  it('traps Tab/Shift+Tab focus within the dialog surface and closes on Escape/backdrop', async () => {
    const onClose = vi.fn()
    const bodySnippet = createRawSnippet(() => ({
      render: () => `
        <div>
          <button type="button" data-testid="first-btn">First</button>
          <button type="button" data-testid="second-btn">Second</button>
        </div>
      `
    }))

    render(Dialog, {
      props: {
        ariaLabel: 'Sample dialog',
        backdropLabel: 'Dismiss modal',
        dataTestId: 'sample-dialog',
        onClose,
        children: bodySnippet
      }
    })

    await tick()
    await Promise.resolve()

    const dialog = screen.getByTestId('sample-dialog')
    const firstBtn = screen.getByTestId('first-btn')
    const secondBtn = screen.getByTestId('second-btn')

    expect(document.activeElement).toBe(dialog)

    // Tab from dialog container (or last element) wraps to first focusable
    secondBtn.focus()
    expect(document.activeElement).toBe(secondBtn)
    await fireEvent.keyDown(window, { key: 'Tab' })
    expect(document.activeElement).toBe(firstBtn)

    // Shift+Tab from first element wraps to last focusable
    await fireEvent.keyDown(window, { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(secondBtn)

    // Escape triggers onClose
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)

    // Backdrop button click triggers onClose
    const backdropBtn = screen.getByLabelText('Dismiss modal')
    await fireEvent.click(backdropBtn)
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  it('suppresses Escape and backdrop dismissal while busy', async () => {
    const onClose = vi.fn()
    render(Dialog, {
      props: {
        ariaLabel: 'Busy dialog',
        backdropLabel: 'Close busy dialog',
        busy: true,
        dataTestId: 'busy-dialog',
        onClose
      }
    })

    await tick()
    const dialog = screen.getByTestId('busy-dialog')
    expect(dialog).toHaveAttribute('aria-busy', 'true')

    await fireEvent.keyDown(window, { key: 'Escape' })
    await fireEvent.click(screen.getByLabelText('Close busy dialog'))
    expect(onClose).not.toHaveBeenCalled()
  })

  it('restores focus to the opener element when unmounted', async () => {
    const opener = document.createElement('button')
    opener.textContent = 'Open Modal'
    document.body.appendChild(opener)
    opener.focus()
    expect(document.activeElement).toBe(opener)

    const { unmount } = render(Dialog, {
      props: {
        ariaLabel: 'Focus return dialog',
        dataTestId: 'restore-dialog'
      }
    })

    await tick()
    await Promise.resolve()

    expect(document.activeElement).toBe(screen.getByTestId('restore-dialog'))

    unmount()
    expect(document.activeElement).toBe(opener)
    opener.remove()
  })
})
