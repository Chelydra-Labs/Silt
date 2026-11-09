import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'
import { tick } from 'svelte'

const mocks = vi.hoisted(() => ({
  QueryBlocksByTag: vi.fn()
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    QueryBlocksByTag: mocks.QueryBlocksByTag
  })
)

import TagsExplorer from './TagsExplorer.svelte'

describe('TagsExplorer', () => {
  beforeEach(() => {
    mocks.QueryBlocksByTag.mockReset()
  })
  afterEach(() => cleanup())

  it('renders empty ViewState when no tag is selected', async () => {
    render(TagsExplorer, { selectedTag: '' })
    await tick()
    expect(screen.getByText('No tag selected')).toBeInTheDocument()
    expect(
      screen.getByText('Select a tag in the sidebar to see its blocks.')
    ).toBeInTheDocument()
  })

  it('renders error ViewState with role="alert" and allows retry on IPC failure', async () => {
    mocks.QueryBlocksByTag.mockRejectedValueOnce(new Error('database locked'))
    render(TagsExplorer, { selectedTag: 'project' })
    await tick()

    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
    expect(screen.getByText('Failed to load tagged blocks')).toBeInTheDocument()
    expect(screen.getByText('database locked')).toBeInTheDocument()

    // Test retry
    mocks.QueryBlocksByTag.mockResolvedValueOnce([
      {
        id: 'blk-1',
        notebook: 'Work',
        section: 'Projects',
        page: 'Roadmap',
        clean_content: 'Ship v2'
      }
    ])
    const retryBtn = screen.getByRole('button', { name: /Retry/i })
    await fireEvent.click(retryBtn)
    await tick()

    await waitFor(() => {
      expect(screen.getByText('Ship v2')).toBeInTheDocument()
    })
  })

  it('renders empty ViewState when tag has 0 blocks', async () => {
    mocks.QueryBlocksByTag.mockResolvedValueOnce([])
    render(TagsExplorer, { selectedTag: 'unused' })
    await tick()

    await waitFor(() => {
      expect(screen.getByText('No blocks tagged')).toBeInTheDocument()
    })
    expect(screen.getByText('No blocks have #unused.')).toBeInTheDocument()
  })

  it('renders tagged blocks and dispatches navigate-to-block on click', async () => {
    const handler = vi.fn()
    window.addEventListener('navigate-to-block', handler)

    mocks.QueryBlocksByTag.mockResolvedValueOnce([
      {
        id: 'blk-1',
        notebook: 'Work',
        section: 'Projects',
        page: 'Roadmap',
        file_date: '2026-05-01',
        clean_content: 'Ship v2 features'
      }
    ])

    render(TagsExplorer, { selectedTag: 'roadmap' })
    await tick()

    await waitFor(() => {
      expect(screen.getByText('Ship v2 features')).toBeInTheDocument()
    })
    expect(screen.getByTestId('tags-count')).toHaveTextContent(
      '#roadmap · 1 block'
    )

    const blockBtn = screen.getByRole('button', { name: /Ship v2 features/i })
    await fireEvent.click(blockBtn)
    expect(handler).toHaveBeenCalledTimes(1)
    expect(handler).toHaveBeenCalledWith(
      expect.objectContaining({
        detail: {
          notebook: 'Work',
          section: 'Projects',
          page: 'Roadmap',
          date: '2026-05-01',
          blockId: 'blk-1'
        }
      })
    )

    window.removeEventListener('navigate-to-block', handler)
  })
})
