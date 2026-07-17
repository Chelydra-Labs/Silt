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

  it('Alt+Enter triggers replace-all when replace row is open (#656)', async () => {
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
    } as any

    render(FindBar, {
      props: { editor, onClose: vi.fn() }
    })

    await fireEvent.keyDown(window, { key: 'Enter', altKey: true })
    expect(replaceAllInPage).toHaveBeenCalled()
  })
})
