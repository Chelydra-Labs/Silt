import { describe, expect, it, vi } from 'vitest'
import { tick } from 'svelte'
import { render } from '@testing-library/svelte'

import Dialog from './Dialog.svelte'

// Escape is dispatched on the focused inner content (the production path),
// never on window — window-dispatch is exactly the jsdom style that masked
// the original Escape-leak bug.
async function escapeOnFocusedContent() {
  const target = document.activeElement as HTMLElement
  expect(target).not.toBe(document.body)
  const event = new KeyboardEvent('keydown', {
    key: 'Escape',
    bubbles: true,
    cancelable: true
  })
  target.dispatchEvent(event)
  await tick()
  return event
}

describe('Dialog Escape contract', () => {
  it('closeOnEscape=false: passes Escape through to inner content', async () => {
    const onClose = vi.fn()
    render(Dialog, { props: { closeOnEscape: false, onClose } })
    await tick()

    const received: KeyboardEvent[] = []
    const nested = (e: KeyboardEvent) => received.push(e)
    window.addEventListener('keydown', nested)
    try {
      const event = await escapeOnFocusedContent()
      expect(onClose).not.toHaveBeenCalled()
      expect(event.defaultPrevented).toBe(false)
      // The key still reaches listeners deeper in the path (e.g. a nested
      // confirm's own handler) — it is not stranded in the modal.
      expect(received).toHaveLength(1)
      expect(received[0].key).toBe('Escape')
    } finally {
      window.removeEventListener('keydown', nested)
    }
  })

  it('busy=true: passes Escape through to inner content', async () => {
    const onClose = vi.fn()
    render(Dialog, { props: { busy: true, onClose } })
    await tick()

    const received: KeyboardEvent[] = []
    const nested = (e: KeyboardEvent) => received.push(e)
    window.addEventListener('keydown', nested)
    try {
      const event = await escapeOnFocusedContent()
      expect(onClose).not.toHaveBeenCalled()
      expect(event.defaultPrevented).toBe(false)
      expect(received).toHaveLength(1)
    } finally {
      window.removeEventListener('keydown', nested)
    }
  })

  it('default: closes, prevents default, and stops propagation', async () => {
    const onClose = vi.fn()
    render(Dialog, { props: { onClose } })
    await tick()

    const outer = vi.fn()
    window.addEventListener('keydown', outer)
    try {
      const event = await escapeOnFocusedContent()
      expect(onClose).toHaveBeenCalledTimes(1)
      expect(event.defaultPrevented).toBe(true)
      // stopPropagation: bubble-phase app listeners never observe the key.
      expect(outer).not.toHaveBeenCalled()
    } finally {
      window.removeEventListener('keydown', outer)
    }
  })
})
