import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { tick } from 'svelte'
import type { Editor } from 'svelte-tiptap'
import { settings } from '../../../settings/store.svelte'
import type {
  SuggestContext,
  MentionContext,
  BlockRefContext,
  TagContext,
  PageLinkContext,
  MetaKey
} from '../../../lib/editor'
import {
  createSuggestsHarness,
  type SuggestsHarness
} from './useSuggestsHarness.svelte'

// --- mocks ------------------------------------------------------------------

const mocks = vi.hoisted(() => ({
  distinctOwners: vi.fn(),
  searchBlocks: vi.fn(),
  queryTagHierarchy: vi.fn(),
  recordTagUsage: vi.fn(),
  searchPages: vi.fn(),
  resolvePageLink: vi.fn(),
  filterMetaKeys: vi.fn(),
  applyMetaSuggestion: vi.fn(),
  filterOwners: vi.fn(),
  applyMentionSuggestion: vi.fn(),
  applyBlockRefSuggestion: vi.fn(),
  filterTags: vi.fn(),
  flattenTagHierarchy: vi.fn(),
  applyTagSuggestion: vi.fn(),
  applyPageLinkSuggestion: vi.fn(),
  dismissPageLinkSuggestion: vi.fn(),
  eventsOn: vi.fn(() => () => {})
}))

vi.mock('../../../../bindings/silt/app.js', () => ({
  DistinctOwners: mocks.distinctOwners,
  SearchBlocks: mocks.searchBlocks,
  QueryTagHierarchy: mocks.queryTagHierarchy,
  RecordTagUsage: mocks.recordTagUsage,
  SearchPages: mocks.searchPages,
  ResolvePageLink: mocks.resolvePageLink
}))

