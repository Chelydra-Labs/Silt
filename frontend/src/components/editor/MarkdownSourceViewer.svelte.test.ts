// Component coverage for the Source view (#194 Shiki highlighting, #171 base,
// #660 editable). The Shiki call is mocked so the test is deterministic and
// never depends on WASM/grammar loading in jsdom; the contract under test is
// the fallback (plain text until the highlighter resolves + on error),
// theme-change re-highlight, the Copy button, line numbers, and ARIA roles.
// Read-only highlight paths use editable={false}; default editable mode uses
// a textarea. Editable seeds prefer fetchPageMarkdown over reconstruct.
import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'

const mocks = vi.hoisted(() => ({
  themeState: {
    mode: 'dark',
    darkTokens: {
      '--color-text-primary': '#eee',
      '--color-surface-panel': '#111'
    },
    lightTokens: {
      '--color-text-primary': '#111',
      '--color-surface-panel': '#eee'
    }
  },
  // The mock highlighter: resolves to a fixed span, or rejects, or never
  // resolves (pending) — each test picks the behaviour.
  highlight: vi.fn(),
  savePageMarkdown: vi.fn(),
  fetchPageMarkdown: vi.fn(),
  AcquireFocusLock: vi.fn().mockResolvedValue(undefined),
  ReleaseFocusLock: vi.fn().mockResolvedValue(undefined),
  RefreshFocusLock: vi.fn().mockResolvedValue(undefined)
}))

vi.mock('../../theme/store.svelte', () => ({ themeState: mocks.themeState }))
vi.mock('../../lib/editor/useMarkdownHighlighter', () => ({
  // Forward both args so tests can assert the theme Shiki would receive.
  // tokensToShikiTheme runs for real (pure); only the async Shiki call is mocked.
  tokensToShikiTheme: (tokens: Record<string, string>, mode: string) => ({
    name: 'silt-source',
    type: mode,
    fg: tokens['--color-text-primary'] ?? '#eee',
    bg: tokens['--color-surface-panel'] ?? '#111',
    colors: {},
    tokenColors: []
  }),
  highlightMarkdown: (code: string, theme: unknown) =>
    mocks.highlight(code, theme)
}))
vi.mock('../../lib/editor/pageMarkdown', () => ({
  savePageMarkdown: (...args: unknown[]) => mocks.savePageMarkdown(...args),
  fetchPageMarkdown: (...args: unknown[]) => mocks.fetchPageMarkdown(...args)
}))
vi.mock('$silt-app', () =>
  createAppIpcMocks({
    AcquireFocusLock: (...args: unknown[]) => mocks.AcquireFocusLock(...args),
    ReleaseFocusLock: (...args: unknown[]) => mocks.ReleaseFocusLock(...args),
    RefreshFocusLock: (...args: unknown[]) => mocks.RefreshFocusLock(...args)
  })
)

import MarkdownSourceViewer from './MarkdownSourceViewer.svelte'
import type { ParsedBlock } from '../../lib/editor/types'

function mkBlock(
  text: string,
  opts: { depth?: number; clean?: string } = {}
): ParsedBlock {
  return {
    id: 'b-' + Math.random().toString(36).slice(2),
    parent_id: '',
    type: 'NOTE',
    depth: opts.depth ?? 0,
    raw_text: text,
    clean_text: opts.clean ?? text,
    line_number: 1
    // ParsedBlock carries many optional fields; only what the viewer reads
    // (raw_text, clean_text, depth) matters here.
  }
}

const BLOCKS: ParsedBlock[] = [
  mkBlock('# Heading'),
  mkBlock('**bold** and *italic*')
]

const FETCH_BODY = '# From disk\n\nFetched body content'

const ro = { editable: false as const }

