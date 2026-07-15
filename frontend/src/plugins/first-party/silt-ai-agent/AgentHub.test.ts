import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import type { PluginContext } from '../../sdk'

const { mockCtl, mockGetCtl } = vi.hoisted(() => {
  const ctl = {
    messages: [] as unknown[],
    running: false,
    send: vi.fn(async () => {}),
    cancel: vi.fn(),
    clear: vi.fn()
  }
  return {
    mockCtl: ctl,
    mockGetCtl: vi.fn(() => ctl)
  }
})

vi.mock('./state.svelte', () => ({
  getAgentController: mockGetCtl
}))

import AgentHub from './AgentHub.svelte'

describe('AgentHub', () => {
  beforeEach(() => {
    mockCtl.messages = []
    mockCtl.running = false
    mockCtl.send.mockReset()
    mockCtl.cancel.mockReset()
    mockCtl.clear.mockReset()
    mockGetCtl.mockReturnValue(mockCtl)
  })

  it('renders the empty-state message and the input', () => {
    const { getByRole, getByLabelText } = render(AgentHub, {
      props: { ctx: {} as PluginContext }
    })
    // Message log region present.
    expect(getByRole('log')).toBeTruthy()
    // Input has an accessible label.
    expect(getByLabelText(/Message for AI agent/i)).toBeTruthy()
  })

  it('has a Send button (disabled until input has text)', async () => {
    const { getByRole, getByLabelText } = render(AgentHub, {
      props: { ctx: {} as PluginContext }
    })
    const send = getByRole('button', { name: /Send message/i })
    expect(send).toBeTruthy()
    expect((send as HTMLButtonElement).disabled).toBe(true)
    const input = getByLabelText(/Message for AI agent/i) as HTMLTextAreaElement
    await fireEvent.input(input, { target: { value: 'hello' } })
    expect((send as HTMLButtonElement).disabled).toBe(false)
    await fireEvent.click(send)
    expect(mockCtl.send).toHaveBeenCalled()
  })

  it('shows a Stop button (with aria-label) when running', () => {
    mockCtl.running = true
    const { getByRole, queryByRole } = render(AgentHub, {
      props: { ctx: {} as PluginContext }
    })
    const stop = getByRole('button', { name: /Stop agent/i })
    expect(stop).toBeTruthy()
    // Send button is replaced by Stop while running.
    expect(queryByRole('button', { name: /Send message/i })).toBeNull()
    fireEvent.click(stop)
    expect(mockCtl.cancel).toHaveBeenCalled()
  })

  it('Enter sends and Escape cancels', async () => {
    const { getByLabelText } = render(AgentHub, {
      props: { ctx: {} as PluginContext }
    })
    const input = getByLabelText(/Message for AI agent/i) as HTMLTextAreaElement
    await fireEvent.input(input, { target: { value: 'go' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    expect(mockCtl.send).toHaveBeenCalled()

    mockCtl.running = true
    await fireEvent.keyDown(input, { key: 'Escape' })
    expect(mockCtl.cancel).toHaveBeenCalled()
  })

  it('message log has aria-live=polite', () => {
    const { getByRole } = render(AgentHub, {
      props: { ctx: {} as PluginContext }
    })
    const log = getByRole('log')
    expect(log.getAttribute('aria-live')).toBe('polite')
  })

  it('renders user and assistant messages', () => {
    mockCtl.messages = [
      { id: '1', role: 'user', content: 'hi' },
      { id: '2', role: 'assistant', content: 'hello back' }
    ]
    const { getByText } = render(AgentHub, {
      props: { ctx: {} as PluginContext }
    })
    expect(getByText('hi')).toBeTruthy()
    expect(getByText('hello back')).toBeTruthy()
  })

  it('tool cards are collapsible with aria-expanded', async () => {
    mockCtl.messages = [
      {
        id: 'tc1',
        role: 'tool',
        content: '',
        toolCall: { id: 'c1', name: 'search_notes', args: { q: 'x' } }
      }
    ]
    const { getByRole } = render(AgentHub, {
      props: { ctx: {} as PluginContext }
    })
    const toggle = getByRole('button', { name: /Tool call: search_notes/i })
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    await fireEvent.click(toggle)
    expect(toggle.getAttribute('aria-expanded')).toBe('true')
  })
})
