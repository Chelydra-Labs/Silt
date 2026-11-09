import { describe, expect, it, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

import PinToggle from './PinToggle.svelte'

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

describe('PinToggle', () => {
  afterEach(cleanup)

  it('announces the pinned state via aria-pressed and the dynamic label', () => {
    const onToggle = vi.fn().mockResolvedValue(true)

    render(PinToggle, { pinned: true, onToggle })
    const pinnedBtn = screen.getByRole('button', { name: 'Unpin task' })
    expect(pinnedBtn).toHaveAttribute('aria-pressed', 'true')
    expect(pinnedBtn).toHaveAttribute('title', 'Unpin task')
    expect(pinnedBtn.className).toContain('text-accent-primary-start')

    cleanup()

    render(PinToggle, { pinned: false, onToggle })
    const unpinnedBtn = screen.getByRole('button', { name: 'Pin task' })
    expect(unpinnedBtn).toHaveAttribute('aria-pressed', 'false')
    expect(unpinnedBtn).toHaveAttribute('title', 'Pin task')
    expect(unpinnedBtn.className).toContain('text-text-muted')
  })

  it('clicking commits the opposite state and flips the icon optimistically', async () => {
    const onToggle = vi.fn().mockResolvedValue(true)
    render(PinToggle, { pinned: false, onToggle, testId: 'pin-test' })

    await fireEvent.click(screen.getByTestId('pin-test'))

    expect(onToggle).toHaveBeenCalledWith(true)
    expect(screen.getByTestId('pin-test')).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    expect(screen.getByTestId('pin-test')).toHaveAccessibleName('Unpin task')
  })

  it('stops click and Enter/Space keydown propagation before a host row sees them', async () => {
    const onToggle = vi.fn().mockResolvedValue(true)
    render(PinToggle, { pinned: false, onToggle })
    const button = screen.getByRole('button', { name: 'Pin task' })

    const clickEvent = new MouseEvent('click', {
      bubbles: true,
      cancelable: true
    })
    const clickStop = vi.spyOn(clickEvent, 'stopPropagation')
    button.dispatchEvent(clickEvent)
    expect(clickStop).toHaveBeenCalledOnce()

    for (const key of ['Enter', ' ']) {
      const keyEvent = new KeyboardEvent('keydown', {
        key,
        bubbles: true,
        cancelable: true
      })
      const keyStop = vi.spyOn(keyEvent, 'stopPropagation')
      button.dispatchEvent(keyEvent)
      expect(keyStop).toHaveBeenCalledOnce()
    }
  })

  it('disables the button while the optimistic write is pending', async () => {
    let resolveWrite: (value: unknown) => void = () => {}
    const onToggle = vi.fn(
      () =>
        new Promise<unknown>((resolve) => {
          resolveWrite = resolve
        })
    )
    render(PinToggle, { pinned: false, onToggle })

    const button = screen.getByRole('button', { name: 'Pin task' })
    await fireEvent.click(button)
    expect(button).toBeDisabled()

    resolveWrite(true)
    await flush()
    expect(button).not.toBeDisabled()
  })

  it('reverts the icon and reports the friendly message when the write fails', async () => {
    const onError = vi.fn()
    const onToggle = vi.fn().mockRejectedValue(new Error('lock held'))
    render(PinToggle, { pinned: false, onToggle, onError })

    await fireEvent.click(screen.getByRole('button', { name: 'Pin task' }))
    await flush()

    expect(onError).toHaveBeenCalledWith('')
    expect(onError).toHaveBeenCalledWith('lock held')
    const button = screen.getByRole('button', { name: 'Pin task' })
    expect(button).toHaveAttribute('aria-pressed', 'false')
    expect(button).not.toBeDisabled()
  })
})
