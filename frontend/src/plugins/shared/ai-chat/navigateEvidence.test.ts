import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render } from '@testing-library/svelte'
import {
  dispatchNavigateEvidence,
  evidenceNavigateDetail
} from './navigateEvidence'
import { evidenceEntry, textEntry, type AIChatEntry } from './types'
import type { EvidenceTarget } from './types'

const browserMocks = vi.hoisted(() => ({
  OpenURL: vi.fn()
}))

vi.mock('@wailsio/runtime', () => ({
  Browser: {
    OpenURL: browserMocks.OpenURL
  },
  Events: {
    On: vi.fn(() => () => {})
  }
}))

import ChatShell from './ChatShell.svelte'

describe('navigateEvidence (#875)', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('builds a full navigate-to-block detail from the evidence target', () => {
    const target: EvidenceTarget = {
      blockId: 'block-1',
      notebook: 'Work',
      section: 'Notes',
      page: 'Plan'
    }
    expect(evidenceNavigateDetail(target)).toEqual({
      notebook: 'Work',
      section: 'Notes',
      page: 'Plan',
      blockId: 'block-1'
    })
  })

  it('forwards optional locator fields when present (not only blockId)', () => {
    // Regression: the drawer used to drop notebook/section/page at dispatch.
    const target: EvidenceTarget = {
      blockId: 'only-id-was-sent',
      notebook: 'Vault NB',
      section: 'Sec',
      page: 'Page'
    }
    const detail = evidenceNavigateDetail(target)
    expect(detail).toHaveProperty('notebook', 'Vault NB')
    expect(detail).toHaveProperty('section', 'Sec')
    expect(detail).toHaveProperty('page', 'Page')
    expect(detail).toHaveProperty('blockId', 'only-id-was-sent')
    expect(Object.keys(detail).sort()).toEqual(
      ['blockId', 'notebook', 'page', 'section'].sort()
    )
  })

  it('dispatches navigate-to-block with the full locator', () => {
    const handler = vi.fn()
    window.addEventListener('navigate-to-block', handler)

    const target: EvidenceTarget = {
      blockId: 'block-42',
      notebook: 'Work',
      section: 'Inbox',
      page: 'Daily'
    }
    dispatchNavigateEvidence(target)

    expect(handler).toHaveBeenCalledTimes(1)
    const event = handler.mock.calls[0][0] as CustomEvent
    expect(event.detail).toEqual({
      notebook: 'Work',
      section: 'Inbox',
      page: 'Daily',
      blockId: 'block-42'
    })

    window.removeEventListener('navigate-to-block', handler)
  })

  it('citation click → onNavigateEvidence → dispatch delivers full locator (drawer path)', async () => {
    // Mirrors AIChatDrawer: ChatShell calls onNavigateEvidence(target), which
    // the drawer wires to dispatchNavigateEvidence.
    const target: EvidenceTarget = {
      blockId: 'block-1',
      notebook: 'Work',
      section: 'Notes',
      page: 'Plan'
    }
    const transcript: AIChatEntry[] = [
      textEntry({ id: 'u', role: 'user', content: 'Where is launch?' }),
      evidenceEntry({
        id: 'e',
        role: 'assistant',
        citationIndex: 1,
        title: 'Launch plan',
        excerpt: 'Ship in August',
        target
      })
    ]
    const handler = vi.fn()
    window.addEventListener('navigate-to-block', handler)

    const { getByRole } = render(ChatShell, {
      props: {
        title: 'Silt AI',
        transcript,
        busy: false,
        lastOutcome: null,
        providerReady: true,
        onSend: vi.fn(),
        onStop: vi.fn(),
        onAcceptProposal: vi.fn(),
        onDiscardProposal: vi.fn(),
        onConfirmStaging: vi.fn(),
        onRejectStaging: vi.fn(),
        onOpenSettings: vi.fn(),
        onNavigateEvidence: dispatchNavigateEvidence,
        onClear: vi.fn()
      }
    })

    const btn = getByRole('button', { name: 'Open source 1: Launch plan' })
    await fireEvent.click(btn)

    expect(handler).toHaveBeenCalledTimes(1)
    expect((handler.mock.calls[0][0] as CustomEvent).detail).toEqual({
      notebook: 'Work',
      section: 'Notes',
      page: 'Plan',
      blockId: 'block-1'
    })

    window.removeEventListener('navigate-to-block', handler)
  })
})
