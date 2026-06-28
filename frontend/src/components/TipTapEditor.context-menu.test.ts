// Context menu tests for TipTapEditor.svelte — verifies rendering, item
// visibility (including block-scoped items), Escape dismissal, role=menuitem
// coverage, and keyboard navigation (ArrowUp/ArrowDown cycling).

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, fireEvent, waitFor } from '@testing-library/svelte'
import TipTapEditor from './TipTapEditor.svelte'
import { mkBlock } from '../lib/editor/nodeview-test-harness'

// jsdom stubs: TipTap's Placeholder viewport tracker touches document.
// elementFromPoint during editor construction (same approach as
// nodeview-test-harness.ts), and the @-mention popup's coordsAtPos call
// resolves caret coordinates through Range.getClientRects, which jsdom does
// not implement. Both are stubbed so the popup can render under jsdom.
if (typeof document !== 'undefined' && !document.elementFromPoint) {
  document.elementFromPoint = () => document.body
}
if (
  typeof window !== 'undefined' &&
  window.Range &&
  !Range.prototype.getClientRects
) {
  const zeroRect: DOMRect = {
    x: 0,
    y: 0,
    left: 0,
    top: 0,
    right: 0,
    bottom: 0,
    width: 0,
    height: 0,
    toJSON() {
      return this
    }
  }
  Range.prototype.getClientRects = (() => [
    zeroRect
  ]) as unknown as typeof Range.prototype.getClientRects
  Range.prototype.getBoundingClientRect = () => zeroRect
}

const mocks = vi.hoisted(() => {
  // The full owner set the editor seeds on mount/focus. The mocked
  // DistinctOwners reproduces the backend's LIKE 'prefix%' (ASCII
  // case-insensitive prefix match) so the typeahead's debounced server refine
  // behaves the same as the real SQLite query. #332.
  const allOwners = ['Alice', 'Bob', 'Aardvark', 'Charlie']
  return {
    saveFileBlocks: vi.fn().mockResolvedValue(undefined),
    acquireFocusLock: vi.fn().mockResolvedValue(undefined),
    refreshFocusLock: vi.fn().mockResolvedValue(undefined),
    releaseFocusLock: vi.fn().mockResolvedValue(undefined),
    distinctOwners: vi.fn(async (prefix = '') => {
      const p = (prefix ?? '').toLowerCase()
      if (!p) return allOwners.slice()
      return allOwners.filter((o) => o.toLowerCase().startsWith(p))
    }),
    eventsOn: vi.fn(() => () => {})
  }
})

vi.mock('../../wailsjs/go/main/App.js', () => ({
  SaveFileBlocks: mocks.saveFileBlocks,
  AcquireFocusLock: mocks.acquireFocusLock,
  RefreshFocusLock: mocks.refreshFocusLock,
  ReleaseFocusLock: mocks.releaseFocusLock,
  // TipTapEditor seeds the @-mention owner list on mount/focus (#184).
  DistinctOwners: mocks.distinctOwners
}))

vi.mock('../../wailsjs/runtime/runtime.js', () => ({
  EventsOn: mocks.eventsOn
}))

vi.mock('../settings/store.svelte', () => ({
  settings: { config: null },
  saveConfig: vi.fn()
}))

vi.mock('../theme/store.svelte', () => ({
  themeState: { mode: 'dark' }
}))

vi.mock('../notifications/store.svelte', () => ({
  pushNotification: vi.fn()
}))

vi.mock('../plugins/events', () => ({
  dispatch: vi.fn()
}))

vi.mock('../lib/perf/frame-budget', () => ({
  measureFrameBudget: vi.fn((_label: string, fn: () => unknown) => fn())
}))

async function openContextMenu(container: HTMLElement): Promise<void> {
  const host = container.querySelector('.tiptap-editor-host') as HTMLElement
  await fireEvent.contextMenu(host)
  await waitFor(() => {
    expect(container.querySelector('[role="menu"]')).toBeTruthy()
  })
}