vi.mock('@wailsio/runtime', () => ({
  Events: { On: mocks.eventsOn },
  Call: { ByID: vi.fn(), ByName: vi.fn() },
  CancellablePromise: class {},
  Create: {
    Nullable: (fn: unknown) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

// Apply + filter helpers are mocked so the test controls popup contents and
// asserts apply calls without a live ProseMirror editor. The debounce / race
// primitives (createDebouncedRunner, createRequestRace, cycleSelected,
// ctxStillMatches) stay REAL — they are the units under test.
vi.mock('../../../lib/editor', () => ({
  filterMetaKeys: mocks.filterMetaKeys,
  applyMetaSuggestion: mocks.applyMetaSuggestion,
  filterOwners: mocks.filterOwners,
  applyMentionSuggestion: mocks.applyMentionSuggestion,
  applyBlockRefSuggestion: mocks.applyBlockRefSuggestion,
  filterTags: mocks.filterTags,
  flattenTagHierarchy: mocks.flattenTagHierarchy,
  applyTagSuggestion: mocks.applyTagSuggestion,
  applyPageLinkSuggestion: mocks.applyPageLinkSuggestion,
  dismissPageLinkSuggestion: mocks.dismissPageLinkSuggestion
}))

// --- fixtures ---------------------------------------------------------------

function makeEditor(): Editor {
  return { isDestroyed: false, isEditable: true } as unknown as Editor
}

function metaCtx(query: string, from = 5): SuggestContext {
  return { triggerPos: from, query, from, to: from + 1 + query.length }
}

function mentionCtx(query: string, from = 5): MentionContext {
  return { triggerPos: from, query, from, to: from + 1 + query.length }
}

function blockRefCtx(query: string, from = 5): BlockRefContext {
  return { triggerPos: from, query, from, to: from + 2 + query.length }
}

function pageLinkCtx(query: string, from = 5): PageLinkContext {
  return { triggerPos: from, query, from, to: from + 2 + query.length }
}

function tagCtx(query: string, from = 5): TagContext {
  return { triggerPos: from, query, from, to: from + 1 + query.length }
}

const META_DUE: MetaKey = { key: 'due', label: 'due', description: 'Due date' }

let harness: SuggestsHarness

beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers()
  settings.config = {
    ui: { recent_tags: [] }
  } as unknown as typeof settings.config

  // Shared defaults — individual tests override as needed.
  mocks.filterOwners.mockReturnValue(['Alice', 'Bob'])
  mocks.filterMetaKeys.mockReturnValue([META_DUE])
  mocks.applyMetaSuggestion.mockReturnValue(true)
  mocks.applyMentionSuggestion.mockReturnValue(true)
  mocks.applyBlockRefSuggestion.mockReturnValue(true)
  mocks.applyTagSuggestion.mockReturnValue(true)
  mocks.applyPageLinkSuggestion.mockResolvedValue(true)
  mocks.recordTagUsage.mockResolvedValue(undefined)
  mocks.distinctOwners.mockResolvedValue(['Alice', 'Bob', 'Carol'])
  mocks.searchBlocks.mockResolvedValue([
    {
      id: 'blk1',
      source: '',
      notebook: 'NB',
      section: 'S',
      page: 'P',
      clean_content: 'Hello'
    }
  ])
  mocks.searchPages.mockResolvedValue([
    { notebook: 'NB', section: 'S', page: 'Welcome' },
    { notebook: 'NB', section: 'S', page: 'World' }
  ])

  harness = createSuggestsHarness(() => makeEditor())
})

afterEach(() => {
  harness.destroy()
  vi.useRealTimers()
})

// --- meta controller --------------------------------------------------------

describe('meta controller', () => {
  it('opens via onChange, then selectActive applies the highlighted key and closes', () => {
    const { meta } = harness.controller
    meta.onChange(metaCtx('d'))
    expect(meta.popup).not.toBeNull()
    expect(meta.popup?.items).toHaveLength(1)
    expect(harness.controller.suggestStatus).toBe('1 metadata key available')

    meta.selectActive()
    expect(mocks.applyMetaSuggestion).toHaveBeenCalledWith(
      expect.anything(),
      'due'
    )
    expect(meta.popup).toBeNull()
  })

  it('pick applies an explicit key and closes', () => {
    const { meta } = harness.controller
    meta.onChange(metaCtx('d'))
    meta.pick('owner')
    expect(mocks.applyMetaSuggestion).toHaveBeenCalledWith(
      expect.anything(),
      'owner'
    )
    expect(meta.popup).toBeNull()
  })

  it('onChange(null) closes the popup and clears status', () => {
    const { meta } = harness.controller
    meta.onChange(metaCtx('d'))
    expect(meta.popup).not.toBeNull()
    meta.onChange(null)
    expect(meta.popup).toBeNull()
    expect(harness.controller.suggestStatus).toBe('')
  })
})

// --- mention controller debounce + race -------------------------------------

describe('mention controller debounce + race', () => {
  it('fires the server refine only after the 120ms debounce window', () => {
    const { mention } = harness.controller
    mention.onChange(mentionCtx('a'))
    expect(mocks.distinctOwners).not.toHaveBeenCalled()

    vi.advanceTimersByTime(119)
    expect(mocks.distinctOwners).not.toHaveBeenCalled()

    vi.advanceTimersByTime(1)
    expect(mocks.distinctOwners).toHaveBeenCalledTimes(1)
    expect(mocks.distinctOwners).toHaveBeenCalledWith('a')
  })

  it('coalesces rapid keystrokes so only the latest query hits the server', () => {
    const { mention } = harness.controller
    mention.onChange(mentionCtx('a'))
    mention.onChange(mentionCtx('al'))
    mention.onChange(mentionCtx('ale'))

    vi.advanceTimersByTime(120)
    expect(mocks.distinctOwners).toHaveBeenCalledTimes(1)
    expect(mocks.distinctOwners).toHaveBeenCalledWith('ale')
  })

  it('overrides the instant popup with the server-refined result', async () => {
    const { mention } = harness.controller
    mocks.distinctOwners.mockResolvedValue(['ServerOwner'])

    mention.onChange(mentionCtx('a'))
    // Instant popup comes from the cached filterOwners mock.
    expect(mention.popup?.items).toEqual(['Alice', 'Bob'])

    await vi.advanceTimersByTimeAsync(120)
    expect(mention.popup?.items).toEqual(['ServerOwner'])
    expect(harness.controller.suggestStatus).toBe('1 owner available')
  })

  it('preserves the highlighted owner across keystrokes', () => {
    const { mention } = harness.controller
    mention.onChange(mentionCtx('a'))
    mention.navigate(1)
    expect(mention.popup?.selected).toBe(1) // Bob

    // Reorder so Bob lands at a different index.
    mocks.filterOwners.mockReturnValue(['Carol', 'Bob', 'Alice'])
    mention.onChange(mentionCtx('ab'))
    expect(mention.popup?.selected).toBe(1) // tracked Bob to its new slot
  })

  it('debounces the focus-driven owner refresh by 150ms', () => {
    const { mention } = harness.controller
    mention.refreshOwners()
    expect(mocks.distinctOwners).not.toHaveBeenCalled()

    vi.advanceTimersByTime(149)
    expect(mocks.distinctOwners).not.toHaveBeenCalled()

    vi.advanceTimersByTime(1)
    expect(mocks.distinctOwners).toHaveBeenCalledTimes(1)
    expect(mocks.distinctOwners).toHaveBeenCalledWith('')
  })
})

// --- blockRef controller status transitions --------------------------------

describe('blockRef controller suggest-status transitions', () => {
  it('moves through empty → searching → available → closed', async () => {
    const { blockRef } = harness.controller

    blockRef.onChange(blockRefCtx(''))
    expect(harness.controller.suggestStatus).toBe('Type to search for a block')
    expect(blockRef.popup?.searching).toBe(false)

    blockRef.onChange(blockRefCtx('foo'))
    expect(harness.controller.suggestStatus).toBe('Searching blocks')
    expect(blockRef.popup?.searching).toBe(true)

    await vi.advanceTimersByTimeAsync(180)
    expect(mocks.searchBlocks).toHaveBeenCalledWith('foo')
    expect(harness.controller.suggestStatus).toBe('1 block available')
    expect(blockRef.popup?.searching).toBe(false)
    expect(blockRef.items).toHaveLength(1)

    blockRef.onChange(null)
    expect(blockRef.popup).toBeNull()
    expect(harness.controller.suggestStatus).toBe('')
  })

  it('surfaces a search failure as an error status', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    mocks.searchBlocks.mockRejectedValueOnce(new Error('offline'))

    const { blockRef } = harness.controller
    blockRef.onChange(blockRefCtx('foo'))
    await vi.advanceTimersByTimeAsync(180)

    expect(harness.controller.suggestStatus).toBe('Block search unavailable')
    expect(blockRef.popup?.error).toBe(true)
    expect(blockRef.items).toHaveLength(0)
    expect(errorSpy).toHaveBeenCalledWith(
      'SearchBlocks failed:',
      expect.any(Error)
    )
    errorSpy.mockRestore()
  })
})

// --- tag controller ---------------------------------------------------------

describe('tag controller', () => {
  it('caches the hierarchy for the TTL window so a reopen skips the IPC', async () => {
    mocks.queryTagHierarchy.mockResolvedValue([
      { name: 'work', path: 'work', count: 1, children: [] }
    ])
    mocks.flattenTagHierarchy.mockReturnValue([{ path: 'work', count: 1 }])
    mocks.filterTags.mockReturnValue([{ path: 'work', count: 1 }])

    const { tag } = harness.controller
    tag.onChange(tagCtx('w'))
    await vi.advanceTimersByTimeAsync(0)

    tag.onChange(null)
    tag.onChange(tagCtx('w'))
    await vi.advanceTimersByTimeAsync(0)

    expect(mocks.queryTagHierarchy).toHaveBeenCalledTimes(1)
    expect(mocks.flattenTagHierarchy).toHaveBeenCalledTimes(1)
  })

  it('preserves the highlighted tag across keystrokes', async () => {
    mocks.queryTagHierarchy.mockResolvedValue([])
    mocks.flattenTagHierarchy.mockReturnValue([])
    mocks.filterTags.mockReturnValue([
      { path: 'a', count: 1 },
      { path: 'b', count: 2 },
      { path: 'c', count: 3 }
    ])

    const { tag } = harness.controller
    tag.onChange(tagCtx('x'))
    await vi.advanceTimersByTimeAsync(0)
    tag.navigate(1)
    expect(tag.popup?.selected).toBe(1) // 'b'

    // Reorder so 'b' lands at a different index.
    mocks.filterTags.mockReturnValue([
      { path: 'c', count: 3 },
      { path: 'a', count: 1 },
      { path: 'b', count: 2 }
    ])
    tag.onChange(tagCtx('xy'))
    expect(tag.popup?.selected).toBe(2) // tracked 'b' to its new index
  })

  it('pick applies the tag, records usage, and closes', async () => {
    mocks.queryTagHierarchy.mockResolvedValue([])
    mocks.flattenTagHierarchy.mockReturnValue([{ path: 'work', count: 1 }])
    mocks.filterTags.mockReturnValue([{ path: 'work', count: 1 }])

    const { tag } = harness.controller
    tag.onChange(tagCtx('w'))
    await vi.advanceTimersByTimeAsync(0)

    tag.pick('work')
    expect(mocks.applyTagSuggestion).toHaveBeenCalledWith(
      expect.anything(),
      'work'
    )
    expect(mocks.recordTagUsage).toHaveBeenCalledWith('work')
    expect(tag.popup).toBeNull()
  })

  it('surfaces a hierarchy load failure as an error status', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    mocks.queryTagHierarchy.mockRejectedValueOnce(new Error('offline'))
    mocks.flattenTagHierarchy.mockReturnValue([])
    mocks.filterTags.mockReturnValue([])

    const { tag } = harness.controller
    tag.onChange(tagCtx('w'))
    await vi.advanceTimersByTimeAsync(0)

    expect(tag.tagsLoadError).toBe(true)
    expect(harness.controller.suggestStatus).toBe('Tag suggestions unavailable')
    expect(errorSpy).toHaveBeenCalledWith(
      'QueryTagHierarchy failed:',
      expect.any(Error)
    )
    errorSpy.mockRestore()
  })

  it('re-ranks an open picker when recent_tags changes on hot config', async () => {
    mocks.queryTagHierarchy.mockResolvedValue([])
    mocks.flattenTagHierarchy.mockReturnValue([])
    mocks.filterTags.mockReturnValue([{ path: 'work', count: 1 }])

    const { tag } = harness.controller
    // Flush the initial $effect run (popup still null → no-op) so it does
    // not inflate the filterTags call count asserted below.
    await vi.advanceTimersByTimeAsync(0)

    tag.onChange(tagCtx('w'))
    await vi.advanceTimersByTimeAsync(0)

    const callsBefore = mocks.filterTags.mock.calls.length

    settings.config = {
      ui: { recent_tags: ['work'] }
    } as unknown as typeof settings.config
    await tick()

    expect(mocks.filterTags.mock.calls.length).toBeGreaterThan(callsBefore)
    expect(mocks.filterTags).toHaveBeenLastCalledWith([], 'w', ['work'])
  })
})

