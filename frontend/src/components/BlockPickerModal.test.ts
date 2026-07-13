import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'

const mocks = vi.hoisted(() => ({
  SearchBlocks: vi.fn()
}))

vi.mock('../../bindings/silt/app.js', () => ({
  SearchBlocks: mocks.SearchBlocks
}))

import BlockPickerModal from './BlockPickerModal.svelte'

describe('BlockPickerModal focus trap', () => {
  beforeEach(() => {
    mocks.SearchBlocks.mockReset()
    mocks.SearchBlocks.mockResolvedValue([])
  })
  afterEach(() => cleanup())

  it('traps Tab from the last control back to the first', async () => {
    mocks.SearchBlocks.mockResolvedValue([
      {
        id: 'b1',
        notebook: 'NB',
        section: 'S',
        page: 'P',
        clean_content: 'hello block'
      }
    ])

    render(BlockPickerModal, {
      props: { onPick: vi.fn(), onClose: vi.fn() }
    })

    const input = screen.getByPlaceholderText(/Search blocks to embed/i)
    await fireEvent.input(input, { target: { value: 'hello' } })
    await waitFor(() => {
      expect(screen.getByText('hello block')).toBeTruthy()
    })

    // Focus the last focusable control (result button) and Tab — should wrap
    // to the first (search input), not escape the dialog.
    const resultBtn = screen.getByText('hello block').closest('button')!
    resultBtn.focus()
    expect(document.activeElement).toBe(resultBtn)

    const tabEvent = new KeyboardEvent('keydown', {
      key: 'Tab',
      bubbles: true,
      cancelable: true
    })
    window.dispatchEvent(tabEvent)

    expect(document.activeElement).toBe(input)
  })

  it('restores focus to the previously focused element on close when still connected', async () => {
    const trigger = document.createElement('button')
    trigger.textContent = 'open picker'
    document.body.appendChild(trigger)
    trigger.focus()

    const onClose = vi.fn()
    const { unmount } = render(BlockPickerModal, {
      props: { onPick: vi.fn(), onClose }
    })

    // Modal steals focus to the search input on mount.
    const input = screen.getByPlaceholderText(/Search blocks to embed/i)
    expect(document.activeElement).toBe(input)

    unmount()
    expect(document.activeElement).toBe(trigger)

    trigger.remove()
  })

  it('Escape closes the picker', async () => {
    const onClose = vi.fn()
    render(BlockPickerModal, {
      props: { onPick: vi.fn(), onClose }
    })

    window.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'Escape',
        bubbles: true,
        cancelable: true
      })
    )
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
