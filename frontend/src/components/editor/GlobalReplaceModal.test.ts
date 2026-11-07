// Component coverage for GlobalReplaceModal's apply/undo/stale-guard paths.
// IPC is mocked via vi.hoisted + vi.mock over the wailsjs binding module
// (canonical pattern — see AppearanceTab.test.ts). The matcher module is
// exercised for real so these tests assert actual find/replace behavior.

import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'
import {
  registerEditor,
  _resetEditorRegistryForTests
} from '../../lib/editor/editorRegistry.svelte'

const mocks = vi.hoisted(() => ({
  SearchBlocksPaged: vi.fn(),
  FetchPageBlocksProgrammatic: vi.fn(),
  SaveFileBlocksProgrammatic: vi.fn()
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    SearchBlocksPaged: mocks.SearchBlocksPaged,
    FetchPageBlocksProgrammatic: mocks.FetchPageBlocksProgrammatic,
    SaveFileBlocksProgrammatic: mocks.SaveFileBlocksProgrammatic
  })
)
vi.mock('@wailsio/runtime', () => ({
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

import GlobalReplaceModal from './GlobalReplaceModal.svelte'
import { wailsIPCError } from '../../lib/testing/wailsIpcError'

// Shared block factory: keep raw_text equal to clean_text so the snapshot
// shape matches what the component actually persists.
function block(id: string, clean_text: string) {
  return { id, clean_text, raw_text: clean_text }
}

// Render, fill Find/Replace, click Preview, and wait until the Replace
// button shows a numeric match count — the signal that preview populated
// accepted matches and the stale-guard is settled (previewStale === false).
async function renderAndPreview(find: string, replace: string) {
  render(GlobalReplaceModal, { onClose: () => {} })
  await tick()
  await fireEvent.input(screen.getByLabelText('Find'), {
    target: { value: find }
  })
  await fireEvent.input(screen.getByLabelText('Replace with'), {
    target: { value: replace }
  })
  await fireEvent.click(screen.getByRole('button', { name: 'Preview' }))
  await waitFor(
    () => {
      expect(
        screen.getByRole('button', { name: /^Replace \d/ })
      ).toBeInTheDocument()
    },
    { timeout: 2000 }
  )
}

describe('GlobalReplaceModal apply/undo/stale-guard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    _resetEditorRegistryForTests()
  })

  afterEach(() => {
    cleanup()
  })

  it('Preview → Apply writes through SaveFileBlocksProgrammatic once per changed page', async () => {
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [
        {
          id: 'b1',
          notebook: 'vault',
          section: 'notes',
          page: 'page1',
          snippet: 'foo bar'
        },
        {
          id: 'b2',
          notebook: 'vault',
          section: 'notes',
          page: 'page1',
          snippet: 'foo baz'
        }
      ],
      total: 2,
      offset: 0,
      limit: 200,
      has_more: false
    })
    mocks.FetchPageBlocksProgrammatic.mockResolvedValue([
      block('b1', 'foo bar'),
      block('b2', 'foo baz')
    ])
    mocks.SaveFileBlocksProgrammatic.mockResolvedValue(undefined)

    await renderAndPreview('foo', 'qux')
    await fireEvent.click(screen.getByRole('button', { name: /^Replace/ }))

    await waitFor(
      () => {
        expect(mocks.SaveFileBlocksProgrammatic).toHaveBeenCalledTimes(1)
      },
      { timeout: 2000 }
    )

    const saved = mocks.SaveFileBlocksProgrammatic.mock.calls[0][3] as Array<{
      clean_text: string
    }>
    expect(saved).toHaveLength(2)
    expect(saved[0].clean_text).toBe('qux bar')
    expect(saved[1].clean_text).toBe('qux baz')
  })

  it('Undo restores original blocks for the last applied page', async () => {
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [
        {
          id: 'b1',
          notebook: 'vault',
          section: 'notes',
          page: 'page1',
          snippet: 'foo bar'
        },
        {
          id: 'b2',
          notebook: 'vault',
          section: 'notes',
          page: 'page1',
          snippet: 'foo baz'
        }
      ],
      total: 2,
      offset: 0,
      limit: 200,
      has_more: false
    })
    mocks.FetchPageBlocksProgrammatic.mockResolvedValue([
      block('b1', 'foo bar'),
      block('b2', 'foo baz')
    ])
    mocks.SaveFileBlocksProgrammatic.mockResolvedValue(undefined)

    await renderAndPreview('foo', 'qux')
    await fireEvent.click(screen.getByRole('button', { name: /^Replace/ }))
    await waitFor(
      () => {
        expect(mocks.SaveFileBlocksProgrammatic).toHaveBeenCalledTimes(1)
      },
      { timeout: 2000 }
    )

    // The revert log now holds one batch (page1); Undo must surface.
    const restoreBtn = await screen.findByRole('button', {
      name: /Restore last apply/
    })
    await fireEvent.click(restoreBtn)

    // Second SaveFileBlocks call restores the pre-replace originals.
    await waitFor(
      () => {
        expect(mocks.SaveFileBlocksProgrammatic).toHaveBeenCalledTimes(2)
      },
      { timeout: 2000 }
    )
    const restored = mocks.SaveFileBlocksProgrammatic.mock
      .calls[1][3] as Array<{
      clean_text: string
    }>
    expect(restored[0].clean_text).toBe('foo bar')
    expect(restored[1].clean_text).toBe('foo baz')
  })

  it('editing findText after Preview marks the preview stale and disables Apply', async () => {
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [
        {
          id: 'b1',
          notebook: 'vault',
          section: 'notes',
          page: 'page1',
          snippet: 'foo bar'
        },
        {
          id: 'b2',
          notebook: 'vault',
          section: 'notes',
          page: 'page1',
          snippet: 'foo baz'
        }
      ],
      total: 2,
      offset: 0,
      limit: 200,
      has_more: false
    })
    mocks.FetchPageBlocksProgrammatic.mockResolvedValue([
      block('b1', 'foo bar'),
      block('b2', 'foo baz')
    ])
    mocks.SaveFileBlocksProgrammatic.mockResolvedValue(undefined)

    await renderAndPreview('foo', 'qux')

    // Retyping Find without re-previewing would otherwise apply a matcher
    // the user never saw — the stale-guard must catch this.
    await fireEvent.input(screen.getByLabelText('Find'), {
      target: { value: 'bar' }
    })
    await tick()

    await waitFor(
      () => {
        expect(screen.getByRole('button', { name: /^Replace/ })).toBeDisabled()
      },
      { timeout: 2000 }
    )
    expect(screen.getByText(/Preview is stale/)).toBeInTheDocument()
    expect(mocks.SaveFileBlocksProgrammatic).not.toHaveBeenCalled()
  })

  it('an input edit during the in-flight preview cannot install the stale response', async () => {
    // First preview populates the actionable surface.
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [
        {
          id: 'b1',
          notebook: 'vault',
          section: 'notes',
          page: 'page1',
          snippet: 'foo bar'
        }
      ],
      total: 1,
      offset: 0,
      limit: 200,
      has_more: false
    })
    await renderAndPreview('foo', 'qux')

    // Second preview: hold the response until after the user edits Find.
    let resolveResponse: (value: unknown) => void = () => {}
    mocks.SearchBlocksPaged.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveResponse = resolve
        })
    )
    await fireEvent.click(screen.getByRole('button', { name: 'Preview' }))
    await tick()
    await fireEvent.input(screen.getByLabelText('Find'), {
      target: { value: 'bar' }
    })
    await tick()

    // The response was computed for "foo", but Apply would rebuild the matcher
    // from "bar" — adopting it would apply a replacement the user never saw.
    resolveResponse({
      results: [
        {
          id: 'stale1',
          notebook: 'vault',
          section: 'notes',
          page: 'stale-page',
          snippet: 'bar bar'
        }
      ],
      total: 1,
      offset: 0,
      limit: 200,
      has_more: false
    })

    await waitFor(
      () => {
        expect(screen.getByRole('button', { name: /^Replace/ })).toBeDisabled()
      },
      { timeout: 2000 }
    )
    expect(screen.getByText(/Preview is stale/)).toBeInTheDocument()
    expect(screen.queryByText('stale-page')).not.toBeInTheDocument()
    expect(mocks.SaveFileBlocksProgrammatic).not.toHaveBeenCalled()
  })

  it('a mid-loop Apply failure leaves Undo available for already-persisted pages', async () => {
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [
        {
          id: 'b1',
          notebook: 'vault',
          section: 'notes',
          page: 'page1',
          snippet: 'foo bar'
        },
        {
          id: 'b2',
          notebook: 'vault',
          section: 'notes',
          page: 'page2',
          snippet: 'foo qux'
        }
      ],
      total: 2,
      offset: 0,
      limit: 200,
      has_more: false
    })
    mocks.FetchPageBlocksProgrammatic.mockImplementation(
      (_nb: string, _sec: string, page: string) => {
        if (page === 'page1') return Promise.resolve([block('b1', 'foo bar')])
        if (page === 'page2') return Promise.resolve([block('b2', 'foo qux')])
        return Promise.resolve([])
      }
    )
    // page1 persists; page2 fails mid-batch. The finally-block must still
    // commit newLog (page1 only) so Undo is available for the written page.
    mocks.SaveFileBlocksProgrammatic.mockImplementation(
      (_nb: string, _sec: string, page: string) => {
        if (page === 'page1') return Promise.resolve()
        return Promise.reject(new Error('disk full'))
      }
    )

    await renderAndPreview('foo', 'ZZZ')
    await fireEvent.click(screen.getByRole('button', { name: /^Replace/ }))

    await waitFor(
      () => {
        expect(screen.getByText(/Apply failed/)).toBeInTheDocument()
      },
      { timeout: 2000 }
    )

    // Batch holds exactly the one page that persisted.
    const restoreBtn = await screen.findByRole('button', {
      name: /Restore last apply \(1 page\)/
    })
    expect(restoreBtn).toBeInTheDocument()
    await fireEvent.click(restoreBtn)

    // Undo restores page1's pre-replace originals.
    await waitFor(
      () => {
        const undoCalls = mocks.SaveFileBlocksProgrammatic.mock.calls.filter(
          (c) => c[2] === 'page1'
        )
        expect(undoCalls.length).toBeGreaterThanOrEqual(2)
      },
      { timeout: 2000 }
    )
    const undoCall = [...mocks.SaveFileBlocksProgrammatic.mock.calls]
      .reverse()
      .find((c) => c[2] === 'page1')!
    const restored = undoCall[3] as Array<{ clean_text: string }>
    expect(restored[0].clean_text).toBe('foo bar')
  })

  // Apply snapshots the replacement with the matcher. Reading the input inside
  // the per-page loop let an edit made while page 2's fetch was in flight write
  // "foo" → old value on page 1 and new value on page 2: a batch the preview
  // never showed, reported as a normal success.
  it('snapshots the replacement for the whole apply batch', async () => {
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [
        {
          id: 'b1',
          notebook: 'vault',
          section: 'notes',
          page: 'page1',
          snippet: 'foo bar'
        },
        {
          id: 'b2',
          notebook: 'vault',
          section: 'notes',
          page: 'page2',
          snippet: 'foo qux'
        }
      ],
      total: 2,
      offset: 0,
      limit: 200,
      has_more: false
    })
    let releasePage2: () => void = () => {}
    mocks.FetchPageBlocksProgrammatic.mockImplementation(
      (_nb: string, _sec: string, page: string) => {
        if (page === 'page2') {
          return new Promise((resolve) => {
            releasePage2 = () => resolve([block('b2', 'foo qux')])
          })
        }
        return Promise.resolve([block('b1', 'foo bar')])
      }
    )
    mocks.SaveFileBlocksProgrammatic.mockResolvedValue(undefined)

    await renderAndPreview('foo', 'qux')
    await fireEvent.click(screen.getByRole('button', { name: /^Replace/ }))
    await waitFor(
      () => {
        expect(mocks.FetchPageBlocksProgrammatic).toHaveBeenCalledTimes(2)
      },
      { timeout: 2000 }
    )

    // Page 2 is still awaiting its fetch: edit Replace before it resolves.
    await fireEvent.input(screen.getByLabelText('Replace with'), {
      target: { value: 'ZZZ' }
    })
    releasePage2()

    await waitFor(
      () => {
        expect(mocks.SaveFileBlocksProgrammatic).toHaveBeenCalledTimes(2)
      },
      { timeout: 2000 }
    )
    const page2Call = mocks.SaveFileBlocksProgrammatic.mock.calls.find(
      (c) => c[2] === 'page2'
    )!
    const saved = page2Call[3] as Array<{ clean_text: string }>
    expect(saved[0].clean_text).toBe('qux qux')
  })

  it('refuses a linked-notebook page even if it slips into the preview (#343)', async () => {
    // Defense in depth: the server VaultOnly filter excludes linked hits,
    // but if one reached the preview it must NOT be written.
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [
        {
          id: 'b1',
          source: 'linked:abc',
          notebook: 'External',
          section: 'notes',
          page: 'linked-page',
          snippet: 'foo bar'
        }
      ],
      total: 1,
      offset: 0,
      limit: 200,
      has_more: false
    })
    mocks.FetchPageBlocksProgrammatic.mockResolvedValue([
      block('b1', 'foo bar')
    ])
    mocks.SaveFileBlocksProgrammatic.mockResolvedValue(undefined)

    await renderAndPreview('foo', 'qux')
    await fireEvent.click(screen.getByRole('button', { name: /^Replace/ }))

    // SaveFileBlocks must never be called for the linked page.
    await waitFor(
      () => {
        expect(mocks.SaveFileBlocksProgrammatic).not.toHaveBeenCalled()
      },
      { timeout: 2000 }
    )
  })

  // #1005/#345: when a page's write is refused (external edit), no
  // block:changed ever lands to consume the just-armed reload — the arm must
  // be dropped on the failed page or it leaks and the next unrelated blocks
  // change setContents over the user's unsaved edits.
  it('clears the armed reload when a page write is refused', async () => {
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [
        {
          id: 'b1',
          source: 'vault',
          notebook: 'vault',
          section: 'notes',
          page: 'page1',
          snippet: 'foo bar'
        }
      ],
      total: 1,
      offset: 0,
      limit: 200,
      has_more: false
    })
    mocks.FetchPageBlocksProgrammatic.mockResolvedValue([
      block('b1', 'foo bar')
    ])
    mocks.SaveFileBlocksProgrammatic.mockRejectedValue(
      wailsIPCError({
        code: 'external_edit_conflict',
        message: 'this page changed on disk since it was opened'
      })
    )

    const order: string[] = []
    registerEditor({
      key: 'vault\x00notes\x00page1',
      isDirty: () => false,
      flush: async () => true,
      forceExternalReload: () => order.push('arm'),
      clearExternalReload: () => order.push('clear'),
      setProposedEdit: () => false,
      clearProposedEdit: () => {},
      hasProposal: () => false,
      acceptProposedEdit: () => false,
      verifySelectionText: () => false
    })

    await renderAndPreview('foo', 'qux')
    await fireEvent.click(screen.getByRole('button', { name: /^Replace/ }))

    await waitFor(
      () => {
        expect(order).toEqual(['arm', 'clear'])
      },
      { timeout: 2000 }
    )
    // The refusal surfaces as held-write copy, not raw JSON.
    expect(screen.getByText(/the write was held/i)).toBeTruthy()
  })

  it('flushes a dirty open editor before applying, then force-reloads it (#345)', async () => {
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [
        {
          id: 'b1',
          source: 'vault',
          notebook: 'vault',
          section: 'notes',
          page: 'page1',
          snippet: 'foo bar'
        }
      ],
      total: 1,
      offset: 0,
      limit: 200,
      has_more: false
    })
    mocks.FetchPageBlocksProgrammatic.mockResolvedValue([
      block('b1', 'foo bar')
    ])
    mocks.SaveFileBlocksProgrammatic.mockResolvedValue(undefined)

    // Register a fake dirty editor for the target page. Record the call order
    // across flush / forceExternalReload / SaveFileBlocks to assert the
    // lifecycle fix (#345): the reload flag must arm BEFORE the write so the
    // editor's sync $effect consumes it on the block:changed reload rather than
    // the flag leaking and later clobbering an unrelated edit.
    const order: string[] = []
    let dirty = true
    registerEditor({
      key: 'vault\x00notes\x00page1',
      isDirty: () => dirty,
      flush: async () => {
        order.push('flush')
        dirty = false
        return true
      },
      forceExternalReload: () => order.push('forceExternalReload'),
      clearExternalReload: () => {},
      setProposedEdit: () => false,
      clearProposedEdit: () => {},
      hasProposal: () => false,
      acceptProposedEdit: () => false,
      verifySelectionText: () => false
    })
    mocks.SaveFileBlocksProgrammatic.mockImplementation(async () => {
      order.push('SaveFileBlocks')
    })

    await renderAndPreview('foo', 'qux')
    await fireEvent.click(screen.getByRole('button', { name: /^Replace/ }))

    await waitFor(
      () => {
        expect(order).toContain('SaveFileBlocks')
      },
      { timeout: 2000 }
    )
    // flush runs first; forceExternalReload must precede SaveFileBlocks.
    const flushAt = order.indexOf('flush')
    const reloadAt = order.indexOf('forceExternalReload')
    const saveAt = order.indexOf('SaveFileBlocks')
    expect(flushAt).toBeLessThan(reloadAt)
    expect(reloadAt).toBeLessThan(saveAt)
  })

  it('skips a page whose dirty editor cannot flush (save error) (#345)', async () => {
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [
        {
          id: 'b1',
          source: 'vault',
          notebook: 'vault',
          section: 'notes',
          page: 'page1',
          snippet: 'foo bar'
        }
      ],
      total: 1,
      offset: 0,
      limit: 200,
      has_more: false
    })
    mocks.FetchPageBlocksProgrammatic.mockResolvedValue([
      block('b1', 'foo bar')
    ])
    mocks.SaveFileBlocksProgrammatic.mockResolvedValue(undefined)

    // Register a dirty editor whose flush fails (stays dirty).
    registerEditor({
      key: 'vault\x00notes\x00page1',
      isDirty: () => true,
      flush: async () => false,
      forceExternalReload: vi.fn(),
      clearExternalReload: vi.fn(),
      setProposedEdit: () => false,
      clearProposedEdit: () => {},
      hasProposal: () => false,
      acceptProposedEdit: () => false,
      verifySelectionText: () => false
    })

    await renderAndPreview('foo', 'qux')
    await fireEvent.click(screen.getByRole('button', { name: /^Replace/ }))

    // The page must NOT be written: writing from stale disk content would
    // silently discard the user's unsaved edits.
    await waitFor(
      () => {
        expect(mocks.SaveFileBlocksProgrammatic).not.toHaveBeenCalled()
      },
      { timeout: 2000 }
    )
    // The skip must be surfaced with the page name + reason, not silently
    // lost (a fully-skipped batch fires an error toast naming the page).
    await waitFor(
      () => {
        expect(
          screen.getByText(/page1.*unsaved edits couldn't be saved/)
        ).toBeInTheDocument()
      },
      { timeout: 2000 }
    )
  })

  // The preview budget is block rows only: with page rows in the window, a
  // find term matching many page names could hide every block match and the
  // banner would report a page-row-inflated total. The banner reports preview
  // ROWS, not matches: the per-page (3) and flat (500) caps mean matching
  // blocks can exist that this preview cannot offer.
  it('asks for block-only results and reports the truncated preview rows', async () => {
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [
        {
          id: 'b1',
          notebook: 'vault',
          section: 'notes',
          page: 'page1',
          snippet: 'foo bar'
        },
        {
          id: 'b2',
          notebook: 'vault',
          section: 'notes',
          page: 'page2',
          snippet: 'foo baz'
        }
      ],
      total: 340,
      offset: 0,
      limit: 200,
      has_more: true
    })
    mocks.FetchPageBlocksProgrammatic.mockResolvedValue([
      block('b1', 'foo bar')
    ])
    mocks.SaveFileBlocksProgrammatic.mockResolvedValue(undefined)

    await renderAndPreview('foo', 'qux')

    expect(mocks.SearchBlocksPaged).toHaveBeenCalledWith(
      'foo',
      0,
      200,
      expect.objectContaining({ excludePageTitles: true })
    )
    expect(screen.getByText(/340 preview rows/)).toBeInTheDocument()
    expect(screen.getByText(/3 per page/)).toBeInTheDocument()
  })

  // A rejected refresh must clear the whole actionable surface, not just show
  // the error: the groups, the truncation banner, and the stale notice all
  // describe a preview that no longer exists. A truncated first preview makes
  // the banner the observable difference (it renders on truncatedCount alone).
  it('clears the applicable preview and its truncation banner when the refresh fails', async () => {
    mocks.SearchBlocksPaged.mockResolvedValueOnce({
      results: [
        {
          id: 'b1',
          notebook: 'vault',
          section: 'notes',
          page: 'page1',
          snippet: 'foo bar'
        }
      ],
      total: 340,
      offset: 0,
      limit: 200,
      has_more: true
    })

    await renderAndPreview('foo', 'qux')
    expect(
      screen.getByRole('button', { name: 'Replace 1' })
    ).toBeInTheDocument()
    expect(screen.getByText(/340 preview rows/)).toBeInTheDocument()

    mocks.SearchBlocksPaged.mockRejectedValueOnce(new Error('ipc down'))
    await fireEvent.click(screen.getByRole('button', { name: 'Preview' }))

    await waitFor(
      () => {
        expect(screen.getByText(/Preview failed/)).toBeInTheDocument()
      },
      { timeout: 2000 }
    )
    expect(screen.queryByText(/preview rows/)).toBeNull()
    expect(screen.queryByRole('button', { name: /^Replace \d/ })).toBeNull()
    expect(screen.getByRole('button', { name: 'Replace' })).toBeDisabled()
  })

  // Page-level title hits (id === "") are navigation rows, not blocks: the
  // backend prepends them to the paged envelope. They used to become phantom
  // matches (preview promised a replacement; Apply could never make it) and
  // inflated the counts.
  it('ignores page-level title rows in the preview', async () => {
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [
        {
          id: '',
          notebook: 'vault',
          section: 'notes',
          page: 'Phantom Page A',
          snippet: '<mark>foo</mark> page'
        },
        {
          id: '',
          notebook: 'vault',
          section: 'notes',
          page: 'Phantom Page B',
          snippet: 'another <mark>foo</mark> page'
        },
        {
          id: 'b1',
          notebook: 'vault',
          section: 'notes',
          page: 'page1',
          snippet: 'foo bar'
        }
      ],
      total: 3,
      offset: 0,
      limit: 200,
      has_more: false
    })
    mocks.FetchPageBlocksProgrammatic.mockResolvedValue([
      block('b1', 'foo bar')
    ])
    mocks.SaveFileBlocksProgrammatic.mockResolvedValue(undefined)

    await renderAndPreview('foo', 'qux')

    // Only the real block is a match; the page rows are not offerable.
    expect(
      screen.getByRole('button', { name: 'Replace 1' })
    ).toBeInTheDocument()
    expect(screen.getByText('1 match selected')).toBeInTheDocument()
    expect(screen.queryByText(/Phantom Page/)).toBeNull()

    await fireEvent.click(screen.getByRole('button', { name: 'Replace 1' }))
    await waitFor(
      () => {
        expect(mocks.SaveFileBlocksProgrammatic).toHaveBeenCalledTimes(1)
      },
      { timeout: 2000 }
    )
    const saved = mocks.SaveFileBlocksProgrammatic.mock.calls[0][3] as Array<{
      clean_text: string
    }>
    expect(saved).toHaveLength(1)
    expect(saved[0].clean_text).toBe('qux bar')
  })
})
