import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  adaptSearchNavigation,
  createRecentPageRecorder,
  hasPageLocator,
  resolveBreadcrumbSectionSelection,
  resolveDashboardOpenTarget,
  resolveSourceNavigationTarget
} from './lib/navigationTargets'

describe('App navigation coordination', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('persists every rapid activation immediately but coalesces preference refreshes', async () => {
    vi.useFakeTimers()
    const persist = vi.fn().mockResolvedValue(undefined)
    const refresh = vi.fn()
    const recorder = createRecentPageRecorder(persist, refresh, vi.fn(), 250)

    recorder.record({ notebook: 'Work', section: 'Notes', page: 'One' })
    recorder.record({ notebook: 'Work', section: 'Notes', page: 'Two' })
    recorder.record({ notebook: 'Work', section: 'Notes', page: 'Three' })

    expect(persist).toHaveBeenCalledTimes(3)
    await Promise.resolve()
    await vi.advanceTimersByTimeAsync(249)
    expect(refresh).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('invalidates pending and in-flight refreshes on a vault transition', async () => {
    vi.useFakeTimers()
    let resolvePersist!: () => void
    const persist = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          resolvePersist = resolve
        })
    )
    const refresh = vi.fn()
    const recorder = createRecentPageRecorder(persist, refresh, vi.fn(), 250)

    recorder.record({ notebook: 'Old', section: 'Notes', page: 'Page' })
    recorder.invalidate()
    resolvePersist()
    await Promise.resolve()
    await vi.runAllTimersAsync()

    expect(refresh).not.toHaveBeenCalled()
  })

  it('waits for older in-flight persistence before refreshing preferences', async () => {
    vi.useFakeTimers()
    const resolvers: Array<() => void> = []
    const persist = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          resolvers.push(resolve)
        })
    )
    const refresh = vi.fn()
    const recorder = createRecentPageRecorder(persist, refresh, vi.fn(), 250)

    recorder.record({ notebook: 'Work', section: 'Notes', page: 'One' })
    recorder.record({ notebook: 'Work', section: 'Notes', page: 'Two' })
    resolvers[1]()
    await Promise.resolve()
    await vi.advanceTimersByTimeAsync(300)
    expect(refresh).not.toHaveBeenCalled()

    resolvers[0]()
    await Promise.resolve()
    await vi.advanceTimersByTimeAsync(250)
    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('scopes a breadcrumb section without opening a descendant page', () => {
    expect(
      resolveBreadcrumbSectionSelection('Inbox', 'Triage', 'Projects')
    ).toEqual({ section: 'Projects', page: '' })
  })

  it('preserves the current page when it is within the clicked section', () => {
    expect(
      resolveBreadcrumbSectionSelection('Projects/Active', 'Launch', 'Projects')
    ).toEqual({ section: 'Projects', page: 'Launch' })
  })

  it('selects the requested source when vault and linked coordinates collide', () => {
    const catalog = [
      {
        source: 'vault',
        notebook: 'Work',
        section: 'Plans',
        page: 'Roadmap'
      },
      {
        source: 'linked:team-drive',
        notebook: 'Work',
        section: 'Plans',
        page: 'Roadmap'
      }
    ]

    expect(
      resolveSourceNavigationTarget(catalog, {
        source: 'linked:team-drive',
        notebook: 'Work',
        section: 'Plans',
        page: 'Roadmap'
      })
    ).toBe(catalog[1])
    expect(
      resolveSourceNavigationTarget(catalog, {
        source: 'vault',
        notebook: 'Work',
        section: 'Plans',
        page: 'Roadmap'
      })
    ).toBe(catalog[0])
  })

  it('adapts a search result without dropping its source-qualified locator', () => {
    expect(
      adaptSearchNavigation({
        id: 'block-712',
        source: 'linked:team-drive',
        notebook: 'Work',
        section: 'Plans',
        page: 'Roadmap',
        file_date: '2026-07-22',
        clean_content: 'Launch plan'
      })
    ).toEqual({
      locator: {
        source: 'linked:team-drive',
        notebook: 'Work',
        section: 'Plans',
        page: 'Roadmap'
      },
      date: '2026-07-22',
      blockId: 'block-712'
    })
  })

  it('leaves a source-less page-link locator unchanged', () => {
    const link = { notebook: 'Work', section: '', page: 'Inbox' }
    expect(resolveSourceNavigationTarget([], link)).toBe(link)
  })

  it('opens a vault dashboard row by path but gates a linked-source row', () => {
    // Vault rows open normally — the tab system can identify them by path.
    expect(
      resolveDashboardOpenTarget({
        source: 'vault',
        notebook: 'Work',
        section: 'Plans',
        page: 'Roadmap'
      })
    ).toEqual({
      kind: 'open',
      ref: { notebook: 'Work', section: 'Plans', page: 'Roadmap' }
    })

    // A linked-source row whose path collides with a vault page would open the
    // wrong tab (tabs carry no source field). Gate it with a clear reason
    // rather than silently dropping source.
    const linked = resolveDashboardOpenTarget({
      source: 'linked:team-drive',
      notebook: 'Work',
      section: 'Plans',
      page: 'Roadmap'
    })
    expect(linked.kind).toBe('blocked')
    if (linked.kind === 'blocked') {
      expect(linked.reason).toBeTruthy()
    }
  })

  it('hasPageLocator requires notebook and page; section may be empty', () => {
    expect(hasPageLocator({ notebook: 'Work', page: 'Plan' })).toBe(true)
    expect(hasPageLocator({ notebook: 'Work', section: '', page: 'Root' })).toBe(
      true
    )
    expect(hasPageLocator({ notebook: 'Work', page: '' })).toBe(false)
    expect(hasPageLocator({ notebook: '', page: 'Plan' })).toBe(false)
    expect(hasPageLocator({ page: 'Plan' })).toBe(false)
    expect(hasPageLocator({ notebook: 'Work' })).toBe(false)
    expect(hasPageLocator({})).toBe(false)
  })
})