describe('MarkdownSourceViewer', () => {
  beforeEach(() => {
    mocks.highlight.mockReset()
    mocks.savePageMarkdown.mockReset()
    mocks.fetchPageMarkdown.mockReset()
    mocks.fetchPageMarkdown.mockResolvedValue(FETCH_BODY)
    mocks.AcquireFocusLock.mockClear()
    mocks.ReleaseFocusLock.mockClear()
    mocks.RefreshFocusLock.mockClear()
    mocks.themeState.mode = 'dark'
  })
  afterEach(() => cleanup())

  it('renders the plain markdown as a fallback before the highlighter resolves', () => {
    // Never-resolving highlighter simulates the lazy grammar load window.
    mocks.highlight.mockReturnValue(new Promise(() => {}))
    render(MarkdownSourceViewer, {
      props: { blocks: BLOCKS, filePath: 'Work/Section/Page.md', ...ro }
    })
    const code = document.querySelector('.source-code')!
    // The raw markdown text is present verbatim (no spans yet).
    expect(code.textContent).toContain('# Heading')
    expect(code.textContent).toContain('**bold**')
  })

  it('renders highlighted HTML once the highlighter resolves', async () => {
    mocks.highlight.mockResolvedValue(
      '<span style="color:#abc"># Heading</span>'
    )
    render(MarkdownSourceViewer, {
      props: { blocks: BLOCKS, filePath: 'Work/Section/Page.md', ...ro }
    })
    await waitFor(() => {
      const span = document.querySelector('.source-code span')
      expect(span).not.toBeNull()
      expect(span!.getAttribute('style')).toContain('#abc')
    })
  })

  it('falls back to plain text when the highlighter errors', async () => {
    mocks.highlight.mockRejectedValue(new Error('grammar load failed'))
    render(MarkdownSourceViewer, {
      props: { blocks: BLOCKS, filePath: 'Work/Section/Page.md', ...ro }
    })
    // The raw text survives the error path (highlightMarkdown returns null).
    await tick()
    await tick()
    const code = document.querySelector('.source-code')!
    expect(code.textContent).toContain('**bold**')
    expect(code.querySelector('span')).toBeNull()
  })

  it('passes a theme whose type matches the active mode to the highlighter', async () => {
    mocks.highlight.mockResolvedValue('<span>x</span>')
    render(MarkdownSourceViewer, {
      props: { blocks: BLOCKS, filePath: 'p.md', ...ro }
    })
    await waitFor(() => expect(mocks.highlight).toHaveBeenCalled())
    // The component resolves mode='dark' → dark theme type.
    const theme = mocks.highlight.mock.calls[0][1] as { type: string }
    expect(theme.type).toBe('dark')
  })

  it('re-highlights when the source content changes', async () => {
    mocks.highlight.mockResolvedValue('<span>highlighted</span>')
    const { rerender } = render(MarkdownSourceViewer, {
      props: { blocks: BLOCKS, filePath: 'p.md', ...ro }
    })
    await waitFor(() => expect(mocks.highlight).toHaveBeenCalledTimes(1))
    const callsAfterFirst = mocks.highlight.mock.calls.length
    // New blocks → new markdown → the $effect re-runs and re-highlights.
    await rerender({
      blocks: [mkBlock('# Other content')],
      filePath: 'p.md',
      ...ro
    })
    await waitFor(() =>
      expect(mocks.highlight.mock.calls.length).toBeGreaterThan(callsAfterFirst)
    )
    // The re-highlight saw the new source.
    const lastCall = mocks.highlight.mock.calls.at(-1)!
    expect(lastCall[0]).toContain('Other content')
  })

  it('renders a line-number gutter matching the line count', () => {
    mocks.highlight.mockReturnValue(new Promise(() => {}))
    render(MarkdownSourceViewer, {
      props: { blocks: BLOCKS, filePath: 'p.md', ...ro }
    })
    const nums = document.querySelectorAll('.line-num')
    // Two blocks → two reconstructed lines.
    expect(nums).toHaveLength(2)
    expect(nums[0].textContent).toBe('1')
    expect(nums[1].textContent).toBe('2')
  })

  it('copies the reconstructed markdown to the clipboard and confirms via aria-live', async () => {
    mocks.highlight.mockReturnValue(new Promise(() => {}))
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })
    render(MarkdownSourceViewer, {
      props: { blocks: BLOCKS, filePath: 'p.md', ...ro }
    })
    await fireEvent.click(
      screen.getByRole('button', { name: /copy markdown/i })
    )
    expect(writeText).toHaveBeenCalledTimes(1)
    expect(writeText.mock.calls[0][0]).toContain('# Heading')
    expect(writeText.mock.calls[0][0]).toContain('**bold**')
    // Success is announced to assistive tech (not a silent action).
    await waitFor(() =>
      expect(screen.getByRole('status')).toHaveTextContent(/copied/i)
    )
  })

  it('announces a copy failure when the clipboard is unavailable', async () => {
    mocks.highlight.mockReturnValue(new Promise(() => {}))
    const writeText = vi.fn().mockRejectedValue(new Error('denied'))
    Object.assign(navigator, { clipboard: { writeText } })
    render(MarkdownSourceViewer, {
      props: { blocks: BLOCKS, filePath: 'p.md', ...ro }
    })
    await fireEvent.click(
      screen.getByRole('button', { name: /copy markdown/i })
    )
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(/failed to copy/i)
    )
  })

  it('exposes the source body as a read-only document landmark', () => {
    mocks.highlight.mockReturnValue(new Promise(() => {}))
    render(MarkdownSourceViewer, {
      props: { blocks: BLOCKS, filePath: 'Work/Page.md', ...ro }
    })
    const doc = screen.getByRole('document')
    expect(doc.getAttribute('aria-label')).toBe('Source view of Work/Page.md')
  })

  it('re-highlights on an OS scheme change while in system mode (#194)', async () => {
    // jsdom has no matchMedia; install a controllable MQL whose `change`
    // listeners the component subscribes to. Direct assignment (with restore)
    // is used because jsdom may not define window.matchMedia, which would
    // make vi.spyOn throw.
    const listeners: Array<(e: { matches: boolean }) => void> = []
    const mql = {
      matches: false, // start in dark
      addEventListener: (
        _ev: string,
        fn: (e: { matches: boolean }) => void
      ): void => {
        listeners.push(fn)
      },
      removeEventListener: (
        _ev: string,
        fn: (e: { matches: boolean }) => void
      ): void => {
        const i = listeners.indexOf(fn)
        if (i >= 0) listeners.splice(i, 1)
      }
    }
    const origMatchMedia = (window as unknown as { matchMedia?: unknown })
      .matchMedia
    Object.assign(window, { matchMedia: () => mql })

    mocks.themeState.mode = 'system'
    mocks.highlight.mockResolvedValue('<span>x</span>')
    try {
      render(MarkdownSourceViewer, {
        props: { blocks: BLOCKS, filePath: 'p.md', ...ro }
      })
      await waitFor(() => expect(mocks.highlight).toHaveBeenCalled())
      // First highlight resolved the dark tokens (effectiveMode = dark).
      expect(mocks.highlight.mock.calls[0][1].type).toBe('dark')

      // Simulate the OS flipping to light. The component's MQL listener must
      // bump its reactive systemLight state → effectiveMode → re-highlight.
      mql.matches = true
      for (const fn of listeners) fn({ matches: true })
      await waitFor(() => {
        const last = mocks.highlight.mock.calls.at(-1)!
        expect(last[1].type).toBe('light')
      })
    } finally {
      Object.assign(window, { matchMedia: origMatchMedia })
    }
  })

  it('renders an editable textarea seeded from fetchPageMarkdown (#660)', async () => {
    render(MarkdownSourceViewer, {
      props: {
        blocks: BLOCKS,
        filePath: 'Work/Page.md',
        notebook: 'Work',
        section: '',
        page: 'Page'
      }
    })
    const ta = await waitFor(() =>
      screen.getByRole('textbox', { name: /markdown source/i })
    )
    expect(ta).toBeInstanceOf(HTMLTextAreaElement)
    await waitFor(() => {
      expect((ta as HTMLTextAreaElement).value).toContain(
        'Fetched body content'
      )
    })
    expect(mocks.fetchPageMarkdown).toHaveBeenCalledWith('Work', '', 'Page')
  })

  it('debounces savePageMarkdown on edit (#660)', async () => {
    vi.useFakeTimers()
    mocks.savePageMarkdown.mockResolvedValue([mkBlock('# Heading')])
    try {
      render(MarkdownSourceViewer, {
        props: {
          blocks: BLOCKS,
          filePath: 'Work/Page.md',
          notebook: 'Work',
          section: 'Sec',
          page: 'Page'
        }
      })
      await vi.advanceTimersByTimeAsync(0)
      await waitFor(() => expect(mocks.fetchPageMarkdown).toHaveBeenCalled())
      const ta = screen.getByRole('textbox', { name: /markdown source/i })
      await fireEvent.input(ta, { target: { value: '# Edited\nline2' } })
      expect(mocks.savePageMarkdown).not.toHaveBeenCalled()
      await vi.advanceTimersByTimeAsync(500)
      expect(mocks.savePageMarkdown).toHaveBeenCalledWith(
        'Work',
        'Sec',
        'Page',
        '# Edited\nline2'
      )
    } finally {
      vi.useRealTimers()
    }
  })

  it('blocks auto-save on external blocks change until Keep mine; Reload resets (#660)', async () => {
    vi.useFakeTimers()
    mocks.savePageMarkdown.mockResolvedValue([mkBlock('# Heading')])
    mocks.fetchPageMarkdown
      .mockResolvedValueOnce(FETCH_BODY)
      .mockResolvedValueOnce('# Reloaded from disk')
    try {
      const { rerender } = render(MarkdownSourceViewer, {
        props: {
          blocks: BLOCKS,
          filePath: 'Work/Page.md',
          notebook: 'Work',
          section: 'Sec',
          page: 'Page'
        }
      })
      await vi.advanceTimersByTimeAsync(0)
      await waitFor(() => expect(screen.getByRole('textbox')).toBeTruthy())
      const ta = screen.getByRole('textbox', { name: /markdown source/i })
      await fireEvent.input(ta, { target: { value: '# Local dirty edit' } })
      // External blocks change while dirty → conflict, no save.
      await rerender({
        blocks: [mkBlock('# Remote heading'), mkBlock('remote body')],
        filePath: 'Work/Page.md',
        notebook: 'Work',
        section: 'Sec',
        page: 'Page'
      })
      await waitFor(() =>
        expect(screen.getByRole('button', { name: /keep mine/i })).toBeTruthy()
      )
      await vi.advanceTimersByTimeAsync(500)
      expect(mocks.savePageMarkdown).not.toHaveBeenCalled()

      // Keep mine allows save of local buffer.
      await fireEvent.click(screen.getByRole('button', { name: /keep mine/i }))
      await vi.advanceTimersByTimeAsync(500)
      expect(mocks.savePageMarkdown).toHaveBeenCalledWith(
        'Work',
        'Sec',
        'Page',
        '# Local dirty edit'
      )
      mocks.savePageMarkdown.mockClear()

      // Dirty again, then Reload discards and re-fetches.
      await fireEvent.input(ta, { target: { value: '# Dirty again' } })
      await rerender({
        blocks: [mkBlock('# Another remote')],
        filePath: 'Work/Page.md',
        notebook: 'Work',
        section: 'Sec',
        page: 'Page'
      })
      await waitFor(() =>
        expect(screen.getByRole('button', { name: /reload/i })).toBeTruthy()
      )
      await fireEvent.click(screen.getByRole('button', { name: /reload/i }))
      await vi.advanceTimersByTimeAsync(0)
      await waitFor(() => {
        expect((ta as HTMLTextAreaElement).value).toContain(
          'Reloaded from disk'
        )
      })
      await vi.advanceTimersByTimeAsync(500)
      expect(mocks.savePageMarkdown).not.toHaveBeenCalled()
    } finally {
      vi.useRealTimers()
    }
  })

  it('does not show conflict when parent re-applies our own save while typing (#660)', async () => {
    vi.useFakeTimers()
    const savedBlocks = [mkBlock('# Saved body')]
    let resolveSave!: (v: typeof savedBlocks) => void
    mocks.savePageMarkdown.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveSave = resolve
        })
    )
    try {
      const { rerender } = render(MarkdownSourceViewer, {
        props: {
          blocks: BLOCKS,
          filePath: 'Work/Page.md',
          notebook: 'Work',
          section: 'Sec',
          page: 'Page',
          onBlocksSaved: (saved: ReturnType<typeof mkBlock>[]) => {
            void rerender({
              blocks: saved,
              filePath: 'Work/Page.md',
              notebook: 'Work',
              section: 'Sec',
              page: 'Page',
              onBlocksSaved: () => {}
            })
          }
        }
      })
      await vi.advanceTimersByTimeAsync(0)
      await waitFor(() => expect(screen.getByRole('textbox')).toBeTruthy())
      const ta = screen.getByRole('textbox', { name: /markdown source/i })
      await fireEvent.input(ta, { target: { value: '# First save' } })
      await vi.advanceTimersByTimeAsync(500)
      expect(mocks.savePageMarkdown).toHaveBeenCalled()
      // Type again while IPC is in flight.
      await fireEvent.input(ta, { target: { value: '# First save more' } })
      resolveSave(savedBlocks)
      await vi.advanceTimersByTimeAsync(0)
      await waitFor(() => {
        expect(screen.queryByRole('button', { name: /keep mine/i })).toBeNull()
      })
      expect((ta as HTMLTextAreaElement).value).toBe('# First save more')
    } finally {
      vi.useRealTimers()
    }
  })
})

