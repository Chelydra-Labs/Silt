import { describe, expect, it, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

import PinToggle from './PinToggle.svelte'
import PinToggleHost from './PinToggleHost.stub.svelte'

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

  it('adds a host-supplied class without dropping its own classes', () => {
    render(PinToggle, {
      pinned: false,
      onToggle: vi.fn(),
      class: 'absolute top-2 right-2'
    })

    const button = screen.getByRole('button', { name: 'Pin task' })
    expect(button.className).toContain('pin-button')
    expect(button.className).toContain('absolute top-2 right-2')
  })

  it('re-syncs to an external pinned prop change while idle', async () => {
    const onToggle = vi.fn().mockResolvedValue(true)
    const { rerender } = render(PinToggle, { pinned: false, onToggle })
    expect(screen.getByRole('button', { name: 'Pin task' })).toHaveAttribute(
      'aria-pressed',
      'false'
    )

    await rerender({ pinned: true, onToggle })

    const button = screen.getByRole('button', { name: 'Unpin task' })
    expect(button).toHaveAttribute('aria-pressed', 'true')
  })

  it('keeps the optimistic value when the prop changes mid-write', async () => {
    let resolveWrite: (value: unknown) => void = () => {}
    const onToggle = vi.fn(
      () =>
        new Promise<unknown>((resolve) => {
          resolveWrite = resolve
        })
    )
    const { rerender } = render(PinToggle, {
      pinned: false,
      onToggle,
      testId: 'pin-test'
    })
    const button = screen.getByTestId('pin-test')
    await fireEvent.click(button)
    expect(button).toHaveAttribute('aria-pressed', 'true')

    // A reload landing mid-write leaves the prop at false (its pre-click
    // value); the pending guard must keep the optimistic true on screen.
    await rerender({ pinned: false, onToggle })
    await tick()
    expect(button).toHaveAttribute('aria-pressed', 'true')

    resolveWrite(true)
    await flush()
    expect(button).toHaveAttribute('aria-pressed', 'true')
  })

  it('stops click and Enter/Space keydown propagation before a host row sees them', async () => {
    const onToggle = vi.fn().mockResolvedValue(true)
    const onHostClick = vi.fn()
    const onHostKeydown = vi.fn()
    render(PinToggleHost, { onToggle, onHostClick, onHostKeydown })

    const button = screen.getByTestId('pin-test')
    await fireEvent.click(button)
    expect(onHostClick).not.toHaveBeenCalled()

    for (const key of ['Enter', ' ']) {
      await fireEvent.keyDown(button, { key })
    }
    expect(onHostKeydown).not.toHaveBeenCalled()

    // Events aimed at the host prove the wrapper listeners are live.
    await fireEvent.click(screen.getByTestId('host-row'))
    await fireEvent.keyDown(screen.getByTestId('host-row'), { key: 'a' })
    expect(onHostClick).toHaveBeenCalledOnce()
    expect(onHostKeydown).toHaveBeenCalledOnce()
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
