// Priority-pill cycling (#1124): copy (tooltip + aria current→next), the
// ghost state, and the click/keyboard containment in-card controls rely on.
// Pure component tests — no IPC (per AGENTS.md).
import { describe, expect, it, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

import PriorityPill from './PriorityPill.svelte'
import PriorityPillHost from './PriorityPillHost.stub.svelte'

describe('PriorityPill', () => {
  afterEach(cleanup)

  it('renders the current level and announces current → next', () => {
    render(PriorityPill, { priority: 3, onCycle: () => {} })
    const pill = screen.getByRole('button')
    expect(pill.textContent).toContain('Low')
    expect(pill.getAttribute('aria-label')).toBe(
      'Priority: Low — click to set Normal'
    )
    expect(pill.getAttribute('aria-label')).toContain('Normal')
  })

  it('renders a ghost pill when unprioritized, bootstrapping to Low', () => {
    render(PriorityPill, { priority: 0, onCycle: () => {} })
    const pill = screen.getByRole('button')
    expect(pill.textContent).toContain('None')
    // Ghost pills hide until the card is hovered or the pill focused.
    expect(pill.className).toContain('opacity-0')
    expect(pill.className).toContain('group-hover:opacity-100')
    expect(pill.getAttribute('aria-label')).toBe(
      'Priority: None — click to set Low'
    )
  })

  it('shows the tooltip on hover with current → next', async () => {
    render(PriorityPill, { priority: 3, onCycle: () => {} })
    const pill = screen.getByRole('button')
    await fireEvent.mouseEnter(pill)
    const tip = screen.getByRole('tooltip')
    expect(tip.textContent).toContain('Priority: Low — click to set Normal')
    expect(pill.getAttribute('aria-describedby')).toBe(tip.id)
    await fireEvent.mouseLeave(pill)
    expect(screen.queryByRole('tooltip')).toBeNull()
  })

  it('shows the tooltip on keyboard focus too', async () => {
    render(PriorityPill, { priority: 1, onCycle: () => {} })
    const pill = screen.getByRole('button')
    await fireEvent.focus(pill)
    expect(screen.getByRole('tooltip').textContent).toContain(
      'Priority: High — click to set Low'
    )
  })

  it('clicking cycles to the next level and does not bubble to the host', async () => {
    const onCycle = vi.fn()
    const onHostClick = vi.fn()
    render(PriorityPillHost, {
      priority: 2,
      onCycle,
      onHostClick,
      onHostKeydown: vi.fn()
    })

    await fireEvent.click(screen.getByTestId('priority-test'))

    expect(onCycle).toHaveBeenCalledWith(1)
    expect(onHostClick).not.toHaveBeenCalled()

    // Events aimed at the host prove the wrapper listeners are live.
    await fireEvent.click(screen.getByTestId('host-row'))
    expect(onHostClick).toHaveBeenCalledOnce()
  })

  it('Enter and Space cycle without letting the host treat them as selection', async () => {
    const onCycle = vi.fn()
    const onHostKeydown = vi.fn()
    render(PriorityPillHost, {
      priority: 3,
      onCycle,
      onHostClick: vi.fn(),
      onHostKeydown
    })
    const user = userEvent.setup()

    screen.getByTestId('priority-test').focus()
    await user.keyboard('{Enter}')
    await tick()
    await user.keyboard(' ')

    expect(onCycle).toHaveBeenCalledTimes(2)
    expect(onCycle).toHaveBeenNthCalledWith(1, 2)
    expect(onCycle).toHaveBeenNthCalledWith(2, 1)
    expect(onHostKeydown).not.toHaveBeenCalled()
  })

  it('flips the label optimistically and reverts when the write fails', async () => {
    let rejectWrite: (e: Error) => void = () => {}
    const onCycle = vi.fn(
      () =>
        new Promise<unknown>((_resolve, reject) => {
          rejectWrite = reject
        })
    )
    render(PriorityPill, { priority: 3, onCycle })
    const pill = screen.getByRole('button')

    await fireEvent.click(pill)
    await tick()
    // Optimistic flip to the next level while the write is pending.
    expect(pill.textContent).toContain('Normal')

    rejectWrite(new Error('write failed'))
    await vi.waitFor(() => expect(pill.textContent).toContain('Low'))
    expect(onCycle).toHaveBeenCalledWith(2)
  })
})