// Phase 7 / #861 — local bounded editing history, keyboard undo/redo,
// Tab/Shift-Tab as history edits, and history boundaries on seed/reload/
// external replacement. fireEvent.input does not dispatch `beforeinput`, so
// each input lands as its own history entry (no coalescing) — exactly what
// these tests need to exercise multi-step undo deterministically.
describe('MarkdownSourceViewer editing history (#861)', () => {
  beforeEach(() => {
    mocks.highlight.mockReset()
    mocks.savePageMarkdown.mockReset()
    mocks.fetchPageMarkdown.mockReset()
    mocks.fetchPageMarkdown.mockResolvedValue(FETCH_BODY)
    mocks.savePageMarkdown.mockResolvedValue([mkBlock('# Saved')])
    mocks.AcquireFocusLock.mockClear()
    mocks.ReleaseFocusLock.mockClear()
    mocks.RefreshFocusLock.mockClear()
  })
  afterEach(() => cleanup())

  async function seed(expectedBody = FETCH_BODY) {
    render(MarkdownSourceViewer, {
      props: {
        blocks: BLOCKS,
        filePath: 'Work/Page.md',
        notebook: 'Work',
        section: 'Sec',
        page: 'Page'
      }
    })
    const ta = await waitFor(() =>
      screen.getByRole('textbox', { name: /markdown source/i })
    )
    await waitFor(() => {
      expect((ta as HTMLTextAreaElement).value).toContain(expectedBody)
    })
    return ta as HTMLTextAreaElement
  }

  it('exposes undo/redo buttons that track history availability', async () => {
    const ta = await seed()
    const undo = screen.getByRole('button', { name: 'Undo' })
    const redo = screen.getByRole('button', { name: 'Redo' })
    // Seeded buffer only — nothing to undo or redo yet.
    expect(undo).toBeDisabled()
    expect(redo).toBeDisabled()

    // One edit enables undo; redo stays disabled until we undo.
    await fireEvent.input(ta, { target: { value: 'edit-A' } })
    await tick()
    expect(undo).toBeEnabled()
    expect(redo).toBeDisabled()
    // aria-keyshortcuts is a space-separated list of chords only (no labels);
    // both Ctrl and Cmd platforms are listed.
    expect(undo).toHaveAttribute('aria-keyshortcuts', 'Control+Z Meta+Z')
    expect(redo).toHaveAttribute(
      'aria-keyshortcuts',
      'Control+Y Control+Shift+Z Meta+Shift+Z'
    )
    // Titles spell out every supported chord for discoverability.
    expect(undo).toHaveAttribute('title', 'Undo (Ctrl+Z / Cmd+Z)')
    expect(redo).toHaveAttribute(
      'title',
      'Redo (Ctrl+Y / Ctrl+Shift+Z / Cmd+Shift+Z)'
    )
  })

  it('undoes and redoes through multiple steps via the toolbar buttons', async () => {
    const ta = await seed()
    await fireEvent.input(ta, { target: { value: 'step-A' } })
    await fireEvent.input(ta, { target: { value: 'step-AB' } })
    await fireEvent.input(ta, { target: { value: 'step-ABC' } })
    await tick()

    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await tick()
    expect(ta.value).toBe('step-AB')
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await tick()
    expect(ta.value).toBe('step-A')
    // Bottom of the stack is the seeded FETCH_BODY.
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await tick()
    expect(ta.value).toBe(FETCH_BODY)
    expect(screen.getByRole('button', { name: 'Undo' })).toBeDisabled()

    await fireEvent.click(screen.getByRole('button', { name: 'Redo' }))
    await tick()
    expect(ta.value).toBe('step-A')
  })

  it('Ctrl+Z / Ctrl+Y / Ctrl+Shift+Z drive history from the keyboard', async () => {
    const ta = await seed()
    await fireEvent.input(ta, { target: { value: 'kb-A' } })
    await fireEvent.input(ta, { target: { value: 'kb-AB' } })
    await tick()

    await fireEvent.keyDown(ta, { key: 'z', ctrlKey: true })
    await tick()
    expect(ta.value).toBe('kb-A')

    // Ctrl+Y and Ctrl+Shift+Z both redo.
    await fireEvent.keyDown(ta, { key: 'y', ctrlKey: true })
    await tick()
    expect(ta.value).toBe('kb-AB')

    await fireEvent.keyDown(ta, { key: 'z', ctrlKey: true })
    await tick()
    expect(ta.value).toBe('kb-A')
    await fireEvent.keyDown(ta, { key: 'Z', ctrlKey: true, shiftKey: true })
    await tick()
    expect(ta.value).toBe('kb-AB')

    // Cmd chord works on macOS bindings.
    await fireEvent.keyDown(ta, { key: 'z', metaKey: true })
    await tick()
    expect(ta.value).toBe('kb-A')
  })

  it('typing after an undo clears the redo branch (redo invalidation)', async () => {
    const ta = await seed()
    await fireEvent.input(ta, { target: { value: 'A' } })
    await fireEvent.input(ta, { target: { value: 'AB' } })
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await tick()
    expect(ta.value).toBe('A')
    expect(screen.getByRole('button', { name: 'Redo' })).toBeEnabled()

    // New input branch — redo must no longer be available.
    await fireEvent.input(ta, { target: { value: 'A-newbranch' } })
    await tick()
    expect(screen.getByRole('button', { name: 'Redo' })).toBeDisabled()
    expect(ta.value).toBe('A-newbranch')
  })

  it('Tab inserts an indent at the caret and is a history edit (undo/redo)', async () => {
    mocks.fetchPageMarkdown.mockResolvedValue('line one\nline two')
    const ta = await seed('line one\nline two')
    // jsdom needs focus for textarea selection to take.
    ta.focus()
    ta.selectionStart = ta.selectionEnd = 'line one'.length
    expect(ta.selectionStart).toBe('line one'.length)
    await fireEvent.keyDown(ta, { key: 'Tab' })
    await tick()
    expect(ta.value).toBe('line one\t\nline two')
    // Caret advanced past the inserted tab.
    expect(ta.selectionStart).toBe('line one\t'.length)

    // Undo restores pre-Tab state.
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await tick()
    expect(ta.value).toBe('line one\nline two')

    // Redo brings the indent back.
    await fireEvent.click(screen.getByRole('button', { name: 'Redo' }))
    await tick()
    expect(ta.value).toBe('line one\t\nline two')
  })

  it('Shift+Tab removes a leading tab from the caret line and is history', async () => {
    mocks.fetchPageMarkdown.mockResolvedValue('\tindented\nplain')
    const ta = await seed('\tindented\nplain')
    ta.focus()
    // Caret anywhere inside the first line; dedent scans back to line start.
    ta.selectionStart = ta.selectionEnd = 4
    await fireEvent.keyDown(ta, { key: 'Tab', shiftKey: true })
    await tick()
    expect(ta.value).toBe('indented\nplain')
    // Caret shifted left by the one removed tab.
    expect(ta.selectionStart).toBe(3)

    // Undo restores the leading tab.
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await tick()
    expect(ta.value).toBe('\tindented\nplain')
  })

  it('Shift+Tab is a no-op (no history entry) when the line has no indent', async () => {
    mocks.fetchPageMarkdown.mockResolvedValue('no indent here')
    const ta = await seed('no indent here')
    ta.focus()
    ta.selectionStart = ta.selectionEnd = 5
    await fireEvent.keyDown(ta, { key: 'Tab', shiftKey: true })
    await tick()
    expect(ta.value).toBe('no indent here')
    // No edit recorded — undo stays unavailable.
    expect(screen.getByRole('button', { name: 'Undo' })).toBeDisabled()
  })

  it('Tab on a multi-line selection prefixes every covered line (no deletion)', async () => {
    // Regression for the destructive replace-selection bug: a 3-line
    // selection used to collapse to a single "\t". It must now indent each
    // covered line and keep the block selected so a follow-up Tab works.
    mocks.fetchPageMarkdown.mockResolvedValue('alpha\nbeta\ngamma')
    const ta = await seed('alpha\nbeta\ngamma')
    ta.focus()
    // Select from middle of "alpha" through middle of "gamma".
    ta.selectionStart = 2
    ta.selectionEnd = 'alpha\nbeta\ng'.length // 13
    await fireEvent.keyDown(ta, { key: 'Tab' })
    await tick()
    expect(ta.value).toBe('\talpha\n\tbeta\n\tgamma')
    // Selection extended to cover the new prefixes (whole block).
    expect(ta.selectionStart).toBe(0)
    expect(ta.selectionEnd).toBe('\talpha\n\tbeta\n\tg'.length)
    // Undo restores the original 3 lines in one step.
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await tick()
    expect(ta.value).toBe('alpha\nbeta\ngamma')
  })

  it('Tab then Tab again deepens the indent (selection preserved across presses)', async () => {
    mocks.fetchPageMarkdown.mockResolvedValue('one\ntwo\nthree')
    const ta = await seed('one\ntwo\nthree')
    ta.focus()
    ta.selectionStart = 0
    ta.selectionEnd = 'one\ntwo\nth'.length // 10
    await fireEvent.keyDown(ta, { key: 'Tab' })
    await tick()
    expect(ta.value).toBe('\tone\n\ttwo\n\tthree')
    // Second Tab deepens every covered line by another tab.
    await fireEvent.keyDown(ta, { key: 'Tab' })
    await tick()
    expect(ta.value).toBe('\t\tone\n\t\ttwo\n\t\tthree')
  })

  it('Tab ignores a trailing line when the selection ends exactly at its start', async () => {
    // VS Code / Sublime convention: if `end` sits at column 0 of a line,
    // the user did not select that line — leave it untouched.
    mocks.fetchPageMarkdown.mockResolvedValue('keep me\nuntouched')
    const ta = await seed('keep me\nuntouched')
    ta.focus()
    ta.selectionStart = 0
    ta.selectionEnd = 'keep me\n'.length // 8 — exactly at the newline boundary
    await fireEvent.keyDown(ta, { key: 'Tab' })
    await tick()
    expect(ta.value).toBe('\tkeep me\nuntouched')
  })

  it('Shift+Tab on a multi-line selection dedents every covered line', async () => {
    mocks.fetchPageMarkdown.mockResolvedValue('\tone\n\ttwo\n\tthree')
    const ta = await seed('\tone\n\ttwo\n\tthree')
    ta.focus()
    // Select the whole block.
    ta.selectionStart = 0
    ta.selectionEnd = '\tone\n\ttwo\n\tthree'.length
    await fireEvent.keyDown(ta, { key: 'Tab', shiftKey: true })
    await tick()
    expect(ta.value).toBe('one\ntwo\nthree')
    // Selection contracted by one removed tab per line.
    expect(ta.selectionStart).toBe(0)
    expect(ta.selectionEnd).toBe('one\ntwo\nthree'.length)

    // Undo restores the indented block.
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await tick()
    expect(ta.value).toBe('\tone\n\ttwo\n\tthree')
  })

  it('Shift+Tab on a mixed-indent selection removes one unit per line and skips blanks', async () => {
    // First line has 2-space indent, second line has a tab, third has none.
    mocks.fetchPageMarkdown.mockResolvedValue('  alpha\n\tbeta\nplain')
    const ta = await seed('  alpha\n\tbeta\nplain')
    ta.focus()
    ta.selectionStart = 0
    ta.selectionEnd = '  alpha\n\tbeta\nplain'.length
    await fireEvent.keyDown(ta, { key: 'Tab', shiftKey: true })
    await tick()
    // Each indented line lost one unit; the plain line was untouched.
    expect(ta.value).toBe('alpha\nbeta\nplain')
  })

  it('Shift+Tab multi-line is a no-op when no covered line has indent', async () => {
    mocks.fetchPageMarkdown.mockResolvedValue('alpha\nbeta\ngamma')
    const ta = await seed('alpha\nbeta\ngamma')
    ta.focus()
    ta.selectionStart = 0
    ta.selectionEnd = 'alpha\nbeta\n'.length
    await fireEvent.keyDown(ta, { key: 'Tab', shiftKey: true })
    await tick()
    expect(ta.value).toBe('alpha\nbeta\ngamma')
    expect(screen.getByRole('button', { name: 'Undo' })).toBeDisabled()
  })

  it('seed sets the initial history boundary (cannot undo past the seed)', async () => {
    const ta = await seed()
    await fireEvent.input(ta, { target: { value: 'only edit' } })
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await tick()
    expect(ta.value).toBe(FETCH_BODY)
    // Undo disabled at the seed.
    expect(screen.getByRole('button', { name: 'Undo' })).toBeDisabled()
  })

  it('Reload is a history boundary — prior edits cannot be undone back', async () => {
    mocks.fetchPageMarkdown
      .mockResolvedValueOnce(FETCH_BODY)
      .mockResolvedValueOnce('# Fresh from disk')
    const { rerender } = render(MarkdownSourceViewer, {
      props: {
        blocks: BLOCKS,
        filePath: 'Work/Page.md',
        notebook: 'Work',
        section: 'Sec',
        page: 'Page'
      }
    })
    const ta = (await waitFor(() =>
      screen.getByRole('textbox', { name: /markdown source/i })
    )) as HTMLTextAreaElement
    await waitFor(() => expect(ta.value).toContain('Fetched body content'))
    await fireEvent.input(ta, { target: { value: '# dirty local edit' } })
    await tick()
    expect(screen.getByRole('button', { name: 'Undo' })).toBeEnabled()

    // External change while dirty → conflict UI.
    await rerender({
      blocks: [mkBlock('# External heading'), mkBlock('remote body')],
      filePath: 'Work/Page.md',
      notebook: 'Work',
      section: 'Sec',
      page: 'Page'
    })
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /reload/i })).toBeTruthy()
    )
    await fireEvent.click(screen.getByRole('button', { name: /reload/i }))
    await waitFor(() => expect(ta.value).toBe('# Fresh from disk'))
    // History was reset — Undo/Redo are unavailable after the boundary.
    expect(screen.getByRole('button', { name: 'Undo' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Redo' })).toBeDisabled()
  })

  it('Keep mine preserves the local history branch', async () => {
    const { rerender } = render(MarkdownSourceViewer, {
      props: {
        blocks: BLOCKS,
        filePath: 'Work/Page.md',
        notebook: 'Work',
        section: 'Sec',
        page: 'Page'
      }
    })
    const ta = (await waitFor(() =>
      screen.getByRole('textbox', { name: /markdown source/i })
    )) as HTMLTextAreaElement
    await waitFor(() => expect(ta.value).toContain('Fetched body content'))
    await fireEvent.input(ta, { target: { value: 'keepme' } })
    await tick()
    expect(screen.getByRole('button', { name: 'Undo' })).toBeEnabled()

    // External change while dirty → conflict; Keep mine keeps buffer + history.
    await rerender({
      blocks: [mkBlock('# Remote change')],
      filePath: 'Work/Page.md',
      notebook: 'Work',
      section: 'Sec',
      page: 'Page'
    })
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /keep mine/i })).toBeTruthy()
    )
    await fireEvent.click(screen.getByRole('button', { name: /keep mine/i }))
    await tick()
    expect(ta.value).toBe('keepme')
    // History preserved: undo restores the seeded buffer.
    expect(screen.getByRole('button', { name: 'Undo' })).toBeEnabled()
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await tick()
    expect(ta.value).toBe(FETCH_BODY)
  })

  it('marks the buffer dirty and schedules save after an undo', async () => {
    vi.useFakeTimers()
    mocks.savePageMarkdown.mockResolvedValue([mkBlock('# Heading')])
    try {
      const ta = await seed()
      await fireEvent.input(ta, { target: { value: '# Edit one' } })
      await vi.advanceTimersByTimeAsync(500)
      expect(mocks.savePageMarkdown).toHaveBeenCalledTimes(1)
      mocks.savePageMarkdown.mockClear()
      // Undo moves buffer back to FETCH_BODY; that change is itself an edit
      // from the saved state, so it must schedule a save.
      await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
      await tick()
      await vi.advanceTimersByTimeAsync(500)
      expect(mocks.savePageMarkdown).toHaveBeenCalledWith(
        'Work',
        'Sec',
        'Page',
        FETCH_BODY
      )
    } finally {
      vi.useRealTimers()
    }
  })

  it('preserves caret position across an undo', async () => {
    const ta = await seed()
    await fireEvent.input(ta, { target: { value: '# Title\n\nBody' } })
    await tick()
    // Park the caret mid-line, then undo; restoreSelection should clamp to
    // the entry's recorded selection rather than collapsing to 0.
    ta.selectionStart = ta.selectionEnd = 3
    await fireEvent.input(ta, { target: { value: '# Ti' } })
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await tick()
    expect(ta.selectionStart).toBe(ta.selectionEnd)
    // Selection restored to the value the prior entry recorded (caret at
    // end of the typed "# Title..." prefix — not 0).
    expect(ta.selectionStart).toBeGreaterThan(0)
  })

  it('announces a meaningful undo/redo message via the stable live region', async () => {
    const ta = await seed()
    // FETCH_BODY is "# From disk\n\nFetched body content" (24 chars). Typing
    // a 5-char extension then undoing it yields a "removed 5 characters"
    // description — concrete enough for an AT user to know what happened.
    await fireEvent.input(ta, { target: { value: `${FETCH_BODY} extra` } })
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await tick()
    const live = document.querySelector('[aria-live="polite"].sr-only')
    expect(live?.textContent).toMatch(/undid:.*removed.*6 characters/i)
  })

  it('re-announces on rapid undo so AT users perceive every press', async () => {
    const ta = await seed()
    await fireEvent.input(ta, { target: { value: `${FETCH_BODY} a b c` } })
    await tick()
    await fireEvent.input(ta, { target: { value: `${FETCH_BODY} a b` } })
    await tick()
    await fireEvent.input(ta, { target: { value: `${FETCH_BODY} a` } })
    await tick()

    const live = document.querySelector('[aria-live="polite"].sr-only')!
    // MutationObserver captures every textContent mutation, including the
    // brief clear that `announceHistory` performs between identical
    // messages. Without that clear, AT would treat the second undo as a
    // no-op (no content change in the polite region).
    const observed: string[] = []
    const observer = new MutationObserver(() => {
      observed.push(live.textContent ?? '')
    })
    observer.observe(live, {
      characterData: true,
      childList: true,
      subtree: true
    })

    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await tick()
    await new Promise((r) => setTimeout(r, 0))
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await tick()
    await new Promise((r) => setTimeout(r, 0))
    observer.disconnect()

    // Final state is announced...
    expect(live.textContent).toMatch(/undid/i)
    // ...and the region went through an empty intermediate state between
    // the two presses — that is what re-fires AT announcement for an
    // otherwise-identical message string.
    expect(observed).toContain('')
  })

  it('renders a single scroll owner that fills the editor area (#861 layout)', async () => {
    await seed()
    const body = document.querySelector('.source-body')! as HTMLElement
    // source-body is the deliberate single scroll owner (overflow:auto).
    expect(body).toBeTruthy()
    // The textarea inside fills vertically (min-height:100%) so the body
    // never collapses to a few rows when the document is short.
    const ta = document.querySelector('.source-textarea')! as HTMLElement
    expect(ta).toBeTruthy()
    // The viewer fills its container (height:100%) — the parent chain
    // (page-zoom h-full in source mode) feeds that height down.
    const viewer = document.querySelector('.source-viewer')! as HTMLElement
    expect(viewer).toBeTruthy()
  })

  it('intercepts native undo/redo InputEvents and routes them to local history', async () => {
    // The Edit menu / IME may fire beforeinput with inputType 'historyUndo'
    // / 'historyRedo'. Left unhandled, native textarea undo would desync
    // the buffer from our explicit stack. Route them through our undo/redo.
    const ta = await seed()
    await fireEvent.input(ta, { target: { value: '# Step 1' } })
    await fireEvent.input(ta, { target: { value: '# Step 12' } })
    await tick()
    const before = ta.value

    // Dispatch a beforeinput with historyUndo inputType (cancellable).
    const evt = new InputEvent('beforeinput', {
      inputType: 'historyUndo',
      bubbles: true,
      cancelable: true
    })
    ta.dispatchEvent(evt)
    await tick()
    expect(evt.defaultPrevented).toBe(true)
    expect(ta.value).not.toBe(before)
    expect(ta.value).toBe('# Step 1')

    // And redo via inputType.
    const evt2 = new InputEvent('beforeinput', {
      inputType: 'historyRedo',
      bubbles: true,
      cancelable: true
    })
    ta.dispatchEvent(evt2)
    await tick()
    expect(evt2.defaultPrevented).toBe(true)
    expect(ta.value).toBe('# Step 12')
  })

  it('flushes the dirty buffer on unmount so edits are not lost', async () => {
    // The debounced save may be in flight when the user closes the tab.
    // onDestroy cancels the timer and writes the buffer directly so the
    // edit survives the teardown.
    mocks.savePageMarkdown.mockResolvedValue([mkBlock('# Saved')])
    const { unmount } = render(MarkdownSourceViewer, {
      props: {
        blocks: BLOCKS,
        filePath: 'Work/Page.md',
        notebook: 'Work',
        section: 'Sec',
        page: 'Page'
      }
    })
    const ta = (await waitFor(() =>
      screen.getByRole('textbox', { name: /markdown source/i })
    )) as HTMLTextAreaElement
    await waitFor(() => expect(ta.value).toContain('Fetched body content'))
    // Type and unmount before the 500ms debounce fires.
    await fireEvent.input(ta, { target: { value: '# unmount flush me' } })
    await tick()
    expect(mocks.savePageMarkdown).not.toHaveBeenCalled()

    unmount()
    // onDestroy's flush path writes the dirty buffer directly (no timer).
    await waitFor(() =>
      expect(mocks.savePageMarkdown).toHaveBeenCalledWith(
        'Work',
        'Sec',
        'Page',
        '# unmount flush me'
      )
    )
  })

  it('does not flush on unmount when there is nothing dirty to write', async () => {
    mocks.savePageMarkdown.mockResolvedValue([mkBlock('# Saved')])
    const { unmount } = render(MarkdownSourceViewer, {
      props: {
        blocks: BLOCKS,
        filePath: 'Work/Page.md',
        notebook: 'Work',
        section: 'Sec',
        page: 'Page'
      }
    })
    await waitFor(() =>
      expect(screen.getByRole('textbox', { name: /markdown source/i }))
    )
    // No edit — buffer is clean (the seed).
    unmount()
    // Give the microtask queue a chance to flush any stray promise.
    await tick()
    expect(mocks.savePageMarkdown).not.toHaveBeenCalled()
  })

  it('completes the Source save round-trip: edit → save → onBlocksSaved payload', async () => {
    // Verifies the Source-mode save contract end-to-end: the parent
    // (VirtualScrollContainer) consumes `onBlocksSaved` to update `blocks`,
    // which Edit mode re-renders from. The saved payload must reflect the
    // buffer the user actually typed.
    vi.useFakeTimers()
    const savedBlocks = [
      mkBlock('# Saved heading', { clean: '# Saved heading' }),
      mkBlock('Saved body line', { clean: 'Saved body line' })
    ]
    mocks.savePageMarkdown.mockResolvedValue(savedBlocks)
    const onBlocksSaved = vi.fn()
    try {
      render(MarkdownSourceViewer, {
        props: {
          blocks: BLOCKS,
          filePath: 'Work/Page.md',
          notebook: 'Work',
          section: 'Sec',
          page: 'Page',
          onBlocksSaved
        }
      })
      const ta = (await waitFor(() =>
        screen.getByRole('textbox', { name: /markdown source/i })
      )) as HTMLTextAreaElement
      await waitFor(() => expect(ta.value).toContain('Fetched body content'))

      // Edit and let the debounced save fire.
      await fireEvent.input(ta, {
        target: { value: '# Saved heading\nSaved body line' }
      })
      await vi.advanceTimersByTimeAsync(500)

      // IPC was called with the typed buffer, and the parent callback
      // received the server-rendered block list.
      await waitFor(() => expect(onBlocksSaved).toHaveBeenCalledTimes(1))
      expect(mocks.savePageMarkdown).toHaveBeenCalledWith(
        'Work',
        'Sec',
        'Page',
        '# Saved heading\nSaved body line'
      )
      expect(onBlocksSaved).toHaveBeenCalledWith(savedBlocks)
      // Buffer preserved across the round-trip; dirty cleared on success.
      expect(ta.value).toBe('# Saved heading\nSaved body line')
    } finally {
      vi.useRealTimers()
    }
  })

  it('survives a Source save round-trip when the parent re-applies saved blocks', async () => {
    // Mirrors the VSC wiring: onBlocksSaved → rerender with saved blocks.
    // After a successful save, dirty clears and the next blocks prop change
    // is treated as an external replacement while clean — the viewer
    // re-fetches and the disk now returns the saved body (the mock
    // simulates SavePageMarkdown having written it).
    vi.useFakeTimers()
    const savedBody = '# Round-trip body'
    const savedBlocks = [mkBlock(savedBody)]
    mocks.savePageMarkdown.mockResolvedValue(savedBlocks)
    mocks.fetchPageMarkdown
      .mockResolvedValueOnce(FETCH_BODY) // initial seed
      .mockResolvedValueOnce(savedBody) // re-seed after save echo
    try {
      const { rerender } = render(MarkdownSourceViewer, {
        props: {
          blocks: BLOCKS,
          filePath: 'Work/Page.md',
          notebook: 'Work',
          section: 'Sec',
          page: 'Page'
        }
      })
      const ta = (await waitFor(() =>
        screen.getByRole('textbox', { name: /markdown source/i })
      )) as HTMLTextAreaElement
      await waitFor(() => expect(ta.value).toContain('Fetched body content'))
      await fireEvent.input(ta, { target: { value: savedBody } })
      await vi.advanceTimersByTimeAsync(500)
      await waitFor(() => expect(mocks.savePageMarkdown).toHaveBeenCalled())

      // Parent re-applies the saved blocks. The viewer's $effect sees a
      // clean buffer (save succeeded) and re-seeds; no conflict surfaces.
      await rerender({
        blocks: savedBlocks,
        filePath: 'Work/Page.md',
        notebook: 'Work',
        section: 'Sec',
        page: 'Page'
      })
      await vi.advanceTimersByTimeAsync(0)
      await waitFor(() =>
        expect(screen.queryByRole('button', { name: /keep mine/i })).toBeNull()
      )
      expect(ta.value).toBe(savedBody)
    } finally {
      vi.useRealTimers()
    }
  })
})

