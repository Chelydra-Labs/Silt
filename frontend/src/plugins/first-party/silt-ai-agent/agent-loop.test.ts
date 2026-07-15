// Mocked ctx.ai.complete streaming. Mirrors the PluginAIStream shape:
// async-iterable of text deltas + result() resolving to the final completion.
import { afterEach, describe, expect, it, vi } from 'vitest'
import type {
  PluginAIChatMessage,
  PluginAICompleteResult,
  PluginAIStream,
  PluginContext
} from '../../sdk'
import {
  createAgentSession,
  MAX_ITERATIONS,
  runAgent,
  truncateToolResult,
  TOOL_RESULT_MAX_BYTES
} from './agent-loop'
import { clearTools, registerTool } from './tool-registry'

function mockStream(
  result: PluginAICompleteResult,
  deltas: string[] = []
): PluginAIStream {
  let i = 0
  return {
    streamId: `s-${Math.random()}`,
    toolDeltas: [],
    cancel: vi.fn(async () => {}),
    result: async () => result,
    async *[Symbol.asyncIterator]() {
      while (i < deltas.length) {
        yield deltas[i++]
      }
    }
  }
}

function mockCtx(
  completeImpl: (calls: number) => PluginAIStream
): PluginContext {
  let calls = 0
  return {
    activeNotebook: 'Work',
    activeSection: '',
    activePage: '',
    ai: {
      complete: vi.fn(async () => {
        calls += 1
        return completeImpl(calls)
      }),
      embed: vi.fn(async () => ({
        embeddings: [],
        model: 'e',
        dimensions: 0
      }))
    }
  } as unknown as PluginContext
}

afterEach(() => {
  clearTools()
})

describe('agent-loop', () => {
  it('dispatches a tool call then returns final text', async () => {
    registerTool({
      name: 'lookup',
      description: 'looks something up',
      parameters: { type: 'object', properties: {} },
      handler: async () => ({ content: 'found it' })
    })

    const calls: string[] = []
    const ctx = mockCtx((n) => {
      if (n === 1) {
        // First turn: model requests a tool call.
        return mockStream({
          content: '',
          model: 'm',
          tool_calls: [{ id: 'tc1', name: 'lookup', arguments: {} }]
        })
      }
      // Second turn: model produces the final answer (no tool calls).
      return mockStream({ content: 'The answer is found it.', model: 'm' }, [
        'The ',
        'answer is ',
        'found it.'
      ])
    })

    const toolCalls: { name: string }[] = []
    const toolResults: { content: string }[] = []
    const chunks: string[] = []
    const res = await runAgent(ctx, 'what did you find?', [], {
      onToolCall: (c) => toolCalls.push(c),
      onToolResult: (r) => toolResults.push(r.result),
      onAssistantText: (_chunk) => chunks.push(_chunk)
    })

    expect(res.cancelled).toBe(false)
    expect(res.hitIterationCap).toBe(false)
    expect(res.iterations).toBe(2)
    expect(res.text).toBe('The answer is found it.')
    expect(toolCalls).toHaveLength(1)
    expect(toolCalls[0].name).toBe('lookup')
    expect(toolResults).toHaveLength(1)
    expect(toolResults[0].content).toBe('found it')
    expect(chunks.join('')).toBe('The answer is found it.')
  })

  it('returns immediately when no tool calls (single-shot answer)', async () => {
    const ctx = mockCtx(() =>
      mockStream({ content: 'direct answer', model: 'm' }, [
        'direct ',
        'answer'
      ])
    )
    const res = await runAgent(ctx, 'hi', [])
    expect(res.iterations).toBe(1)
    expect(res.text).toBe('direct answer')
    expect(res.hitIterationCap).toBe(false)
  })

  it('stops at MAX_ITERATIONS when the model keeps calling tools', async () => {
    registerTool({
      name: 'loop',
      description: 'always called',
      parameters: { type: 'object', properties: {} },
      handler: async () => ({ content: 'again' })
    })
    // Every turn returns a tool call — the loop must cap at MAX_ITERATIONS.
    const ctx = mockCtx(() =>
      mockStream({
        content: '',
        model: 'm',
        tool_calls: [{ id: 'tc', name: 'loop', arguments: {} }]
      })
    )
    const res = await runAgent(ctx, 'go', [])
    expect(res.hitIterationCap).toBe(true)
    expect(res.iterations).toBe(MAX_ITERATIONS)
  })

  it('stops when the abort signal is already aborted', async () => {
    const ctx = mockCtx(() => mockStream({ content: 'x', model: 'm' }))
    const controller = new AbortController()
    controller.abort()
    const res = await runAgent(ctx, 'q', [], { signal: controller.signal })
    expect(res.cancelled).toBe(true)
    // Never reached a complete() call.
    expect(
      (ctx.ai.complete as ReturnType<typeof vi.fn>).mock.calls
    ).toHaveLength(0)
  })

  it('stops mid-loop when cancelled during tool dispatch', async () => {
    registerTool({
      name: 'slow',
      description: 'slow tool',
      parameters: { type: 'object', properties: {} },
      handler: async () => ({ content: 'slow-result' })
    })
    const controller = new AbortController()
    const ctx = mockCtx(() =>
      mockStream({
        content: '',
        model: 'm',
        tool_calls: [{ id: 'tc', name: 'slow', arguments: {} }]
      })
    )
    // Abort the moment the tool is requested.
    const res = await runAgent(ctx, 'q', [], {
      signal: controller.signal,
      onToolCall: () => controller.abort()
    })
    expect(res.cancelled).toBe(true)
  })

  it('truncates tool results above TOOL_RESULT_MAX_BYTES', () => {
    const small = 'x'.repeat(100)
    expect(truncateToolResult(small)).toBe(small)
    const big = 'y'.repeat(TOOL_RESULT_MAX_BYTES + 500)
    const out = truncateToolResult(big)
    expect(out.length).toBeLessThan(big.length)
    expect(out).toMatch(/… truncated at 10KB/)
  })

  it('createAgentSession.cancel aborts the in-flight run', async () => {
    registerTool({
      name: 'loop',
      description: 'loop',
      parameters: { type: 'object', properties: {} },
      handler: async () => ({ content: 'x' })
    })
    const ctx = mockCtx(() =>
      mockStream({
        content: '',
        model: 'm',
        tool_calls: [{ id: 'tc', name: 'loop', arguments: {} }]
      })
    )
    const session = createAgentSession(ctx)
    const history: PluginAIChatMessage[] = []
    const p = session.run('go', history, {
      onToolCall: () => session.cancel()
    })
    const res = await p
    expect(res.cancelled).toBe(true)
  })
})