// --- pageLink controller gates ----------------------------------------------

describe('pageLink controller gates', () => {
  it('requires at least 2 non-space characters before searching', () => {
    const { pageLink } = harness.controller
    expect(pageLink.hasEnoughQuery('a')).toBe(false)
    expect(pageLink.hasEnoughQuery(' a ')).toBe(false)
    expect(pageLink.hasEnoughQuery('ab')).toBe(true)
    expect(pageLink.hasEnoughQuery('a b')).toBe(true)

    pageLink.onChange(pageLinkCtx('a'))
    expect(harness.controller.suggestStatus).toBe(
      'Type at least 2 characters for page suggestions'
    )
    expect(mocks.searchPages).not.toHaveBeenCalled()
  })

  it('debounces the page search by 150ms and reports availability', async () => {
    const { pageLink } = harness.controller
    pageLink.onChange(pageLinkCtx('ab'))
    expect(harness.controller.suggestStatus).toBe('Searching pages')
    expect(mocks.searchPages).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(150)
    expect(mocks.searchPages).toHaveBeenCalledWith('ab', 50)
    expect(harness.controller.suggestStatus).toBe('2 pages available')
    expect(pageLink.items).toHaveLength(2)
  })

  it('blocks navigation while a link resolve is in flight', async () => {
    const { pageLink } = harness.controller
    let resolvePick!: (value: boolean) => void
    mocks.applyPageLinkSuggestion.mockReturnValue(
      new Promise<boolean>((res) => {
        resolvePick = res
      })
    )

    pageLink.onChange(pageLinkCtx('ab'))
    await vi.advanceTimersByTimeAsync(150)
    expect(pageLink.items).toHaveLength(2)
    expect(pageLink.popup?.selected).toBe(0)

    // Navigation is live while only searching has happened (resolving false).
    pageLink.navigate(1)
    expect(pageLink.popup?.selected).toBe(1)

    // A pick flips resolving on — the canNavigate gate must lock arrow keys.
    void pageLink.selectActive()
    expect(pageLink.resolving).toBe(true)
    expect(harness.controller.suggestStatus).toBe('Resolving page link')

    pageLink.navigate(1)
    expect(pageLink.popup?.selected).toBe(1) // locked, would have wrapped to 0

    resolvePick(true)
    await vi.advanceTimersByTimeAsync(0)
    expect(pageLink.popup).toBeNull()
    expect(harness.controller.suggestStatus).toBe('')
  })
})