// Revalidation fixes: three correctness regressions in the async lifecycle.
// 1. Seed completion must not overwrite a dirty buffer (user typed during
//    the in-flight fetchPageMarkdown).
// 2. AcquireFocusLock completing after destroy must release the lock and
//    not start a heartbeat interval (otherwise the backend lease leaks
//    and RefreshFocusLock fires forever).
// 3. Backward selection anchor/caret must survive multi-line indent/dedent.
describe('MarkdownSourceViewer revalidation fixes', () => {
  beforeEach(() => {
    mocks.highlight.mockReset()
    mocks.savePageMarkdown.mockReset()
    mocks.fetchPageMarkdown.mockReset()
    mocks.fetchPageMarkdown.mockResolvedValue(FETCH_BODY)
    mocks.savePageMarkdown.mockResolvedValue([mkBlock('# Saved')])
    mocks.AcquireFocusLock.mockReset()
    mocks.AcquireFocusLock.mockResolvedValue(undefined)
    mocks.ReleaseFocusLock.mockReset()
    mocks.ReleaseFocusLock.mockResolvedValue(undefined)
    mocks.RefreshFocusLock.mockReset()
    mocks.RefreshFocusLock.mockResolvedValue(undefined)
  })
  afterEach(() => cleanup())

  it('does not overwrite dirty local edits when the initial seed completes late', async () => {
    vi.useFakeTimers()
    let resolveFetch!: (v: string) => void
    mocks.fetchPageMarkdown.mockImplementation(
      () => new Promise<string>((r) => (resolveFetch = r))
    )
    try {
      render(MarkdownSourceViewer, {
        props: {
          blocks: BLOCKS,
          filePath: 'Work/Page.md',
          notebook: 'Work',
          section: 'Sec',
          page: 'Page'
        }
      })
      const ta = (await waitFor(() =>
        screen.getByRole('textbox', { name: /markdown source/i })
      )) as HTMLTextAreaElement
      // The seed is still in flight; buffer is empty.
      expect(ta.value).toBe('')

      // User starts editing before the on-disk content arrives.
      await fireEvent.input(ta, { target: { value: '# my new content' } })
      await tick()
      expect(ta.value).toBe('# my new content')

      // The fetch resolves with content that differs from what the user
      // typed. The seed must NOT clobber the buffer.
      resolveFetch('# from disk\n\nexisting body')
      await vi.advanceTimersByTimeAsync(0)
      await tick()

      expect(ta.value).toBe('# my new content')
      // Conflict surfaced so the user can make an informed choice.
      await waitFor(() =>
        expect(screen.getByRole('button', { name: /keep mine/i })).toBeTruthy()
      )
    } finally {
      vi.useRealTimers()
    }
  })

  it('does not overwrite dirty edits when an external-change re-seed races with typing', async () => {
    vi.useFakeTimers()
    let resolveFetch!: (v: string) => void
    mocks.fetchPageMarkdown.mockImplementation(
      () => new Promise<string>((r) => (resolveFetch = r))
    )
    try {
      const { rerender } = render(MarkdownSourceViewer, {
        props: {
          blocks: BLOCKS,
          filePath: 'Work/Page.md',
          notebook: 'Work',
          section: 'Sec',
          page: 'Page'
        }
      })
      const ta = (await waitFor(() =>
        screen.getByRole('textbox', { name: /markdown source/i })
      )) as HTMLTextAreaElement
      // Resolve the initial seed so the buffer is clean.
      resolveFetch(FETCH_BODY)
      await vi.advanceTimersByTimeAsync(0)
      await waitFor(() => expect(ta.value).toContain('Fetched body content'))

      // External blocks change while clean → $effect starts a re-seed.
      // That re-seed's fetch is now pending (mockImplementation still defers).
      let resolveReFetch!: (v: string) => void
      mocks.fetchPageMarkdown.mockImplementation(
        () => new Promise<string>((r) => (resolveReFetch = r))
      )
      await rerender({
        blocks: [mkBlock('# External heading'), mkBlock('remote body')],
        filePath: 'Work/Page.md',
        notebook: 'Work',
        section: 'Sec',
        page: 'Page'
      })
      await tick()

      // User starts typing during the in-flight re-seed.
      await fireEvent.input(ta, { target: { value: '# typed during reseed' } })
      await tick()
      expect(ta.value).toBe('# typed during reseed')

      // Re-seed resolves with the external content. Must NOT overwrite.
      resolveReFetch('# fresh external body')
      await vi.advanceTimersByTimeAsync(0)
      await tick()
      expect(ta.value).toBe('# typed during reseed')
      await waitFor(() =>
        expect(screen.getByRole('button', { name: /keep mine/i })).toBeTruthy()
      )
    } finally {
      vi.useRealTimers()
    }
  })

  it('does not leak a heartbeat or backend lock when AcquireFocusLock resolves after destroy', async () => {
    vi.useFakeTimers()
    let resolveAcquire!: () => void
    mocks.AcquireFocusLock.mockImplementation(
      () => new Promise<void>((r) => (resolveAcquire = r))
    )
    try {
      const { unmount } = render(MarkdownSourceViewer, {
        props: {
          blocks: BLOCKS,
          filePath: 'Work/Page.md',
          notebook: 'Work',
          section: 'Sec',
          page: 'Page'
        }
      })
      // AcquireFocusLock is in flight (onMount → acquireLock).
      await vi.advanceTimersByTimeAsync(0)
      expect(mocks.AcquireFocusLock).toHaveBeenCalledTimes(1)

      // Destroy before the IPC resolves.
      unmount()
      // onDestroy's releaseLock sees hasFocusLock=false (acquire never
      // completed) and returns early without calling ReleaseFocusLock.
      expect(mocks.ReleaseFocusLock).not.toHaveBeenCalled()

      // The late acquire resolves.
      resolveAcquire()
      await vi.advanceTimersByTimeAsync(0)

      // The component released the lock it just acquired (no backend leak).
      await waitFor(() =>
        expect(mocks.ReleaseFocusLock).toHaveBeenCalledWith(
          'Work',
          'Sec',
          'Page'
        )
      )

      // No heartbeat was started, so RefreshFocusLock never fires even
      // after the 20s interval would have elapsed.
      await vi.advanceTimersByTimeAsync(60_000)
      expect(mocks.RefreshFocusLock).not.toHaveBeenCalled()
    } finally {
      vi.useRealTimers()
    }
  })

  it('heartbeat stops after a normal destroy (acquire had completed)', async () => {
    vi.useFakeTimers()
    mocks.AcquireFocusLock.mockResolvedValue(undefined)
    try {
      const { unmount } = render(MarkdownSourceViewer, {
        props: {
          blocks: BLOCKS,
          filePath: 'Work/Page.md',
          notebook: 'Work',
          section: 'Sec',
          page: 'Page'
        }
      })
      // Let acquire complete and the first heartbeat tick fire.
      await vi.advanceTimersByTimeAsync(20_000)
      expect(mocks.RefreshFocusLock).toHaveBeenCalledTimes(1)

      unmount()
      mocks.ReleaseFocusLock.mockClear()
      mocks.RefreshFocusLock.mockClear()

      // No further heartbeats after teardown.
      await vi.advanceTimersByTimeAsync(60_000)
      expect(mocks.RefreshFocusLock).not.toHaveBeenCalled()
    } finally {
      vi.useRealTimers()
    }
  })

  it('preserves backward selection direction through multi-line Tab', async () => {
    mocks.fetchPageMarkdown.mockResolvedValue('alpha\nbeta\ngamma')
    const ta = await seedAndWait('alpha\nbeta\ngamma')
    ta.focus()
    // Backward selection spanning lines 1-2: caret at the low index (col 2
    // of "alpha"), anchor at the high index (col 3 of "beta").
    ta.setSelectionRange(2, 'alpha\nbet'.length, 'backward')
    expect(ta.selectionDirection).toBe('backward')

    await fireEvent.keyDown(ta, { key: 'Tab' })
    await tick()

    expect(ta.value).toBe('\talpha\n\tbeta\ngamma')
    // Direction preserved — focus (caret) stays at the low-index end.
    expect(ta.selectionDirection).toBe('backward')
    // Selection extended to cover the new prefixes.
    expect(ta.selectionStart).toBe(0)
    expect(ta.selectionEnd).toBe('\talpha\n\tbet'.length)
  })

  it('preserves backward selection direction through multi-line Shift+Tab', async () => {
    mocks.fetchPageMarkdown.mockResolvedValue('\talpha\n\tbeta\ngamma')
    const ta = await seedAndWait('\talpha\n\tbeta\ngamma')
    ta.focus()
    // Backward selection: caret in alpha, anchor in beta.
    ta.setSelectionRange(2, '\talpha\n\tbet'.length, 'backward')
    expect(ta.selectionDirection).toBe('backward')

    await fireEvent.keyDown(ta, { key: 'Tab', shiftKey: true })
    await tick()

    expect(ta.value).toBe('alpha\nbeta\ngamma')
    expect(ta.selectionDirection).toBe('backward')
    // Caret at the low-index end (one position left of the original start
    // because the leading tab before it was removed).
    expect(ta.selectionStart).toBe(1)
  })

  it('preserves forward selection direction through multi-line Tab (regression)', async () => {
    mocks.fetchPageMarkdown.mockResolvedValue('alpha\nbeta\ngamma')
    const ta = await seedAndWait('alpha\nbeta\ngamma')
    ta.focus()
    ta.setSelectionRange(2, 'alpha\nbet'.length, 'forward')
    expect(ta.selectionDirection).toBe('forward')

    await fireEvent.keyDown(ta, { key: 'Tab' })
    await tick()

    expect(ta.value).toBe('\talpha\n\tbeta\ngamma')
    expect(ta.selectionDirection).toBe('forward')
    // Caret at the high-index end (forward = focus on the trailing edge).
    expect(ta.selectionEnd).toBe('\talpha\n\tbet'.length)
  })

  it('restores backward selection direction after undo of an indent', async () => {
    mocks.fetchPageMarkdown.mockResolvedValue('alpha\nbeta\ngamma')
    const ta = await seedAndWait('alpha\nbeta\ngamma')
    ta.focus()
    // Two indents — both record the live backward selection direction.
    ta.setSelectionRange(2, 'alpha\nbet'.length, 'backward')
    await fireEvent.keyDown(ta, { key: 'Tab' })
    await tick()
    expect(ta.selectionDirection).toBe('backward')

    await fireEvent.keyDown(ta, { key: 'Tab' })
    await tick()
    expect(ta.selectionDirection).toBe('backward')

    // Undo once — returns to the first indent's state, including its
    // recorded backward selection direction.
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await tick()
    expect(ta.value).toBe('\talpha\n\tbeta\ngamma')
    expect(ta.selectionDirection).toBe('backward')
  })

  // Local helper: seed and wait for a specific body to settle.
  async function seedAndWait(expected: string): Promise<HTMLTextAreaElement> {
    render(MarkdownSourceViewer, {
      props: {
        blocks: BLOCKS,
        filePath: 'Work/Page.md',
        notebook: 'Work',
        section: 'Sec',
        page: 'Page'
      }
    })
    const ta = (await waitFor(() =>
      screen.getByRole('textbox', { name: /markdown source/i })
    )) as HTMLTextAreaElement
    await waitFor(() => expect(ta.value).toBe(expected))
    return ta
  }
})
