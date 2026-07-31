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

// Wire-shaped fixtures match Go encoding/json tags (Wails IPC). The panel
// normalizes to a camelCase view model at the boundary.
const links = [
  {
    linkKind: 'page' as const,
    source_notebook: 'Work',
    source_section: 'Projects',
    source_page: 'Launch plan',
    source: 'vault',
    source_block_id: '',
    snippet: 'See [[Research]] before launch.'
  },
  {
    linkKind: 'block-ref' as const,
    source_notebook: 'Work',
    source_section: 'Projects',
    source_page: 'Launch plan',
    source: 'vault',
    source_block_id: 'block-42',
    snippet: 'Evidence from ((block-7)).'
  },
  {
    linkKind: 'embed' as const,
    source_notebook: 'Archive',
    source_section: '',
    source_page: 'Reading notes',
    source: 'linked:archive',
    source_block_id: 'block-99',
    snippet: 'Embedded context for the review.'
  }
]

function renderPanel() {
  return render(BacklinksSidebarPanel, {
    props: { notebook: 'Work', section: 'Knowledge', page: 'Research' }
  })
}

function pageResult(results = links, cursor = '', hasMore = false) {
  return { results, cursor, has_more: hasMore }
}

function unlinkedWire(opts: {
  results?: Record<string, unknown>[]
  cursor?: string
  hasMore?: boolean
  truncated?: boolean
  scanCursor?: string
}) {
  return {
    results: opts.results ?? [],
    cursor: opts.cursor ?? '',
    has_more: opts.hasMore ?? false,
    truncated: opts.truncated ?? false,
    scan_cursor: opts.scanCursor ?? ''
  }
}

function unlinkedMentionWire(partial: {
  source_page: string
  source_block_ids?: string[]
  source_snippets?: string[]
  source_notebook?: string
  source_section?: string
  source?: string
  match_count?: number
  title?: string
  ambiguous?: boolean
  candidates?: {
    source?: string
    notebook: string
    section: string
    page: string
  }[]
}) {
  return {
    source: partial.source ?? 'vault',
    source_notebook: partial.source_notebook ?? 'Work',
    source_section: partial.source_section ?? 'Projects',
    source_page: partial.source_page,
    source_block_ids: partial.source_block_ids ?? ['block-42'],
    source_snippets: partial.source_snippets ?? ['see Research here'],
    match_count: partial.match_count ?? 1,
    title: partial.title ?? 'Research',
    ambiguous: partial.ambiguous ?? false,
    ...(partial.candidates ? { candidates: partial.candidates } : {})
  }
}

