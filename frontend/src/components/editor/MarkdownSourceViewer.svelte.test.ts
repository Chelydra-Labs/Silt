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
vi.mock('../../../bindings/silt/app.js', () => ({
  AcquireFocusLock: (...args: unknown[]) => mocks.AcquireFocusLock(...args),
  ReleaseFocusLock: (...args: unknown[]) => mocks.ReleaseFocusLock(...args),
  RefreshFocusLock: (...args: unknown[]) => mocks.RefreshFocusLock(...args)
}))

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
