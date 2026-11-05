import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, fireEvent, cleanup } from '@testing-library/svelte'
import { tick } from 'svelte'

// Spy the disclosure setter while delegating to the real rune-backed state, so
// reveal tests can assert on the rendered DOM (a plain closure never re-renders).
const findBarMock = vi.hoisted(() => ({
  setReplaceOpen: vi.fn()
}))

vi.mock('../../lib/editor/search/findBarState.svelte', async () => {
  const actual = await vi.importActual<
    typeof import('../../lib/editor/search/findBarState.svelte')
  >('../../lib/editor/search/findBarState.svelte')
  findBarMock.setReplaceOpen.mockImplementation((next: boolean) =>
    actual.findBarState.setReplaceOpen(next)
  )
  return {
    findBarState: {
      get open() {
        return actual.findBarState.open
      },
      get replaceOpen() {
        return actual.findBarState.replaceOpen
      },
      openFind: () => actual.findBarState.openFind(),
      openReplace: () => actual.findBarState.openReplace(),
      close: () => actual.findBarState.close(),
      setReplaceOpen: findBarMock.setReplaceOpen
    }
  }
})

vi.mock('../../lib/editor/search/searchExtension', () => ({
  getMatchCount: () => 3,
  getActiveMatchIndex: () => 0,
  clearSearch: vi.fn()
}))

import FindBar from './FindBar.svelte'
import { findBarState } from '../../lib/editor/search/findBarState.svelte'

function editorMock() {
  return {
    isEditable: true,
    on: vi.fn(),
    off: vi.fn(),
    commands: {
      setSearchQuery: vi.fn(),
      findNextInPage: vi.fn(),
      findPrevInPage: vi.fn(),
      replaceNextInPage: vi.fn(),
      replaceAllInPage: vi.fn()
    }
  }
}

