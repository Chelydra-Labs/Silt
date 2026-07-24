import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  dateGlance,
  openDateGlance,
  openDateGlanceNearEditor,
  closeDateGlance,
  toggleDateGlance,
  clearInsertEditor,
  setDateGlanceAnchor,
  caretRectFromEditor
} from './dateGlanceState.svelte'

// Reset shared state between tests so assertions are independent.
afterEach(() => {
  closeDateGlance()
  setDateGlanceAnchor(null)
  dateGlance.openGen = 0
  // Remove any leftover ephemeral markers.
  document
    .querySelectorAll('[data-date-glance-placement]')
    .forEach((n) => n.remove())
})

describe('dateGlanceState', () => {
  it('starts closed with no anchors, insert target, or open generation', () => {
    expect(dateGlance.open).toBe(false)
    expect(dateGlance.anchor).toBeNull()
    expect(dateGlance.activeAnchor).toBeNull()
    expect(dateGlance.insertEditor).toBeNull()
    expect(dateGlance.openGen).toBe(0)
  })

  it('openDateGlance sets open + insertEditor + bumps openGen without disturbing the chip anchor', () => {
    const el = document.createElement('div')
    document.body.appendChild(el)
    setDateGlanceAnchor(el)
    const ok = openDateGlance({} as never)
    expect(ok).toBe(true)
    expect(dateGlance.open).toBe(true)
    expect(dateGlance.insertEditor).toBeDefined()
    expect(dateGlance.openGen).toBe(1)
    expect(dateGlance.anchor).toBe(el)
    expect(dateGlance.activeAnchor).toBe(el)
    el.remove()
  })

  it('each successful open bumps openGen so consumers can detect re-opens', () => {
    const el = document.createElement('div')
    document.body.appendChild(el)
    setDateGlanceAnchor(el)
    openDateGlance()
    expect(dateGlance.openGen).toBe(1)
    closeDateGlance()
    openDateGlance()
    expect(dateGlance.openGen).toBe(2)
    el.remove()
  })

  it('openDateGlance defaults insertEditor to null (copy mode)', () => {
    const el = document.createElement('div')
    document.body.appendChild(el)
    setDateGlanceAnchor(el)
    openDateGlance()
    expect(dateGlance.open).toBe(true)
    expect(dateGlance.insertEditor).toBeNull()
    el.remove()
  })

  it('closeDateGlance clears open + insertEditor + activeAnchor but leaves the chip anchor', () => {
    const el = document.createElement('div')
    document.body.appendChild(el)
    setDateGlanceAnchor(el)
    openDateGlance({} as never)
    closeDateGlance()
    expect(dateGlance.open).toBe(false)
    expect(dateGlance.insertEditor).toBeNull()
    expect(dateGlance.activeAnchor).toBeNull()
    // Chip registration persists for the next open.
    expect(dateGlance.anchor).toBe(el)
    el.remove()
  })

  it('toggleDateGlance opens when closed and closes when open', () => {
    const el = document.createElement('div')
    document.body.appendChild(el)
    setDateGlanceAnchor(el)
    expect(toggleDateGlance()).toBe(true)
    expect(dateGlance.open).toBe(true)
    expect(dateGlance.openGen).toBe(1)
    expect(toggleDateGlance()).toBe(false)
    expect(dateGlance.open).toBe(false)
    expect(toggleDateGlance()).toBe(true)
    expect(dateGlance.open).toBe(true)
    expect(dateGlance.openGen).toBe(2)
    el.remove()
  })

  it('clearInsertEditor drops the insert target (editor unmount)', () => {
    const el = document.createElement('div')
    document.body.appendChild(el)
    setDateGlanceAnchor(el)
    openDateGlance({} as never)
    expect(dateGlance.insertEditor).toBeDefined()
    clearInsertEditor()
    expect(dateGlance.insertEditor).toBeNull()
    // open state is unchanged — only the target is cleared.
    expect(dateGlance.open).toBe(true)
    el.remove()
  })

  it('refuses to open without a chip or caret rect (no body fallback)', () => {
    const err = vi.spyOn(console, 'error').mockImplementation(() => {})
    const ok = openDateGlance()
    expect(ok).toBe(false)
    expect(dateGlance.open).toBe(false)
    expect(dateGlance.activeAnchor).toBeNull()
    expect(err).toHaveBeenCalledWith(
      expect.stringContaining('no placeable anchor')
    )
    err.mockRestore()
  })

  it('opens against an ephemeral caret placement when rect is provided', () => {
    // No chip registered — caret rect alone must be enough.
    const ok = openDateGlance(null, {
      rect: { top: 300, bottom: 318, left: 240 }
    })
    expect(ok).toBe(true)
    expect(dateGlance.open).toBe(true)
    expect(dateGlance.activeAnchor).toBeTruthy()
    expect(dateGlance.activeAnchor).not.toBe(document.body)
    expect(
      dateGlance.activeAnchor?.hasAttribute('data-date-glance-placement')
    ).toBe(true)
    const r = dateGlance.activeAnchor!.getBoundingClientRect()
    // jsdom returns zeros for layout; style is the source of truth for placement.
    expect(dateGlance.activeAnchor!.style.left).toBe('240px')
    expect(dateGlance.activeAnchor!.style.top).toBe('300px')
    expect(dateGlance.activeAnchor!.style.height).toBe('18px')
    void r
  })

  it('prefers caret rect over the chip when both are available', () => {
    const chip = document.createElement('button')
    document.body.appendChild(chip)
    setDateGlanceAnchor(chip)
    openDateGlance(null, { rect: { top: 100, bottom: 120, left: 50 } })
    expect(dateGlance.activeAnchor).not.toBe(chip)
    expect(
      dateGlance.activeAnchor?.hasAttribute('data-date-glance-placement')
    ).toBe(true)
    expect(dateGlance.anchor).toBe(chip)
    chip.remove()
  })

  it('prefers an explicit element over the globally registered chip', () => {
    // Simulates multi-tab: last-registered chip is a hidden (display:none)
    // instance whose client rect is 0×0; the click target is a different node.
    const hiddenChip = document.createElement('button')
    hiddenChip.style.display = 'none'
    document.body.appendChild(hiddenChip)
    setDateGlanceAnchor(hiddenChip)

    const clickedChip = document.createElement('button')
    document.body.appendChild(clickedChip)

    const ok = openDateGlance(null, { element: clickedChip })
    expect(ok).toBe(true)
    expect(dateGlance.activeAnchor).toBe(clickedChip)
    expect(dateGlance.activeAnchor).not.toBe(hiddenChip)

    hiddenChip.remove()
    clickedChip.remove()
  })

  it('removes the ephemeral placement marker on close', () => {
    openDateGlance(null, { rect: { top: 10, bottom: 20, left: 10 } })
    expect(
      document.querySelectorAll('[data-date-glance-placement]').length
    ).toBe(1)
    closeDateGlance()
    expect(
      document.querySelectorAll('[data-date-glance-placement]').length
    ).toBe(0)
    expect(dateGlance.activeAnchor).toBeNull()
  })

  it('caretRectFromEditor maps coordsAtPos to a placement rect', () => {
    const coordsAtPos = vi.fn().mockReturnValue({
      left: 240,
      top: 300,
      bottom: 318,
      right: 250
    })
    const editor = {
      isDestroyed: false,
      state: { selection: { from: 7 } },
      view: { coordsAtPos }
    } as never
    expect(caretRectFromEditor(editor)).toEqual({
      top: 300,
      bottom: 318,
      left: 240
    })
    expect(coordsAtPos).toHaveBeenCalledWith(7)
  })

  it('caretRectFromEditor returns null and logs when coordsAtPos throws', () => {
    const err = vi.spyOn(console, 'error').mockImplementation(() => {})
    const editor = {
      isDestroyed: false,
      state: { selection: { from: 0 } },
      view: {
        coordsAtPos: () => {
          throw new Error('bad pos')
        }
      }
    } as never
    expect(caretRectFromEditor(editor)).toBeNull()
    expect(err).toHaveBeenCalled()
    err.mockRestore()
  })

  it('openDateGlanceNearEditor uses caret coords when the editor is live', () => {
    const editor = {
      isDestroyed: false,
      state: { selection: { from: 3 } },
      view: {
        coordsAtPos: () => ({ left: 120, top: 200, bottom: 216, right: 130 })
      }
    } as never
    const ok = openDateGlanceNearEditor(editor)
    expect(ok).toBe(true)
    // $state proxy makes Object.is against the raw mock unreliable.
    expect(dateGlance.insertEditor).toBeTruthy()
    expect(dateGlance.insertEditor?.isDestroyed).toBe(false)
    expect(dateGlance.activeAnchor?.style.left).toBe('120px')
    expect(dateGlance.activeAnchor?.style.top).toBe('200px')
  })

  it('openDateGlanceNearEditor falls back to the chip when caret coords fail', () => {
    const err = vi.spyOn(console, 'error').mockImplementation(() => {})
    const chip = document.createElement('button')
    document.body.appendChild(chip)
    setDateGlanceAnchor(chip)
    const editor = {
      isDestroyed: false,
      state: { selection: { from: 0 } },
      view: {
        coordsAtPos: () => {
          throw new Error('no coords')
        }
      }
    } as never
    const ok = openDateGlanceNearEditor(editor)
    expect(ok).toBe(true)
    expect(dateGlance.activeAnchor).toBe(chip)
    chip.remove()
    err.mockRestore()
  })

  it('openDateGlanceNearEditor refuses when caret fails and no chip is registered', () => {
    const err = vi.spyOn(console, 'error').mockImplementation(() => {})
    const editor = {
      isDestroyed: false,
      state: { selection: { from: 0 } },
      view: {
        coordsAtPos: () => {
          throw new Error('no coords')
        }
      }
    } as never
    expect(openDateGlanceNearEditor(editor)).toBe(false)
    expect(dateGlance.open).toBe(false)
    err.mockRestore()
  })
})
