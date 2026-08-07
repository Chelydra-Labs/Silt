import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'

const appMocks = vi.hoisted(() =>
  createAppIpcMocks({
    GetPageType: vi.fn(),
    GetPageProperties: vi.fn(),
    GetPageCoreMetadata: vi.fn().mockResolvedValue({
      notebook: '',
      section: '',
      page: '',
      type: '',
      date: '',
      tags: [],
      aliases: [],
      created: '',
      modified: '',
      tagsAreReadOnly: false
    }),
    SetPageCoreMetadata: vi.fn().mockResolvedValue(undefined),
    ListTypes: vi
      .fn()
      .mockResolvedValue({ types: [], errors: [], warnings: [] })
  })
)
vi.mock('$silt-app', () => appMocks)

// pushNotification is imported directly by the controller; stub it so the
// projection-error toast is observable without the real notification store.
const pushNotification = vi.hoisted(() => vi.fn())
vi.mock('../notifications/store.svelte', () => ({
  pushNotification
}))

// Capture Events.On registrations so the `types:changed` /
// `types:projection-error` handlers can be fired in-test (mirrors
// AppearanceTab.test.ts's approach).
const eventsHandlers = {} as Record<string, (ev?: { data?: unknown }) => void>
vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn((name: string, handler: (ev?: { data?: unknown }) => void) => {
      eventsHandlers[name] = handler
      return () => {}
    })
  }
}))

import { createPageTypeController } from './pageTypeState.svelte'

const locator = { notebook: 'Work', section: 'Projects', page: 'Plan' }

beforeEach(() => {
  appMocks.GetPageType.mockReset()
  appMocks.GetPageProperties.mockReset()
  appMocks.GetPageCoreMetadata.mockReset().mockResolvedValue({
    notebook: '',
    section: '',
    page: '',
    type: '',
    date: '',
    tags: [],
    aliases: [],
    created: '',
    modified: '',
    tagsAreReadOnly: false
  })
  appMocks.SetPageCoreMetadata.mockReset().mockResolvedValue(undefined)
  appMocks.ListTypes.mockReset().mockResolvedValue({
    types: [],
    errors: [],
    warnings: []
  })
  pushNotification.mockReset()
})

afterEach(() => {
  for (const k of Object.keys(eventsHandlers)) delete eventsHandlers[k]
})

