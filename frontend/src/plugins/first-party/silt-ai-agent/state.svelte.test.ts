import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../sdk'

const { mockSession, mockCreateSession, resetMockSession } = vi.hoisted(() => {
  const session = {
    run: vi.fn(),
    cancel: vi.fn(),
    resolveStaging: vi.fn()
  }
  return {
    mockSession: session,
    mockCreateSession: vi.fn(() => session),
    resetMockSession: () => {
      session.run.mockReset()
      session.cancel.mockReset()
      session.resolveStaging.mockReset()
    }
  }
})

vi.mock('./agent-loop', () => ({
  createAgentSession: mockCreateSession
}))

import { createAgentController } from './state.svelte'

function completeRun(
  user: string,
  history: unknown[],
  options: {
    onAssistantToolCalls?: (calls: unknown[], content: string) => void
    onToolCall?: (call: {
      id: string
      name: string
      args: Record<string, unknown>
    }) => void
    onToolMessage?: (result: {
      id: string
      name: string
      content: string
    }) => void
    onDone?: (text: string) => void
  }
) {
  const callId = user === 'first question' ? 'tc1' : 'tc2'
  options.onAssistantToolCalls?.(
    [{ id: callId, name: 'lookup', arguments: {} }],
    ''
  )
  options.onToolCall?.({ id: callId, name: 'lookup', args: {} })
  options.onToolMessage?.({ id: callId, name: 'lookup', content: 'found' })
  options.onDone?.(user === 'first question' ? 'first answer' : 'second answer')
  return Promise.resolve({
    text: user === 'first question' ? 'first answer' : 'second answer',
    iterations: 2,
    cancelled: false,
    hitIterationCap: false,
    user,
    history
  })
}

describe('silt-ai-agent state', () => {
  beforeEach(() => {
    resetMockSession()
    mockCreateSession.mockClear()
  })

  it('preserves assistant tool_calls and ordered results across turns', async () => {
    mockSession.run.mockImplementation((user, history, options) =>
      completeRun(user, history, options)
    )
    const controller = createAgentController()
    const ctx = {} as PluginContext

    await controller.send(ctx, 'first question')
    await controller.send(ctx, 'second question')

    const secondTurn = mockSession.run.mock.calls[1]
    const history = secondTurn[1] as {
      role: string
      content: string
      tool_calls?: unknown[]
      tool_call_id?: string
    }[]

    expect(history.map((message) => message.role)).toEqual([
      'user',
      'assistant',
      'tool',
      'assistant'
    ])
    expect(history[1]).toMatchObject({
      role: 'assistant',
      tool_calls: [{ id: 'tc1', name: 'lookup', arguments: {} }]
    })
    expect(history[2]).toEqual({
      role: 'tool',
      tool_call_id: 'tc1',
      content: 'found'
    })
    expect(secondTurn[0]).toBe('second question')
  })

  it('renders tool work before the final assistant text', async () => {
    mockSession.run.mockImplementation((user, history, options) =>
      completeRun(user, history, options)
    )
    const controller = createAgentController()

    await controller.send({} as PluginContext, 'first question')

    expect(controller.messages.map((message) => message.role)).toEqual([
      'user',
      'tool',
      'tool',
      'assistant'
    ])
    expect(controller.messages[1].toolCall?.id).toBe('tc1')
    expect(controller.messages[2].toolResult?.content).toBe('found')
    expect(controller.messages[3].content).toBe('first answer')
  })

  it('does nothing for an empty send and ignores a concurrent send', async () => {
    const release: {
      current:
        | ((result: {
            text: string
            iterations: number
            cancelled: boolean
            hitIterationCap: boolean
          }) => void)
        | null
    } = { current: null }
    mockSession.run.mockImplementation(
      (user: string, history: unknown[], options: unknown) =>
        new Promise((resolve) => {
          release.current = resolve
          void user
          void history
          void options
        })
    )
    const controller = createAgentController()
    const ctx = {} as PluginContext

    await controller.send(ctx, '   ')
    expect(controller.messages).toEqual([])
    expect(mockSession.run).not.toHaveBeenCalled()

    const first = controller.send(ctx, 'first question')
    await vi.waitFor(() => expect(controller.running).toBe(true))
    await controller.send(ctx, 'second question')
    expect(mockSession.run).toHaveBeenCalledTimes(1)

    release.current?.({
      text: '',
      iterations: 0,
      cancelled: true,
      hitIterationCap: false
    })
    await first
    expect(
      controller.messages.filter((message) => message.role === 'user')
    ).toHaveLength(1)
  })
})
