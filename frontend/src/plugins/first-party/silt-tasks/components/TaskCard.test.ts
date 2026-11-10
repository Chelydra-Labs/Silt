// Board-card rendering of the shortlist badge (#1092). TaskCard is a pure
// presentational component — props in, markup out — so these are plain render
// assertions (no IPC, per AGENTS.md: Vitest component tests only).
import { describe, expect, it, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/svelte'

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
    onPinToggle: () => {}
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
