import { describe, expect, it, vi } from 'vitest'
import { isContextMenuKey, openContextMenuFromKeyboard } from './contextMenuKey'

describe('isContextMenuKey', () => {
  it('matches the Menu key', () => {
    expect(isContextMenuKey(new KeyboardEvent('keydown', { key: 'ContextMenu' }))).toBe(true)
  })

  it('matches Shift+F10', () => {
    expect(
      isContextMenuKey(new KeyboardEvent('keydown', { key: 'F10', shiftKey: true }))
    ).toBe(true)
  })

  it('does not match F10 without Shift', () => {
    expect(isContextMenuKey(new KeyboardEvent('keydown', { key: 'F10' }))).toBe(false)
  })

  it('does not match other keys', () => {
    for (const key of ['Enter', ' ', 'ArrowDown', 'c']) {
      expect(isContextMenuKey(new KeyboardEvent('keydown', { key }))).toBe(false)
    }
  })
})

describe('openContextMenuFromKeyboard', () => {
  it('dispatches a bubbling contextmenu at the element bottom-left corner', () => {
    const el = document.createElement('button')
    const parent = document.createElement('div')
    parent.appendChild(el)
    document.body.appendChild(parent)

    const onEl = vi.fn()
    const onParent = vi.fn()
    el.addEventListener('contextmenu', onEl)
    parent.addEventListener('contextmenu', onParent)
    vi.spyOn(el, 'getBoundingClientRect').mockReturnValue({
      left: 40,
      top: 10,
      right: 240,
      bottom: 44,
      width: 200,
      height: 34,
      x: 40,
      y: 10,
      toJSON: () => ({})
    } as DOMRect)

    openContextMenuFromKeyboard(el)

    expect(onEl).toHaveBeenCalledTimes(1)
    // Bubbles: an ancestor-level handler (the usual wiring) also sees it.
    expect(onParent).toHaveBeenCalledTimes(1)
    const event = onEl.mock.calls[0][0] as MouseEvent
    expect(event.type).toBe('contextmenu')
    expect(event.bubbles).toBe(true)
    expect(event.clientX).toBe(40)
    expect(event.clientY).toBe(44)

    document.body.removeChild(parent)
  })
})
