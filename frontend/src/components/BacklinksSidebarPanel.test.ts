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
  getBacklinks: vi.fn(),
  eventsOn: vi.fn()
}))

vi.mock('../../bindings/silt/app.js', () => ({
  GetBacklinks: mocks.getBacklinks
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

describe('BacklinksSidebarPanel', () => {
  let blockChanged: (() => void) | undefined
  const off = vi.fn()

  beforeEach(() => {
    blockChanged = undefined
    off.mockReset()
    mocks.getBacklinks.mockReset().mockResolvedValue(links)
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
    mocks.getBacklinks.mockResolvedValue([
      links[0],
      { ...links[0], source: 'linked:team-drive', sourceBlockId: 'linked-1' }
    ])
    renderPanel()

    await screen.findByText('2 pages link here')
    expect(screen.getAllByRole('heading', { level: 3 })).toHaveLength(2)
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
    await vi.runAllTicks()
    await vi.advanceTimersByTimeAsync(0)
    expect(mocks.getBacklinks).toHaveBeenCalledTimes(1)

    blockChanged?.()
    blockChanged?.()
    await vi.advanceTimersByTimeAsync(199)
    expect(mocks.getBacklinks).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(mocks.getBacklinks).toHaveBeenCalledTimes(2)

    blockChanged?.()
    view.unmount()
    await vi.advanceTimersByTimeAsync(200)
    expect(mocks.getBacklinks).toHaveBeenCalledTimes(2)
    expect(off).toHaveBeenCalledOnce()
  })

  it('shows the empty state and authoring hint', async () => {
    mocks.getBacklinks.mockResolvedValue([])
    renderPanel()
    await screen.findByText('No pages link here yet')
    expect(screen.getByText('0 pages link here')).toBeInTheDocument()
    expect(screen.getByText('[[page]]')).toBeInTheDocument()
    expect(screen.getByText('((block))')).toBeInTheDocument()
  })

  it('keeps the previous projection and offers retry after a refresh fails', async () => {
    mocks.getBacklinks.mockRejectedValueOnce(new Error('database busy'))
    renderPanel()
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Backlinks could not be refreshed.'
    )
    mocks.getBacklinks.mockResolvedValueOnce(links)
    await fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    await waitFor(() =>
      expect(screen.getByText('2 pages link here')).toBeInTheDocument()
    )
  })
})
