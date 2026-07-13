// Component-level test for PageLinkChip (#545). Resolves [[target]] via IPC
// and dispatches navigate-to-page on click.

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
  resolvePageLink: vi.fn()
}))

vi.mock('../../bindings/silt/app.js', () => ({
  ResolvePageLink: mocks.resolvePageLink
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
})
