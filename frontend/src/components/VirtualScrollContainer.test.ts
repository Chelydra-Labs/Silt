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
      editor: { focus_mode: false, show_word_count: false as boolean },
      hotkeys: { toggle_view_mode: 'Ctrl+Shift+V' }
    }
  },
  toggleFocusMode: vi.fn(() => Promise.resolve(true)),
  toggleFormatToolbar: vi.fn(() => Promise.resolve(true)),
  onToggleViewMode: vi.fn()
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    FetchPageBlocks: vi.fn(() => Promise.resolve([])),
    RenamePage: vi.fn(() => Promise.resolve(undefined))
  })
)
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
    Nullable: <T>(fn: T) => fn,
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
    mocks.settings.config.editor.focus_mode = false
    mocks.settings.config.editor.show_word_count = false
    // Reset the hotkey (one test remaps it) so test order can't bleed.
    mocks.settings.config.hotkeys = { toggle_view_mode: 'Ctrl+Shift+V' }
  })
  afterEach(() => cleanup())

  /**
   * jsdom does not apply :hover/:focus-within styles, so collapsed-tray
   * controls stay opacity:0. Query with hidden:true (they remain in the a11y
   * tree and tab order; real browsers expand via CSS focus-within/hover).
   */
  function actionButton(name: string | RegExp) {
    return screen.getByRole('button', { name, hidden: true })
  }

  it('renders the EditorUtilityBar in edit mode with the toolbar enabled', () => {
    render(VirtualScrollContainer, { props: baseProps() })
    expect(screen.getByTestId('editor-utility-bar-stub')).toBeInTheDocument()
  })

  it('keeps the page title but omits the duplicate document-canvas breadcrumb', () => {
    render(VirtualScrollContainer, {
      props: { ...baseProps(), section: 'Projects/Active' }
    })
    expect(
      screen.getByRole('heading', { name: 'Page title' })
    ).toHaveTextContent('PG')
    expect(screen.queryByRole('navigation')).not.toBeInTheDocument()
    expect(screen.queryByText('Projects/Active')).not.toBeInTheDocument()
  })

  it('keeps page actions available in source view mode', () => {
    render(VirtualScrollContainer, {
      props: { ...baseProps(), viewMode: 'source' }
    })
    expect(screen.getByTestId('editor-utility-bar-stub')).toBeInTheDocument()
    // Source view: the read-only markdown projection renders in place of the
    // editor (#171/#194).
    expect(screen.getByTestId('markdown-source-stub')).toBeInTheDocument()
    // Source pins the tray open, so the toggle is visible without hover.
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
    void rerender({ ...baseProps(), viewMode: 'source' })
    expect(screen.queryByTestId('tiptap-stub')).toBeNull()
    expect(screen.getByTestId('markdown-source-stub')).toBeInTheDocument()
  })

  it('keeps the utility bar when the formatting toolbar is disabled', () => {
    mocks.settings.config.ui.show_format_toolbar = false
    render(VirtualScrollContainer, { props: baseProps() })
    expect(screen.getByTestId('editor-utility-bar-stub')).toBeInTheDocument()
  })

  it('applies session note zoom on the content wrapper only (#843)', async () => {
    const { noteZoom } = await import('../lib/noteZoom.svelte')
    noteZoom.reset()
    render(VirtualScrollContainer, { props: baseProps() })
    const zoomRoot = screen.getByTestId('note-page-zoom')
    expect(zoomRoot).toHaveStyle({ zoom: '1' })
    noteZoom.zoomIn()
    await waitFor(() => {
      expect(screen.getByTestId('note-page-zoom')).toHaveStyle({ zoom: '1.1' })
    })
    // Ctrl+wheel on the scroll surface steps zoom (#843).
    const surface = zoomRoot.parentElement
    expect(surface).toBeTruthy()
    await fireEvent.wheel(surface!, { deltaY: 100, ctrlKey: true })
    await waitFor(() => {
      expect(screen.getByTestId('note-page-zoom')).toHaveStyle({ zoom: '1' })
    })
    noteZoom.reset()
  })

  it('hosts page zoom in the bottom status pill (outside content zoom)', async () => {
    const { noteZoom } = await import('../lib/noteZoom.svelte')
    noteZoom.reset()
    render(VirtualScrollContainer, { props: baseProps() })

    const pill = screen.getByTestId('editor-status-pill')
    expect(pill).toBeInTheDocument()
    // Pill must not live under the scaled content wrapper.
    expect(pill.closest('[data-testid="note-page-zoom"]')).toBeNull()

    expect(screen.getByRole('group', { name: 'Page zoom' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Zoom out' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Zoom in' })).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Zoom 100%. Reset to 100%' })
    ).toHaveTextContent('100%')

    await fireEvent.click(screen.getByRole('button', { name: 'Zoom in' }))
    await waitFor(() => {
      expect(
        screen.getByRole('button', { name: 'Zoom 110%. Reset to 100%' })
      ).toHaveTextContent('110%')
    })
    await fireEvent.click(
      screen.getByRole('button', { name: 'Zoom 110%. Reset to 100%' })
    )
    await waitFor(() => {
      expect(
        screen.getByRole('button', { name: 'Zoom 100%. Reset to 100%' })
      ).toHaveTextContent('100%')
    })
    noteZoom.reset()
  })

  it('shows 0 words in the status pill when word count is enabled', () => {
    mocks.settings.config.editor.show_word_count = true
    render(VirtualScrollContainer, { props: baseProps() })
    expect(screen.getByTestId('editor-status-pill')).toHaveTextContent(
      '0 words'
    )
  })

  it('hides word count text when the preference is off (zoom still shown)', () => {
    mocks.settings.config.editor.show_word_count = false
    render(VirtualScrollContainer, { props: baseProps() })
    const pill = screen.getByTestId('editor-status-pill')
    expect(pill).toBeInTheDocument()
    expect(pill).not.toHaveTextContent('words')
    expect(screen.getByRole('group', { name: 'Page zoom' })).toBeInTheDocument()
  })

  it('does not add page chrome for standalone task content', () => {
    mocks.settings.config.ui.show_format_toolbar = false
    render(VirtualScrollContainer, {
      props: { ...baseProps(), notebook: '.silt', page: 'tasks' }
    })
    expect(screen.queryByTestId('editor-utility-bar-stub')).toBeNull()
    expect(screen.queryByTestId('editor-status-pill')).toBeNull()
  })

  it('does not apply note zoom on standalone .silt tasks (#843)', async () => {
    const { noteZoom } = await import('../lib/noteZoom.svelte')
    noteZoom.setFactor(1.5)
    render(VirtualScrollContainer, {
      props: { ...baseProps(), notebook: '.silt', page: 'tasks' }
    })
    const zoomRoot = screen.getByTestId('note-page-zoom')
    expect(zoomRoot.getAttribute('style') ?? '').not.toMatch(/zoom/)
    noteZoom.reset()
  })

  it('collapses top-right actions by default; date glance stays reachable', () => {
    render(VirtualScrollContainer, { props: baseProps() })
    const cluster = screen.getByTestId('editor-float-actions')
    // Idle = not pinned. Expansion is CSS :hover/:focus-within (not
    // simulable via getComputedStyle in jsdom); pin class is the JS signal.
    expect(cluster).not.toHaveClass('editor-float-actions--pinned')
    expect(
      cluster.querySelector('.editor-float-actions__peek')
    ).toBeInTheDocument()
    expect(
      cluster.querySelector('.editor-float-actions__tray')
    ).toBeInTheDocument()
    // Controls remain in the DOM for keyboard tab order.
    expect(actionButton('Toggle Focus Mode')).toBeInTheDocument()
    // Date glance is outside the collapsing tray so it stays reachable.
    expect(
      screen.getByRole('button', { name: 'Pick a date' })
    ).toBeInTheDocument()
  })

  it('pins the action tray open in source view', () => {
    render(VirtualScrollContainer, {
      props: { ...baseProps(), viewMode: 'source' }
    })
    expect(screen.getByTestId('editor-float-actions')).toHaveClass(
      'editor-float-actions--pinned'
    )
    expect(
      screen.getByRole('button', { name: 'Toggle source view' })
    ).toBeInTheDocument()
  })

  it('renders the floating toggle buttons and dispatches their handlers', async () => {
    render(VirtualScrollContainer, { props: baseProps() })
    await fireEvent.click(actionButton('Toggle Focus Mode'))
    expect(mocks.toggleFocusMode).toHaveBeenCalledTimes(1)
    await fireEvent.click(actionButton('Toggle Formatting Toolbar'))
    expect(mocks.toggleFormatToolbar).toHaveBeenCalledTimes(1)
    // The view-mode button fires the onToggleViewMode callback (#195) — App
    // owns the per-tab state now, not a module store.
    await fireEvent.click(actionButton('Toggle source view'))
    expect(mocks.onToggleViewMode).toHaveBeenCalledTimes(1)
  })

  it('reads the view-mode hotkey live from settings (no stale shortcut text)', () => {
    // Remap the hotkey; the tooltip + aria-keyshortcuts must follow.
    mocks.settings.config.hotkeys = { toggle_view_mode: 'Ctrl+E' }
    render(VirtualScrollContainer, { props: baseProps() })
    const toggle = actionButton('Toggle source view')
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

  it('stays silent while a write is in flight #546', async () => {
    render(VirtualScrollContainer, { props: baseProps() })
    await fireEvent.click(screen.getByTestId('tiptap-stub-emit-saving'))
    expect(screen.queryByText('Saving…')).toBeNull()
  })

  it('stays silent on success confirmation #546', async () => {
    render(VirtualScrollContainer, { props: baseProps() })
    await fireEvent.click(screen.getByTestId('tiptap-stub-emit-saved'))
    expect(screen.queryByText('Saved')).toBeNull()
    expect(screen.queryByText('✓ Saved')).toBeNull()
  })

  it('announces the save error via a persistent assertive live region', async () => {
    render(VirtualScrollContainer, { props: baseProps() })
    await fireEvent.click(screen.getByTestId('tiptap-stub-emit-error'))
    // The visible pill labels the failure.
    expect(screen.getByText('Save failed')).toBeInTheDocument()
    // A stable assertive live region carries the error message for screen
    // readers. A fresh-mount live block (the pill appearing only on error)
    // can be missed by some screen readers, so the text lives in a persistent
    // region rather than on the pill itself.
    const live = document.querySelector('[aria-live="assertive"]')
    expect(live?.textContent).toContain('disk full')
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
    void rerender({ ...baseProps(), viewMode: 'source' })
    expect(screen.getByTestId('markdown-source-stub')).toBeInTheDocument()
    // Simulate the fresh editor remount starting back at the top.
    scrollTopVal = 0
    // Return to Edit: the remounted editor signals readiness → restore.
    void rerender(baseProps())
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
    void rerender({ ...baseProps(), viewMode: 'source' })
    // Doc shortened while in Source view (autosave/fsnotify external edit).
    scrollHeightVal = 300
    scrollTopVal = 0
    void rerender(baseProps())
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
    ;(globalThis as unknown as Record<string, unknown>).__tiptapStubSeed = {
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
    ;(globalThis as unknown as Record<string, unknown>).__tiptapStubDoc = {
      descendants(
        f: (
          node: { attrs?: { id?: string }; content?: { size: number } },
          pos: number
        ) => boolean | void
      ) {
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
    delete (globalThis as unknown as Record<string, unknown>).__tiptapStubSeed
    delete (globalThis as unknown as Record<string, unknown>).__tiptapStubDoc
    delete (globalThis as unknown as Record<string, unknown>)
      .__tiptapStubSelection
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
    delete (globalThis as unknown as Record<string, unknown>).__tiptapStubSeed
    delete (globalThis as unknown as Record<string, unknown>).__tiptapStubDoc
    delete (globalThis as unknown as Record<string, unknown>)
      .__tiptapStubSelection
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
    void rerender({ ...baseProps(), viewMode: 'source' })
    scrollTopVal = 0
    // Clear seed so remount doesn't re-bind capture selection; keep doc for resolve.
    delete (globalThis as unknown as Record<string, unknown>).__tiptapStubSeed
    void rerender(baseProps())

    await waitFor(() => {
      expect(
        (globalThis as unknown as Record<string, unknown>).__tiptapStubSelection
      ).toBe(7)
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
    void rerender({ ...baseProps(), viewMode: 'source' })
    scrollTopVal = 0
    delete (globalThis as unknown as Record<string, unknown>).__tiptapStubSeed
    void rerender(baseProps())

    await waitFor(() => {
      expect(scrollTopVal).toBe(200)
    })
    // No matching block → setTextSelection never called (or not with a resolve).
    expect(
      (globalThis as unknown as Record<string, unknown>).__tiptapStubSelection
    ).toBeUndefined()
  })

  it('clamps caret offset when the block content shrank', async () => {
    // offset 50, content size 4 → clamp to 4 → pos 0+1+4 = 5
    seedCaret('block-a', 51, 1)
    seedDoc([{ id: 'block-a', pos: 0, contentSize: 4 }])

    const { rerender } = render(VirtualScrollContainer, {
      props: baseProps()
    })
    scrollTopVal = 100
    void rerender({ ...baseProps(), viewMode: 'source' })
    scrollTopVal = 0
    delete (globalThis as unknown as Record<string, unknown>).__tiptapStubSeed
    void rerender(baseProps())

    await waitFor(() => {
      expect(
        (globalThis as unknown as Record<string, unknown>).__tiptapStubSelection
      ).toBe(5)
    })
  })

  it('does not force a caret jump on a cold Edit open', async () => {
    seedCaret('block-a', 9, 1)
    seedDoc([{ id: 'block-a', pos: 0, contentSize: 20 }])
    render(VirtualScrollContainer, { props: baseProps() })
    await new Promise((r) => setTimeout(r, 0))
    expect(
      (globalThis as unknown as Record<string, unknown>).__tiptapStubSelection
    ).toBeUndefined()
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
    Element.prototype.scrollIntoView =
      scrollIntoViewMock as unknown as typeof Element.prototype.scrollIntoView
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
    const { FetchPageBlocks } = await import('$silt-app')
    vi.mocked(FetchPageBlocks).mockResolvedValue([headerBlock] as never)

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
    const { FetchPageBlocks } = await import('$silt-app')
    vi.mocked(FetchPageBlocks).mockResolvedValue([
      { ...headerBlock, id: 'other', clean_text: 'Other' }
    ] as never)

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
    const { FetchPageBlocks } = await import('$silt-app')
    vi.mocked(FetchPageBlocks).mockResolvedValue([headerBlock] as never)

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
