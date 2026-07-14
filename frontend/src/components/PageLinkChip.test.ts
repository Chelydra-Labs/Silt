// Component-level test for PageLinkChip (#545, #549). Resolves [[target]] via
// IPC, dispatches navigate-to-page on click, and offers create-page for
// ambiguous and unresolved links.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'
import PageLinkChip from './PageLinkChip.svelte'

const mocks = vi.hoisted(() => ({
  resolvePageLink: vi.fn(),
  createPage: vi.fn(),
  listNavigation: vi.fn(),
  activeNotebook: 'Work',
  activeSection: 'Journal'
}))

vi.mock('../../bindings/silt/app.js', () => ({
  ResolvePageLink: mocks.resolvePageLink,
  CreatePage: mocks.createPage,
  ListNavigation: mocks.listNavigation
}))

vi.mock('../plugins/location.svelte', () => ({
  getActiveLocation: () => ({
    notebook: mocks.activeNotebook,
    section: mocks.activeSection,
    page: ''
  })
}))

describe('PageLinkChip (#545)', () => {
  beforeEach(() => vi.clearAllMocks())
  afterEach(() => cleanup())

  it('shows a loading state while resolving', () => {
    mocks.resolvePageLink.mockReturnValue(new Promise(() => {}))
    render(PageLinkChip, { props: { target: 'Inbox' } })
    expect(screen.getByText(/\[\[/)).toBeTruthy()
  })

  it('renders the alias (or target) on the happy path', async () => {
    mocks.resolvePageLink.mockResolvedValue({
      exists: true,
      notebook: 'Work',
      section: 'Projects',
      page: 'Site',
      shortest: 'Site'
    })
    render(PageLinkChip, {
      props: { target: 'Site', alias: 'The site' }
    })
    await waitFor(() => {
      expect(screen.getByText('The site')).toBeTruthy()
    })
  })

  it('dispatches navigate-to-page on click', async () => {
    mocks.resolvePageLink.mockResolvedValue({
      exists: true,
      notebook: 'Work',
      section: '',
      page: 'Inbox',
      shortest: 'Inbox'
    })
    const handler = vi.fn()
    window.addEventListener('navigate-to-page', handler)

    render(PageLinkChip, {
      props: { target: 'Inbox', heading: 'Goals' }
    })
    await waitFor(() => {
      expect(screen.getByRole('link')).toBeTruthy()
    })
    await fireEvent.click(screen.getByRole('link'))
    expect(handler).toHaveBeenCalledTimes(1)
    const detail = handler.mock.calls[0][0].detail
    expect(detail.notebook).toBe('Work')
    expect(detail.page).toBe('Inbox')
    expect(detail.heading).toBe('Goals')
    window.removeEventListener('navigate-to-page', handler)
  })

  it('renders unresolved strikethrough when missing', async () => {
    mocks.resolvePageLink.mockResolvedValue({ exists: false })
    render(PageLinkChip, { props: { target: 'Missing' } })
    await waitFor(() => {
      expect(screen.getByText('[[Missing]]')).toBeTruthy()
    })
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('renders ambiguous state without a link role', async () => {
    mocks.resolvePageLink.mockResolvedValue({
      exists: false,
      ambiguous: true,
      candidates: [
        { notebook: 'A', section: '', page: 'X' },
        { notebook: 'B', section: '', page: 'X' }
      ]
    })
    render(PageLinkChip, { props: { target: 'X' } })
    await waitFor(() => {
      expect(screen.getByText('[[X]]')).toBeTruthy()
    })
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('shows candidate list on hover and navigates on pick', async () => {
    mocks.resolvePageLink.mockResolvedValue({
      exists: false,
      ambiguous: true,
      candidates: [
        { notebook: 'Work', section: 'A', page: 'Site' },
        { notebook: 'Archive', section: 'B', page: 'Site' }
      ]
    })
    const handler = vi.fn()
    window.addEventListener('navigate-to-page', handler)
    render(PageLinkChip, { props: { target: 'Site' } })
    await waitFor(() => {
      expect(screen.getByRole('button')).toBeTruthy()
    })
    await fireEvent.mouseEnter(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByText('Work › A › Site')).toBeTruthy()
    })
    await fireEvent.click(screen.getByText('Work › A › Site'))
    expect(handler).toHaveBeenCalledTimes(1)
    expect(handler.mock.calls[0][0].detail.notebook).toBe('Work')
    expect(handler.mock.calls[0][0].detail.page).toBe('Site')
    window.removeEventListener('navigate-to-page', handler)
  })
})

describe('PageLinkChip create-or-pick (#549)', () => {
  beforeEach(() => vi.clearAllMocks())
  afterEach(() => cleanup())

  it('ambiguous chip shows Create page button on hover', async () => {
    mocks.resolvePageLink.mockResolvedValue({
      exists: false,
      ambiguous: true,
      candidates: [{ notebook: 'A', section: '', page: 'X' }]
    })
    render(PageLinkChip, { props: { target: 'X' } })
    await waitFor(() => {
      expect(screen.getByText('[[X]]')).toBeTruthy()
    })
    await fireEvent.mouseEnter(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByText('Create page')).toBeTruthy()
    })
  })

  it('ambiguous chip creates page via CreatePage and navigates', async () => {
    mocks.resolvePageLink.mockResolvedValue({
      exists: false,
      ambiguous: true,
      candidates: [{ notebook: 'A', section: '', page: 'X' }]
    })
    mocks.createPage.mockResolvedValue('')
    const handler = vi.fn()
    window.addEventListener('navigate-to-page', handler)

    render(PageLinkChip, { props: { target: 'X' } })
    await waitFor(() => {
      expect(screen.getByRole('button')).toBeTruthy()
    })
    await fireEvent.mouseEnter(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByText('Create page')).toBeTruthy()
    })
    await fireEvent.click(screen.getByText('Create page'))

    await waitFor(() => {
      expect(mocks.createPage).toHaveBeenCalledTimes(1)
    })
    expect(mocks.createPage).toHaveBeenCalledWith('Work', 'Journal', 'X', '')
    expect(handler).toHaveBeenCalledTimes(1)
    expect(handler.mock.calls[0][0].detail.notebook).toBe('Work')
    expect(handler.mock.calls[0][0].detail.page).toBe('X')
    window.removeEventListener('navigate-to-page', handler)
  })

  it('unresolved chip is interactive and shows Create page on hover', async () => {
    mocks.resolvePageLink.mockResolvedValue({ exists: false })
    render(PageLinkChip, { props: { target: 'Missing' } })
    await waitFor(() => {
      expect(screen.getByText('[[Missing]]')).toBeTruthy()
    })
    const btn = screen.getByRole('button')
    expect(btn.getAttribute('aria-haspopup')).toBe('true')
    await fireEvent.mouseEnter(btn)
    await waitFor(() => {
      expect(screen.getByText('Create page')).toBeTruthy()
    })
  })

  it('unresolved chip creates page and navigates', async () => {
    mocks.resolvePageLink.mockResolvedValue({ exists: false })
    mocks.createPage.mockResolvedValue('')
    const handler = vi.fn()
    window.addEventListener('navigate-to-page', handler)

    render(PageLinkChip, { props: { target: 'NewPage' } })
    await waitFor(() => {
      expect(screen.getByText('[[NewPage]]')).toBeTruthy()
    })
    await fireEvent.mouseEnter(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByText('Create page')).toBeTruthy()
    })
    await fireEvent.click(screen.getByText('Create page'))

    await waitFor(() => {
      expect(mocks.createPage).toHaveBeenCalledTimes(1)
    })
    expect(mocks.createPage).toHaveBeenCalledWith(
      'Work',
      'Journal',
      'NewPage',
      ''
    )
    expect(handler).toHaveBeenCalledTimes(1)
    expect(handler.mock.calls[0][0].detail.page).toBe('NewPage')
    window.removeEventListener('navigate-to-page', handler)
  })

  it('parses 3-segment path target for CreatePage', async () => {
    mocks.resolvePageLink.mockResolvedValue({ exists: false })
    mocks.createPage.mockResolvedValue('')
    const handler = vi.fn()
    window.addEventListener('navigate-to-page', handler)

    render(PageLinkChip, { props: { target: 'Work/Projects/Meeting' } })
    await waitFor(() => {
      expect(screen.getByText('[[Work/Projects/Meeting]]')).toBeTruthy()
    })
    await fireEvent.mouseEnter(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByText('Create page')).toBeTruthy()
    })
    await fireEvent.click(screen.getByText('Create page'))

    await waitFor(() => {
      expect(mocks.createPage).toHaveBeenCalledTimes(1)
    })
    expect(mocks.createPage).toHaveBeenCalledWith(
      'Work',
      'Projects',
      'Meeting',
      ''
    )
    expect(handler.mock.calls[0][0].detail.notebook).toBe('Work')
    expect(handler.mock.calls[0][0].detail.section).toBe('Projects')
    expect(handler.mock.calls[0][0].detail.page).toBe('Meeting')
    window.removeEventListener('navigate-to-page', handler)
  })

  it('parses 2-segment path target as section/page with active notebook', async () => {
    mocks.resolvePageLink.mockResolvedValue({ exists: false })
    mocks.createPage.mockResolvedValue('')
    const handler = vi.fn()
    window.addEventListener('navigate-to-page', handler)

    render(PageLinkChip, { props: { target: 'Projects/Meeting' } })
    await waitFor(() => {
      expect(screen.getByText('[[Projects/Meeting]]')).toBeTruthy()
    })
    await fireEvent.mouseEnter(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByText('Create page')).toBeTruthy()
    })
    await fireEvent.click(screen.getByText('Create page'))

    await waitFor(() => {
      expect(mocks.createPage).toHaveBeenCalledTimes(1)
    })
    expect(mocks.createPage).toHaveBeenCalledWith(
      'Work',
      'Projects',
      'Meeting',
      ''
    )
    window.removeEventListener('navigate-to-page', handler)
  })

  it('shows error in popover when CreatePage fails (no navigation)', async () => {
    mocks.resolvePageLink.mockResolvedValue({ exists: false })
    mocks.createPage.mockRejectedValue(new Error('Notebook not found'))
    const handler = vi.fn()
    window.addEventListener('navigate-to-page', handler)

    render(PageLinkChip, { props: { target: 'GhostPage' } })
    await waitFor(() => {
      expect(screen.getByText('[[GhostPage]]')).toBeTruthy()
    })
    await fireEvent.mouseEnter(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByText('Create page')).toBeTruthy()
    })
    await fireEvent.click(screen.getByText('Create page'))

    await waitFor(() => {
      expect(screen.getByText('Notebook not found')).toBeTruthy()
    })
    expect(handler).not.toHaveBeenCalled()
    window.removeEventListener('navigate-to-page', handler)
  })

  it('ambiguous chip pick still works alongside create', async () => {
    mocks.resolvePageLink.mockResolvedValue({
      exists: false,
      ambiguous: true,
      candidates: [{ notebook: 'Work', section: 'A', page: 'Site' }]
    })
    const handler = vi.fn()
    window.addEventListener('navigate-to-page', handler)
    render(PageLinkChip, { props: { target: 'Site' } })
    await waitFor(() => {
      expect(screen.getByRole('button')).toBeTruthy()
    })
    await fireEvent.mouseEnter(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByText('Work › A › Site')).toBeTruthy()
    })
    await fireEvent.click(screen.getByText('Work › A › Site'))
    expect(handler).toHaveBeenCalledTimes(1)
    expect(mocks.createPage).not.toHaveBeenCalled()
    window.removeEventListener('navigate-to-page', handler)
  })

  it('shows error when no active notebook is open (CreatePage not called)', async () => {
    mocks.activeNotebook = ''
    mocks.activeSection = ''
    mocks.resolvePageLink.mockResolvedValue({ exists: false })
    mocks.createPage.mockResolvedValue('')
    const handler = vi.fn()
    window.addEventListener('navigate-to-page', handler)

    render(PageLinkChip, { props: { target: 'NewPage' } })
    await waitFor(() => {
      expect(screen.getByText('[[NewPage]]')).toBeTruthy()
    })
    await fireEvent.mouseEnter(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByText('Create page')).toBeTruthy()
    })
    await fireEvent.click(screen.getByText('Create page'))

    await waitFor(() => {
      expect(screen.getByText('Open a notebook first.')).toBeTruthy()
    })
    expect(mocks.createPage).not.toHaveBeenCalled()
    expect(handler).not.toHaveBeenCalled()
    window.removeEventListener('navigate-to-page', handler)
    // Restore defaults for subsequent tests
    mocks.activeNotebook = 'Work'
    mocks.activeSection = 'Journal'
  })

  it('ambiguous Create button shows heads-up subtitle about deepening ambiguity', async () => {
    mocks.resolvePageLink.mockResolvedValue({
      exists: false,
      ambiguous: true,
      candidates: [{ notebook: 'A', section: '', page: 'X' }]
    })
    render(PageLinkChip, { props: { target: 'X' } })
    await waitFor(() => {
      expect(screen.getByRole('button')).toBeTruthy()
    })
    await fireEvent.mouseEnter(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByText('Create page')).toBeTruthy()
    })
    expect(
      screen.getByText('Creates a new page; existing matches remain.')
    ).toBeTruthy()
  })
})

