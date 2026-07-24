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
    const run = vi.fn()
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
        name: /date glance/i
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
})
