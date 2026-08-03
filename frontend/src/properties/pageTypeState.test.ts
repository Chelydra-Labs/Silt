import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'

const appMocks = vi.hoisted(() =>
  createAppIpcMocks({
    GetPageType: vi.fn(),
    GetPageProperties: vi.fn(),
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

  it('on types:projection-error, refreshes the active page and surfaces a transient warning', async () => {
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

    // Fire the subscribed `types:projection-error` handler. There's no
    // debounce on this path (unlike `types:changed`) — the refresh + toast
    // fire immediately.
    eventsHandlers['types:projection-error']?.()
    await new Promise((r) => setTimeout(r, 0))
    await tick()

    // (a) refresh triggered: GetPageType was called again.
    expect(appMocks.GetPageType.mock.calls.length).toBeGreaterThan(
      callsAfterFirst
    )
    // (b) warning surface received a non-error (info/polite) notification.
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
})
