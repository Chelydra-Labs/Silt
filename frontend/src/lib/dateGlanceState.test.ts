import { afterEach, describe, expect, it } from 'vitest'
import {
  dateGlance,
  openDateGlance,
  closeDateGlance,
  toggleDateGlance,
  clearInsertEditor,
  setDateGlanceAnchor
} from './dateGlanceState.svelte'

// Reset shared state between tests so assertions are independent.
afterEach(() => {
  closeDateGlance()
  setDateGlanceAnchor(null)
  dateGlance.openGen = 0
})

describe('dateGlanceState', () => {
  it('starts closed with no anchor, insert target, or open generation', () => {
    expect(dateGlance.open).toBe(false)
    expect(dateGlance.anchor).toBeNull()
    expect(dateGlance.insertEditor).toBeNull()
    expect(dateGlance.openGen).toBe(0)
  })

  it('openDateGlance sets open + insertEditor + bumps openGen without disturbing the anchor', () => {
    const el = document.createElement('div')
    setDateGlanceAnchor(el)
    openDateGlance({} as never)
    expect(dateGlance.open).toBe(true)
    expect(dateGlance.insertEditor).toBeDefined()
    expect(dateGlance.openGen).toBe(1)
    // The anchor is owned by the chip; open must not clear it.
    expect(dateGlance.anchor).toBe(el)
  })

  it('each open bumps openGen so consumers can detect re-opens', () => {
    openDateGlance()
    expect(dateGlance.openGen).toBe(1)
    closeDateGlance()
    openDateGlance()
    expect(dateGlance.openGen).toBe(2)
  })

  it('openDateGlance defaults insertEditor to null (copy mode)', () => {
    openDateGlance()
    expect(dateGlance.open).toBe(true)
    expect(dateGlance.insertEditor).toBeNull()
  })

  it('closeDateGlance clears open + insertEditor but leaves the anchor', () => {
    const el = document.createElement('div')
    setDateGlanceAnchor(el)
    openDateGlance({} as never)
    closeDateGlance()
    expect(dateGlance.open).toBe(false)
    expect(dateGlance.insertEditor).toBeNull()
    // Anchor persists so the chip is still registered for the next open.
    expect(dateGlance.anchor).toBe(el)
  })

  it('toggleDateGlance opens when closed and closes when open', () => {
    toggleDateGlance()
    expect(dateGlance.open).toBe(true)
    expect(dateGlance.openGen).toBe(1)
    toggleDateGlance()
    expect(dateGlance.open).toBe(false)
    toggleDateGlance()
    expect(dateGlance.open).toBe(true)
    expect(dateGlance.openGen).toBe(2)
  })

  it('clearInsertEditor drops the insert target (editor unmount)', () => {
    openDateGlance({} as never)
    expect(dateGlance.insertEditor).toBeDefined()
    clearInsertEditor()
    expect(dateGlance.insertEditor).toBeNull()
    // open state is unchanged — only the target is cleared.
    expect(dateGlance.open).toBe(true)
  })
})
