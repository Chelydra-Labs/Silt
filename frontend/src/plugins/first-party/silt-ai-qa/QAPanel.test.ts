import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import type { PluginContext } from '../../sdk'

const { mockCtl, mockGetCtl } = vi.hoisted(() => {
  const ctl = {
    messages: [] as { role: string; content: string; citations?: any[] }[],
    citations: [] as any[],
    panelStatus: 'idle' as string,
    errorMessage: '',
    answer: '',
    progress: { status: 'ready', done: 0, total: 0, chunkCount: 3 },
    chatReady: () => true,
    embedReady: () => true,
    ask: vi.fn(async () => {}),
    stop: vi.fn(async () => {}),
    clear: vi.fn()
  }
  return {
    mockCtl: ctl,
    mockGetCtl: vi.fn(() => ctl)
  }
})

vi.mock('./state.svelte', () => ({
  getQAController: mockGetCtl
}))

import QAPanel from './QAPanel.svelte'

describe('QAPanel', () => {
  beforeEach(() => {
    mockCtl.messages = []
    mockCtl.citations = []
    mockCtl.panelStatus = 'idle'
    mockCtl.errorMessage = ''
    mockCtl.ask.mockReset()
    mockCtl.stop.mockReset()
    mockCtl.clear.mockReset()
    mockGetCtl.mockReturnValue(mockCtl)
    mockCtl.chatReady = () => true
    mockCtl.embedReady = () => true
  })

  it('renders empty-state guidance when no messages', () => {
    const { getByText } = render(QAPanel, {
      props: { ctx: {} as PluginContext }
    })
    expect(getByText(/Answers cite source blocks/i)).toBeTruthy()
  })

  it('calls ask on Search button', async () => {
    const { getByPlaceholderText, getByRole } = render(QAPanel, {
      props: { ctx: {} as PluginContext }
    })
    const input = getByPlaceholderText(
      /Search your vault/i
    ) as HTMLTextAreaElement
    await fireEvent.input(input, { target: { value: 'billing?' } })
    await fireEvent.click(getByRole('button', { name: /^Search$/i }))
    expect(mockCtl.ask).toHaveBeenCalled()
  })

  it('shows no-provider banner when chat not ready', () => {
    mockCtl.chatReady = () => false
    const { getByText } = render(QAPanel, {
      props: { ctx: {} as PluginContext }
    })
    expect(getByText(/Configure a chat model/i)).toBeTruthy()
  })

  it('shows no-embedding banner when embed not ready', () => {
    mockCtl.embedReady = () => false
    const { getByText } = render(QAPanel, {
      props: { ctx: {} as PluginContext }
    })
    expect(getByText(/Configure an embedding model/i)).toBeTruthy()
  })

  it('dispatches navigate-to-block on citation click', async () => {
    mockCtl.messages = [
      {
        role: 'assistant',
        content: 'Answer [1]',
        citations: [
          {
            index: 1,
            blockId: 'blk-9',
            notebook: 'N',
            section: 'S',
            page: 'P',
            lineNumber: 1,
            snippet: 'snip'
          }
        ]
      }
    ]
    mockCtl.citations = mockCtl.messages[0].citations!
    const nav = vi.fn()
    window.addEventListener('navigate-to-block', nav as any)
    const { getAllByRole } = render(QAPanel, {
      props: { ctx: {} as PluginContext }
    })
    const buttons = getAllByRole('button').filter((b) =>
      /\[1\]/.test(b.textContent ?? '')
    )
    expect(buttons.length).toBeGreaterThan(0)
    await fireEvent.click(buttons[0])
    expect(nav).toHaveBeenCalled()
    const detail = (nav.mock.calls[0][0] as CustomEvent).detail
    expect(detail.blockId).toBe('blk-9')
    window.removeEventListener('navigate-to-block', nav as any)
  })

  it('New chat resets conversation', async () => {
    mockCtl.messages = [{ role: 'user', content: 'hi' }]
    const { getByRole } = render(QAPanel, {
      props: { ctx: {} as PluginContext }
    })
    await fireEvent.click(getByRole('button', { name: /New chat/i }))
    expect(mockCtl.clear).toHaveBeenCalled()
  })
})
