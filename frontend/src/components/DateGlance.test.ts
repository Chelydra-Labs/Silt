import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, fireEvent } from '@testing-library/svelte'

// Mock copyText (uses navigator.clipboard) + pushNotification so the test
// never touches the real clipboard or toast DOM.
const copyTextMock = vi.hoisted(() => vi.fn().mockResolvedValue(true))
const pushNotificationMock = vi.hoisted(() => vi.fn())
vi.mock('../lib/pageActions', () => ({ copyText: copyTextMock }))
vi.mock('../notifications/store.svelte', () => ({
  pushNotification: pushNotificationMock
}))

import DateGlance from './DateGlance.svelte'
import {
  dateGlance,
  closeDateGlance,
  setDateGlanceAnchor
} from '../lib/dateGlanceState.svelte'

const anchor = document.createElement('div')
document.body.append(anchor)

beforeEach(() => {
  copyTextMock.mockClear()
  copyTextMock.mockResolvedValue(true)
  pushNotificationMock.mockClear()
  setDateGlanceAnchor(anchor)
  dateGlance.open = true
  dateGlance.insertEditor = null
})

afterEach(() => {
  cleanup()
  closeDateGlance()
  setDateGlanceAnchor(null)
})

async function clickFirstDayCell(): Promise<void> {
  const cells = await screen.findAllByRole('gridcell')
  expect(cells.length).toBeGreaterThan(0)
  await fireEvent.click(cells[0])
  // Flush the async pickDay promise (copy path awaits copyText).
  await new Promise((r) => setTimeout(r, 0))
}

describe('DateGlance', () => {
  it('inserts the date at the editor cursor when an insert target exists', async () => {
    const run = vi.fn(() => true)
    const insertContent = vi.fn(() => ({ run }))
    const focus = vi.fn(() => ({ insertContent, run }))
    const chain = vi.fn(() => ({ focus, insertContent, run }))
    dateGlance.insertEditor = {
      isDestroyed: false,
      chain
    } as never

    render(DateGlance)
    await clickFirstDayCell()

    expect(insertContent).toHaveBeenCalledTimes(1)
    expect(insertContent).toHaveBeenCalledWith(
      expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/)
    )
    expect(run).toHaveBeenCalledOnce()
    // Insert path does NOT copy to the clipboard.
    expect(copyTextMock).not.toHaveBeenCalled()
    // Popover closes after picking.
    expect(dateGlance.open).toBe(false)
  })

  it('copies the date to the clipboard + toasts when no editor is available', async () => {
    dateGlance.insertEditor = null

    render(DateGlance)
    await clickFirstDayCell()

    expect(copyTextMock).toHaveBeenCalledWith(
      expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/)
    )
    expect(pushNotificationMock).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: 'success',
        message: expect.stringMatching(/^Copied \d{4}-\d{2}-\d{2}$/)
      })
    )
    expect(dateGlance.open).toBe(false)
  })

  it('renders the month grid with navigation controls', async () => {
    render(DateGlance)
    expect(
      await screen.findByRole('dialog', {
        name: /pick a date/i
      })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Previous month' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Next month' })
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Today' })).toBeInTheDocument()
    expect((await screen.findAllByRole('gridcell')).length).toBeGreaterThan(0)
  })

  it('does not push a success toast when clipboard copy fails', async () => {
    dateGlance.insertEditor = null
    copyTextMock.mockResolvedValue(false)

    render(DateGlance)
    await clickFirstDayCell()

    expect(copyTextMock).toHaveBeenCalledOnce()
    // No success toast — copyText returned false (its own error toast path
    // handles the failure in the real module; here it's mocked out).
    expect(pushNotificationMock).not.toHaveBeenCalled()
    expect(dateGlance.open).toBe(false)
  })

  it('falls back to clipboard when the editor insert is rejected', async () => {
    const run = vi.fn(() => false)
    const insertContent = vi.fn(() => ({ run }))
    const focus = vi.fn(() => ({ insertContent, run }))
    const chain = vi.fn(() => ({ focus, insertContent, run }))
    dateGlance.insertEditor = { isDestroyed: false, chain } as never

    render(DateGlance)
    await clickFirstDayCell()

    // Insert was attempted then fell through to clipboard on rejection.
    expect(insertContent).toHaveBeenCalledOnce()
    expect(copyTextMock).toHaveBeenCalledOnce()
    expect(dateGlance.open).toBe(false)
  })

  it('falls back to clipboard when the editor insert throws', async () => {
    const run = vi.fn(() => {
      throw new Error('dispatch failed')
    })
    const insertContent = vi.fn(() => ({ run }))
    const focus = vi.fn(() => ({ insertContent, run }))
    const chain = vi.fn(() => ({ focus, insertContent, run }))
    dateGlance.insertEditor = { isDestroyed: false, chain } as never

    render(DateGlance)
    await clickFirstDayCell()

    expect(copyTextMock).toHaveBeenCalledOnce()
    expect(dateGlance.open).toBe(false)
  })

  it('advances keyboard focus to the next day with ArrowRight', async () => {
    render(DateGlance)
    await screen.findAllByRole('gridcell')
    // The auto-focus effect defers via tick().then()
    await new Promise((r) => setTimeout(r, 0))

    const focusedBefore = document.activeElement as HTMLElement
    expect(focusedBefore?.getAttribute('data-glance-date')).toBeTruthy()

    await fireEvent.keyDown(focusedBefore, { key: 'ArrowRight' })

    expect(document.activeElement).not.toBe(focusedBefore)
    expect(
      (document.activeElement as HTMLElement)?.getAttribute('data-glance-date')
    ).toBeTruthy()
  })

  it('retains keyboard focus within the grid after PageDown changes the month', async () => {
    render(DateGlance)
    await screen.findAllByRole('gridcell')
    await new Promise((r) => setTimeout(r, 0))

    const focusedBefore = document.activeElement as HTMLElement
    expect(focusedBefore?.getAttribute('data-glance-date')).toBeTruthy()

    await fireEvent.keyDown(focusedBefore, { key: 'PageDown' })
    // refocusCell() defers DOM focus via tick().then()
    await new Promise((r) => setTimeout(r, 10))

    // Focus is still on a gridcell — not stranded on document.body
    const focusedAfter = document.activeElement as HTMLElement
    expect(focusedAfter?.getAttribute('role')).toBe('gridcell')
    expect(focusedAfter?.getAttribute('data-glance-date')).toBeTruthy()
  })
})
