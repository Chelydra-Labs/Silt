import { describe, expect, it, beforeEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

// jsdom polyfill: Svelte 5 transition:fly calls element.animate().
if (!Element.prototype.animate) {
  Element.prototype.animate = function () {
    return {
      cancel() {},
      finish() {},
      play() {},
      pause() {},
      reverse() {},
      commitStyles() {},
      addEventListener() {},
      removeEventListener() {},
      dispatchEvent() {
        return true
      },
      onfinish: null,
      oncancel: null,
      onremove: null,
      currentTime: 0,
      startTime: null,
      playbackRate: 1,
      playState: 'finished',
      replaceState: 'active',
      pending: false,
      id: '',
      effect: null,
      timeline: null,
      get finished() {
        return Promise.resolve()
      },
      get ready() {
        return Promise.resolve()
      }
    }
  } as unknown as Element['animate']
}

vi.mock('../../../../wailsjs/runtime/runtime.js', () => ({
  EventsOn: vi.fn(() => () => {})
}))

import CardDetailPanel from './CardDetailPanel.svelte'
import type { PluginContext } from '../../sdk'
import type { KanbanCard } from './types'
import { v2CtxStubs } from '../../test-helpers'

function makeCard(overrides: Partial<KanbanCard> = {}): KanbanCard {
  return {
    id: 'task-1',
    notebook: 'Work',
    section: 'Journal',
    page: 'Daily',
    file_date: '2026-07-01',
    clean_content: 'Water plants',
    status: 'TODO',
    owner: '',
    start_date: '',
    due_date: '2026-07-15',
    priority: 3,
    pinned: false,
    progress: 0,
    recurrence: '',
    comments_count: 0,
    links_count: 0,
    ...overrides
  }
}

function makeCtx(overrides: Partial<PluginContext> = {}): PluginContext {
  return {
    activeNotebook: 'Work',
    activeSection: 'Journal',
    activePage: 'Daily',
    today: '2026-07-02',
    sqliteQuery: vi.fn(),
    mutateBlock: vi.fn(),
    updateBlockState: vi.fn(),
    updateTaskMeta: vi.fn(),
    getPluginSettings: vi.fn(() => Promise.resolve({})),
    on: () => () => {},
    ...v2CtxStubs,
    ...overrides
  } as unknown as PluginContext
}

describe('CardDetailPanel — recurrence', () => {
  beforeEach(() => cleanup())

  it('does not render the repeat badge when recurrence is empty', () => {
    const ctx = makeCtx()
    const { container } = render(CardDetailPanel, {
      props: { card: makeCard(), ctx, onClose: () => {} }
    })
    expect(container.querySelector('.material-symbols-outlined')).toBeTruthy()
    // No event_repeat icon in the header area.
    const icons = screen.queryAllByText('event_repeat')
    // The recurrence editor button always shows event_repeat; the header
    // badge should NOT appear for a non-recurring card. We check the header
    // h2 specifically.
    const title = container.querySelector('#card-detail-title')
    expect(title?.querySelector('.material-symbols-outlined')).toBeNull()
    void icons
  })

  it('renders the repeat badge in the header when recurrence is set', () => {
    const ctx = makeCtx()
    const { container } = render(CardDetailPanel, {
      props: {
        card: makeCard({ recurrence: 'every week' }),
        ctx,
        onClose: () => {}
      }
    })
    const title = container.querySelector('#card-detail-title')
    const badge = title?.querySelector('.material-symbols-outlined')
    expect(badge).toBeTruthy()
    expect(badge?.textContent?.trim()).toBe('event_repeat')
  })

  it('shows recurrence value in metadata dl', () => {
    const ctx = makeCtx()
    const { container } = render(CardDetailPanel, {
      props: {
        card: makeCard({ recurrence: 'every month' }),
        ctx,
        onClose: () => {}
      }
    })
    // The metadata <dd> is inside the <dl>.
    const dl = container.querySelector('dl')
    expect(dl?.textContent).toContain('every month')
  })

  it('opens the dropdown and selects an interval calling setTaskRecurrence', async () => {
    const setTaskRecurrence = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskRecurrence })
    const { container } = render(CardDetailPanel, {
      props: { card: makeCard(), ctx, onClose: () => {} }
    })
    // Click the recurrence trigger button (aria-haspopup="listbox").
    const trigger = container.querySelector('button[aria-haspopup="listbox"]')
    expect(trigger).toBeTruthy()
    await fireEvent.click(trigger as HTMLElement)
    // Dropdown should show presets.
    const option = screen.getByRole('option', { name: 'every week' })
    await fireEvent.click(option)
    expect(setTaskRecurrence).toHaveBeenCalledWith('task-1', 'every week')
  })

  it('shows Stop recurring when a recurrence is already set', async () => {
    const setTaskRecurrence = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskRecurrence })
    const { container } = render(CardDetailPanel, {
      props: {
        card: makeCard({ recurrence: 'every week' }),
        ctx,
        onClose: () => {}
      }
    })
    // Open the dropdown via the trigger button (aria-haspopup).
    const trigger = container.querySelector('button[aria-haspopup="listbox"]')
    expect(trigger).toBeTruthy()
    await fireEvent.click(trigger as HTMLElement)
    const stopBtn = screen.getByText('Stop recurring')
    await fireEvent.click(stopBtn)
    expect(setTaskRecurrence).toHaveBeenCalledWith('task-1', '')
  })

  it('reverts local state on error', async () => {
    const setTaskRecurrence = vi
      .fn()
      .mockRejectedValue(new Error('disk locked'))
    const ctx = makeCtx({ setTaskRecurrence })
    render(CardDetailPanel, {
      props: { card: makeCard(), ctx, onClose: () => {} }
    })
    const trigger = screen.getByText('Set recurrence…')
    await fireEvent.click(trigger)
    await fireEvent.click(screen.getByText('every day'))
    // Wait for the promise to settle.
    await new Promise((r) => setTimeout(r, 10))
    // The error region should surface the failure.
    expect(screen.getByText(/Couldn't save/)).toBeTruthy()
  })

  it('disables the recurrence editor when no due date', () => {
    const ctx = makeCtx()
    render(CardDetailPanel, {
      props: {
        card: makeCard({ due_date: '' }),
        ctx,
        onClose: () => {}
      }
    })
    expect(screen.getByText(/Set a due date first/)).toBeTruthy()
    // The trigger button should not be present.
    expect(screen.queryByText('Set recurrence…')).toBeNull()
  })

  it('portals the recurrence listbox out of the scroll container so it is not clipped (#376)', async () => {
    const ctx = makeCtx()
    const { container } = render(CardDetailPanel, {
      props: { card: makeCard(), ctx, onClose: () => {} }
    })
    const trigger = container.querySelector(
      'button[aria-haspopup="listbox"]'
    ) as HTMLElement
    await fireEvent.click(trigger)
    // The listbox options render into document.body via the shared <Popover>
    // portal — NOT inside the panel's overflow-y-auto container, so lower
    // options can no longer be clipped when the section sits low.
    const option = await screen.findByRole('option', { name: 'every week' })
    expect(document.body.contains(option)).toBe(true)
    expect(container.contains(option)).toBe(false)
    // "Stop recurring" only appears when a recurrence is already set; here it
    // must be absent, confirming the conditional still works through the portal.
    expect(screen.queryByText('Stop recurring')).toBeNull()
  })
})