describe('pageType controller', () => {
  it('refresh() fetches type + properties for the active locator', async () => {
    appMocks.GetPageType.mockResolvedValue({
      typeId: 'book',
      type: { id: 'book', name: 'Book', heroField: 'title' },
      isSet: true,
      rawType: ''
    })
    appMocks.GetPageProperties.mockResolvedValue([
      {
        name: 'title',
        label: 'Title',
        type: 'text',
        value: 'Dune',
        isSet: true,
        required: false
      }
    ])
    const ctrl = createPageTypeController({ getLocator: () => locator })
    const dispose = ctrl.attach()
    await tick()
    await ctrl.refresh()
    await tick()

    expect(appMocks.GetPageType).toHaveBeenCalledWith(
      'Work',
      'Projects',
      'Plan'
    )
    expect(appMocks.GetPageProperties).toHaveBeenCalledWith(
      'Work',
      'Projects',
      'Plan'
    )
    expect(ctrl.info.isSet).toBe(true)
    expect(ctrl.values).toHaveLength(1)
    expect(ctrl.heroValue).toBe('Dune')
    dispose()
  })

  it('clears state when the locator has no page', async () => {
    const ctrl = createPageTypeController({
      getLocator: () => ({ notebook: '', section: '', page: '' })
    })
    await ctrl.refresh()
    expect(appMocks.GetPageType).not.toHaveBeenCalled()
    expect(ctrl.info.isSet).toBe(false)
    expect(ctrl.values).toEqual([])
  })

  it('re-fetches the active page on block:changed for the same locator', async () => {
    appMocks.GetPageType.mockResolvedValue({
      isSet: false,
      type: {},
      rawType: ''
    })
    appMocks.GetPageProperties.mockResolvedValue([])
    const ctrl = createPageTypeController({ getLocator: () => locator })
    const dispose = ctrl.attach()
    await tick()
    await ctrl.refresh()
    await tick()
    const callsAfterFirst = appMocks.GetPageType.mock.calls.length

    eventsHandlers['block:changed']?.({
      data: { notebook: 'Work', section: 'Projects', page: 'Plan' }
    })
    await new Promise((r) => setTimeout(r, 130))
    await tick()

    expect(appMocks.GetPageType.mock.calls.length).toBeGreaterThan(
      callsAfterFirst
    )
    dispose()
  })

  it('ignores block:changed for a different page', async () => {
    appMocks.GetPageType.mockResolvedValue({
      isSet: false,
      type: {},
      rawType: ''
    })
    appMocks.GetPageProperties.mockResolvedValue([])
    const ctrl = createPageTypeController({ getLocator: () => locator })
    const dispose = ctrl.attach()
    await tick()
    await ctrl.refresh()
    await tick()
    const callsAfterFirst = appMocks.GetPageType.mock.calls.length

    eventsHandlers['block:changed']?.({
      data: { notebook: 'Work', section: 'Projects', page: 'Other' }
    })
    await new Promise((r) => setTimeout(r, 130))
    await tick()

    expect(appMocks.GetPageType.mock.calls.length).toBe(callsAfterFirst)
    dispose()
  })

  it('retains ListTypes per-file load errors on the controller', async () => {
    appMocks.ListTypes.mockResolvedValue({
      types: [{ id: 'book', name: 'Book', properties: [] }],
      errors: [{ file: 'broken.yaml', message: 'type broken.yaml is invalid' }],
      warnings: []
    })
    const ctrl = createPageTypeController({ getLocator: () => locator })
    const dispose = ctrl.attach()
    await vi.waitFor(() => {
      expect(ctrl.typesLoading).toBe(false)
    })
    expect(ctrl.typeLoadErrors).toEqual([
      { file: 'broken.yaml', message: 'type broken.yaml is invalid' }
    ])
    expect(ctrl.types).toHaveLength(1)
    dispose()
  })

  it('re-fetches the active page on the types:changed event', async () => {
    appMocks.GetPageType.mockResolvedValue({
      isSet: false,
      type: {},
      rawType: ''
    })
    appMocks.GetPageProperties.mockResolvedValue([])
    const ctrl = createPageTypeController({ getLocator: () => locator })
    const dispose = ctrl.attach()
    await tick()
    await ctrl.refresh()
    await tick()
    const callsAfterFirst = appMocks.GetPageType.mock.calls.length

    // Fire the subscribed `types:changed` handler + advance the debounce.
    eventsHandlers['types:changed']?.()
    await new Promise((r) => setTimeout(r, 130))
    await tick()

    expect(appMocks.GetPageType.mock.calls.length).toBeGreaterThan(
      callsAfterFirst
    )
    dispose()
  })

  it('on types:projection-error, debounces refresh + toast so a burst coalesces', async () => {
    appMocks.GetPageType.mockResolvedValue({
      isSet: false,
      type: {},
      rawType: ''
    })
    appMocks.GetPageProperties.mockResolvedValue([])
    const ctrl = createPageTypeController({ getLocator: () => locator })
    const dispose = ctrl.attach()
    await tick()
    await ctrl.refresh()
    await tick()
    const callsAfterFirst = appMocks.GetPageType.mock.calls.length
    const listCallsAfterFirst = appMocks.ListTypes.mock.calls.length
    pushNotification.mockClear()

    // Burst of projection-error events — must not fire refresh/toast yet.
    eventsHandlers['types:projection-error']?.()
    eventsHandlers['types:projection-error']?.()
    eventsHandlers['types:projection-error']?.()
    await new Promise((r) => setTimeout(r, 0))
    await tick()
    expect(appMocks.GetPageType.mock.calls.length).toBe(callsAfterFirst)
    expect(pushNotification).not.toHaveBeenCalled()

    // After the trailing debounce window, one reload + one toast.
    await new Promise((r) => setTimeout(r, 130))
    await tick()

    expect(appMocks.GetPageType.mock.calls.length).toBeGreaterThan(
      callsAfterFirst
    )
    expect(appMocks.ListTypes.mock.calls.length).toBeGreaterThan(
      listCallsAfterFirst
    )
    expect(pushNotification).toHaveBeenCalledTimes(1)
    expect(pushNotification).toHaveBeenCalledWith(
      expect.objectContaining({ kind: 'info' })
    )
    dispose()
  })

  it('open/close/toggle drive the panelOpen flag', () => {
    const ctrl = createPageTypeController({ getLocator: () => locator })
    expect(ctrl.panelOpen).toBe(false)
    ctrl.open()
    expect(ctrl.panelOpen).toBe(true)
    ctrl.close()
    expect(ctrl.panelOpen).toBe(false)
    ctrl.toggle()
    expect(ctrl.panelOpen).toBe(true)
  })

  it('clears a stale error after a successful refresh follows a failed one', async () => {
    appMocks.GetPageType.mockResolvedValue({
      typeId: 'book',
      type: { id: 'book', name: 'Book' },
      isSet: true,
      rawType: ''
    })
    appMocks.GetPageProperties.mockResolvedValue([])
    const ctrl = createPageTypeController({ getLocator: () => locator })

    // First refresh: GetPageProperties rejects → error banner is shown.
    appMocks.GetPageProperties.mockRejectedValueOnce(new Error('disk gone'))
    await ctrl.refresh()
    await tick()
    expect(ctrl.error).toBe('disk gone')

    // Second refresh succeeds → the stale error must be cleared.
    await ctrl.refresh()
    await tick()
    expect(ctrl.error).toBe('')
    expect(ctrl.info.isSet).toBe(true)
  })

  it('resets info/values at the start of refresh so navigation does not retain the previous page type', async () => {
    // Seed the controller with a resolved typed page.
    appMocks.GetPageType.mockResolvedValue({
      typeId: 'book',
      type: { id: 'book', name: 'Book' },
      isSet: true,
      rawType: ''
    })
    appMocks.GetPageProperties.mockResolvedValue([
      {
        name: 'title',
        label: 'Title',
        type: 'text',
        value: 'Dune',
        isSet: true,
        required: false
      }
    ])
    let current = { ...locator }
    const ctrl = createPageTypeController({ getLocator: () => current })
    await ctrl.refresh()
    await tick()
    expect(ctrl.info.isSet).toBe(true)
    expect(ctrl.values).toHaveLength(1)

    // Navigate to a new page. Block both fetches with controllable resolvers
    // so we can observe the in-flight state before they settle.
    let resolveType!: (v: unknown) => void
    let resolveProps!: (v: unknown) => void
    appMocks.GetPageType.mockReturnValue(
      new Promise((r) => {
        resolveType = r
      })
    )
    appMocks.GetPageProperties.mockReturnValue(
      new Promise((r) => {
        resolveProps = r
      })
    )
    current = { notebook: 'Work', section: 'Projects', page: 'Other' }
    const pending = ctrl.refresh()
    await tick()
    // While the new fetch is in flight, the previous page's data is already
    // gone — navigation shows a clean slate, not the stale chip/fields.
    expect(ctrl.info.isSet).toBe(false)
    expect(ctrl.values).toEqual([])
    expect(ctrl.loading).toBe(true)

    // Resolving with the new page's data replaces (not merges) the old state.
    resolveType({
      typeId: 'movie',
      type: { id: 'movie', name: 'Movie' },
      isSet: true,
      rawType: ''
    })
    resolveProps([])
    await pending
    await tick()
    expect(ctrl.info.type.id).toBe('movie')
    expect(ctrl.values).toEqual([])
  })

  it('preserves mismatched warnings across the post-switch refresh (MB-1 regression)', async () => {
    // commitType runs onMismatched([...]) then onChanged()→refresh() in the
    // same synchronous block. Before the fix, refresh's prologue set
    // mismatched=[] before its first await, so Svelte 5 batched the
    // setMismatched write and the wipe together — the warnings never
    // rendered. The fix gates the clear on a LOCATOR change, so a same-page
    // refresh keeps them.
    appMocks.GetPageType.mockResolvedValue({
      typeId: 'book',
      type: { id: 'book', name: 'Book' },
      isSet: true,
      rawType: ''
    })
    appMocks.GetPageProperties.mockResolvedValue([
      {
        name: 'rating',
        label: 'Rating',
        type: 'number',
        value: 9,
        isSet: true,
        required: false
      }
    ])
    const ctrl = createPageTypeController({ getLocator: () => locator })
    await ctrl.refresh()
    await tick()
    expect(ctrl.mismatched).toEqual([])

    // Mirror commitType's synchronous ordering: setMismatched, THEN refresh()
    // on the SAME locator.
    ctrl.setMismatched(['rating'])
    const pending = ctrl.refresh()
    // The synchronous prologue has already executed before the first await.
    // With the bug, mismatched was [] here (the prologue wiped it).
    expect(ctrl.mismatched).toEqual(['rating'])
    await pending
    await tick()
    // The full fetch must not clear them either.
    expect(ctrl.mismatched).toEqual(['rating'])
  })

  it('clears mismatched warnings when the locator changes to a different page', async () => {
    appMocks.GetPageType.mockResolvedValue({
      isSet: false,
      type: {},
      rawType: ''
    })
    appMocks.GetPageProperties.mockResolvedValue([])
    let current = { ...locator }
    const ctrl = createPageTypeController({ getLocator: () => current })
    ctrl.setMismatched(['rating'])
    // Navigating to a different page clears the warnings — they describe a
    // type switch on the previous page.
    current = { notebook: 'Work', section: 'Projects', page: 'Other' }
    await ctrl.refresh()
    await tick()
    expect(ctrl.mismatched).toEqual([])
  })

  it('same-locator refresh keeps values mounted (no EMPTY wipe) so field focus survives', async () => {
    // Post-commit onChanged→refresh must NOT flash EMPTY_INFO / values=[] —
    // that unmounts every PropertyField (panel shows "Loading…" when
    // loading && values.length === 0) and steals focus from sibling edits.
    appMocks.GetPageType.mockResolvedValue({
      typeId: 'book',
      type: { id: 'book', name: 'Book' },
      isSet: true,
      rawType: ''
    })
    appMocks.GetPageProperties.mockResolvedValue([
      {
        name: 'title',
        label: 'Title',
        type: 'text',
        value: 'Dune',
        isSet: true,
        required: false
      },
      {
        name: 'rating',
        label: 'Rating',
        type: 'number',
        value: 5,
        isSet: true,
        required: false
      }
    ])
    const ctrl = createPageTypeController({ getLocator: () => locator })
    await ctrl.refresh()
    await tick()
    expect(ctrl.values).toHaveLength(2)
    const valuesBefore = ctrl.values

    let resolveType!: (v: unknown) => void
    let resolveProps!: (v: unknown) => void
    appMocks.GetPageType.mockReturnValue(
      new Promise((r) => {
        resolveType = r
      })
    )
    appMocks.GetPageProperties.mockReturnValue(
      new Promise((r) => {
        resolveProps = r
      })
    )
    const pending = ctrl.refresh()
    await tick()
    // In-flight same-page refresh: previous values stay put (fields mounted).
    expect(ctrl.values).toBe(valuesBefore)
    expect(ctrl.values).toHaveLength(2)
    expect(ctrl.info.isSet).toBe(true)
    // No skeleton — panel only shows Loading when values are empty.
    expect(ctrl.loading).toBe(false)

    resolveType({
      typeId: 'book',
      type: { id: 'book', name: 'Book' },
      isSet: true,
      rawType: ''
    })
    resolveProps([
      {
        name: 'title',
        label: 'Title',
        type: 'text',
        value: 'Dune Messiah',
        isSet: true,
        required: false
      },
      {
        name: 'rating',
        label: 'Rating',
        type: 'number',
        value: 5,
        isSet: true,
        required: false
      }
    ])
    await pending
    await tick()
    expect(ctrl.values).toHaveLength(2)
    expect(ctrl.values[0].value).toBe('Dune Messiah')
  })

  it('discards a stale in-flight refresh when navigating to a page-less view before it resolves', async () => {
    // Seed page A (resolved) so there is real data a stale response could paint.
    appMocks.GetPageType.mockResolvedValue({
      typeId: 'book',
      type: { id: 'book', name: 'Book' },
      isSet: true,
      rawType: ''
    })
    appMocks.GetPageProperties.mockResolvedValue([
      {
        name: 'title',
        label: 'Title',
        type: 'text',
        value: 'Dune',
        isSet: true,
        required: false
      }
    ])
    let current = { ...locator }
    const ctrl = createPageTypeController({ getLocator: () => current })
    await ctrl.refresh()
    await tick()
    expect(ctrl.info.isSet).toBe(true)
    expect(ctrl.values).toHaveLength(1)

    // Start a slow refresh for page A (deferred resolvers, still page A).
    let resolveType!: (v: unknown) => void
    let resolveProps!: (v: unknown) => void
    appMocks.GetPageType.mockReturnValue(
      new Promise((r) => {
        resolveType = r
      })
    )
    appMocks.GetPageProperties.mockReturnValue(
      new Promise((r) => {
        resolveProps = r
      })
    )
    const pending = ctrl.refresh()
    await tick()

    // Navigate to a page-less view BEFORE A's response resolves. The early
    // return must invalidate the in-flight token so A cannot repaint later.
    current = { notebook: '', section: '', page: '' }
    await ctrl.refresh()
    await tick()
    expect(ctrl.info.isSet).toBe(false)
    expect(ctrl.values).toEqual([])
    expect(ctrl.loading).toBe(false)

    // A's stale response resolves — it must NOT repaint the cleared state.
    resolveType({
      typeId: 'book',
      type: { id: 'book', name: 'Book' },
      isSet: true,
      rawType: ''
    })
    resolveProps([
      {
        name: 'title',
        label: 'Title',
        type: 'text',
        value: 'Stale',
        isSet: true,
        required: false
      }
    ])
    await pending
    await tick()
    expect(ctrl.info.isSet).toBe(false)
    expect(ctrl.values).toEqual([])
  })

  it('commitCore applies a field-granular update and refetches the core payload (#867)', async () => {
    appMocks.GetPageType.mockResolvedValue({
      isSet: false,
      type: {},
      rawType: ''
    })
    appMocks.GetPageProperties.mockResolvedValue([])
    appMocks.GetPageCoreMetadata.mockResolvedValueOnce({
      notebook: 'Work',
      section: 'Projects',
      page: 'Plan',
      type: '',
      date: '2026-08-05',
      tags: ['work'],
      aliases: [],
      created: '',
      modified: '',
      tagsAreReadOnly: false
    })
    appMocks.GetPageCoreMetadata.mockResolvedValueOnce({
      notebook: 'Work',
      section: 'Projects',
      page: 'Plan',
      type: '',
      date: '2026-09-01',
      tags: ['work'],
      aliases: [],
      created: '',
      modified: '',
      tagsAreReadOnly: false
    })
    const ctrl = createPageTypeController({ getLocator: () => locator })
    await ctrl.refresh()
    await tick()
    expect(ctrl.core.date).toBe('2026-08-05')

    await ctrl.commitCore({ date: '2026-09-01' })
    await tick()

    // Setter called with the changed field only.
    expect(appMocks.SetPageCoreMetadata).toHaveBeenCalledWith(
      'Work',
      'Projects',
      'Plan',
      { date: '2026-09-01' }
    )
    // Refetched the core payload (NOT info/values — a core edit does not
    // reshape the type-defined section).
    expect(ctrl.core.date).toBe('2026-09-01')
  })

  it('commitCore surfaces an IPC error via setError AND rejects so the panel rollback fires (#867, MB#1)', async () => {
    appMocks.GetPageType.mockResolvedValue({
      isSet: false,
      type: {},
      rawType: ''
    })
    appMocks.GetPageProperties.mockResolvedValue([])
    // The real backend echoes the request locator into the core response
    // (app_types_props.go: ParseFileContent is passed the locator, and the
    // response's Notebook/Section/Page are read from the parsed meta).
    // commitCore sources its write target from these fields (coreLocator),
    // so the mock must populate them or the commit early-returns.
    appMocks.GetPageCoreMetadata.mockResolvedValue({
      notebook: 'Work',
      section: 'Projects',
      page: 'Plan',
      type: '',
      date: '',
      tags: [],
      aliases: [],
      created: '',
      modified: '',
      tagsAreReadOnly: false
    })
    appMocks.SetPageCoreMetadata.mockRejectedValueOnce(new Error('disk full'))
    const ctrl = createPageTypeController({ getLocator: () => locator })
    await ctrl.refresh()
    await tick()

    // MB#1: commitCore MUST reject (not swallow) so CoreMetadataSection.commit()'s
    // catch fires the aria-live banner + rollbackNonce. The old swallow returned
    // a resolved promise, leaving the success path to clear the banner.
    await expect(
      ctrl.commitCore({ created: '2026-10-02T08:00:00' })
    ).rejects.toThrow(/disk full/i)
    await tick()

    expect(ctrl.error).toMatch(/disk full/i)
    // Setter was attempted; the refresh was NOT triggered because the commit
    // failed before the refetch step.
    expect(appMocks.SetPageCoreMetadata).toHaveBeenCalled()
    // No core refetch after a failed write (the early throw skips it).
    expect(appMocks.GetPageCoreMetadata).toHaveBeenCalledTimes(1)
  })

  it('commitCore refreshes core.modified from disk truth after a write (#10a)', async () => {
    appMocks.GetPageType.mockResolvedValue({
      isSet: false,
      type: {},
      rawType: ''
    })
    appMocks.GetPageProperties.mockResolvedValue([])
    // Initial load: modified is the pre-write mtime.
    appMocks.GetPageCoreMetadata.mockResolvedValueOnce({
      notebook: 'Work',
      section: 'Projects',
      page: 'Plan',
      type: '',
      date: '2026-08-05',
      tags: [],
      aliases: [],
      created: '',
      modified: '2026-08-05T10:00:00Z',
      tagsAreReadOnly: false
    })
    // Post-write refetch: the backend bumped modified (file just rewritten).
    appMocks.GetPageCoreMetadata.mockResolvedValueOnce({
      notebook: 'Work',
      section: 'Projects',
      page: 'Plan',
      type: '',
      date: '2026-09-01',
      tags: [],
      aliases: [],
      created: '',
      modified: '2026-08-05T11:00:00Z',
      tagsAreReadOnly: false
    })
    const ctrl = createPageTypeController({ getLocator: () => locator })
    await ctrl.refresh()
    await tick()
    expect(ctrl.core.modified).toBe('2026-08-05T10:00:00Z')

    await ctrl.commitCore({ date: '2026-09-01' })
    await tick()

    // The read-only modified field advanced — the post-write refetch landed.
    expect(ctrl.core.modified).toBe('2026-08-05T11:00:00Z')
    expect(ctrl.core.date).toBe('2026-09-01')
  })

  it('commitCore skips the old-page core refetch when the locator changes mid-commit (#10b)', async () => {
    appMocks.GetPageType.mockResolvedValue({
      isSet: false,
      type: {},
      rawType: ''
    })
    appMocks.GetPageProperties.mockResolvedValue([])
    appMocks.GetPageCoreMetadata.mockResolvedValue({
      notebook: 'Work',
      section: 'Projects',
      page: 'A',
      type: '',
      date: '2026-08-05',
      tags: [],
      aliases: [],
      created: '',
      modified: '',
      tagsAreReadOnly: false
    })
    let current = { notebook: 'Work', section: 'Projects', page: 'A' }
    const ctrl = createPageTypeController({ getLocator: () => current })
    await ctrl.refresh()
    await tick()
    const coreCallsAfterRefresh = appMocks.GetPageCoreMetadata.mock.calls.length

    // Block the SET so we can navigate A→B while A's commit is in flight.
    let resolveSet!: (v: unknown) => void
    appMocks.SetPageCoreMetadata.mockReturnValue(
      new Promise((r) => {
        resolveSet = r
      })
    )
    const commitPromise = ctrl.commitCore({ date: '2026-09-01' })
    await tick()
    // Navigate A→B during the in-flight commit.
    current = { notebook: 'Work', section: 'Projects', page: 'B' }
    resolveSet(undefined)
    await commitPromise
    await tick()

    // The locator guard (pageTypeState.svelte.ts:266-271) bailed before the
    // refetch: no extra GetPageCoreMetadata call. Without the guard, A's
    // refetched core would paint onto B's panel as a stale snapshot.
    expect(appMocks.GetPageCoreMetadata.mock.calls.length).toBe(
      coreCallsAfterRefresh
    )
  })

  it('commitCore targets the captured core locator, not the post-navigation locator (NB#3)', async () => {
    // The blur of page A's tags/aliases input fires AFTER the user navigates
    // to page B (the {#key pageLocator} remount destroys A's input, and the
    // browser fires blur on the destroying element). At that moment
    // deps.getLocator() returns B — so without the fix, A's draft was
    // silently written to B. commitCore must source the write target from
    // the locator captured when A's core was loaded (coreLocator), not from
    // deps.getLocator() at commit time.
    appMocks.GetPageType.mockResolvedValue({
      isSet: false,
      type: {},
      rawType: ''
    })
    appMocks.GetPageProperties.mockResolvedValue([])
    appMocks.GetPageCoreMetadata.mockResolvedValue({
      notebook: 'Work',
      section: 'Projects',
      page: 'A',
      type: '',
      date: '2026-08-05',
      tags: ['draft'],
      aliases: [],
      created: '',
      modified: '',
      tagsAreReadOnly: false
    })
    let current = { notebook: 'Work', section: 'Projects', page: 'A' }
    const ctrl = createPageTypeController({ getLocator: () => current })
    await ctrl.refresh()
    await tick()
    expect(ctrl.core.page).toBe('A')
    const coreCallsAfterRefresh = appMocks.GetPageCoreMetadata.mock.calls.length

    // Navigate A→B before the commit fires. (In the real app this triggers
    // a refresh that wipes core; here we just mutate the locator to isolate
    // the commit-target behavior from the wipe/refetch bookkeeping.)
    current = { notebook: 'Work', section: 'Projects', page: 'B' }

    await ctrl.commitCore({ tags: ['draft', 'final'] })
    await tick()

    // The write targeted A (whose core data seeded the edit), NOT B (the
    // post-navigation locator). Pre-fix, SetPageCoreMetadata would have been
    // called with page 'B'.
    expect(appMocks.SetPageCoreMetadata).toHaveBeenCalledWith(
      'Work',
      'Projects',
      'A',
      { tags: ['draft', 'final'] }
    )
    // The post-write refetch is skipped because the locator moved A→B — no
    // extra GetPageCoreMetadata call beyond the refresh() baseline.
    expect(appMocks.GetPageCoreMetadata.mock.calls.length).toBe(
      coreCallsAfterRefresh
    )
  })

  it('wipes core to EMPTY_CORE when the locator loses its page (#10c)', async () => {
    appMocks.GetPageType.mockResolvedValue({
      isSet: false,
      type: {},
      rawType: ''
    })
    appMocks.GetPageProperties.mockResolvedValue([])
    appMocks.GetPageCoreMetadata.mockResolvedValue({
      notebook: 'Work',
      section: 'Projects',
      page: 'A',
      type: '',
      date: '2026-08-05',
      tags: ['stale'],
      aliases: ['ghost'],
      created: '2026-01-01',
      modified: '2026-01-02',
      tagsAreReadOnly: false
    })
    let current: { notebook: string; section: string; page: string } = {
      notebook: 'Work',
      section: 'Projects',
      page: 'A'
    }
    const ctrl = createPageTypeController({ getLocator: () => current })
    await ctrl.refresh()
    await tick()
    expect(ctrl.core.tags).toEqual(['stale'])

    // Navigate to a page-less view — core must wipe so the prior page's fields
    // never paint over the empty chrome.
    current = { notebook: '', section: '', page: '' }
    await ctrl.refresh()
    await tick()

    expect(ctrl.core).toEqual({
      notebook: '',
      section: '',
      page: '',
      type: '',
      date: '',
      tags: [],
      aliases: [],
      created: '',
      modified: '',
      tagsAreReadOnly: false
    })
  })
})