describe('PageLinkChip 2-segment wiki-link disambiguation (#551)', () => {
  beforeEach(() => vi.clearAllMocks())
  afterEach(() => cleanup())

  it('2-seg where first segment matches a notebook name → notebook/page (section empty)', async () => {
    mocks.resolvePageLink.mockResolvedValue({ exists: false })
    mocks.createPage.mockResolvedValue('')
    mocks.listNavigation.mockResolvedValue({
      notebooks: [
        { name: 'Archive', sections: [] },
        { name: 'Work', sections: [] }
      ]
    })
    const handler = vi.fn()
    window.addEventListener('navigate-to-page', handler)

    render(PageLinkChip, { props: { target: 'Archive/Page' } })
    await waitFor(() => {
      expect(screen.getByText('[[Archive/Page]]')).toBeTruthy()
    })
    await fireEvent.mouseEnter(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByText('Create page')).toBeTruthy()
    })
    await fireEvent.click(screen.getByText('Create page'))

    await waitFor(() => {
      expect(mocks.createPage).toHaveBeenCalledTimes(1)
    })
    expect(mocks.createPage).toHaveBeenCalledWith('Archive', '', 'Page', '')
    expect(handler.mock.calls[0][0].detail.notebook).toBe('Archive')
    expect(handler.mock.calls[0][0].detail.section).toBe('')
    expect(handler.mock.calls[0][0].detail.page).toBe('Page')
    window.removeEventListener('navigate-to-page', handler)
  })

  it('2-seg where first segment does NOT match a notebook → section/page (active notebook)', async () => {
    mocks.resolvePageLink.mockResolvedValue({ exists: false })
    mocks.createPage.mockResolvedValue('')
    mocks.listNavigation.mockResolvedValue({
      notebooks: [{ name: 'Archive', sections: [] }]
    })
    const handler = vi.fn()
    window.addEventListener('navigate-to-page', handler)

    render(PageLinkChip, { props: { target: 'Projects/Meeting' } })
    await waitFor(() => {
      expect(screen.getByText('[[Projects/Meeting]]')).toBeTruthy()
    })
    await fireEvent.mouseEnter(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByText('Create page')).toBeTruthy()
    })
    await fireEvent.click(screen.getByText('Create page'))

    await waitFor(() => {
      expect(mocks.createPage).toHaveBeenCalledTimes(1)
    })
    expect(mocks.createPage).toHaveBeenCalledWith(
      'Work',
      'Projects',
      'Meeting',
      ''
    )
    window.removeEventListener('navigate-to-page', handler)
  })

  it('2-seg falls back to section/page when ListNavigation rejects', async () => {
    mocks.resolvePageLink.mockResolvedValue({ exists: false })
    mocks.createPage.mockResolvedValue('')
    mocks.listNavigation.mockRejectedValue(new Error('IPC unavailable'))

    render(PageLinkChip, { props: { target: 'Projects/Meeting' } })
    await waitFor(() => {
      expect(screen.getByText('[[Projects/Meeting]]')).toBeTruthy()
    })
    await fireEvent.mouseEnter(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByText('Create page')).toBeTruthy()
    })
    await fireEvent.click(screen.getByText('Create page'))

    await waitFor(() => {
      expect(mocks.createPage).toHaveBeenCalledTimes(1)
    })
    expect(mocks.createPage).toHaveBeenCalledWith(
      'Work',
      'Projects',
      'Meeting',
      ''
    )
  })

  it('2-seg matches notebook names case-insensitively (mirrors ResolvePageLink)', async () => {
    mocks.resolvePageLink.mockResolvedValue({ exists: false })
    mocks.createPage.mockResolvedValue('')
    mocks.listNavigation.mockResolvedValue({
      notebooks: [{ name: 'Archive', sections: [] }]
    })
    const handler = vi.fn()
    window.addEventListener('navigate-to-page', handler)

    render(PageLinkChip, { props: { target: 'archive/Page' } })
    await waitFor(() => {
      expect(screen.getByText('[[archive/Page]]')).toBeTruthy()
    })
    await fireEvent.mouseEnter(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByText('Create page')).toBeTruthy()
    })
    await fireEvent.click(screen.getByText('Create page'))

    await waitFor(() => {
      expect(mocks.createPage).toHaveBeenCalledTimes(1)
    })
    // 'archive' ≈ 'Archive' (EqualFold, as in PageMatchesTarget) → notebook/page,
    // and the notebook keeps its registered canonical casing so CreatePage routes
    // via resolveSourceByName (exact match) to the right folder.
    expect(mocks.createPage).toHaveBeenCalledWith('Archive', '', 'Page', '')
    expect(handler.mock.calls[0][0].detail.notebook).toBe('Archive')
    expect(handler.mock.calls[0][0].detail.section).toBe('')
    expect(handler.mock.calls[0][0].detail.page).toBe('Page')
    window.removeEventListener('navigate-to-page', handler)
  })

  it('1-seg target is unaffected by ListNavigation', async () => {
    mocks.resolvePageLink.mockResolvedValue({ exists: false })
    mocks.createPage.mockResolvedValue('')
    mocks.listNavigation.mockResolvedValue({
      notebooks: [{ name: 'Work', sections: [] }]
    })

    render(PageLinkChip, { props: { target: 'SimplePage' } })
    await waitFor(() => {
      expect(screen.getByText('[[SimplePage]]')).toBeTruthy()
    })
    await fireEvent.mouseEnter(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByText('Create page')).toBeTruthy()
    })
    await fireEvent.click(screen.getByText('Create page'))

    await waitFor(() => {
      expect(mocks.createPage).toHaveBeenCalledTimes(1)
    })
    expect(mocks.createPage).toHaveBeenCalledWith(
      'Work',
      'Journal',
      'SimplePage',
      ''
    )
    // ListNavigation must not be called for 1-seg targets
    expect(mocks.listNavigation).not.toHaveBeenCalled()
  })

  it('3-seg target is unaffected by ListNavigation', async () => {
    mocks.resolvePageLink.mockResolvedValue({ exists: false })
    mocks.createPage.mockResolvedValue('')
    mocks.listNavigation.mockResolvedValue({
      notebooks: [{ name: 'Other', sections: [] }]
    })

    render(PageLinkChip, { props: { target: 'Notebook/Section/Page' } })
    await waitFor(() => {
      expect(screen.getByText('[[Notebook/Section/Page]]')).toBeTruthy()
    })
    await fireEvent.mouseEnter(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByText('Create page')).toBeTruthy()
    })
    await fireEvent.click(screen.getByText('Create page'))

    await waitFor(() => {
      expect(mocks.createPage).toHaveBeenCalledTimes(1)
    })
    expect(mocks.createPage).toHaveBeenCalledWith(
      'Notebook',
      'Section',
      'Page',
      ''
    )
    // ListNavigation must not be called for 3+ seg targets
    expect(mocks.listNavigation).not.toHaveBeenCalled()
  })

  it('shows resolved path subtitle in the hover popover', async () => {
    mocks.resolvePageLink.mockResolvedValue({ exists: false })
    mocks.listNavigation.mockResolvedValue({
      notebooks: [{ name: 'Archive', sections: [] }]
    })

    render(PageLinkChip, { props: { target: 'Archive/Page' } })
    await waitFor(() => {
      expect(screen.getByText('[[Archive/Page]]')).toBeTruthy()
    })
    await fireEvent.mouseEnter(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByText('Archive › Page')).toBeTruthy()
    })
  })
})
