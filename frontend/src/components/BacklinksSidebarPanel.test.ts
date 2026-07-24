import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor
} from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const mocks = vi.hoisted(() => ({
  getBacklinksPaged: vi.fn(),
  eventsOn: vi.fn()
}))

vi.mock('../../bindings/silt/app.js', () => ({
  GetBacklinksPaged: mocks.getBacklinksPaged
}))

vi.mock('@wailsio/runtime', () => ({
  Events: { On: mocks.eventsOn },
  Call: { ByID: vi.fn(), ByName: vi.fn() },
  CancellablePromise: class {},
  Create: { Nullable: (value: unknown) => value, Array: () => [] }
}))

import BacklinksSidebarPanel from './BacklinksSidebarPanel.svelte'

const links = [
  {
    linkKind: 'page' as const,
    sourceNotebook: 'Work',
    sourceSection: 'Projects',
    sourcePage: 'Launch plan',
    source: 'vault',
    sourceBlockId: '',
    snippet: 'See [[Research]] before launch.'
  },
  {
    linkKind: 'block-ref' as const,
    sourceNotebook: 'Work',
    sourceSection: 'Projects',
    sourcePage: 'Launch plan',
    source: 'vault',
    sourceBlockId: 'block-42',
    snippet: 'Evidence from ((block-7)).'
  },
  {
    linkKind: 'embed' as const,
    sourceNotebook: 'Archive',
    sourceSection: '',
    sourcePage: 'Reading notes',
    source: 'linked:archive',
    sourceBlockId: 'block-99',
    snippet: 'Embedded context for the review.'
  }
]

function renderPanel() {
  return render(BacklinksSidebarPanel, {
    props: { notebook: 'Work', section: 'Knowledge', page: 'Research' }
  })
}

function pageResult(results = links, cursor = '', hasMore = false) {
  return { results, cursor, hasMore }
}