describe('TipTapEditor context menu', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('opens on right-click with standard items visible', async () => {
    const blocks = [mkBlock('NOTE', { clean_text: 'hello' })]
    const { container, unmount } = render(TipTapEditor, {
      props: {
        notebook: 'NB',
        section: 'S',
        page: 'P',
        blocks,
        onUpdate: () => {}
      }
    })

    await waitFor(() => {
      expect(container.querySelector('.ProseMirror')).toBeTruthy()
    })

    await openContextMenu(container)

    const text = container.textContent!
    for (const label of [
      'Cut',
      'Copy',
      'Paste',
      'Copy as Markdown',
      'Copy as Plain Text',
      'Clear Formatting'
    ]) {
      expect(text).toContain(label)
    }

    unmount()
  })

  it('shows block-scoped items when a block is resolved', async () => {
    const blocks = [mkBlock('NOTE', { clean_text: 'hello' })]
    const { container, unmount } = render(TipTapEditor, {
      props: {
        notebook: 'NB',
        section: 'S',
        page: 'P',
        blocks,
        onUpdate: () => {}
      }
    })

    await waitFor(() => {
      expect(container.querySelector('.ProseMirror')).toBeTruthy()
    })

    await openContextMenu(container)

    const text = container.textContent!
    expect(text).toContain('Duplicate Block')
    expect(text).toContain('Delete Block')
    expect(text).toContain('Copy Block Reference')
    expect(text).toContain('Copy Block Embed')

    unmount()
  })

  it('closes on Escape key', async () => {
    const blocks = [mkBlock('NOTE', { clean_text: 'hello' })]
    const { container, unmount } = render(TipTapEditor, {
      props: {
        notebook: 'NB',
        section: 'S',
        page: 'P',
        blocks,
        onUpdate: () => {}
      }
    })

    await waitFor(() => {
      expect(container.querySelector('.ProseMirror')).toBeTruthy()
    })

    await openContextMenu(container)
    expect(container.querySelector('[role="menu"]')).toBeTruthy()

    await fireEvent.keyDown(window, { key: 'Escape' })

    await waitFor(() => {
      expect(container.querySelector('[role="menu"]')).toBeNull()
    })

    unmount()
  })

  it('has role=menuitem on every menu button', async () => {
    const blocks = [mkBlock('NOTE', { clean_text: 'hello' })]
    const { container, unmount } = render(TipTapEditor, {
      props: {
        notebook: 'NB',
        section: 'S',
        page: 'P',
        blocks,
        onUpdate: () => {}
      }
    })

    await waitFor(() => {
      expect(container.querySelector('.ProseMirror')).toBeTruthy()
    })

    await openContextMenu(container)

    const menuItems = container.querySelectorAll('[role="menuitem"]')
    // 6 standard + 4 block-scoped = 10
    expect(menuItems.length).toBe(10)

    unmount()
  })

  it('navigates items with ArrowDown/ArrowUp', async () => {
    const blocks = [mkBlock('NOTE', { clean_text: 'hello' })]
    const { container, unmount } = render(TipTapEditor, {
      props: {
        notebook: 'NB',
        section: 'S',
        page: 'P',
        blocks,
        onUpdate: () => {}
      }
    })

    await waitFor(() => {
      expect(container.querySelector('.ProseMirror')).toBeTruthy()
    })

    await openContextMenu(container)

    // After open, the first enabled item should be focused via the
    // requestAnimationFrame effect. Wait for it.
    const menu = container.querySelector('[role="menu"]') as HTMLElement
    await waitFor(() => {
      expect(document.activeElement).toBe(
        menu.querySelector('button:not([disabled])')
      )
    })

    // ArrowDown should move focus to the second item
    await fireEvent.keyDown(menu, { key: 'ArrowDown' })
    const items = Array.from(
      menu.querySelectorAll<HTMLButtonElement>('button:not([disabled])')
    )
    expect(document.activeElement).toBe(items[1])

    // ArrowUp should wrap to the last item
    await fireEvent.keyDown(menu, { key: 'ArrowUp' })
    // currentIndex is 1, ArrowUp goes to 0... wait, we're at index 1 now.
    // ArrowUp: (1 - 1 + N) % N = 0. So back to first item.
    // Let me press ArrowUp again to test wrap:
    await fireEvent.keyDown(menu, { key: 'ArrowUp' })
    // currentIndex is 0, ArrowUp: (0 - 1 + N) % N = N-1 = last item
    expect(document.activeElement).toBe(items[items.length - 1])

    unmount()
  })

  it('disables Delete Block when only one block remains', async () => {
    const blocks = [mkBlock('NOTE', { clean_text: 'only block' })]
    const { container, unmount } = render(TipTapEditor, {
      props: {
        notebook: 'NB',
        section: 'S',
        page: 'P',
        blocks,
        onUpdate: () => {}
      }
    })

    await waitFor(() => {
      expect(container.querySelector('.ProseMirror')).toBeTruthy()
    })

    await openContextMenu(container)

    const deleteBtn = Array.from(
      container.querySelectorAll('[role="menuitem"]')
    ).find((btn) =>
      btn.textContent?.includes('Delete Block')
    ) as HTMLButtonElement
    expect(deleteBtn).toBeTruthy()
    expect(deleteBtn.disabled).toBe(true)

    unmount()
  })
})

