import { afterEach, describe, expect, it, vi } from 'vitest'
import * as AppModule from './App.svelte'

type Ref = { notebook: string; section: string; page: string }
const {
  adaptSearchNavigation,
  createRecentPageRecorder,
  resolveBreadcrumbSectionSelection,
  resolveSourceNavigationTarget
} = AppModule as unknown as {
  adaptSearchNavigation: (result: {
    id: string
    source: string
    notebook: string
    section: string
    page: string
    file_date: string
    clean_content: string
  }) => {
    locator: Ref & { source?: string }
    date: string
    blockId: string
  }
  createRecentPageRecorder: (
    persist: (ref: Ref) => Promise<unknown>,
    refresh: () => void,
    onError: (error: unknown) => void,
    delay?: number
  ) => {
    record: (ref: Ref) => void
    invalidate: () => void
  }
  resolveBreadcrumbSectionSelection: (
    currentSection: string,
    currentPage: string,
    selectedSection: string
  ) => { section: string; page: string }
  resolveSourceNavigationTarget: <T extends Ref & { source?: string }>(
    catalog: readonly T[],
    target: Ref & { source?: string }
  ) => Ref & { source?: string }
}

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
})
