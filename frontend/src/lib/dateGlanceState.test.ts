import { afterEach, describe, expect, it } from 'vitest'
import {
  dateGlance,
  openDateGlance,
  closeDateGlance,
  setDateGlanceAnchor
} from './dateGlanceState.svelte'

// Reset shared state between tests so assertions are independent.
afterEach(() => {
  closeDateGlance()
  setDateGlanceAnchor(null)
})

describe('dateGlanceState', () => {
  it('starts closed with no anchor or insert target', () => {
    expect(dateGlance.open).toBe(false)
    expect(dateGlance.anchor).toBeNull()
    expect(dateGlance.insertEditor).toBeNull()
  })

  it('openDateGlance sets open + insertEditor without disturbing the anchor', () => {
    const el = document.createElement('div')
    setDateGlanceAnchor(el)
    openDateGlance({} as never)
    expect(dateGlance.open).toBe(true)
    expect(dateGlance.insertEditor).toBeDefined()
    // The anchor is owned by the chip; open must not clear it.
    expect(dateGlance.anchor).toBe(el)
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
})
