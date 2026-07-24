import { fireEvent, render, waitFor } from '@testing-library/svelte'
import { describe, expect, it, vi, beforeEach } from 'vitest'

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
import {
  confirmationEntry,
  evidenceEntry,
  proposalEntry,
  statusEntry,
  textEntry,
  toolCallEntry,
  toolResultEntry,
  type AIChatEntry
} from './types'

function props(transcript: AIChatEntry[] = [], busy = false) {
  return {
    title: 'Silt AI',
    transcript,
    busy,
    lastOutcome: null as 'complete' | 'stopped' | 'error' | null,
    providerReady: true,
    onSend: vi.fn(),
    onStop: vi.fn(),
    onAcceptProposal: vi.fn(),
    onDiscardProposal: vi.fn(),
    onConfirmStaging: vi.fn(),
    onRejectStaging: vi.fn(),
    onOpenSettings: vi.fn(),
    onNavigateEvidence: vi.fn(),
    onClear: vi.fn()
  }
}

function everyEntry(): AIChatEntry[] {
  return [
    textEntry({ id: 'user', role: 'user', content: 'Find my launch plan.' }),
    textEntry({
      id: 'assistant',
      role: 'assistant',
      content: 'I found it',
      streaming: true
    }),
    evidenceEntry({
      id: 'evidence',
      role: 'assistant',
      citationIndex: 1,
      title: 'Launch plan',
      excerpt: 'Ship in August',
      target: { blockId: 'block-1', notebook: 'Work' }
    }),
    toolCallEntry({
      id: 'call',
      role: 'assistant',
      toolCallId: 'call-1',
      toolName: 'search_notes',
      args: { query: 'launch' }
    }),
    toolResultEntry({
      id: 'result',
      role: 'system',
      toolCallId: 'call-1',
      toolName: 'search_notes',
      output: 'Launch plan result',
      truncated: true
    }),
    proposalEntry({
      id: 'proposal',
      role: 'assistant',
      title: 'Draft launch note',
      content: 'Launch checklist'
    }),
    confirmationEntry({
      id: 'confirmation',
      role: 'system',
      token: 'stage-token',
      operation: 'delete blocks',
      summary: 'Delete two duplicate blocks',
      affectedCount: 2,
      state: 'rejected'
    }),
    statusEntry({
      id: 'status',
      role: 'system',
      status: 'running',
      message: 'Running search_notes…'
    })
  ]
}