// @-mention typeahead: focus-debounce + TTL cache + debounced/race-guarded
// server refine (#332). DistinctOwners now takes a prefix so a large vault never
// ships an unbounded payload per keystroke; the editor seeds the cache on mount
// (DistinctOwners('')) and refines from the server for non-empty queries
// (DistinctOwners('<prefix>')) on a 120ms debounce.
describe('TipTapEditor @-mention prefix refine (#332)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('seeds the owner cache on mount via DistinctOwners("")', async () => {
    vi.useFakeTimers()
    try {
      const blocks = [mkBlock('NOTE', { clean_text: 'hello' })]
      const { unmount } = render(TipTapEditor, {
        props: {
          notebook: 'NB',
          section: 'S',
          page: 'P',
          blocks,
          onUpdate: () => {}
        }
      })
      // Flush mount-time loadOwners() microtask (onCreate fires it directly —
      // the focus debounce only wraps onFocus).
      await vi.advanceTimersByTimeAsync(0)
      expect(mocks.distinctOwners).toHaveBeenCalledWith('')
      unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('debounces a prefix-filtered DistinctOwners("<query>") and refines the popup', async () => {
    vi.useFakeTimers()
    try {
      // The mock seeds: Alice, Bob, Aardvark, Charlie. Note 'Charlie' contains
      // 'a' mid-name — the instant cache filter (substring) includes it, but
      // the server's LIKE 'a%' (prefix) excludes it. The popup must narrow
      // after the debounce fires DistinctOwners('a').
      const blocks = [mkBlock('NOTE', { clean_text: 'ping ' })]
      const { container, unmount } = render(TipTapEditor, {
        props: {
          notebook: 'NB',
          section: 'S',
          page: 'P',
          blocks,
          onUpdate: () => {}
        }
      })

      // Mount + flush the cache-seed loadOwners() (DistinctOwners('')).
      await vi.advanceTimersByTimeAsync(0)
      expect(mocks.distinctOwners).toHaveBeenCalledWith('')

      // TipTap attaches the Editor instance to its .ProseMirror DOM node, so a
      // component test can drive the editor via commands without faking
      // contenteditable input events (which jsdom cannot reliably emulate —
      // see AGENTS.md: do not drive the rendered webview).
      const pm = container.querySelector('.ProseMirror') as unknown as {
        editor: {
          commands: {
            focus: (pos?: string) => void
            insertContent: (content: string) => void
          }
        }
      }
      const editor = pm.editor
      expect(editor).toBeTruthy()

      // Place the caret at the end of "ping " and type "@a".
      editor.commands.focus('end')
      editor.commands.insertContent('@a')
      await tick()

      // Instant popup from the cached full set (filterOwners substring): the
      // 'a' matches Alice, Aardvark AND Charlie (mid-name). onMentionChange
      // runs synchronously inside insertContent; tick() flushes the DOM update
      // (avoids waitFor, which can't poll under fake timers).
      expect(container.querySelector('.mention-suggest')).toBeTruthy()
      const instantItems = Array.from(
        container.querySelectorAll('.mention-suggest-item')
      ).map((el) => el.textContent?.replace(/^@/, ''))
      expect(instantItems).toEqual(
        expect.arrayContaining(['Alice', 'Aardvark', 'Charlie'])
      )

      // Clear mock calls counted so far (the mount cache-seed) so the assertion
      // targets only the debounced server refine.
      mocks.distinctOwners.mockClear()

      // No server refine yet (within the debounce window).
      await vi.advanceTimersByTimeAsync(50)
      expect(mocks.distinctOwners).not.toHaveBeenCalled()

      // Cross the 120ms debounce; the prefix-filtered server refine fires.
      await vi.advanceTimersByTimeAsync(100)
      expect(mocks.distinctOwners).toHaveBeenCalledWith('a')

      // Popup has narrowed to prefix matches: Alice, Aardvark — NOT Charlie.
      const refinedItems = Array.from(
        container.querySelectorAll('.mention-suggest-item')
      ).map((el) => el.textContent?.replace(/^@/, ''))
      expect(refinedItems).toEqual(['Alice', 'Aardvark'])
      expect(refinedItems).not.toContain('Charlie')

      unmount()
    } finally {
      vi.useRealTimers()
    }
  })
})
