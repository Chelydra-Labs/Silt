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

// Capture Events.On registrations so the `types:changed` handler can be fired
// in-test (mirrors AppearanceTab.test.ts's approach).
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
})