describe('FindBar', () => {
  beforeEach(() => {
    findBarMock.setReplaceOpen(true)
    // The setup call above is fixture state, not an assertion target.
    findBarMock.setReplaceOpen.mockClear()
  })
  afterEach(() => cleanup())

  it('Alt+Enter on replace input triggers replace-all once (#656)', async () => {
    const replaceAllInPage = vi.fn()
    const editor = {
      isEditable: true,
      on: vi.fn(),
      off: vi.fn(),
      commands: {
        setSearchQuery: vi.fn(),
        findNextInPage: vi.fn(),
        findPrevInPage: vi.fn(),
        replaceNextInPage: vi.fn(),
        replaceAllInPage: replaceAllInPage
      }
    } as never

    const { getByLabelText } = render(FindBar, {
      props: { editor, onClose: vi.fn() }
    })

    const replaceInput = getByLabelText('Replace with')
    replaceInput.focus()
    await fireEvent.keyDown(replaceInput, { key: 'Enter', altKey: true })
    expect(replaceAllInPage).toHaveBeenCalledTimes(1)

    // Window-level handler must not double-fire replace-all.
    await fireEvent.keyDown(window, { key: 'Enter', altKey: true })
    expect(replaceAllInPage).toHaveBeenCalledTimes(1)
  })

  it('Source target: Alt+Enter replace-all rewrites buffer once (#884)', async () => {
    let text = 'foo bar foo'
    const listeners = new Set<() => void>()
    const setText = vi.fn((next: string) => {
      text = next
      for (const cb of listeners) cb()
    })
    const sourceTarget = {
      getText: () => text,
      getCaret: () => 0,
      setSelection: vi.fn(),
      replaceRange: vi.fn(),
      setText,
      subscribe: (cb: () => void) => {
        listeners.add(cb)
        return () => listeners.delete(cb)
      }
    }

    const { getByLabelText } = render(FindBar, {
      props: { sourceTarget, onClose: vi.fn() }
    })

    const findInput = getByLabelText('Find')
    await fireEvent.input(findInput, { target: { value: 'foo' } })
    const replaceInput = getByLabelText('Replace with')
    await fireEvent.input(replaceInput, { target: { value: 'baz' } })
    replaceInput.focus()
    await fireEvent.keyDown(replaceInput, { key: 'Enter', altKey: true })

    expect(setText).toHaveBeenCalledTimes(1)
    expect(setText).toHaveBeenCalledWith('baz bar baz')
  })

  it('Source target: next/prev cycle selection and single replace updates range', async () => {
    findBarMock.setReplaceOpen(true)
    let text = 'aa bb aa'
    const listeners = new Set<() => void>()
    const setSelection = vi.fn()
    const replaceRange = vi.fn((from: number, to: number, rep: string) => {
      text = text.slice(0, from) + rep + text.slice(to)
      for (const cb of listeners) cb()
    })
    const sourceTarget = {
      getText: () => text,
      getCaret: () => 0,
      setSelection,
      replaceRange,
      setText: vi.fn(),
      subscribe: (cb: () => void) => {
        listeners.add(cb)
        return () => listeners.delete(cb)
      }
    }

    const { getByLabelText } = render(FindBar, {
      props: { sourceTarget, onClose: vi.fn() }
    })

    const findInput = getByLabelText('Find')
    await fireEvent.input(findInput, { target: { value: 'aa' } })

    // First match selected on apply (caret at 0 → index 0).
    expect(setSelection).toHaveBeenCalledWith(0, 2)
    setSelection.mockClear()

    await fireEvent.click(getByLabelText('Next match (Enter)'))
    expect(setSelection).toHaveBeenCalledWith(6, 8)
    setSelection.mockClear()

    await fireEvent.click(getByLabelText('Previous match (Shift+Enter)'))
    expect(setSelection).toHaveBeenCalledWith(0, 2)

    const replaceInput = getByLabelText('Replace with')
    await fireEvent.input(replaceInput, { target: { value: 'XX' } })
    await fireEvent.click(getByLabelText('Replace selected match'))
    expect(replaceRange).toHaveBeenCalledWith(0, 2, 'XX')
    expect(text.startsWith('XX')).toBe(true)
  })

  it('Esc on find input calls onClose', async () => {
    const onClose = vi.fn()
    const editor = {
      isEditable: true,
      on: vi.fn(),
      off: vi.fn(),
      commands: {
        setSearchQuery: vi.fn(),
        findNextInPage: vi.fn(),
        findPrevInPage: vi.fn(),
        replaceNextInPage: vi.fn(),
        replaceAllInPage: vi.fn()
      }
    } as never

    const { getByLabelText } = render(FindBar, {
      props: { editor, onClose }
    })
    const findInput = getByLabelText('Find')
    await fireEvent.keyDown(findInput, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})

describe('FindBar replace disclosure (#1044)', () => {
  // Start every test from the find-only state so the toggle exercises the reveal.
  beforeEach(() => {
    findBarMock.setReplaceOpen(false)
    findBarMock.setReplaceOpen.mockClear()
  })
  afterEach(() => cleanup())

  function renderWithEditor(editor = editorMock()) {
    return render(FindBar, {
      props: { editor: editor as never, onClose: vi.fn() }
    })
  }

  it('renders an aria-expanded disclosure button reflecting the state', () => {
    findBarMock.setReplaceOpen(true)
    findBarMock.setReplaceOpen.mockClear()
    const { getByLabelText } = renderWithEditor()
    const toggle = getByLabelText('Toggle replace')
    expect(toggle.tagName).toBe('BUTTON')
    expect(toggle.getAttribute('aria-expanded')).toBe('true')
  })

  it('toggles replace on click without changing the query or options', async () => {
    const { getByLabelText, getByRole, queryByLabelText } = renderWithEditor()
    const toggle = getByLabelText('Toggle replace')
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    expect(queryByLabelText('Replace with')).toBeNull()

    // The toolbar itself is labelled "Find" while replace is hidden.
    const findInput = getByRole('searchbox', {
      name: 'Find'
    }) as HTMLInputElement
    await fireEvent.input(findInput, { target: { value: 'foo' } })
    await fireEvent.click(getByLabelText('Case sensitive (Alt+C)'))

    await fireEvent.click(toggle)

    expect(findBarMock.setReplaceOpen).toHaveBeenCalledWith(true)
    expect(toggle.getAttribute('aria-expanded')).toBe('true')
    expect(getByLabelText('Replace with')).toBeTruthy()
    expect(findInput.value).toBe('foo')
    expect(
      getByLabelText('Case sensitive (Alt+C)').getAttribute('aria-pressed')
    ).toBe('true')

    // Second activation must hide the row again, not pin it open.
    await fireEvent.click(toggle)

    expect(findBarMock.setReplaceOpen).toHaveBeenLastCalledWith(false)
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    expect(queryByLabelText('Replace with')).toBeNull()
  })

  it('does not re-run the search when revealing replace (Edit mode)', async () => {
    const editor = editorMock()
    const { getByLabelText, getByRole } = renderWithEditor(editor)
    await fireEvent.input(getByRole('searchbox', { name: 'Find' }), {
      target: { value: 'foo' }
    })
    const callsAfterTyping = editor.commands.findNextInPage.mock.calls.length
    expect(callsAfterTyping).toBeGreaterThan(0)

    await fireEvent.click(getByLabelText('Toggle replace'))
    expect(editor.commands.findNextInPage).toHaveBeenCalledTimes(
      callsAfterTyping
    )
  })

  it('replaces the selected match after revealing from find (Edit)', async () => {
    const editor = editorMock()
    const { getByLabelText, getByRole } = renderWithEditor(editor)
    await fireEvent.input(getByRole('searchbox', { name: 'Find' }), {
      target: { value: 'foo' }
    })
    await fireEvent.click(getByLabelText('Toggle replace'))
    await fireEvent.input(getByLabelText('Replace with'), {
      target: { value: 'baz' }
    })
    await fireEvent.click(getByLabelText('Replace selected match'))

    expect(editor.commands.replaceNextInPage).toHaveBeenCalledTimes(1)
    expect(editor.commands.setSearchQuery).toHaveBeenLastCalledWith(
      expect.objectContaining({ replace: 'baz' })
    )
  })

  it('replaces all matches after revealing from find (Edit)', async () => {
    const editor = editorMock()
    const { getByLabelText, getByRole } = renderWithEditor(editor)
    await fireEvent.input(getByRole('searchbox', { name: 'Find' }), {
      target: { value: 'foo' }
    })
    await fireEvent.click(getByLabelText('Toggle replace'))
    await fireEvent.input(getByLabelText('Replace with'), {
      target: { value: 'baz' }
    })
    await fireEvent.click(getByLabelText('Replace all matches'))

    expect(editor.commands.replaceAllInPage).toHaveBeenCalledTimes(1)
  })

  // The stale case: hiding the row lets the query effect rewrite the stored
  // `replace` to '', so a reopened row must sync at replace time, not reveal.
  it('keeps the replacement text current when Ctrl+H reopens the row (Edit)', async () => {
    const editor = editorMock()
    const { getByLabelText, getByRole } = renderWithEditor(editor)
    const findInput = getByRole('searchbox', {
      name: 'Find'
    }) as HTMLInputElement
    await fireEvent.input(findInput, { target: { value: 'foo' } })
    await fireEvent.click(getByLabelText('Toggle replace'))
    await fireEvent.input(getByLabelText('Replace with'), {
      target: { value: 'baz' }
    })

    await fireEvent.click(getByLabelText('Toggle replace'))
    await fireEvent.input(findInput, { target: { value: 'foobar' } })

    // Ctrl+H while the bar is already mounted flips the row with no
    // transition-time sync (only the ⇄ toggle used to sync).
    findBarState.openReplace()
    await tick()
    expect((getByLabelText('Replace with') as HTMLInputElement).value).toBe(
      'baz'
    )

    // Replace without retyping the replacement.
    await fireEvent.click(getByLabelText('Replace selected match'))

    expect(editor.commands.replaceNextInPage).toHaveBeenCalledTimes(1)
    expect(editor.commands.setSearchQuery).toHaveBeenLastCalledWith(
      expect.objectContaining({ replace: 'baz' })
    )
  })

  it('keeps the replacement text current for replace-all after a hidden query edit', async () => {
    const editor = editorMock()
    const { getByLabelText, getByRole } = renderWithEditor(editor)
    const findInput = getByRole('searchbox', {
      name: 'Find'
    }) as HTMLInputElement
    await fireEvent.input(findInput, { target: { value: 'foo' } })
    await fireEvent.click(getByLabelText('Toggle replace'))
    await fireEvent.input(getByLabelText('Replace with'), {
      target: { value: 'baz' }
    })

    await fireEvent.click(getByLabelText('Toggle replace'))
    await fireEvent.input(findInput, { target: { value: 'foobar' } })
    await fireEvent.click(getByLabelText('Toggle replace'))

    await fireEvent.click(getByLabelText('Replace all matches'))

    expect(editor.commands.replaceAllInPage).toHaveBeenCalledTimes(1)
    expect(editor.commands.setSearchQuery).toHaveBeenLastCalledWith(
      expect.objectContaining({ replace: 'baz' })
    )
  })

  it('replaces the active match after revealing from find (Source)', async () => {
    let text = 'aa bb aa'
    const listeners = new Set<() => void>()
    const replaceRange = vi.fn((from: number, to: number, rep: string) => {
      text = text.slice(0, from) + rep + text.slice(to)
      for (const cb of listeners) cb()
    })
    const sourceTarget = {
      getText: () => text,
      getCaret: () => 0,
      setSelection: vi.fn(),
      replaceRange,
      setText: vi.fn(),
      subscribe: (cb: () => void) => {
        listeners.add(cb)
        return () => listeners.delete(cb)
      }
    }

    const { getByLabelText, getByRole } = render(FindBar, {
      props: { sourceTarget, onClose: vi.fn() }
    })
    await fireEvent.input(getByRole('searchbox', { name: 'Find' }), {
      target: { value: 'aa' }
    })
    await fireEvent.click(getByLabelText('Toggle replace'))
    await fireEvent.input(getByLabelText('Replace with'), {
      target: { value: 'XX' }
    })
    await fireEvent.click(getByLabelText('Replace selected match'))

    expect(replaceRange).toHaveBeenCalledWith(0, 2, 'XX')
  })

  it('replaces all matches after revealing from find (Source)', async () => {
    let text = 'aa bb aa'
    const listeners = new Set<() => void>()
    const setText = vi.fn((next: string) => {
      text = next
      for (const cb of listeners) cb()
    })
    const sourceTarget = {
      getText: () => text,
      getCaret: () => 0,
      setSelection: vi.fn(),
      replaceRange: vi.fn(),
      setText,
      subscribe: (cb: () => void) => {
        listeners.add(cb)
        return () => listeners.delete(cb)
      }
    }

    const { getByLabelText, getByRole } = render(FindBar, {
      props: { sourceTarget, onClose: vi.fn() }
    })
    await fireEvent.input(getByRole('searchbox', { name: 'Find' }), {
      target: { value: 'aa' }
    })
    await fireEvent.click(getByLabelText('Toggle replace'))
    await fireEvent.input(getByLabelText('Replace with'), {
      target: { value: 'XX' }
    })
    await fireEvent.click(getByLabelText('Replace all matches'))

    expect(setText).toHaveBeenCalledWith('XX bb XX')
  })
})

// #1005: a conflict-held editor is setEditable(false) — replace dispatches
// through commands, which isEditable never gates; the edit would render and
// the armed reload would destroy it.
describe('FindBar conflict hold (#1005)', () => {
  beforeEach(() => {
    findBarMock.setReplaceOpen(true)
    findBarMock.setReplaceOpen.mockClear()
  })
  afterEach(() => cleanup())

  it('refuses replace and replace-all on a non-editable editor', async () => {
    const replaceNextInPage = vi.fn()
    const replaceAllInPage = vi.fn()
    const editor = {
      isEditable: false,
      on: vi.fn(),
      off: vi.fn(),
      commands: {
        setSearchQuery: vi.fn(),
        findNextInPage: vi.fn(),
        findPrevInPage: vi.fn(),
        replaceNextInPage,
        replaceAllInPage
      }
    } as never

    const { getByLabelText } = render(FindBar, {
      props: { editor, onClose: vi.fn() }
    })

    // Alt+Enter → replace-all is refused…
    const replaceInput = getByLabelText('Replace with')
    replaceInput.focus()
    await fireEvent.keyDown(replaceInput, { key: 'Enter', altKey: true })
    expect(replaceAllInPage).not.toHaveBeenCalled()
    // …and so are the Replace / Replace-all buttons.
    await fireEvent.click(getByLabelText('Replace selected match'))
    expect(replaceNextInPage).not.toHaveBeenCalled()
    await fireEvent.click(getByLabelText('Replace all matches'))
    expect(replaceAllInPage).not.toHaveBeenCalled()
  })
})
