import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent } from '@testing-library/svelte'
import { getFocusable, trapFocus } from './focusTrap'

// Minimal DOM harness: build a container with the given inner HTML, focus a
// target, and return helpers to fire Tab keydown events.
function setup(html: string) {
  document.body.innerHTML = ''
  const container = document.createElement('div')
  container.innerHTML = html
  document.body.appendChild(container)
  return container
}

function tab(shift = false): void {
  // Capture-phase listener lives on window; fireEvent.keyDown(window, …) is
  // the repo's proven pattern for reaching a window keydown listener (a bare
  // window.dispatchEvent(KeyboardEvent) is not equivalent in this jsdom).
  fireEvent.keyDown(window, { key: 'Tab', shiftKey: shift })
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('getFocusable', () => {
  it('includes native form controls the old traps omitted (select, textarea)', () => {
    const container = setup(`
      <button id="b">B</button>
      <select id="s"><option>x</option></select>
      <textarea id="t"></textarea>
      <input id="i" />
    `)
    const ids = getFocusable(container).map((el) => el.id)
    expect(ids).toEqual(['b', 's', 't', 'i'])
  })

  it('includes a[href], [contenteditable], and tabindex>=0', () => {
    const container = setup(`
      <a href="/x" id="a">link</a>
      <div contenteditable="true" id="ce"></div>
      <span tabindex="0" id="tb">tabbable span</span>
      <span tabindex="-1" id="skip">not tabbable</span>
    `)
    const ids = getFocusable(container).map((el) => el.id)
    expect(ids).toEqual(['a', 'ce', 'tb'])
  })

  it('excludes disabled, hidden, inert, and aria-hidden elements', () => {
    const container = setup(`
      <button id="ok">OK</button>
      <button id="d" disabled>disabled</button>
      <button id="h" hidden>hidden</button>
      <div inert><button id="inert">in inert</button></div>
      <div aria-hidden="true"><button id="ah">in aria-hidden</button></div>
    `)
    const ids = getFocusable(container).map((el) => el.id)
    expect(ids).toEqual(['ok'])
  })

  it('excludes negative-tabindex elements (backdrop sentinel buttons)', () => {
    // Every overlay dialog uses a full-size tabindex="-1" button as its
    // click-to-close backdrop; it must NOT enter the Tab cycle.
    const container = setup(`
      <button id="sentinel" tabindex="-1">backdrop</button>
      <button id="first">First</button>
      <button id="last">Last</button>
    `)
    const ids = getFocusable(container).map((el) => el.id)
    expect(ids).toEqual(['first', 'last'])
  })
})

describe('trapFocus', () => {
  it('wraps focus from the last element to the first on Tab', () => {
    const container = setup(`
      <button id="first">First</button>
      <button id="last">Last</button>
    `)
    const dispose = trapFocus(container)
    const last = document.getElementById('last') as HTMLButtonElement
    last.focus()
    expect(document.activeElement).toBe(last)

    tab()
    expect(document.activeElement?.id).toBe('first')

    dispose()
  })

  it('wraps focus from the first element to the last on Shift+Tab', () => {
    const container = setup(`
      <button id="first">First</button>
      <button id="last">Last</button>
    `)
    const dispose = trapFocus(container)
    const first = document.getElementById('first') as HTMLButtonElement
    first.focus()
    expect(document.activeElement).toBe(first)

    tab(true)
    expect(document.activeElement?.id).toBe('last')

    dispose()
  })

  it('treats a <select> as a wrap boundary (the latent bug the old selectors had)', () => {
    // The old hand-rolled selectors omitted <select>, so a trap whose first or
    // last focusable was a select would wrap to the wrong element (skipping it).
    // Put the select at BOTH boundaries and assert the wrap lands on/off it.
    const container = setup(`
      <select id="first"><option>a</option><option>b</option></select>
      <button id="last">Last</button>
    `)
    const dispose = trapFocus(container)
    const sel = document.getElementById('first') as HTMLSelectElement
    const last = document.getElementById('last') as HTMLButtonElement

    // Forward Tab from the last (button) wraps to the select (first) — the
    // select is a real wrap target, not skipped.
    last.focus()
    tab()
    expect(document.activeElement?.id).toBe('first')

    // Shift+Tab from the select (first) wraps to the button (last) — the
    // select is recognized as the first boundary.
    sel.focus()
    tab(true)
    expect(document.activeElement?.id).toBe('last')

    dispose()
  })

  it('keeps focus inside when an outside element is active', () => {
    const container = setup(`<button id="inside">In</button>`)
    // An outside button that is NOT inside the trapped container.
    const outside = document.createElement('button')
    outside.id = 'outside'
    document.body.appendChild(outside)
    outside.focus()
    const dispose = trapFocus(container)

    tab()
    // Focus snapped back into the trap's first element.
    expect(document.activeElement?.id).toBe('inside')

    dispose()
  })

  it('disposer removes the listener', () => {
    const container = setup(`<button id="first">First</button>`)
    const dispose = trapFocus(container)
    dispose()
    const outside = document.createElement('button')
    outside.id = 'outside'
    document.body.appendChild(outside)
    outside.focus()

    tab()
    // No trap active — focus stays where the browser left it.
    expect(document.activeElement?.id).toBe('outside')
  })

  it('is a no-op (no crash) when the container has no focusable elements', () => {
    const container = setup(`<p>no controls here</p>`)
    const dispose = trapFocus(container)
    expect(() => tab()).not.toThrow()
    dispose()
  })

  it('does not interfere with non-Tab keys', () => {
    const container = setup(`<button id="b">B</button>`)
    const dispose = trapFocus(container)
    const b = document.getElementById('b') as HTMLButtonElement
    b.focus()
    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })
    )
    expect(document.activeElement).toBe(b)
    dispose()
  })
})

// Keep the import used (vi) so lint doesn't flag it in environments without
// the hoisted mock helper; harmless otherwise.
void vi
