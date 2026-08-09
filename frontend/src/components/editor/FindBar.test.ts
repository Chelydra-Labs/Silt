import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, fireEvent, cleanup } from '@testing-library/svelte'

const findBarMock = vi.hoisted(() => {
  let replaceOpen = true
  return {
    findBarState: {
      get open() {
        return true
      },
      get replaceOpen() {
        return replaceOpen
      },
      openFind: vi.fn(),
      openReplace: vi.fn(),
      close: vi.fn(),
      setReplaceOpen(v: boolean) {
        replaceOpen = v
      }
    }
  }
})

vi.mock('../../lib/editor/search/findBarState.svelte', () => ({
  findBarState: findBarMock.findBarState
}))

vi.mock('../../lib/editor/search/searchExtension', () => ({
  getMatchCount: () => 3,
  getActiveMatchIndex: () => 0,
  clearSearch: vi.fn()
}))

import FindBar from './FindBar.svelte'

describe('FindBar', () => {
  beforeEach(() => {
    findBarMock.findBarState.setReplaceOpen(true)
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
})