describe('BacklinksSidebarPanel', () => {
  let blockChanged: (() => void) | undefined
  const off = vi.fn()

  beforeEach(() => {
    blockChanged = undefined
    off.mockReset()
    mocks.getBacklinksPaged.mockReset().mockResolvedValue(pageResult())
    mocks.getUnlinkedMentionsPaged
      .mockReset()
      .mockResolvedValue(unlinkedWire({}))
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
        {
          ...links[0],
          source: 'linked:team-drive',
          source_block_id: 'linked-1'
        }
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
          source_notebook: 'Work',
          source_section: 'Projects',
          source_page: 'Launch plan',
          source_block_ids: ['block-42'],
          source_snippets: ['review the Research notes before launch'],
          match_count: 1,
          title: 'Research',
          ambiguous: false
        }
      ],
      cursor: '',
      has_more: false,
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
          source_notebook: 'Work',
          source_section: 'Projects',
          source_page: 'Launch plan',
          // aria-label uses blockId.slice(0, 8) → "block-42"
          source_block_ids: ['block-42'],
          source_snippets: ['see [[Onboarding]] for the Onboarding details'],
          match_count: 1,
          title: 'Onboarding',
          ambiguous: false
        }
      ],
      cursor: '',
      has_more: false
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
          source_notebook: 'Work',
          source_section: 'Projects',
          source_page: 'Launch plan',
          source_block_ids: ['block-42'],
          source_snippets: [
            'see [[Onboarding#Setup|start]] then Onboarding again'
          ],
          match_count: 1,
          title: 'Onboarding',
          ambiguous: false
        }
      ],
      cursor: '',
      has_more: false
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
      source_notebook: 'Work',
      source_section: 'Projects',
      source_page: 'Launch plan',
      source_block_ids: ['block-42'],
      source_snippets: ['see Research here'],
      match_count: 1,
      title: 'Research',
      ambiguous: false
    }
    mocks.getUnlinkedMentionsPaged.mockImplementation(() =>
      Promise.resolve({
        results: unlinkedCleared ? [] : [mention],
        cursor: '',
        has_more: false
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
          source_notebook: 'Work',
          source_section: 'Journal',
          source_page: 'Notes',
          source_block_ids: ['block-7'],
          source_snippets: ['Standup notes from today'],
          match_count: 1,
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
      has_more: false
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

  it('shows +N more when ambiguous candidates are truncated', async () => {
    mocks.getUnlinkedMentionsPaged.mockResolvedValue({
      results: [
        {
          source: 'vault',
          source_notebook: 'Work',
          source_section: 'Journal',
          source_page: 'Notes',
          source_block_ids: ['block-7'],
          source_snippets: ['Standup notes from today'],
          match_count: 1,
          title: 'Standup',
          ambiguous: true,
          candidates_truncated: true,
          candidates_total: 40,
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
      has_more: false
    })
    renderPanel()
    await screen.findByText('1 page mentions this title')
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    expect(
      await screen.findByLabelText('38 more matching paths not shown')
    ).toBeInTheDocument()
    expect(screen.getByText('+38 more')).toBeInTheDocument()
  })

  it('retries unlinked load via Try again after a fetch error', async () => {
    mocks.getUnlinkedMentionsPaged
      .mockRejectedValueOnce(new Error('index unavailable'))
      .mockResolvedValueOnce({
        results: [
          {
            source: 'vault',
            source_notebook: 'Work',
            source_section: 'Projects',
            source_page: 'Launch plan',
            source_block_ids: ['block-42'],
            source_snippets: ['see Research here'],
            match_count: 1,
            title: 'Research',
            ambiguous: false
          }
        ],
        cursor: '',
        has_more: false
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
                source_notebook: 'Work',
                source_section: 'Projects',
                source_page: 'Meeting notes',
                source_block_ids: ['block-99'],
                source_snippets: ['second Research note'],
                match_count: 1,
                title: 'Research',
                ambiguous: false
              }
            ]
          : [
              {
                source: 'vault',
                source_notebook: 'Work',
                source_section: 'Projects',
                source_page: 'Meeting notes',
                source_block_ids: ['block-42', 'block-99'],
                source_snippets: [
                  'first Research note',
                  'second Research note'
                ],
                match_count: 2,
                title: 'Research',
                ambiguous: false
              }
            ],
        cursor: '',
        has_more: false
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
      source_notebook: 'Work',
      source_section: 'Projects',
      source_page: 'Daily log',
      source_block_ids: ['block-42'],
      source_snippets: ['Research today'],
      match_count: 1,
      title: 'Research',
      ambiguous: false
    }
    const second = {
      source: 'vault',
      source_notebook: 'Archive',
      source_section: '',
      source_page: 'Inbox notes',
      source_block_ids: ['block-7'],
      source_snippets: ['more Research'],
      match_count: 1,
      title: 'Research',
      ambiguous: false
    }
    mocks.getUnlinkedMentionsPaged
      .mockResolvedValueOnce({
        results: [first],
        cursor: 'next-page',
        has_more: true
      })
      .mockResolvedValueOnce({ results: [second], cursor: '', has_more: false })
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

  it('Load more merges additional blocks when the same page reappears', async () => {
    const page1 = unlinkedMentionWire({
      source_page: 'Shared residual',
      source_block_ids: ['block-r1'],
      source_snippets: ['first Research'],
      match_count: 1
    })
    const page1more = unlinkedMentionWire({
      source_page: 'Shared residual',
      source_block_ids: ['block-r2'],
      source_snippets: ['second Research'],
      match_count: 1
    })
    mocks.getUnlinkedMentionsPaged
      .mockResolvedValueOnce(
        unlinkedWire({
          results: [page1],
          cursor: 'res-cursor',
          hasMore: true
        })
      )
      .mockResolvedValueOnce(
        unlinkedWire({
          results: [page1more],
          cursor: '',
          hasMore: false
        })
      )
    renderPanel()
    await screen.findByText('1+ pages mention this title')
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    expect(screen.getByLabelText('1 mention')).toBeInTheDocument()
    await fireEvent.click(
      screen.getByRole('button', { name: 'Load more unlinked mentions' })
    )
    await waitFor(() => {
      expect(screen.getByLabelText('2 mentions')).toBeInTheDocument()
    })
    expect(screen.getAllByText('Shared residual')).toHaveLength(1)
    expect(
      screen.getAllByRole('button', { name: /Link mention of Research/ })
    ).toHaveLength(2)
  })

  it('disables only the in-flight row while a promote is pending (per-row busy)', async () => {
    let resolvePromote!: () => void
    mocks.getUnlinkedMentionsPaged.mockResolvedValue({
      results: [
        {
          source: 'vault',
          source_notebook: 'Work',
          source_section: 'Projects',
          source_page: 'Meeting notes',
          source_block_ids: ['block-42', 'block-99'],
          source_snippets: ['first Research', 'second Research'],
          match_count: 2,
          title: 'Research',
          ambiguous: false
        }
      ],
      cursor: '',
      has_more: false
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
          source_notebook: 'Work',
          source_section: 'Projects',
          source_page: 'Launch plan',
          source_block_ids: ['block-42'],
          source_snippets: ['see Research here'],
          match_count: 1,
          title: 'Research',
          ambiguous: false
        }
      ],
      cursor: '',
      has_more: false,
      truncated: true,
      scan_cursor: 'scan-next'
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
    expect(
      screen.getByRole('button', {
        name: 'Scan more unlinked mention candidates'
      })
    ).toBeInTheDocument()
  })

  it('does not show incomplete banner when truncated is false or omitted', async () => {
    mocks.getUnlinkedMentionsPaged.mockResolvedValue({
      results: [
        {
          source: 'vault',
          source_notebook: 'Work',
          source_section: 'Projects',
          source_page: 'Launch plan',
          source_block_ids: ['block-42'],
          source_snippets: ['see Research here'],
          match_count: 1,
          title: 'Research',
          ambiguous: false
        }
      ],
      cursor: '',
      has_more: false
      // truncated omitted — defaults false via dual-read mapper
    } as Record<string, unknown>)
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
      source_notebook: 'Work',
      source_section: 'Projects',
      source_page: page,
      source_block_ids: [blockId],
      source_snippets: ['see Research here'],
      match_count: 1,
      title: 'Research',
      ambiguous: false
    })
    mocks.getUnlinkedMentionsPaged
      .mockResolvedValueOnce({
        results: [mention('Page A', 'block-01')],
        cursor: 'cursor-1',
        has_more: true,
        truncated: true,
        scan_cursor: 'scan-next'
      })
      .mockResolvedValueOnce({
        results: [mention('Page B', 'block-02')],
        cursor: '',
        has_more: false,
        truncated: true,
        scan_cursor: 'scan-next'
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
    // Residual Load more keeps the same batch scan input (empty = first batch).
    expect(mocks.getUnlinkedMentionsPaged).toHaveBeenLastCalledWith(
      'Work',
      'Knowledge',
      'Research',
      'cursor-1',
      '',
      50
    )
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
      has_more: false,
      truncated: true,
      scan_cursor: 'scan-next'
    })
    renderPanel()
    await screen.findByText(
      /No promotable plain mentions in this batch · may be incomplete/
    )
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    expect(
      await screen.findByText(
        /Matching text is capped for performance — no promotable plain mentions in this batch/
      )
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', {
        name: 'Scan more unlinked mention candidates'
      })
    ).toBeInTheDocument()
  })

  it('keeps truncated cue after a failed same-page refresh', async () => {
    mocks.getUnlinkedMentionsPaged.mockResolvedValue({
      results: [
        {
          source: 'vault',
          source_notebook: 'Work',
          source_section: 'Projects',
          source_page: 'Launch plan',
          source_block_ids: ['block-42'],
          source_snippets: ['see Research here'],
          match_count: 1,
          title: 'Research',
          ambiguous: false
        }
      ],
      cursor: '',
      has_more: false,
      truncated: true,
      scan_cursor: 'scan-next'
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

  it('maps Wails snake_case wire payloads for unlinked page title, load more, and truncated', async () => {
    // Regression: production IPC uses Go json tags (has_more, source_page, …).
    mocks.getUnlinkedMentionsPaged
      .mockResolvedValueOnce(
        unlinkedWire({
          results: [
            unlinkedMentionWire({
              source_page: 'Wire page',
              source_block_ids: ['block-wire'],
              source_snippets: ['plain Research hit']
            })
          ],
          cursor: 'wire-cursor',
          hasMore: true,
          truncated: true,
          scanCursor: 'scan-wire'
        })
      )
      .mockResolvedValueOnce(
        unlinkedWire({
          results: [
            unlinkedMentionWire({
              source_page: 'Second wire',
              source_block_ids: ['block-wire-2'],
              source_snippets: ['another Research']
            })
          ],
          hasMore: false,
          truncated: true,
          scanCursor: 'scan-wire'
        })
      )
    renderPanel()
    await screen.findByText(/may be incomplete/)
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    expect(await screen.findByText('Wire page')).toBeInTheDocument()
    expect(
      screen.getByRole('button', {
        name: /Link mention of Research in block block-wi/
      })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Load more unlinked mentions' })
    ).toBeInTheDocument()
    await fireEvent.click(
      screen.getByRole('button', { name: 'Load more unlinked mentions' })
    )
    await screen.findByText('Second wire')
    expect(
      screen.getByText(
        /Results may be incomplete — matching text is capped for performance/
      )
    ).toBeInTheDocument()
  })

  it('still accepts camelCase mocks via dual-read boundary', async () => {
    mocks.getUnlinkedMentionsPaged.mockResolvedValue({
      results: [
        {
          source: 'vault',
          sourceNotebook: 'Work',
          sourceSection: 'Projects',
          sourcePage: 'Camel page',
          sourceBlockIds: ['block-camel'],
          sourceSnippets: ['camel Research'],
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
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    expect(await screen.findByText('Camel page')).toBeInTheDocument()
  })

  it('clears load-more loading when a same-page refresh supersedes mid-flight', async () => {
    const first = unlinkedMentionWire({
      source_page: 'Page A',
      source_block_ids: ['block-01']
    })
    const second = unlinkedMentionWire({
      source_page: 'Page B',
      source_block_ids: ['block-02']
    })
    const refreshed = unlinkedMentionWire({
      source_page: 'Refreshed page',
      source_block_ids: ['block-01'],
      source_snippets: ['refreshed Research']
    })

    let resolveMore!: (value: unknown) => void
    let unlinkedCalls = 0
    mocks.getUnlinkedMentionsPaged.mockImplementation(() => {
      unlinkedCalls += 1
      if (unlinkedCalls === 1) {
        return Promise.resolve(
          unlinkedWire({
            results: [first],
            cursor: 'cursor-1',
            hasMore: true,
            truncated: true,
            scanCursor: 'scan-next'
          })
        )
      }
      if (unlinkedCalls === 2) {
        return new Promise((resolve) => {
          resolveMore = resolve
        })
      }
      // Refresh (and any later) full reload.
      return Promise.resolve(
        unlinkedWire({
          results: [refreshed],
          hasMore: false,
          truncated: true,
          scanCursor: 'scan-next'
        })
      )
    })

    renderPanel()
    await screen.findByText(/may be incomplete/)
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    await screen.findByText('Page A')

    await fireEvent.click(
      screen.getByRole('button', { name: 'Load more unlinked mentions' })
    )
    expect(
      screen.getByRole('button', { name: 'Loading more unlinked mentions' })
    ).toBeDisabled()

    // Same-page refresh bumps unlinkedRequest and clears load-more loading.
    blockChanged?.()
    await new Promise((resolve) => setTimeout(resolve, 210))
    await screen.findByText('Refreshed page')

    // Late load-more resolution must not re-stick Loading more… or append Page B.
    resolveMore(
      unlinkedWire({
        results: [second],
        hasMore: false,
        truncated: true,
        scanCursor: 'scan-next'
      })
    )
    await waitFor(() => {
      expect(
        screen.queryByRole('button', {
          name: 'Loading more unlinked mentions'
        })
      ).not.toBeInTheDocument()
    })
    expect(screen.queryByText('Page B')).not.toBeInTheDocument()
    expect(
      screen.getAllByText(/may be incomplete/).length
    ).toBeGreaterThanOrEqual(1)
  })

  it('does not show Scan more when truncated without scan_cursor', async () => {
    mocks.getUnlinkedMentionsPaged.mockResolvedValue(
      unlinkedWire({
        results: [
          unlinkedMentionWire({
            source_page: 'Only page',
            source_block_ids: ['block-1']
          })
        ],
        truncated: true
        // scan_cursor omitted / empty
      })
    )
    renderPanel()
    await screen.findByText(/may be incomplete/)
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    expect(
      screen.queryByRole('button', {
        name: 'Scan more unlinked mention candidates'
      })
    ).not.toBeInTheDocument()
  })

  it('Scan more fetches next FTS batch with scan_cursor and appends residuals', async () => {
    const first = unlinkedMentionWire({
      source_page: 'Batch one',
      source_block_ids: ['block-01']
    })
    const second = unlinkedMentionWire({
      source_page: 'Batch two',
      source_block_ids: ['block-02']
    })
    mocks.getUnlinkedMentionsPaged
      .mockResolvedValueOnce(
        unlinkedWire({
          results: [first],
          truncated: true,
          scanCursor: 'scan-token-1'
        })
      )
      .mockResolvedValueOnce(
        unlinkedWire({
          results: [second],
          truncated: false,
          scanCursor: ''
        })
      )
    renderPanel()
    await screen.findByText(/may be incomplete/)
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    await screen.findByText('Batch one')
    const scanBtn = screen.getByRole('button', {
      name: 'Scan more unlinked mention candidates'
    })
    scanBtn.focus()
    await fireEvent.click(scanBtn)
    await screen.findByText('Batch two')
    expect(mocks.getUnlinkedMentionsPaged).toHaveBeenLastCalledWith(
      'Work',
      'Knowledge',
      'Research',
      '',
      'scan-token-1',
      50
    )
    expect(screen.getByText('Batch one')).toBeInTheDocument()
    expect(
      screen.queryByRole('button', {
        name: 'Scan more unlinked mention candidates'
      })
    ).not.toBeInTheDocument()
    expect(screen.queryByText(/may be incomplete/)).not.toBeInTheDocument()
    // Final scan: announce completion and move focus to the section toggle.
    await waitFor(() => {
      expect(screen.getByText(/Scan complete/)).toBeInTheDocument()
    })
    const toggle = screen.getByRole('button', { name: /Unlinked mentions/ })
    expect(document.activeElement).toBe(toggle)
  })

  it('Load more after final Scan clears one-shot scan status so count returns', async () => {
    const batch1 = unlinkedMentionWire({
      source_page: 'Scan page',
      source_block_ids: ['b1']
    })
    const batch2a = unlinkedMentionWire({
      source_page: 'Residual A',
      source_block_ids: ['b2a']
    })
    const batch2b = unlinkedMentionWire({
      source_page: 'Residual B',
      source_block_ids: ['b2b']
    })
    mocks.getUnlinkedMentionsPaged
      .mockResolvedValueOnce(
        unlinkedWire({
          results: [batch1],
          truncated: true,
          scanCursor: 'scan-final'
        })
      )
      .mockResolvedValueOnce(
        unlinkedWire({
          results: [batch2a],
          cursor: 'res-next',
          hasMore: true,
          truncated: false,
          scanCursor: ''
        })
      )
      .mockResolvedValueOnce(
        unlinkedWire({
          results: [batch2b],
          cursor: '',
          hasMore: false,
          truncated: false,
          scanCursor: ''
        })
      )
    renderPanel()
    await screen.findByText(/may be incomplete/)
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    await fireEvent.click(
      screen.getByRole('button', {
        name: 'Scan more unlinked mention candidates'
      })
    )
    await waitFor(() => {
      expect(screen.getByText(/Scan complete/)).toBeInTheDocument()
    })
    // Residual Load more must clear the one-shot status so the live count shows.
    await fireEvent.click(
      screen.getByRole('button', { name: 'Load more unlinked mentions' })
    )
    await waitFor(() => {
      expect(screen.queryByText(/Scan complete/)).not.toBeInTheDocument()
    })
    expect(screen.getByText(/pages mention this title/)).toBeInTheDocument()
    expect(screen.getByText('Residual B')).toBeInTheDocument()
  })

  it('Scan more merges additional blocks when the same page reappears', async () => {
    const batch1 = unlinkedMentionWire({
      source_page: 'Shared page',
      source_block_ids: ['block-a'],
      source_snippets: ['alpha Research hit'],
      match_count: 1
    })
    const batch2 = unlinkedMentionWire({
      source_page: 'Shared page',
      source_block_ids: ['block-b', 'block-c'],
      source_snippets: ['beta Research hit', 'gamma Research hit'],
      match_count: 2
    })
    mocks.getUnlinkedMentionsPaged
      .mockResolvedValueOnce(
        unlinkedWire({
          results: [batch1],
          truncated: true,
          scanCursor: 'scan-token-1'
        })
      )
      .mockResolvedValueOnce(
        unlinkedWire({
          results: [batch2],
          truncated: false,
          scanCursor: ''
        })
      )
    renderPanel()
    await screen.findByText(/may be incomplete/)
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    await screen.findByText('Shared page')
    expect(screen.getByLabelText('1 mention')).toBeInTheDocument()
    await fireEvent.click(
      screen.getByRole('button', {
        name: 'Scan more unlinked mention candidates'
      })
    )
    await waitFor(() => {
      expect(screen.getByLabelText('3 mentions')).toBeInTheDocument()
    })
    // One page row; three link targets (one per merged block).
    expect(screen.getAllByText('Shared page')).toHaveLength(1)
    expect(
      screen.getAllByRole('button', { name: /Link mention of Research/ })
    ).toHaveLength(3)
    expect(
      screen.getByRole('button', {
        name: /Link mention of Research in block block-a/
      })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', {
        name: /Link mention of Research in block block-b/
      })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', {
        name: /Link mention of Research in block block-c/
      })
    ).toBeInTheDocument()
  })

  it('disables Scan more while residual has_more so unread batch pages are not dropped', async () => {
    mocks.getUnlinkedMentionsPaged.mockResolvedValue(
      unlinkedWire({
        results: [
          unlinkedMentionWire({
            source_page: 'Page A',
            source_block_ids: ['block-01']
          })
        ],
        cursor: 'cursor-1',
        hasMore: true,
        truncated: true,
        scanCursor: 'scan-next'
      })
    )
    renderPanel()
    await screen.findByText(/may be incomplete/)
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    const scanBtn = await screen.findByRole('button', {
      name: 'Load remaining unlinked pages in this batch before scanning more candidates'
    })
    expect(scanBtn).toBeDisabled()
    expect(
      screen.getByText(
        /Load remaining pages in this batch before scanning more/
      )
    ).toBeInTheDocument()
  })

  it('Load more after Scan more passes the batch scan_cursor input', async () => {
    const batch1 = unlinkedMentionWire({
      source_page: 'Batch1 page',
      source_block_ids: ['b1']
    })
    const batch2a = unlinkedMentionWire({
      source_page: 'Batch2a',
      source_block_ids: ['b2a']
    })
    const batch2b = unlinkedMentionWire({
      source_page: 'Batch2b',
      source_block_ids: ['b2b']
    })
    mocks.getUnlinkedMentionsPaged
      .mockResolvedValueOnce(
        unlinkedWire({
          results: [batch1],
          truncated: true,
          scanCursor: 'scan-token-1'
        })
      )
      .mockResolvedValueOnce(
        unlinkedWire({
          results: [batch2a],
          cursor: 'res-cursor-2',
          hasMore: true,
          truncated: true,
          scanCursor: 'scan-token-2'
        })
      )
      .mockResolvedValueOnce(
        unlinkedWire({
          results: [batch2b],
          hasMore: false,
          truncated: true,
          scanCursor: 'scan-token-2'
        })
      )
    renderPanel()
    await screen.findByText(/may be incomplete/)
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    await screen.findByText('Batch1 page')
    await fireEvent.click(
      screen.getByRole('button', {
        name: 'Scan more unlinked mention candidates'
      })
    )
    await screen.findByText('Batch2a')
    // Residual has_more on batch 2 — Scan more blocked until Load more finishes.
    expect(
      screen.getByRole('button', {
        name: 'Load remaining unlinked pages in this batch before scanning more candidates'
      })
    ).toBeDisabled()
    await fireEvent.click(
      screen.getByRole('button', { name: 'Load more unlinked mentions' })
    )
    await screen.findByText('Batch2b')
    expect(mocks.getUnlinkedMentionsPaged).toHaveBeenLastCalledWith(
      'Work',
      'Knowledge',
      'Research',
      'res-cursor-2',
      'scan-token-1',
      50
    )
  })

  it('retries Scan more via Try again after a scan failure', async () => {
    const first = unlinkedMentionWire({
      source_page: 'Batch one',
      source_block_ids: ['block-01']
    })
    const second = unlinkedMentionWire({
      source_page: 'Batch two',
      source_block_ids: ['block-02']
    })
    mocks.getUnlinkedMentionsPaged
      .mockResolvedValueOnce(
        unlinkedWire({
          results: [first],
          truncated: true,
          scanCursor: 'scan-token-1'
        })
      )
      .mockRejectedValueOnce(new Error('scan failed'))
      .mockResolvedValueOnce(
        unlinkedWire({
          results: [second],
          truncated: false
        })
      )
    renderPanel()
    await screen.findByText(/may be incomplete/)
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    await screen.findByText('Batch one')
    await fireEvent.click(
      screen.getByRole('button', {
        name: 'Scan more unlinked mention candidates'
      })
    )
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'More mention candidates could not be scanned.'
    )
    expect(screen.getByRole('alert')).toHaveTextContent('scan failed')
    await fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    await screen.findByText('Batch two')
    expect(mocks.getUnlinkedMentionsPaged).toHaveBeenLastCalledWith(
      'Work',
      'Knowledge',
      'Research',
      '',
      'scan-token-1',
      50
    )
  })

  it('keeps scan_cursor after a failed same-page refresh so Scan more still works', async () => {
    mocks.getUnlinkedMentionsPaged.mockResolvedValue(
      unlinkedWire({
        results: [
          unlinkedMentionWire({
            source_page: 'Launch plan',
            source_block_ids: ['block-42']
          })
        ],
        truncated: true,
        scanCursor: 'scan-keep'
      })
    )
    renderPanel()
    await screen.findByText(/may be incomplete/)
    await fireEvent.click(
      screen.getByRole('button', { name: /Unlinked mentions/ })
    )
    await screen.findByRole('button', {
      name: 'Scan more unlinked mention candidates'
    })

    mocks.getUnlinkedMentionsPaged.mockRejectedValueOnce(
      new Error('index unavailable')
    )
    blockChanged?.()
    await new Promise((resolve) => setTimeout(resolve, 210))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'index unavailable'
    )
    // Prior scan control remains available after failed refresh.
    expect(
      screen.getByRole('button', {
        name: 'Scan more unlinked mention candidates'
      })
    ).toBeInTheDocument()
  })

  it('initial unlinked fetch passes empty residual and scan cursors', async () => {
    mocks.getUnlinkedMentionsPaged.mockResolvedValue(
      unlinkedWire({
        results: [
          unlinkedMentionWire({
            source_page: 'Notes',
            source_block_ids: ['b1']
          })
        ]
      })
    )
    renderPanel()
    await screen.findByText('1 page mentions this title')
    expect(mocks.getUnlinkedMentionsPaged).toHaveBeenCalledWith(
      'Work',
      'Knowledge',
      'Research',
      '',
      '',
      50
    )
  })
})
