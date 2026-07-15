// Phase 5 StagingConfirm component tests (#605).
//
// Covers: renders the staged summary; Confirm/Reject buttons have accessible
// names; aria-live region announces; Escape rejects; click Confirm/Reject
// invokes the right callback. Mocks nothing — the component is pure DOM
// over its props.

import { describe, expect, it, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import StagingConfirm from './StagingConfirm.svelte'
import type { StagingEvent } from './agent-loop'

function makeEvent(overrides: Partial<StagingEvent> = {}): StagingEvent {
  return {
    token: 'a'.repeat(32),
    preview: {
      kind: 'delete_blocks',
      summary: 'Delete 3 blocks in Work/Notes/Decisions',
      affectedCount: 3,
      details: '- block 1\n- block 2\n- block 3'
    },
    ...overrides
  }
}

describe('StagingConfirm', () => {
  it('renders the operation title + summary in the live region', () => {
    const { getByText, getByRole } = render(StagingConfirm, {
      props: {
        event: makeEvent(),
        onConfirm: () => {},
        onReject: () => {}
      }
    })
    // Title is humanized from the kind.
    expect(getByText('Delete blocks')).toBeTruthy()
    // Dialog role present for screen readers.
    expect(getByRole('dialog', { name: 'Delete blocks' })).toBeTruthy()
    // Summary appears in the body.
    expect(getByText(/Delete 3 blocks in Work\/Notes\/Decisions/)).toBeTruthy()
  })

  it('Confirm and Reject buttons have accessible labels', () => {
    const { getByRole } = render(StagingConfirm, {
      props: {
        event: makeEvent(),
        onConfirm: () => {},
        onReject: () => {}
      }
    })
    expect(getByRole('button', { name: /Confirm operation/i })).toBeTruthy()
    expect(getByRole('button', { name: /Reject operation/i })).toBeTruthy()
  })

  it('clicking Confirm invokes onConfirm with the token', async () => {
    const onConfirm = vi.fn()
    const { getByRole } = render(StagingConfirm, {
      props: { event: makeEvent(), onConfirm, onReject: () => {} }
    })
    await fireEvent.click(getByRole('button', { name: /Confirm operation/i }))
    expect(onConfirm).toHaveBeenCalledWith('a'.repeat(32))
  })

  it('clicking Reject invokes onReject with the token', async () => {
    const onReject = vi.fn()
    const { getByRole } = render(StagingConfirm, {
      props: {
        event: makeEvent({ token: 'b'.repeat(32) }),
        onConfirm: () => {},
        onReject
      }
    })
    await fireEvent.click(getByRole('button', { name: /Reject operation/i }))
    expect(onReject).toHaveBeenCalledWith('b'.repeat(32))
  })

  it('Escape key triggers onReject', async () => {
    const onReject = vi.fn()
    const { getByRole } = render(StagingConfirm, {
      props: { event: makeEvent(), onConfirm: () => {}, onReject }
    })
    const dialog = getByRole('dialog')
    await fireEvent.keyDown(dialog, { key: 'Escape' })
    expect(onReject).toHaveBeenCalledWith('a'.repeat(32))
  })

  it('summary live region is aria-live=assertive', () => {
    const { getByText } = render(StagingConfirm, {
      props: {
        event: makeEvent(),
        onConfirm: () => {},
        onReject: () => {}
      }
    })
    const summary = getByText(/Delete 3 blocks in Work/)
    expect(summary.getAttribute('aria-live')).toBe('assertive')
  })

  it('renders details block when provided', () => {
    const { getByText } = render(StagingConfirm, {
      props: {
        event: makeEvent(),
        onConfirm: () => {},
        onReject: () => {}
      }
    })
    // The details block is a <pre> with the multi-line breakdown.
    expect(getByText(/block 1/)).toBeTruthy()
  })

  it('omits details block when not provided', () => {
    const { container } = render(StagingConfirm, {
      props: {
        event: makeEvent({
          preview: { kind: 'rename_tag', summary: 'Rename #foo → #bar' }
        }),
        onConfirm: () => {},
        onReject: () => {}
      }
    })
    expect(container.querySelector('.staging-details')).toBeNull()
  })

  it('auto-focuses the Confirm button on mount', async () => {
    const { getByRole } = render(StagingConfirm, {
      props: {
        event: makeEvent(),
        onConfirm: () => {},
        onReject: () => {}
      }
    })
    const confirm = getByRole('button', { name: /Confirm operation/i })
    expect(document.activeElement).toBe(confirm)
  })
})
