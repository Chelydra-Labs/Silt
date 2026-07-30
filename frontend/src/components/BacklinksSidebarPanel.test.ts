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
  getUnlinkedMentionsPaged: vi.fn(),
  promoteUnlinkedMention: vi.fn(),
  eventsOn: vi.fn(),
  pushNotification: vi.fn()
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    GetBacklinksPaged: mocks.getBacklinksPaged,
    GetUnlinkedMentionsPaged: mocks.getUnlinkedMentionsPaged,
    PromoteUnlinkedMention: mocks.promoteUnlinkedMention
  })
)

vi.mock('../notifications/store.svelte', () => ({
  pushNotification: mocks.pushNotification
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
    mocks.getUnlinkedMentionsPaged.mockReset().mockResolvedValue({
      results: [],
      cursor: '',
      hasMore: false,
      truncated: false
    })
    mocks.promoteUnlinkedMention.mockReset().mockResolvedValue(undefined)
    mocks.pushNotification.mockReset()
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

  it('renders the Unlinked mentions section collapsed by default, then expands to reveal Link buttons', async () => {
    mocks.getUnlinkedMentionsPaged.mockResolvedValue({
      results: [
        {
          source: 'vault',
          sourceNotebook: 'Work',
          sourceSection: 'Projects',
          sourcePage: 'Launch plan',
          sourceBlockIds: ['block-42'],
          sourceSnippets: ['review the Research notes before launch'],
          matchCount: 1,
          title: 'Research',
          ambiguous: false
        }
      ],
      cursor: '',
      hasMore: false,
      truncated: false
    })
    renderPanel()
    await screen.findByText('1 page mentions this title')

    const disclosure = screen.getByRole('button', {
      name: /Unlinked mentions/
    })
    expect(disclosure).toHaveAttribute('aria-expanded', 'false')
    // Collapsed by default: no Link button yet.
    expect(
      screen.queryByRole('button', { name: /Link mention of Research/ })
    ).not.toBeInTheDocument()

    await fireEvent.click(disclosure)
    expect(disclosure).toHaveAttribute('aria-expanded', 'true')
    expect(
      await screen.findByRole('button', {
        name: /Link mention of Research in block block-42/
      })
    ).toBeInTheDocument()
    // Snippet is primary display (not a truncated block id).
    expect(screen.getByText(/review the/, { exact: false })).toBeInTheDocument()
    expect(
      screen.getByText('Research', { selector: 'mark' })
    ).toBeInTheDocument()
    expect(screen.queryByText(/block-42/)).not.toBeInTheDocument()
  })

  it('emphasizes residual plain title in mixed [[link]] + plain snippets', async () => {
    mocks.getUnlinkedMentionsPaged.mockResolvedValue({
      results: [
        {
          source: 'vault',
          sourceNotebook: 'Work',
          sourceSection: 'Projects',
          sourcePage: 'Launch plan',
          // aria-label uses blockId.slice(0, 8) → "block-42"
          sourceBlockIds: ['block-42'],
          sourceSnippets: ['see [[Onboarding]] for the Onboarding details'],
          matchCount: 1,
          title: 'Onboarding',
          ambiguous: false
        }
      ],
      cursor: '',
      hasMore: false
    })
    renderPanel()
    await screen.findByText('1 page mentions this title')
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    await screen.findByRole('button', {
      name: /Link mention of Onboarding in block block-42/
    })

    const marked = screen.getByText('Onboarding', { selector: 'mark' })
    expect(marked).toBeInTheDocument()
    const row = marked.parentElement
    expect(row?.textContent).toBe(
      'see [[Onboarding]] for the Onboarding details'
    )
    // Residual plain is marked: wiki link appears before <mark>, not inside it.
    const html = row?.innerHTML ?? ''
    expect(html).toMatch(
      /\[\[Onboarding\]\][\s\S]*<mark[^>]*>Onboarding<\/mark>/
    )
    expect(html).not.toMatch(/^[\s\S]*<mark[^>]*>Onboarding<\/mark>[\s\S]*\[\[/)
  })

  it('skips heading/alias wiki spans when emphasizing residual plain titles', async () => {
    mocks.getUnlinkedMentionsPaged.mockResolvedValue({
      results: [
        {
          source: 'vault',
          sourceNotebook: 'Work',
          sourceSection: 'Projects',
          sourcePage: 'Launch plan',
          sourceBlockIds: ['block-42'],
          sourceSnippets: [
            'see [[Onboarding#Setup|start]] then Onboarding again'
          ],
          matchCount: 1,
          title: 'Onboarding',
          ambiguous: false
        }
      ],
      cursor: '',
      hasMore: false
    })
    renderPanel()
    await screen.findByText('1 page mentions this title')
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    await screen.findByRole('button', {
      name: /Link mention of Onboarding in block block-42/
    })

    const marked = screen.getByText('Onboarding', { selector: 'mark' })
    const html = marked.parentElement?.innerHTML ?? ''
    // Full wiki span (heading+alias) stays unmarked; residual plain is marked.
    expect(html).toMatch(
      /\[\[Onboarding#Setup\|start\]\][\s\S]*<mark[^>]*>Onboarding<\/mark>/
    )
  })

  it('calls PromoteUnlinkedMention on Link and migrates the row out of the unlinked leg', async () => {
    // Return the mention until PromoteUnlinkedMention runs; the post-promote
    // refresh's loadUnlinked then returns the now-empty page (server migrated
    // the mention into the backlinks leg), so the row disappears for good.
    let unlinkedCleared = false
    const mention = {
      source: 'vault',
      sourceNotebook: 'Work',
      sourceSection: 'Projects',
      sourcePage: 'Launch plan',
      sourceBlockIds: ['block-42'],
      sourceSnippets: ['see Research here'],
      matchCount: 1,
      title: 'Research',
      ambiguous: false
    }
    mocks.getUnlinkedMentionsPaged.mockImplementation(() =>
      Promise.resolve({
        results: unlinkedCleared ? [] : [mention],
        cursor: '',
        hasMore: false
      })
    )
    mocks.promoteUnlinkedMention.mockImplementation(async () => {
      unlinkedCleared = true
    })
    renderPanel()
    await screen.findByText('1 page mentions this title')
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )

    await fireEvent.click(
      await screen.findByRole('button', {
        name: /Link mention of Research in block block-42/
      })
    )

    await waitFor(() =>
      expect(mocks.promoteUnlinkedMention).toHaveBeenCalledWith(
        'block-42',
        'Work',
        'Knowledge',
        'Research'
      )
    )
    await waitFor(() =>
      expect(
        screen.queryByRole('button', { name: /Link mention of Research/ })
      ).not.toBeInTheDocument()
    )
  })

  it('renders candidate chips for ambiguous mentions and promotes the chosen path', async () => {
    mocks.getUnlinkedMentionsPaged.mockResolvedValue({
      results: [
        {
          source: 'vault',
          sourceNotebook: 'Work',
          sourceSection: 'Journal',
          sourcePage: 'Notes',
          sourceBlockIds: ['block-7'],
          sourceSnippets: ['Standup notes from today'],
          matchCount: 1,
          title: 'Standup',
          ambiguous: true,
          candidates: [
            {
              source: 'vault',
              notebook: 'Work',
              section: 'Journal',
              page: 'Standup'
            },
            {
              source: 'vault',
              notebook: 'Work',
              section: 'Log',
              page: 'Standup'
            }
          ]
        }
      ],
      cursor: '',
      hasMore: false
    })
    renderPanel()
    await screen.findByText('1 page mentions this title')
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )

    expect(await screen.findByText('Ambiguous')).toBeInTheDocument()
    // No single Link action — chips only.
    expect(
      screen.queryByRole('button', { name: /^Link$/ })
    ).not.toBeInTheDocument()

    const journalChip = await screen.findByRole('button', {
      name: /Link mention of Standup as Work\/Journal\/Standup/
    })
    await fireEvent.click(journalChip)

    await waitFor(() =>
      expect(mocks.promoteUnlinkedMention).toHaveBeenCalledWith(
        'block-7',
        'Work',
        'Journal',
        'Standup'
      )
    )
    await waitFor(() =>
      expect(mocks.pushNotification).toHaveBeenCalledWith(
        expect.objectContaining({
          kind: 'success',
          message: expect.stringContaining('as Work/Journal/Standup')
        })
      )
    )
  })

  it('retries unlinked load via Try again after a fetch error', async () => {
    mocks.getUnlinkedMentionsPaged
      .mockRejectedValueOnce(new Error('index unavailable'))
      .mockResolvedValueOnce({
        results: [
          {
            source: 'vault',
            sourceNotebook: 'Work',
            sourceSection: 'Projects',
            sourcePage: 'Launch plan',
            sourceBlockIds: ['block-42'],
            sourceSnippets: ['see Research here'],
            matchCount: 1,
            title: 'Research',
            ambiguous: false
          }
        ],
        cursor: '',
        hasMore: false
      })
    renderPanel()
    await screen.findByText('Unlinked mentions could not be loaded.')
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    const retry = await screen.findByRole('button', { name: 'Try again' })
    await fireEvent.click(retry)
    await screen.findByText('1 page mentions this title')
    expect(mocks.getUnlinkedMentionsPaged).toHaveBeenCalledTimes(2)
  })

  it('keeps remaining block snippets paired after promoting one of two blocks', async () => {
    // After promote, refresh reloads unlinked — return the post-promote shape
    // so the optimistic drop is not overwritten with the pre-promote payload.
    let promoted = false
    mocks.getUnlinkedMentionsPaged.mockImplementation(() =>
      Promise.resolve({
        results: promoted
          ? [
              {
                source: 'vault',
                sourceNotebook: 'Work',
                sourceSection: 'Projects',
                sourcePage: 'Meeting notes',
                sourceBlockIds: ['block-99'],
                sourceSnippets: ['second Research note'],
                matchCount: 1,
                title: 'Research',
                ambiguous: false
              }
            ]
          : [
              {
                source: 'vault',
                sourceNotebook: 'Work',
                sourceSection: 'Projects',
                sourcePage: 'Meeting notes',
                sourceBlockIds: ['block-42', 'block-99'],
                sourceSnippets: ['first Research note', 'second Research note'],
                matchCount: 2,
                title: 'Research',
                ambiguous: false
              }
            ],
        cursor: '',
        hasMore: false
      })
    )
    mocks.promoteUnlinkedMention.mockImplementation(async () => {
      promoted = true
    })
    renderPanel()
    await screen.findByText('1 page mentions this title')
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    await fireEvent.click(
      await screen.findByRole('button', {
        name: /Link mention of Research in block block-42/
      })
    )
    await waitFor(() =>
      expect(
        screen.queryByRole('button', {
          name: /Link mention of Research in block block-42/
        })
      ).not.toBeInTheDocument()
    )
    expect(screen.getByText(/second/)).toBeInTheDocument()
    expect(
      screen.getByRole('button', {
        name: /Link mention of Research in block block-99/
      })
    ).toBeInTheDocument()
  })

  it('loads more unlinked mentions via the Load more button and appends', async () => {
    const first = {
      source: 'vault',
      sourceNotebook: 'Work',
      sourceSection: 'Projects',
      sourcePage: 'Daily log',
      sourceBlockIds: ['block-42'],
      sourceSnippets: ['Research today'],
      matchCount: 1,
      title: 'Research',
      ambiguous: false
    }
    const second = {
      source: 'vault',
      sourceNotebook: 'Archive',
      sourceSection: '',
      sourcePage: 'Inbox notes',
      sourceBlockIds: ['block-7'],
      sourceSnippets: ['more Research'],
      matchCount: 1,
      title: 'Research',
      ambiguous: false
    }
    mocks.getUnlinkedMentionsPaged
      .mockResolvedValueOnce({
        results: [first],
        cursor: 'next-page',
        hasMore: true
      })
      .mockResolvedValueOnce({ results: [second], cursor: '', hasMore: false })
    renderPanel()

    await screen.findByText('1+ pages mention this title')
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )

    const loadMore = await screen.findByRole('button', {
      name: 'Load more unlinked mentions'
    })
    await fireEvent.click(loadMore)

    await screen.findByText('2 pages mention this title')
    expect(screen.getByText('Daily log')).toBeInTheDocument()
    expect(screen.getByText('Inbox notes')).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Load more unlinked mentions' })
    ).not.toBeInTheDocument()
  })

  it('disables only the in-flight row while a promote is pending (per-row busy)', async () => {
    let resolvePromote!: () => void
    mocks.getUnlinkedMentionsPaged.mockResolvedValue({
      results: [
        {
          source: 'vault',
          sourceNotebook: 'Work',
          sourceSection: 'Projects',
          sourcePage: 'Meeting notes',
          sourceBlockIds: ['block-42', 'block-99'],
          sourceSnippets: ['first Research', 'second Research'],
          matchCount: 2,
          title: 'Research',
          ambiguous: false
        }
      ],
      cursor: '',
      hasMore: false
    })
    mocks.promoteUnlinkedMention.mockImplementation(
      () => new Promise<void>((resolve) => (resolvePromote = resolve))
    )
    renderPanel()
    await screen.findByText('1 page mentions this title')
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )

    const first = await screen.findByRole('button', {
      name: /Link mention of Research in block block-42/
    })
    const second = await screen.findByRole('button', {
      name: /Link mention of Research in block block-99/
    })

    await fireEvent.click(first)
    await waitFor(() => expect(first).toBeDisabled())
    // Per-row busy: only the in-flight block is locked; the sibling stays open.
    expect(second).not.toBeDisabled()

    resolvePromote()
    await waitFor(() =>
      expect(mocks.promoteUnlinkedMention).toHaveBeenCalledWith(
        'block-42',
        'Work',
        'Knowledge',
        'Research'
      )
    )
  })

  it('shows incomplete status when FTS scan is truncated', async () => {
    mocks.getUnlinkedMentionsPaged.mockResolvedValue({
      results: [
        {
          source: 'vault',
          sourceNotebook: 'Work',
          sourceSection: 'Projects',
          sourcePage: 'Launch plan',
          sourceBlockIds: ['block-42'],
          sourceSnippets: ['see Research here'],
          matchCount: 1,
          title: 'Research',
          ambiguous: false
        }
      ],
      cursor: '',
      hasMore: false,
      truncated: true
    })
    renderPanel()
    await screen.findByText(/may be incomplete/)
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    expect(
      await screen.findByText(
        /Results may be incomplete — matching text is capped for performance/
      )
    ).toBeInTheDocument()
  })

  it('does not show incomplete banner when truncated is false or omitted', async () => {
    mocks.getUnlinkedMentionsPaged.mockResolvedValue({
      results: [
        {
          source: 'vault',
          sourceNotebook: 'Work',
          sourceSection: 'Projects',
          sourcePage: 'Launch plan',
          sourceBlockIds: ['block-42'],
          sourceSnippets: ['see Research here'],
          matchCount: 1,
          title: 'Research',
          ambiguous: false
        }
      ],
      cursor: '',
      hasMore: false
      // truncated omitted — defaults false via Boolean(result?.truncated)
    } as { results: unknown[]; cursor: string; hasMore: boolean })
    renderPanel()
    await screen.findByText('1 page mentions this title')
    expect(screen.queryByText(/may be incomplete/)).not.toBeInTheDocument()
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    expect(
      screen.queryByText(/Results may be incomplete/)
    ).not.toBeInTheDocument()
  })

  it('keeps incomplete banner after Load more when scan is truncated', async () => {
    const mention = (page: string, blockId: string) => ({
      source: 'vault',
      sourceNotebook: 'Work',
      sourceSection: 'Projects',
      sourcePage: page,
      sourceBlockIds: [blockId],
      sourceSnippets: ['see Research here'],
      matchCount: 1,
      title: 'Research',
      ambiguous: false
    })
    mocks.getUnlinkedMentionsPaged
      .mockResolvedValueOnce({
        results: [mention('Page A', 'block-01')],
        cursor: 'cursor-1',
        hasMore: true,
        truncated: true
      })
      .mockResolvedValueOnce({
        results: [mention('Page B', 'block-02')],
        cursor: '',
        hasMore: false,
        truncated: true
      })
    renderPanel()
    await screen.findByText(/may be incomplete/)
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    expect(
      await screen.findByText(
        /Results may be incomplete — matching text is capped for performance/
      )
    ).toBeInTheDocument()
    await fireEvent.click(
      screen.getByRole('button', { name: 'Load more unlinked mentions' })
    )
    await screen.findByText('Page B')
    expect(
      screen.getByText(
        /Results may be incomplete — matching text is capped for performance/
      )
    ).toBeInTheDocument()
    expect(
      screen.getByText(/pages mention this title · may be incomplete/)
    ).toBeInTheDocument()
  })

  it('shows truncated-only empty state when scan is capped with no residual pages', async () => {
    mocks.getUnlinkedMentionsPaged.mockResolvedValue({
      results: [],
      cursor: '',
      hasMore: false,
      truncated: true
    })
    renderPanel()
    await screen.findByText(
      /Scan capped — no promotable plain mentions in the first results/
    )
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    expect(
      await screen.findByText(
        /Matching text is capped for performance — no promotable plain mentions/
      )
    ).toBeInTheDocument()
  })

  it('keeps truncated cue after a failed same-page refresh', async () => {
    mocks.getUnlinkedMentionsPaged.mockResolvedValue({
      results: [
        {
          source: 'vault',
          sourceNotebook: 'Work',
          sourceSection: 'Projects',
          sourcePage: 'Launch plan',
          sourceBlockIds: ['block-42'],
          sourceSnippets: ['see Research here'],
          matchCount: 1,
          title: 'Research',
          ambiguous: false
        }
      ],
      cursor: '',
      hasMore: false,
      truncated: true
    })
    renderPanel()
    await screen.findByText(/may be incomplete/)
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    await screen.findByText(
      /Results may be incomplete — matching text is capped for performance/
    )

    let reject!: (reason: Error) => void
    mocks.getUnlinkedMentionsPaged.mockReturnValueOnce(
      new Promise((_resolve, fail) => {
        reject = fail
      })
    )
    blockChanged?.()
    await new Promise((resolve) => setTimeout(resolve, 210))
    reject(new Error('index unavailable'))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'index unavailable'
    )
    // Prior residual row + incompleteness cue survive the failed refresh.
    // Backlinks + unlinked both name a Launch plan open control.
    expect(
      screen.getAllByRole('button', { name: 'Open page Launch plan' }).length
    ).toBeGreaterThanOrEqual(2)
    expect(
      await screen.findByText(
        /Results may be incomplete — matching text is capped for performance/
      )
    ).toBeInTheDocument()
    await waitFor(() => {
      // Header subtitle + expanded status both mention incompleteness.
      expect(
        screen.getAllByText(/may be incomplete/).length
      ).toBeGreaterThanOrEqual(2)
    })
  })
})