describe('ChatShell', () => {
  beforeEach(() => {
    browserMocks.OpenURL.mockReset()
  })

  it('renders every result kind and navigates evidence', async () => {
    const value = props(everyEntry())
    const { getByText, getAllByText, getByRole } = render(ChatShell, {
      props: value
    })

    expect(getByText('Find my launch plan.')).toBeInTheDocument()
    expect(getByText('I found it')).toBeInTheDocument()
    expect(getByText('Ship in August')).toBeInTheDocument()
    expect(getAllByText('search_notes', { selector: 'strong' })).toHaveLength(2)
    expect(getByText('Truncated')).toBeInTheDocument()
    expect(getByText('Draft launch note')).toBeInTheDocument()
    expect(getByText(/Delete two duplicate blocks/)).toBeInTheDocument()
    expect(getByText('Running search_notes…')).toBeInTheDocument()

    await fireEvent.click(
      getByRole('button', { name: 'Open source 1: Launch plan' })
    )
    expect(value.onNavigateEvidence).toHaveBeenCalledWith({
      blockId: 'block-1',
      notebook: 'Work'
    })
  })

  it('exposes an additions-only busy log and a labelled composer', () => {
    const { getByRole, getByLabelText } = render(ChatShell, {
      props: props([], true)
    })
    const log = getByRole('log', { name: 'Silt AI conversation' })

    expect(log).toHaveAttribute('aria-live', 'polite')
    expect(log).toHaveAttribute('aria-relevant', 'additions')
    expect(log).toHaveAttribute('aria-atomic', 'false')
    expect(log).toHaveAttribute('aria-busy', 'true')
    expect(getByLabelText('Message Silt AI')).toBeInTheDocument()
    expect(
      getByRole('button', { name: 'Stop AI response' })
    ).toBeInTheDocument()
  })

  it('renders assistant markdown as HTML, not raw syntax', () => {
    const { container, getByText } = render(ChatShell, {
      props: props([
        textEntry({
          id: 'a1',
          role: 'assistant',
          content: '**bold** and a list:\n\n- one\n- two'
        }),
        textEntry({
          id: 'u1',
          role: 'user',
          content: 'keep **raw** for user'
        })
      ])
    })
    const md = container.querySelector('.message-md')
    expect(md?.querySelector('strong')?.textContent).toBe('bold')
    expect(md?.textContent).not.toContain('**bold**')
    // User messages stay plain text (escaped), not HTML.
    expect(getByText('keep **raw** for user')).toBeInTheDocument()
  })

  it('opens markdown links via Browser.OpenURL instead of webview navigation', async () => {
    const { container } = render(ChatShell, {
      props: props([
        textEntry({
          id: 'a1',
          role: 'assistant',
          content: 'See [docs](https://example.com/docs) for more.'
        })
      ])
    })
    const link = container.querySelector(
      '.message-md a[href="https://example.com/docs"]'
    )
    expect(link).toBeTruthy()
    await fireEvent.click(link!)
    expect(browserMocks.OpenURL).toHaveBeenCalledWith(
      'https://example.com/docs'
    )
  })

  it('does not open protocol-relative or javascript links', async () => {
    const { container } = render(ChatShell, {
      props: props([
        textEntry({
          id: 'a1',
          role: 'assistant',
          content:
            '[bad](//evil.example/x) and [js](javascript:alert(1)) and [ok](https://safe.example)'
        })
      ])
    })
    const bad = container.querySelector('a[href="//evil.example/x"]')
    const js = container.querySelector('a[href^="javascript"]')
    if (bad) {
      await fireEvent.click(bad)
      expect(browserMocks.OpenURL).not.toHaveBeenCalled()
    }
    if (js) {
      await fireEvent.click(js)
      expect(browserMocks.OpenURL).not.toHaveBeenCalled()
    }
    const ok = container.querySelector('a[href="https://safe.example"]')
    expect(ok).toBeTruthy()
    await fireEvent.click(ok!)
    expect(browserMocks.OpenURL).toHaveBeenCalledWith('https://safe.example')
  })

  it('sends on Enter, preserves Shift+Enter, and stops on scoped Escape', async () => {
    const value = props()
    const { getByLabelText, rerender } = render(ChatShell, { props: value })
    const composer = getByLabelText('Message Silt AI') as HTMLTextAreaElement

    await fireEvent.input(composer, { target: { value: 'Explore this' } })
    await fireEvent.keyDown(composer, { key: 'Enter', shiftKey: true })
    expect(value.onSend).not.toHaveBeenCalled()

    await fireEvent.keyDown(composer, { key: 'Enter' })
    expect(value.onSend).toHaveBeenCalledWith('Explore this')

    await rerender({ ...value, busy: true })
    const busyComposer = getByLabelText('Message Silt AI')
    const escape = new KeyboardEvent('keydown', {
      key: 'Escape',
      bubbles: true,
      cancelable: true
    })
    const escapedSurface = vi.fn()
    window.addEventListener('keydown', escapedSurface)
    busyComposer.dispatchEvent(escape)
    expect(value.onStop).toHaveBeenCalledOnce()
    expect(escapedSurface).not.toHaveBeenCalled()
    window.removeEventListener('keydown', escapedSurface)
  })

  it('blocks sending and opens provider settings when unconfigured', async () => {
    const value = { ...props(), providerReady: false }
    const { getByLabelText, getByRole } = render(ChatShell, { props: value })

    expect(getByLabelText('Message Silt AI')).toBeDisabled()
    expect(getByRole('button', { name: 'Send message' })).toBeDisabled()
    await fireEvent.click(getByRole('button', { name: 'Open AI settings' }))
    expect(value.onOpenSettings).toHaveBeenCalledOnce()
  })

  it('routes proposal acceptance and discard actions', async () => {
    const value = props([
      proposalEntry({
        id: 'proposal-1',
        role: 'assistant',
        title: 'Rewrite summary',
        content: 'A shorter summary.'
      })
    ])
    const { getByRole } = render(ChatShell, { props: value })

    await fireEvent.click(getByRole('button', { name: 'Accept' }))
    await fireEvent.click(getByRole('button', { name: 'Discard' }))
    expect(value.onAcceptProposal).toHaveBeenCalledWith('proposal-1')
    expect(value.onDiscardProposal).toHaveBeenCalledWith('proposal-1')
  })

  it('keeps confirmation focus on safe actions and Escape rejects', async () => {
    const value = props([
      confirmationEntry({
        id: 'confirmation-1',
        role: 'system',
        token: 'stage-token',
        operation: 'delete blocks',
        summary: 'Delete two blocks'
      })
    ])
    const { getByRole } = render(ChatShell, { props: value })
    const dialog = getByRole('alertdialog')
    const reject = getByRole('button', { name: 'Reject' })
    const confirm = getByRole('button', { name: 'Confirm delete blocks' })

    await waitFor(() => expect(reject).toHaveFocus())
    await fireEvent.keyDown(reject, { key: 'Tab', shiftKey: true })
    expect(confirm).toHaveFocus()
    await fireEvent.keyDown(confirm, { key: 'Tab' })
    expect(reject).toHaveFocus()
    await fireEvent.keyDown(dialog, { key: 'Escape' })
    expect(value.onRejectStaging).toHaveBeenCalledWith('stage-token')
  })

  it('sticks to new entries until the user scrolls away', async () => {
    const first = textEntry({
      id: 'first',
      role: 'assistant',
      content: 'First'
    })
    const value = props([first])
    const { getByRole, rerender } = render(ChatShell, { props: value })
    const log = getByRole('log')
    Object.defineProperties(log, {
      scrollHeight: { configurable: true, get: () => 500 },
      clientHeight: { configurable: true, get: () => 100 }
    })

    await rerender({
      ...value,
      transcript: [
        first,
        textEntry({ id: 'second', role: 'assistant', content: 'Second' })
      ]
    })
    await waitFor(() => expect(log.scrollTop).toBe(500))

    log.scrollTop = 100
    await fireEvent.scroll(log)
    await rerender({
      ...value,
      transcript: [
        first,
        textEntry({ id: 'third', role: 'assistant', content: 'Third' })
      ]
    })
    await waitFor(() => expect(log.scrollTop).toBe(100))
  })
})
