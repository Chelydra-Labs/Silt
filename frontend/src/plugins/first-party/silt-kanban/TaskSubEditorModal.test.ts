import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

// jsdom polyfills: Svelte 5 transitions call element.animate(); the glassy
// modal uses transition:fly. ProseMirror's scrollToSelection / coordsAtPos
// (fired by setContent + focus) resolve caret rects through
// Range.getClientRects, which jsdom omits — stub it the same way the main
// editor's context-menu test does. elementFromPoint is touched by the
// Placeholder viewport tracker during editor construction.
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
if (!Element.prototype.animate) {
  Element.prototype.animate = function () {
    return {
      cancel() {},
      finish() {},
      play() {},
      pause() {},
      reverse() {},
      commitStyles() {},
      addEventListener() {},
      removeEventListener() {},
      dispatchEvent() {
        return true
      },
      onfinish: null,
      oncancel: null,
      onremove: null,
      currentTime: 0,
      startTime: null,
      playbackRate: 1,
      playState: 'finished',
      replaceState: 'active',
      pending: false,
      id: '',
      effect: null,
      timeline: null,
      get finished() {
        return Promise.resolve()
      },
      get ready() {
        return Promise.resolve()
      }
    }
  } as unknown as Element['animate']
}

vi.mock('../../../../wailsjs/runtime/runtime.js', () => ({
  EventsOn: vi.fn(() => () => {})
}))

import TaskSubEditorModal from './TaskSubEditorModal.svelte'
import type { PluginContext } from '../../sdk'
import { v2CtxStubs } from '../../test-helpers'

const mocks = vi.hoisted(() => ({
  fetchSubtree: vi.fn(),
  saveSubtreeBlocks: vi.fn()
}))

function makeCtx(overrides: Partial<PluginContext> = {}): PluginContext {
  return {
    activeNotebook: 'Work',
    activeSection: 'Journal',
    activePage: 'Daily',
    today: '2026-07-01',
    ...v2CtxStubs,
    fetchSubtree: mocks.fetchSubtree,
    saveSubtreeBlocks: mocks.saveSubtreeBlocks,
    ...overrides
  } as PluginContext
}

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

const BASE_PROPS = {
  blockId: 'task-1',
  notebook: 'Work',
  section: 'Journal',
  page: 'Daily',
  parentTaskText: 'Ship the feature'
}

describe('TaskSubEditorModal (#304)', () => {
  beforeEach(() => {
    mocks.fetchSubtree.mockReset().mockResolvedValue([])
    mocks.saveSubtreeBlocks.mockReset().mockResolvedValue(true)
  })

  afterEach(() => cleanup())

  it('renders a glassy dialog with the parent task title + breadcrumb', async () => {
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),

      onClose: () => {}
    })
    await flush()

    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    // The title is the parent task text.
    expect(dialog).toHaveTextContent('Ship the feature')
    // The breadcrumb carries notebook › section › page.
    expect(dialog).toHaveTextContent('Work')
    expect(dialog).toHaveTextContent('Journal')
    expect(dialog).toHaveTextContent('Daily')
  })

  it('mounts the TipTap editor once the subtree loads (loading state clears)', async () => {
    mocks.fetchSubtree.mockResolvedValue([
      {
        id: 'child-1',
        parent_id: 'task-1',
        type: 'NOTE',
        depth: 1,
        raw_text: '- a sub-note',
        clean_text: 'a sub-note',
        line_number: 2,
        file_date: '2026-07-01'
      }
    ])
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),

      onClose: () => {}
    })
    // The mocked fetch resolves immediately, so the loading state is
    // transient; assert it clears and the editor (ProseMirror content) takes
    // its place. waitFor tolerates the brief loading window.
    await vi.waitFor(() =>
      expect(screen.queryByText('Loading sub-notes…')).toBeNull()
    )
    // The TipTap editor renders a .ProseMirror element once mounted.
    await vi.waitFor(() => {
      expect(document.querySelector('.ProseMirror')).not.toBeNull()
    })
  })

  it('calls fetchSubtree with the block id on mount', async () => {
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),

      onClose: () => {}
    })
    await flush()

    expect(mocks.fetchSubtree).toHaveBeenCalledWith('task-1')
  })

  it('backdrop click closes the modal (calls onClose)', async () => {
    const onClose = vi.fn()
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),

      onClose
    })
    await flush()

    // Both the backdrop and the header X carry the same aria-label; the
    // backdrop is the first in DOM order and covers the full overlay.
    const closeBtns = screen.getAllByLabelText('Close sub-editor')
    await fireEvent.click(closeBtns[0])
    await flush()

    expect(onClose).toHaveBeenCalled()
  })

  it('Esc closes the modal', async () => {
    const onClose = vi.fn()
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),

      onClose
    })
    await flush()

    await fireEvent.keyDown(window, { key: 'Escape' })
    await flush()

    expect(onClose).toHaveBeenCalled()
  })

  it('the close (X) button calls onClose', async () => {
    const onClose = vi.fn()
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),

      onClose
    })
    await flush()

    // The header X button is the last Close-affordance in DOM order (the
    // backdrop button is first).
    const closeBtns = screen.getAllByLabelText('Close sub-editor')
    await fireEvent.click(closeBtns[closeBtns.length - 1])
    await flush()

    expect(onClose).toHaveBeenCalled()
  })
})
