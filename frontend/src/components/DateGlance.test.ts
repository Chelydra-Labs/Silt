import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, fireEvent } from '@testing-library/svelte'

// Mock copyText (uses navigator.clipboard) + pushNotification so the test
// never touches the real clipboard or toast DOM.
const copyTextMock = vi.hoisted(() => vi.fn().mockResolvedValue(true))
const pushNotificationMock = vi.hoisted(() => vi.fn())
const dateGlanceSettingsMock = vi.hoisted(() => ({
  config: {
    editor: {}
  }
}))
vi.mock('../lib/pageActions', () => ({ copyText: copyTextMock }))
vi.mock('../notifications/store.svelte', () => ({
  pushNotification: pushNotificationMock
}))
vi.mock('../settings/store.svelte', () => ({
  settings: dateGlanceSettingsMock
}))

import DateGlance from './DateGlance.svelte'
import {
  dateGlance,
  closeDateGlance,
  openDateGlance,
  setDateGlanceAnchor
} from '../lib/dateGlanceState.svelte'
import { POPOVER_MARGIN } from '../lib/editor/popoverPositioning'
import {
  resetTaskWeekStart,
  setTaskWeekStart
} from '../lib/taskWeekStart.svelte'

const anchor = document.createElement('div')
document.body.append(anchor)

function mockAnchorRect(
  el: HTMLElement,
  rect: {
    left: number
    top: number
    bottom: number
    width?: number
    height?: number
  }
): void {
  const width = rect.width ?? 32
  const height = rect.height ?? rect.bottom - rect.top
  el.getBoundingClientRect = () =>
    ({
      top: rect.top,
      bottom: rect.bottom,
      left: rect.left,
      right: rect.left + width,
      width,
      height,
      x: rect.left,
      y: rect.top,
      toJSON: () => ({})
    }) as DOMRect
}

function floatingLayer(): HTMLElement {
  const dialog = document.querySelector(
    '[role="dialog"][aria-label="Pick a date to insert or copy"]'
  ) as HTMLElement | null
  expect(dialog).not.toBeNull()
  // Popover structure: portal > backdrop + floating layer > content
  const layer = dialog!.parentElement as HTMLElement
  expect(layer).toBeTruthy()
  return layer
}

beforeEach(() => {
  copyTextMock.mockClear()
  copyTextMock.mockResolvedValue(true)
  pushNotificationMock.mockClear()
  setDateGlanceAnchor(anchor)
  // Resolve activeAnchor via the real open path (no body fallback).
  openDateGlance()
  dateGlance.insertEditor = null
  setTaskWeekStart('sunday')
})

afterEach(() => {
  cleanup()
  closeDateGlance()
  setDateGlanceAnchor(null)
  resetTaskWeekStart()
  document
    .querySelectorAll('[data-date-glance-placement]')
    .forEach((n) => n.remove())
})

async function clickFirstDayCell(): Promise<void> {
  const cells = await screen.findAllByRole('gridcell')
  expect(cells.length).toBeGreaterThan(0)
  await fireEvent.click(cells[0])
  // Flush the async pickDay promise (copy path awaits copyText).
  await new Promise((r) => setTimeout(r, 0))
}

