// Component coverage for the editor chrome relocated into VirtualScrollContainer
// (the EditorUtilityBar/FormatToolbar conditional + the floating action buttons).
// The heavy editor child + utility bar are stubbed (existing *.stub.svelte
// components) and the IPC/store seams are mocked, so this exercises only VSC's
// own conditional wiring — the contract the deleted EditorUtilityBar tests
// used to cover.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'
import { waitFor } from '@testing-library/dom'
import TipTapEditorStub from './TipTapEditor.stub.svelte'
import EditorUtilityBarStub from './editor/EditorUtilityBar.stub.svelte'
import MarkdownSourceViewerStub from './editor/MarkdownSourceViewer.stub.svelte'

const mocks = vi.hoisted(() => ({
  settings: {
    config: {
      ui: { show_format_toolbar: true },
      editor: { focus_mode: false },
      hotkeys: { toggle_view_mode: 'Ctrl+Shift+V' } as Record<string, string>
    }
  },
  toggleFocusMode: vi.fn(() => Promise.resolve(true)),
  toggleFormatToolbar: vi.fn(() => Promise.resolve(true)),
  onToggleViewMode: vi.fn()
}))

vi.mock('../../bindings/silt/app.js', () => ({
  FetchPageBlocks: vi.fn(() => Promise.resolve([])),
  RenamePage: vi.fn(() => Promise.resolve(undefined))
}))
vi.mock('@wailsio/runtime', () => ({
  // VSC stores the returned unsubscribe and calls it on destroy; return a noop.
  Events: {
    On: vi.fn(() => () => {})
  },
  Call: { ByID: vi.fn(), ByName: vi.fn() },
  CancellablePromise: class {
    then() {
      return this
    }
    catch() {
      return this
    }
    finally() {
      return this
    }
  },
  Create: {
    Nullable: (fn: any) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

vi.mock('./TipTapEditor.svelte', () => ({ default: TipTapEditorStub }))
vi.mock('./editor/EditorUtilityBar.svelte', () => ({
  default: EditorUtilityBarStub
}))
vi.mock('./editor/MarkdownSourceViewer.svelte', () => ({
  default: MarkdownSourceViewerStub
}))

vi.mock('../settings/store.svelte.ts', () => ({
  settings: mocks.settings,
  toggleFocusMode: mocks.toggleFocusMode,
  toggleFormatToolbar: mocks.toggleFormatToolbar
}))

import VirtualScrollContainer from './VirtualScrollContainer.svelte'

// Common props: viewMode is now a required prop owned by App.svelte's
// TabEntry (#195). onToggleViewMode is the callback the floating button fires.
const baseProps = () => ({
  notebook: 'NB',
  section: '',
  page: 'PG',
  viewMode: 'edit' as const,
  onToggleViewMode: mocks.onToggleViewMode
})

describe('VirtualScrollContainer editor chrome', () => {
  beforeEach(() => {
    mocks.toggleFocusMode.mockClear()
    mocks.toggleFormatToolbar.mockClear()
    mocks.onToggleViewMode.mockClear()
    mocks.settings.config.ui.show_format_toolbar = true
    // Reset the hotkey (one test remaps it) so test order can't bleed.
    mocks.settings.config.hotkeys = { toggle_view_mode: 'Ctrl+Shift+V' }
  })
  afterEach(() => cleanup())

  it('renders the EditorUtilityBar in edit mode with the toolbar enabled', () => {
    render(VirtualScrollContainer, { props: baseProps() })
    expect(screen.getByTestId('editor-utility-bar-stub')).toBeInTheDocument()
  })

  it('hides the EditorUtilityBar in source view mode', () => {
    render(VirtualScrollContainer, {
      props: { ...baseProps(), viewMode: 'source' }
    })
    expect(screen.queryByTestId('editor-utility-bar-stub')).toBeNull()
    // Source view: the read-only markdown projection renders in place of the
    // editor (#171/#194).
    expect(screen.getByTestId('markdown-source-stub')).toBeInTheDocument()
    // The toggle is a toggle button: a STABLE accessible name + aria-pressed
    // conveys state (no dynamic-label/pressed redundancy). The title carries
    // the contextual action + the live (remappable) hotkey.
    const toggle = screen.getByRole('button', { name: 'Toggle source view' })
    expect(toggle).toHaveAttribute('aria-pressed', 'true')
    expect(toggle).toHaveAttribute('title', 'View Rich Text (Ctrl+Shift+V)')
    expect(toggle).toHaveAttribute('aria-keyshortcuts', 'Ctrl+Shift+V')
  })

  it('mounts TipTapEditor in edit mode but tears it down in source mode (#178)', () => {
    // Edit mode: the full editor (ProseMirror + NodeViews) is mounted.
    const { rerender } = render(VirtualScrollContainer, {
      props: baseProps()
    })
    expect(screen.getByTestId('tiptap-stub')).toBeInTheDocument()
    expect(screen.queryByTestId('markdown-source-stub')).toBeNull()

    // Source mode: TipTapEditor is NOT mounted (Svelte destroyed it), so a tab
    // held in Source view pays no editor memory cost. Only the read-only
    // markdown projection is present.
    rerender({ ...baseProps(), viewMode: 'source' })
    expect(screen.queryByTestId('tiptap-stub')).toBeNull()
    expect(screen.getByTestId('markdown-source-stub')).toBeInTheDocument()
  })

  it('hides the EditorUtilityBar when show_format_toolbar is false', () => {
    mocks.settings.config.ui.show_format_toolbar = false
    render(VirtualScrollContainer, { props: baseProps() })
    expect(screen.queryByTestId('editor-utility-bar-stub')).toBeNull()
  })

  it('renders the floating toggle buttons and dispatches their handlers', async () => {
    render(VirtualScrollContainer, { props: baseProps() })
    await fireEvent.click(
      screen.getByRole('button', { name: 'Toggle Focus Mode' })
    )
    expect(mocks.toggleFocusMode).toHaveBeenCalledTimes(1)
    await fireEvent.click(
      screen.getByRole('button', { name: 'Toggle Formatting Toolbar' })
    )
    expect(mocks.toggleFormatToolbar).toHaveBeenCalledTimes(1)
    // The view-mode button fires the onToggleViewMode callback (#195) — App
    // owns the per-tab state now, not a module store.
    await fireEvent.click(
      screen.getByRole('button', { name: 'Toggle source view' })
    )
    expect(mocks.onToggleViewMode).toHaveBeenCalledTimes(1)
  })

  it('reads the view-mode hotkey live from settings (no stale shortcut text)', () => {
    // Remap the hotkey; the tooltip + aria-keyshortcuts must follow.
    mocks.settings.config.hotkeys = { toggle_view_mode: 'Ctrl+E' }
    render(VirtualScrollContainer, { props: baseProps() })
    const toggle = screen.getByRole('button', { name: 'Toggle source view' })
    expect(toggle).toHaveAttribute('aria-keyshortcuts', 'Ctrl+E')
    expect(toggle.getAttribute('title')).toContain('(Ctrl+E)')
  })

  it('announces the view-mode button state via aria-pressed', () => {
    render(VirtualScrollContainer, {
      props: { ...baseProps(), viewMode: 'source' }
    })
    const btn = screen.getByRole('button', { name: 'Toggle source view' })
    expect(btn).toHaveAttribute('aria-pressed', 'true')
    expect(btn).toHaveAttribute('aria-keyshortcuts', 'Ctrl+Shift+V')
  })

  it('stays silent on dirty/pending (debounce is not Saving…) #546', async () => {
    render(VirtualScrollContainer, { props: baseProps() })
    await fireEvent.click(screen.getByTestId('tiptap-stub-emit-dirty'))
    expect(screen.queryByText('Saving…')).toBeNull()
    expect(screen.queryByText('Saved')).toBeNull()
    expect(screen.queryByText('Save failed')).toBeNull()
  })

  it('shows polite Saving… while a write is in flight #546', async () => {
    render(VirtualScrollContainer, { props: baseProps() })
    await fireEvent.click(screen.getByTestId('tiptap-stub-emit-saving'))
    const status = screen.getByText('Saving…')
    const live = status.closest('[aria-live]')
    expect(live?.getAttribute('aria-live')).toBe('polite')
  })

  it('shows polite Saved on success confirmation #546', async () => {
    render(VirtualScrollContainer, { props: baseProps() })
    await fireEvent.click(screen.getByTestId('tiptap-stub-emit-saved'))
    const status = screen.getByText('Saved')
    const live = status.closest('[aria-live]')
    expect(live?.getAttribute('aria-live')).toBe('polite')
  })

  it('shows assertive Save failed when the editor reports an error', async () => {
    render(VirtualScrollContainer, { props: baseProps() })
    await fireEvent.click(screen.getByTestId('tiptap-stub-emit-error'))
    const status = screen.getByText('Save failed')
    expect(status).toBeTruthy()
    const live = status.closest('[aria-live]')
    expect(live?.getAttribute('aria-live')).toBe('assertive')
  })
})

describe('Edit↔Source scroll preservation (#319)', () => {
  // jsdom has no layout, so back scrollTop/scrollHeight with a controlled mock
  // scoped to this describe block (restored in afterEach). All elements share
  // one value, which is fine here — only the scroll container reads/writes it.
  let scrollTopVal = 0
  let scrollHeightVal = 1000
  const origScrollTop = Object.getOwnPropertyDescriptor(
    HTMLElement.prototype,
    'scrollTop'
  )
  const origScrollHeight = Object.getOwnPropertyDescriptor(
    HTMLElement.prototype,
    'scrollHeight'
  )

  beforeEach(() => {
    mocks.onToggleViewMode.mockClear()
    scrollTopVal = 0
    scrollHeightVal = 1000
    Object.defineProperty(HTMLElement.prototype, 'scrollTop', {
      configurable: true,
      get() {
        return scrollTopVal
      },
      set(v: number) {
        scrollTopVal = v
      }
    })
    Object.defineProperty(HTMLElement.prototype, 'scrollHeight', {
      configurable: true,
      get() {
        return scrollHeightVal
      }
    })
  })
  afterEach(() => {
    if (origScrollTop)
      Object.defineProperty(HTMLElement.prototype, 'scrollTop', origScrollTop)
    if (origScrollHeight)
      Object.defineProperty(
        HTMLElement.prototype,
        'scrollHeight',
        origScrollHeight
      )
    cleanup()
  })

  it('restores the Edit scroll offset after an Edit→Source→Edit round-trip', async () => {
    const { rerender } = render(VirtualScrollContainer, {
      props: baseProps()
    })
    // User scrolled down in Edit mode.
    scrollTopVal = 480
    // Leave Edit: $effect.pre captures 480 before the editor unmounts.
    rerender({ ...baseProps(), viewMode: 'source' })
    expect(screen.getByTestId('markdown-source-stub')).toBeInTheDocument()
    // Simulate the fresh editor remount starting back at the top.
    scrollTopVal = 0
    // Return to Edit: the remounted editor signals readiness → restore.
    rerender(baseProps())
    expect(screen.getByTestId('tiptap-stub')).toBeInTheDocument()
    await waitFor(() => {
      expect(scrollTopVal).toBe(480)
    })
  })

  it('clamps a stale offset that exceeds the current scroll height', async () => {
    const { rerender } = render(VirtualScrollContainer, {
      props: baseProps()
    })
    scrollTopVal = 900
    rerender({ ...baseProps(), viewMode: 'source' })
    // Doc shortened while in Source view (autosave/fsnotify external edit).
    scrollHeightVal = 300
    scrollTopVal = 0
    rerender(baseProps())
    await waitFor(() => {
      // Clamped to the shorter height — no overscroll, no crash.
      expect(scrollTopVal).toBe(300)
    })
  })

  it('does not force-scroll on a cold Edit open (no prior Source detour)', async () => {
    scrollTopVal = 0
    render(VirtualScrollContainer, { props: baseProps() })
    // No edit→source transition happened, so pendingRestore stays false and
    // the readiness handler is a no-op (target-block nav owns cold opens).
    await new Promise((r) => setTimeout(r, 0))
    expect(scrollTopVal).toBe(0)
  })
})

describe('Edit↔Source caret restoration (#331)', () => {
  let scrollTopVal = 0
  let scrollHeightVal = 1000
  const origScrollTop = Object.getOwnPropertyDescriptor(
    HTMLElement.prototype,
    'scrollTop'
  )
  const origScrollHeight = Object.getOwnPropertyDescriptor(
    HTMLElement.prototype,
    'scrollHeight'
  )

  function seedCaret(blockId: string, from: number, contentStart: number) {
    ;(globalThis as any).__tiptapStubSeed = {
      from,
      $from: {
        depth: 1,
        pos: from,
        start: (d: number) => (d === 1 ? contentStart : 0),
        node: (d: number) =>
          d === 1
            ? { type: { name: 'noteBlock' }, attrs: { id: blockId } }
            : { type: { name: 'doc' }, attrs: {} }
      }
    }
  }

  function seedDoc(blocks: { id: string; pos: number; contentSize: number }[]) {
    ;(globalThis as any).__tiptapStubDoc = {
      descendants(f: (node: any, pos: number) => boolean | void) {
        for (const b of blocks) {
          if (
            f(
              { attrs: { id: b.id }, content: { size: b.contentSize } },
              b.pos
            ) === false
          ) {
            break
          }
        }
      }
    }
  }

  beforeEach(() => {
    mocks.onToggleViewMode.mockClear()
    scrollTopVal = 0
    scrollHeightVal = 1000
    delete (globalThis as any).__tiptapStubSeed
    delete (globalThis as any).__tiptapStubDoc
    delete (globalThis as any).__tiptapStubSelection
    Object.defineProperty(HTMLElement.prototype, 'scrollTop', {
      configurable: true,
      get() {
        return scrollTopVal
      },
      set(v: number) {
        scrollTopVal = v
      }
    })
    Object.defineProperty(HTMLElement.prototype, 'scrollHeight', {
      configurable: true,
      get() {
        return scrollHeightVal
      }
    })
  })
  afterEach(() => {
    if (origScrollTop)
      Object.defineProperty(HTMLElement.prototype, 'scrollTop', origScrollTop)
    if (origScrollHeight)
      Object.defineProperty(
        HTMLElement.prototype,
        'scrollHeight',
        origScrollHeight
      )
    delete (globalThis as any).__tiptapStubSeed
    delete (globalThis as any).__tiptapStubDoc
    delete (globalThis as any).__tiptapStubSelection
    cleanup()
  })

  it('restores selection after Edit→Source→Edit when the block still exists', async () => {
    // contentStart 1, from 7 → offset 6; remount doc block at pos 0 size 20
    // → resolve to 0 + 1 + 6 = 7
    seedCaret('block-a', 7, 1)
    seedDoc([{ id: 'block-a', pos: 0, contentSize: 20 }])

    const { rerender } = render(VirtualScrollContainer, {
      props: baseProps()
    })
    scrollTopVal = 320
    rerender({ ...baseProps(), viewMode: 'source' })
    scrollTopVal = 0
    // Clear seed so remount doesn't re-bind capture selection; keep doc for resolve.
    delete (globalThis as any).__tiptapStubSeed
    rerender(baseProps())

    await waitFor(() => {
      expect((globalThis as any).__tiptapStubSelection).toBe(7)
    })
    await waitFor(() => {
      expect(scrollTopVal).toBe(320)
    })
  })

  it('still restores scroll when the block id is missing (scroll-only fallback)', async () => {
    seedCaret('stale-id', 5, 1)
    seedDoc([{ id: 'other', pos: 0, contentSize: 10 }])

    const { rerender } = render(VirtualScrollContainer, {
      props: baseProps()
    })
    scrollTopVal = 200
    rerender({ ...baseProps(), viewMode: 'source' })
    scrollTopVal = 0
    delete (globalThis as any).__tiptapStubSeed
    rerender(baseProps())

    await waitFor(() => {
      expect(scrollTopVal).toBe(200)
    })
    // No matching block → setTextSelection never called (or not with a resolve).
    expect((globalThis as any).__tiptapStubSelection).toBeUndefined()
  })

  it('clamps caret offset when the block content shrank', async () => {
    // offset 50, content size 4 → clamp to 4 → pos 0+1+4 = 5
    seedCaret('block-a', 51, 1)
    seedDoc([{ id: 'block-a', pos: 0, contentSize: 4 }])

    const { rerender } = render(VirtualScrollContainer, {
      props: baseProps()
    })
    scrollTopVal = 100
    rerender({ ...baseProps(), viewMode: 'source' })
    scrollTopVal = 0
    delete (globalThis as any).__tiptapStubSeed
    rerender(baseProps())

    await waitFor(() => {
      expect((globalThis as any).__tiptapStubSelection).toBe(5)
    })
  })

  it('does not force a caret jump on a cold Edit open', async () => {
    seedCaret('block-a', 9, 1)
    seedDoc([{ id: 'block-a', pos: 0, contentSize: 20 }])
    render(VirtualScrollContainer, { props: baseProps() })
    await new Promise((r) => setTimeout(r, 0))
    expect((globalThis as any).__tiptapStubSelection).toBeUndefined()
  })
})

// Wiki-link [[Page#Heading]] scroll-to-heading + retry logic (#545 harden).
// Exercises tryScrollToTarget: success, retry-after-load, give-up, and
// block-id scroll.
describe('VirtualScrollContainer heading/block scroll (#545)', () => {
  let scrollIntoViewMock: ReturnType<typeof vi.fn>

  const headerBlock = {
    id: 'hdr-1111-2222-3333-4444',
    parent_id: '',
    type: 'HEADER',
    depth: 0,
    raw_text: '## Goals',
    clean_text: '## Goals',
    status: '',
    owner: '',
    start_date: '',
    due_date: '',
    priority: 3,
    line_number: 1,
    file_date: '2026-07-13'
  }

  // The module-level vi.mock for bindings is already hoisted above. We
  // re-import FetchPageBlocks inside the test to override its return value.
  beforeEach(() => {
    scrollIntoViewMock = vi.fn()
    Element.prototype.scrollIntoView = scrollIntoViewMock as any
    // Inject a DOM element matching the header block's data-id so
    // querySelector('[data-id="..."]') finds it (the TipTapEditor stub
    // doesn't render real block DOM).
    const el = document.createElement('div')
    el.setAttribute('data-id', 'hdr-1111-2222-3333-4444')
    el.id = 'scroll-target'
    document.body.appendChild(el)
  })
  afterEach(() => {
    document.getElementById('scroll-target')?.remove()
    cleanup()
  })

  it('scrolls to a matching HEADER on targetHeading + targetKey', async () => {
    const { FetchPageBlocks } = await import('../../bindings/silt/app.js')
    vi.mocked(FetchPageBlocks).mockResolvedValue([headerBlock] as any)

    render(VirtualScrollContainer, {
      props: {
        ...baseProps(),
        targetHeading: 'Goals',
        targetKey: 'heading:Goals:1'
      }
    })
    // Wait for blocks to load and the retry effect to fire.
    await waitFor(() => {
      expect(scrollIntoViewMock).toHaveBeenCalled()
    })
  })

  it('gives up after MAX_SCROLL_ATTEMPTS when no header matches', async () => {
    const { FetchPageBlocks } = await import('../../bindings/silt/app.js')
    vi.mocked(FetchPageBlocks).mockResolvedValue([
      { ...headerBlock, id: 'other', clean_text: 'Other' }
    ] as any)

    render(VirtualScrollContainer, {
      props: {
        ...baseProps(),
        targetHeading: 'NonExistent',
        targetKey: 'heading:NonExistent:1'
      }
    })
    // Wait a few ticks — the retry effect should fire and give up.
    await new Promise((r) => setTimeout(r, 50))
    expect(scrollIntoViewMock).not.toHaveBeenCalled()
  })

  it('scrolls to a block by targetBlockId when the block exists', async () => {
    const { FetchPageBlocks } = await import('../../bindings/silt/app.js')
    vi.mocked(FetchPageBlocks).mockResolvedValue([headerBlock] as any)

    render(VirtualScrollContainer, {
      props: {
        ...baseProps(),
        targetBlockId: 'hdr-1111-2222-3333-4444',
        targetKey: 'date:hdr:1'
      }
    })
    await waitFor(() => {
      expect(scrollIntoViewMock).toHaveBeenCalled()
    })
  })
})
