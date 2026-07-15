import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import { tick } from 'svelte'

// Mock flipOrClamp so positioning assertions don't depend on real viewport
// math (same pattern as ContextMenu.test.ts). vi.hoisted guarantees the mock
// exists when vi.mock's factory runs.
const mocks = vi.hoisted(() => ({
  flipOrClamp: vi.fn().mockReturnValue({ left: 42, top: 24 })
}))

vi.mock('../../lib/editor/popoverPositioning', () => ({
  flipOrClamp: mocks.flipOrClamp,
  POPOVER_MARGIN: 8
}))

import TableSizePicker from './TableSizePicker.svelte'

const flush = () => new Promise((r) => setTimeout(r, 0))

describe('TableSizePicker', () => {
  beforeEach(() => {
    mocks.flipOrClamp.mockClear()
    mocks.flipOrClamp.mockReturnValue({ left: 42, top: 24 })
    Object.defineProperty(window, 'innerWidth', { value: 1280, writable: true })
    Object.defineProperty(window, 'innerHeight', {
      value: 800,
      writable: true
    })
  })

  const anchor = { top: 600, bottom: 620, left: 300 }
  const baseProps = () => ({
    anchor,
    onConfirm: vi.fn(),
    onCancel: vi.fn()
  })

  it('positions via flipOrClamp with the anchor rect', async () => {
    render(TableSizePicker, { props: baseProps() })
    await tick()
    expect(mocks.flipOrClamp).toHaveBeenCalled()
    const call = mocks.flipOrClamp.mock.calls[0][0]
    expect(call).toEqual(anchor)
  })

  it('moves focus into the dialog on mount', async () => {
    const { container } = render(TableSizePicker, { props: baseProps() })
    await flush()
    const grid = container.querySelector('[role="grid"]') as HTMLElement
    expect(grid).toBeTruthy()
    expect(document.activeElement).toBe(grid)
  })

  it('cancels on Escape regardless of focus location', async () => {
    const props = baseProps()
    render(TableSizePicker, { props })
    await flush()
    // Escape dispatched on the document (not from inside an input).
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(props.onCancel).toHaveBeenCalledTimes(1)
  })

  it('cancels on click outside the picker', async () => {
    const props = baseProps()
    render(TableSizePicker, { props })
    await flush()
    // A click on the body (outside the popover) cancels.
    fireEvent.click(document.body)
    expect(props.onCancel).toHaveBeenCalledTimes(1)
  })

  it('does NOT cancel when clicking inside the picker', async () => {
    const props = baseProps()
    const { container } = render(TableSizePicker, { props })
    await flush()
    const cell = container.querySelector('.tsp-cell') as HTMLElement
    cell.click()
    expect(props.onCancel).not.toHaveBeenCalled()
  })

  it('shows the allowed dimension range (1–20)', async () => {
    const { container } = render(TableSizePicker, { props: baseProps() })
    await tick()
    expect(container.textContent ?? '').toContain('1–20')
  })

  it('visibly corrects out-of-range input (non-silent clamp)', async () => {
    const { container } = render(TableSizePicker, { props: baseProps() })
    await flush()
    const rowsInput = container.querySelector(
      'input[aria-label="Rows"]'
    ) as HTMLInputElement
    fireEvent.input(rowsInput, { target: { value: '50' } })
    fireEvent.blur(rowsInput)
    await flush()
    expect(container.textContent ?? '').toContain('Adjusted rows to 20')
    expect(rowsInput.value).toBe('20')
  })

  // Grid highlight must track numeric entry beyond the 8×8 grid cap, not just
  // grid clicks/arrows. Typing 12 in Columns lights the full 8-wide extent.
  it('keeps the grid highlight in sync with numeric entry beyond the grid cap', async () => {
    const { container } = render(TableSizePicker, { props: baseProps() })
    await flush()
    const colsInput = container.querySelector(
      'input[aria-label="Columns"]'
    ) as HTMLInputElement
    fireEvent.input(colsInput, { target: { value: '12' } })
    await tick()
    // gridC derives to clampGrid(12) = 8; default rows=3 → 3 × 8 = 24 filled.
    const filled = container.querySelectorAll('.tsp-cell-filled')
    expect(filled.length).toBe(24)
  })

  it('committing a grid cell updates the dimensions and the live preview', async () => {
    const { container } = render(TableSizePicker, { props: baseProps() })
    await flush()
    // Cell at row 5, col 4 (0-indexed: (5-1)*8 + (4-1) = 35).
    const cells = container.querySelectorAll('.tsp-cell')
    ;(cells[35] as HTMLElement).click()
    await tick()
    expect(container.textContent ?? '').toContain('5 rows × 4 columns')
  })

  it('Insert calls onConfirm with the current dimensions', async () => {
    const props = baseProps()
    const { container } = render(TableSizePicker, { props })
    await flush()
    const insert = container.querySelector('.tsp-insert') as HTMLButtonElement
    insert.click()
    expect(props.onConfirm).toHaveBeenCalledWith(3, 3)
  })
})