describe('DateGlance', () => {
  it('uses the active Tasks Monday week start for its shared month grid', async () => {
    setTaskWeekStart('monday')
    render(DateGlance)
    const headers = screen
      .getByRole('dialog')
      .querySelectorAll('[aria-hidden="true"] span')
    expect(headers[0]?.textContent).toBe('M')
  })

  it('updates its grid when the active Tasks preference changes', async () => {
    render(DateGlance)
    expect(
      screen.getByRole('dialog').querySelector('[aria-hidden="true"] span')
        ?.textContent
    ).toBe('S')

    setTaskWeekStart('monday')
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(
      screen.getByRole('dialog').querySelector('[aria-hidden="true"] span')
        ?.textContent
    ).toBe('M')
  })

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

  it('shows the month picker when the month label is clicked', async () => {
    render(DateGlance)
    const monthName = new Date().toLocaleDateString(undefined, {
      month: 'long'
    })
    await fireEvent.click(screen.getByRole('button', { name: monthName }))

    expect(screen.getByRole('button', { name: 'Jan' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Dec' })).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Previous year' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Next year' })
    ).toBeInTheDocument()
  })

  it('returns to the day grid after picking a month', async () => {
    render(DateGlance)
    const monthName = new Date().toLocaleDateString(undefined, {
      month: 'long'
    })
    await fireEvent.click(screen.getByRole('button', { name: monthName }))
    await fireEvent.click(screen.getByRole('button', { name: 'Jul' }))

    expect((await screen.findAllByRole('gridcell')).length).toBeGreaterThan(0)
    expect(
      screen.getByRole('button', { name: 'Previous month' })
    ).toBeInTheDocument()
  })

  it('shows the year picker when the year label is clicked', async () => {
    render(DateGlance)
    const yearLabel = String(new Date().getFullYear())
    await fireEvent.click(screen.getByRole('button', { name: yearLabel }))

    expect(screen.getByRole('button', { name: yearLabel })).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Previous year range' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Next year range' })
    ).toBeInTheDocument()
  })

  it('does not render the popover when opened without a placeable anchor', async () => {
    closeDateGlance()
    setDateGlanceAnchor(null)
    const err = vi.spyOn(console, 'error').mockImplementation(() => {})
    // Force the broken state the body-fallback used to paper over.
    dateGlance.open = true
    dateGlance.activeAnchor = null

    render(DateGlance)
    // Guard effect closes + refuses to paint at the viewport origin.
    await new Promise((r) => setTimeout(r, 0))

    expect(screen.queryByRole('dialog', { name: /pick a date/i })).toBeNull()
    expect(dateGlance.open).toBe(false)
    err.mockRestore()
  })

  it('positions the popover below the chip anchor, not at the top-left origin', async () => {
    closeDateGlance()
    mockAnchorRect(anchor, {
      left: 400,
      top: 40,
      bottom: 72,
      width: 32,
      height: 32
    })
    Object.defineProperty(window, 'innerWidth', {
      value: 1200,
      configurable: true
    })
    Object.defineProperty(window, 'innerHeight', {
      value: 800,
      configurable: true
    })
    setDateGlanceAnchor(anchor)
    openDateGlance()

    render(DateGlance)
    await screen.findByRole('dialog', { name: /pick a date/i })
    // measure() runs sync + again after tick
    await new Promise((r) => setTimeout(r, 0))

    const layer = floatingLayer()
    const left = Number(layer.style.left.replace('px', ''))
    const top = Number(layer.style.top.replace('px', ''))
    // Below chip: top ≈ bottom + gap(4). left ≈ chip left. Not origin margin.
    expect(left).toBe(400)
    expect(top).toBe(76)
    expect(left).not.toBe(POPOVER_MARGIN)
    expect(top).not.toBe(POPOVER_MARGIN)
    expect(layer.style.left).not.toBe('-9999px')
  })

  it('positions the popover beside a caret placement rect from slash/hotkey', async () => {
    closeDateGlance()
    setDateGlanceAnchor(null)
    Object.defineProperty(window, 'innerWidth', {
      value: 1200,
      configurable: true
    })
    Object.defineProperty(window, 'innerHeight', {
      value: 800,
      configurable: true
    })
    openDateGlance(null, { rect: { top: 300, bottom: 318, left: 240 } })
    // Ephemeral marker needs a real client rect for Popover.measure().
    mockAnchorRect(dateGlance.activeAnchor!, {
      left: 240,
      top: 300,
      bottom: 318,
      width: 0,
      height: 18
    })

    render(DateGlance)
    await screen.findByRole('dialog', { name: /pick a date/i })
    await new Promise((r) => setTimeout(r, 0))

    const layer = floatingLayer()
    const left = Number(layer.style.left.replace('px', ''))
    const top = Number(layer.style.top.replace('px', ''))
    expect(left).toBe(240)
    // gap default 4 → 318 + 4
    expect(top).toBe(322)
    expect(left).not.toBe(POPOVER_MARGIN)
    expect(top).not.toBe(POPOVER_MARGIN)
  })

  it('positions against the clicked chip even when a hidden chip owns the global anchor', async () => {
    closeDateGlance()
    // Last-registered chip is display:none (inactive tab) → 0×0 rect.
    const hidden = document.createElement('button')
    hidden.style.display = 'none'
    document.body.appendChild(hidden)
    setDateGlanceAnchor(hidden)

    const visible = document.createElement('button')
    document.body.appendChild(visible)
    mockAnchorRect(visible, {
      left: 900,
      top: 20,
      bottom: 52,
      width: 32,
      height: 32
    })
    Object.defineProperty(window, 'innerWidth', {
      value: 1200,
      configurable: true
    })
    Object.defineProperty(window, 'innerHeight', {
      value: 800,
      configurable: true
    })

    // Chip click path: pass the element that was actually pressed.
    openDateGlance(null, { element: visible })

    render(DateGlance)
    await screen.findByRole('dialog', { name: /pick a date/i })
    await new Promise((r) => setTimeout(r, 0))

    const layer = floatingLayer()
    const left = Number(layer.style.left.replace('px', ''))
    const top = Number(layer.style.top.replace('px', ''))
    expect(left).toBe(900)
    expect(top).toBe(56)
    // Must not collapse to the hidden chip's origin rect.
    expect(left).not.toBe(POPOVER_MARGIN)
    expect(top).not.toBe(POPOVER_MARGIN)

    hidden.remove()
    visible.remove()
  })
})
