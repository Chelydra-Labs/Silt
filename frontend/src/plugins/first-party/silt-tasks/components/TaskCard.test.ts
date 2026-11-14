// Board-card rendering of the shortlist badge (#1092). TaskCard is a pure
// presentational component — props in, markup out — so these are plain render
// assertions (no IPC, per AGENTS.md: Vitest component tests only).
import { describe, expect, it, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

import TaskCard from './TaskCard.svelte'
import type { TaskDetail } from '../types'

function makeCard(overrides: Partial<TaskDetail> = {}): TaskDetail {
  return {
    id: 'c1',
    source: 'vault',
    notebook: 'Work',
    section: 'Journal',
    page: 'Daily',
    file_date: '2026-11-09',
    clean_content: 'Ship the shortlist',
    status: 'TODO',
    owner: '',
    start_date: '',
    due_date: '',
    priority: 2,
    pinned: false,
    progress: 0,
    recurrence: '',
    comments_count: 0,
    links_count: 0,
    created_at: '',
    completed_at: '',
    manual_order: 0,
    modified_at: '',
    estimate_minutes: null,
    shortlist_rank: null,
    subtask_total: 0,
    subtask_done: 0,
    ...overrides
  }
}

function makeProps(card: TaskDetail) {
  return {
    card,
    index: 0,
    colLabel: 'To Do',
    dndEnabled: false,
    groupBy: 'status' as const,
    today: '2026-11-09',
    dragging: false,
    dragOver: false,
    onDragStart: () => {},
    onDragEnd: () => {},
    onCardDragOver: () => {},
    onCardDragLeave: () => {},
    onCardDrop: () => {},
    onKeydown: () => {},
    onSelect: () => {},
    onPinToggle: () => {},
    onPriorityCycle: () => {},
    onContextMenu: () => {}
  }
}

describe('TaskCard shortlist badge (#1092)', () => {
  afterEach(cleanup)

  it('renders "Shortlist 0" beside the priority chip — 0 is a real rank', () => {
    render(TaskCard, makeProps(makeCard({ shortlist_rank: 0, priority: 1 })))
    const badge = screen.getByTestId('board-shortlist-badge-c1')
    expect(badge.textContent).toContain('Shortlist 0')
    // The priority chip still renders next to it.
    expect(badge.parentElement?.textContent).toContain('High')
  })

  it('renders "Shortlist 3" the same way', () => {
    render(TaskCard, makeProps(makeCard({ shortlist_rank: 3 })))
    expect(
      screen.getByTestId('board-shortlist-badge-c1').textContent
    ).toContain('Shortlist 3')
  })

  it('renders no badge when unranked', () => {
    render(TaskCard, makeProps(makeCard({ shortlist_rank: null })))
    expect(screen.queryByTestId('board-shortlist-badge-c1')).toBeNull()
  })

  it('uses neutral mono styling — outside the priority palette', () => {
    render(TaskCard, makeProps(makeCard({ shortlist_rank: 2, priority: 3 })))
    const badge = screen.getByTestId('board-shortlist-badge-c1')
    // The guard: the badge must not carry any priorityClass hue.
    expect(badge.className).toContain('font-mono')
    expect(badge.className).not.toMatch(/text-error|accent-primary/)
  })

  it('announces the rank in the card label — 0 included, unranked silent', () => {
    // The explicit aria-label overrides subtree text, so the badge span is
    // invisible to AT; the rank must ride the label itself.
    render(TaskCard, makeProps(makeCard({ shortlist_rank: 0, priority: 1 })))
    expect(
      document.querySelector('[data-card="c1"]')!.getAttribute('aria-label')
    ).toBe('Ship the shortlist, To Do, shortlist rank 0.')

    cleanup()
    render(TaskCard, makeProps(makeCard({ shortlist_rank: null })))
    expect(
      document.querySelector('[data-card="c1"]')!.getAttribute('aria-label')
    ).not.toContain('shortlist rank')
  })
})

describe('TaskCard priority pill', () => {
  afterEach(cleanup)

  it('always renders the pill — ghost state when unprioritized', () => {
    render(TaskCard, makeProps(makeCard({ priority: 0 })))
    const pill = screen.getByTestId('tasks-priority-c1')
    expect(pill.textContent).toContain('None')
    expect(pill.getAttribute('aria-label')).toBe(
      'Priority: None — click to set Low'
    )
  })

  it('announces the current priority and the next click value', () => {
    render(TaskCard, makeProps(makeCard({ priority: 1 })))
    const pill = screen.getByTestId('tasks-priority-c1')
    expect(pill.textContent).toContain('High')
    // High wraps to Low — the pinned cycle is Low→Normal→High→Low.
    expect(pill.getAttribute('aria-label')).toBe(
      'Priority: High — click to set Low'
    )
  })

  it('clicking the pill cycles priority without opening the card', async () => {
    const onSelect = vi.fn()
    const onPriorityCycle = vi.fn()
    render(TaskCard, {
      ...makeProps(makeCard({ priority: 3 })),
      onSelect,
      onPriorityCycle
    })

    await fireEvent.click(screen.getByTestId('tasks-priority-c1'))

    expect(onPriorityCycle).toHaveBeenCalledWith(2)
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('Enter/Space on the pill cycle without selecting the card', async () => {
    const onSelect = vi.fn()
    const onPriorityCycle = vi.fn()
    render(TaskCard, {
      ...makeProps(makeCard({ priority: 2 })),
      onSelect,
      onPriorityCycle
    })
    const user = userEvent.setup()

    const pill = screen.getByTestId('tasks-priority-c1')
    pill.focus()
    await user.keyboard('{Enter}')
    await tick()
    await user.keyboard(' ')

    expect(onPriorityCycle).toHaveBeenCalledTimes(2)
    expect(onPriorityCycle).toHaveBeenNthCalledWith(1, 1)
    expect(onPriorityCycle).toHaveBeenNthCalledWith(2, 3)
    expect(onSelect).not.toHaveBeenCalled()
  })
})

describe('TaskCard comment + notes indicators (#1152)', () => {
  afterEach(cleanup)

  it('renders the chat_bubble count badge when comments_count > 0', () => {
    render(TaskCard, makeProps(makeCard({ comments_count: 3 })))
    const badge = screen.getByTestId('board-comments-badge-c1')
    expect(badge.textContent).toContain('3')
    expect(badge).toHaveAttribute('title', '3 comments')
    expect(badge.querySelector('.material-symbols-outlined')?.textContent).toBe(
      'chat_bubble'
    )
  })

  it('renders no comment badge and no notes indicator at 0', () => {
    render(TaskCard, makeProps(makeCard({ comments_count: 0 })))
    expect(screen.queryByTestId('board-comments-badge-c1')).toBeNull()
    expect(screen.queryByTestId('board-notes-badge-c1')).toBeNull()
    expect(
      document.querySelector('[data-card="c1"]')!.getAttribute('aria-label')
    ).not.toContain('comments')
  })

  it('has-notes affordance is presence-only with an accessible name', () => {
    render(TaskCard, makeProps(makeCard({ comments_count: 2 })))
    const notes = screen.getByTestId('board-notes-badge-c1')
    expect(notes).toHaveAttribute('role', 'img')
    // Presence-only name — the card root name already announces the count.
    expect(notes).toHaveAttribute('aria-label', 'Has notes')
    expect(notes).toHaveAttribute('title', 'Has notes')
    // Presence state — never a second numeric glyph repeating the count.
    expect(notes.textContent?.trim()).toBe('edit_note')
  })

  it('announces comment presence in the card accessible name', () => {
    render(TaskCard, makeProps(makeCard({ comments_count: 2 })))
    expect(
      screen.getByRole('button', {
        name: 'Ship the shortlist, To Do, 2 comments.'
      })
    ).toBeTruthy()
  })
})