describe('BacklinksSidebarPanel', () => {
  let blockChanged: (() => void) | undefined
  const off = vi.fn()

  beforeEach(() => {
    blockChanged = undefined
    off.mockReset()
    mocks.getBacklinksPaged.mockReset().mockResolvedValue(pageResult())
    mocks.eventsOn.mockReset().mockImplementation((event, callback) => {
      if (event === 'block:changed') blockChanged = callback
      return off
    })
  })

  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  it('renders a labelled navigation landmark grouped by source page', async () => {
    renderPanel()

    expect(
      screen.getByRole('navigation', { name: 'Backlinks to current page' })
    ).toBeInTheDocument()
    await screen.findByText('2 pages link here')
    expect(screen.getAllByRole('heading', { level: 3 })).toHaveLength(2)
    expect(screen.getByText('Page link')).toBeInTheDocument()
    expect(screen.getByText('Block reference')).toBeInTheDocument()
    expect(screen.getByText('Embed')).toBeInTheDocument()
    expect(screen.getByText('Evidence from ((block-7)).')).toBeInTheDocument()
    expect(screen.getByText('2 pages link here')).toHaveAttribute(
      'aria-live',
      'polite'
    )
    expect(mocks.getBacklinksPaged).toHaveBeenCalledWith(
      'Work',
      'Knowledge',
      'Research',
      '',
      50
    )
  })

  it('uses separate page and exact-block navigation affordances', async () => {
    renderPanel()
    await screen.findByText('2 pages link here')
    const onPage = vi.fn()
    const onBlock = vi.fn()
    window.addEventListener('navigate-to-page', onPage)
    window.addEventListener('navigate-to-block', onBlock)

    await fireEvent.click(
      screen.getByRole('button', {
        name: /Open block reference in page Launch plan/
      })
    )
    expect((onPage.mock.calls[0][0] as CustomEvent).detail).toEqual({
      notebook: 'Work',
      section: 'Projects',
      page: 'Launch plan',
      source: 'vault'
    })

    const exact = screen.getByRole('button', {
      name: /Jump to exact block for block reference in Launch plan/
    })
    await fireEvent.click(exact)
    expect((onBlock.mock.calls[0][0] as CustomEvent).detail).toEqual({
      notebook: 'Work',
      section: 'Projects',
      page: 'Launch plan',
      blockId: 'block-42',
      source: 'vault'
    })

    window.removeEventListener('navigate-to-page', onPage)
    window.removeEventListener('navigate-to-block', onBlock)
  })

  it('keeps same-coordinate vault and linked backlinks in separate groups', async () => {
    mocks.getBacklinksPaged.mockResolvedValue(
      pageResult([
        links[0],
        { ...links[0], source: 'linked:team-drive', sourceBlockId: 'linked-1' }
      ])
    )
    renderPanel()

    await screen.findByText('2 pages link here')
    expect(screen.getAllByRole('heading', { level: 3 })).toHaveLength(2)
    expect(screen.getByText('Vault')).toBeVisible()
    expect(screen.getByText('Linked')).toBeVisible()
  })

  it('operates both row and exact-block actions from the keyboard', async () => {
    const user = userEvent.setup()
    renderPanel()
    await screen.findByText('2 pages link here')
    const onPage = vi.fn()
    const onBlock = vi.fn()
    window.addEventListener('navigate-to-page', onPage)
    window.addEventListener('navigate-to-block', onBlock)

    const row = screen.getByRole('button', {
      name: /Open embed in page Reading notes/
    })
    row.focus()
    await user.keyboard('{Enter}')
    expect(onPage).toHaveBeenCalledOnce()
    expect(onBlock).not.toHaveBeenCalled()

    const exact = screen.getByRole('button', {
      name: /Jump to exact block for embed in Reading notes/
    })
    exact.focus()
    await user.keyboard(' ')
    expect(onBlock).toHaveBeenCalledOnce()

    window.removeEventListener('navigate-to-page', onPage)
    window.removeEventListener('navigate-to-block', onBlock)
  })

  it('debounces block changes by 200ms and cleans up its listener and timer', async () => {
    vi.useFakeTimers()
    const view = renderPanel()
    vi.runAllTicks()
    await vi.advanceTimersByTimeAsync(0)
    expect(mocks.getBacklinksPaged).toHaveBeenCalledTimes(1)

    blockChanged?.()
    blockChanged?.()
    await vi.advanceTimersByTimeAsync(199)
    expect(mocks.getBacklinksPaged).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(mocks.getBacklinksPaged).toHaveBeenCalledTimes(2)

    blockChanged?.()
    view.unmount()
    await vi.advanceTimersByTimeAsync(200)
    expect(mocks.getBacklinksPaged).toHaveBeenCalledTimes(2)
    expect(off).toHaveBeenCalledOnce()
  })

  it('shows the empty state and authoring hint', async () => {
    mocks.getBacklinksPaged.mockResolvedValue(pageResult([]))
    renderPanel()
    await screen.findByText('No pages link here yet')
    expect(screen.getByText('0 pages link here')).toBeInTheDocument()
    expect(screen.getByText('[[page]]')).toBeInTheDocument()
    expect(screen.getByText('((block))')).toBeInTheDocument()
  })

  it('offers a useful retry after the initial load fails', async () => {
    mocks.getBacklinksPaged.mockRejectedValueOnce(new Error('database busy'))
    renderPanel()
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Backlinks could not be loaded.'
    )
    expect(screen.getByRole('alert')).toHaveTextContent('database busy')
    mocks.getBacklinksPaged.mockResolvedValueOnce(pageResult())
    await fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    await waitFor(() =>
      expect(screen.getByText('2 pages link here')).toBeInTheDocument()
    )
  })

  it('shows an explicit initial loading state', async () => {
    let resolve!: (value: ReturnType<typeof pageResult>) => void
    mocks.getBacklinksPaged.mockReturnValueOnce(
      new Promise((done) => {
        resolve = done
      })
    )
    renderPanel()

    expect(await screen.findByRole('status')).toHaveTextContent(
      'Loading backlinks…'
    )
    resolve(pageResult())
    await screen.findByText('2 pages link here')
  })

  it('loads another page explicitly, appends, and removes duplicate boundaries', async () => {
    mocks.getBacklinksPaged
      .mockResolvedValueOnce(pageResult([links[0]], 'next-page', true))
      .mockResolvedValueOnce(pageResult([links[0], links[2]]))
    renderPanel()

    await screen.findByText('1 page links here')
    await fireEvent.click(
      screen.getByRole('button', { name: 'Load more backlinks' })
    )

    await screen.findByText('2 pages link here')
    expect(screen.getAllByText('See [[Research]] before launch.')).toHaveLength(
      1
    )
    expect(mocks.getBacklinksPaged).toHaveBeenLastCalledWith(
      'Work',
      'Knowledge',
      'Research',
      'next-page',
      50
    )
    expect(
      screen.queryByRole('button', { name: 'Load more backlinks' })
    ).not.toBeInTheDocument()
  })

  it('resets paging and projection when the target changes', async () => {
    mocks.getBacklinksPaged
      .mockResolvedValueOnce(pageResult([links[0]], 'old-cursor', true))
      .mockResolvedValueOnce(pageResult([links[2]]))
    const view = renderPanel()
    await screen.findByText('Launch plan')

    await view.rerender({
      notebook: 'Archive',
      section: '',
      page: 'New target'
    })

    expect(screen.queryByText('Launch plan')).not.toBeInTheDocument()
    expect(await screen.findByRole('status')).toHaveTextContent(
      'Loading backlinks…'
    )
    await screen.findByText('Reading notes')
    expect(mocks.getBacklinksPaged).toHaveBeenLastCalledWith(
      'Archive',
      '',
      'New target',
      '',
      50
    )
  })

  it('retains the projection while refresh is pending and after it fails', async () => {
    renderPanel()
    await screen.findByText('Launch plan')

    let reject!: (reason: Error) => void
    mocks.getBacklinksPaged.mockReturnValueOnce(
      new Promise((_resolve, fail) => {
        reject = fail
      })
    )
    blockChanged?.()
    await new Promise((resolve) => setTimeout(resolve, 210))

    expect(screen.getByText('Launch plan')).toBeVisible()
    expect(screen.getByText('Refreshing backlinks…')).toBeInTheDocument()
    reject(new Error('index unavailable'))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'index unavailable'
    )
    expect(screen.getByText('Launch plan')).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Load more backlinks' })
    ).not.toBeInTheDocument()
  })
})
