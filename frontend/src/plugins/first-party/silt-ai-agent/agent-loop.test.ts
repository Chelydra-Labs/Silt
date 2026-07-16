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
  TOOL_RESULT_MAX_BYTES,
  type StagingEvent
} from './agent-loop'
import { clearTools, registerTool } from './tool-registry'
import { stageOperation } from './staging'

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

  it('truncates Unicode results by UTF-8 bytes without splitting a code point', () => {
    const big = '😀'.repeat(TOOL_RESULT_MAX_BYTES)
    const out = truncateToolResult(big)
    const bytes = new TextEncoder().encode(out).byteLength

    expect(bytes).toBeLessThanOrEqual(TOOL_RESULT_MAX_BYTES)
    expect(new TextDecoder().decode(new TextEncoder().encode(out))).toBe(out)
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

  it('cancelling while awaiting staging terminates the run', async () => {
    registerTool({
      name: 'stage',
      description: 'stage',
      parameters: { type: 'object', properties: {} },
      handler: async () => ({
        content: '',
        isStaged: true,
        stagedToken: 'pending-token',
        stagedPreview: { kind: 'stage', summary: 'Stage an operation' }
      })
    })
    const ctx = mockCtx(() =>
      mockStream({
        content: '',
        model: 'm',
        tool_calls: [{ id: 'tc-stage', name: 'stage', arguments: {} }]
      })
    )
    const session = createAgentSession(ctx)
    let staged = false
    const run = session.run('stage it', [], {
      onStaging: () => {
        staged = true
        session.cancel()
      }
    })

    const res = await run
    expect(staged).toBe(true)
    expect(res.cancelled).toBe(true)
  })

  it('cancels the active stream directly even when it has no next delta', async () => {
    let releaseNext: ((step: IteratorResult<string>) => void) | null = null
    const stream = {
      streamId: 'waiting',
      toolDeltas: [],
      cancel: vi.fn(async () => {
        releaseNext?.({ done: true, value: '' })
      }),
      result: vi.fn(async () => ({ content: 'late', model: 'm' })),
      [Symbol.asyncIterator]() {
        return {
          next: () =>
            new Promise<IteratorResult<string>>((resolve) => {
              releaseNext = resolve
            }),
          [Symbol.asyncIterator]() {
            return this
          }
        }
      }
    } as unknown as PluginAIStream
    const ctx = mockCtx(() => stream)
    const session = createAgentSession(ctx)
    const run = session.run('wait', [])
    await vi.waitFor(() => expect(releaseNext).not.toBeNull())

    session.cancel()
    const res = await run
    expect(res.cancelled).toBe(true)
    expect(stream.cancel).toHaveBeenCalled()
  })

  it('a new run is not aborted by cleanup from the cancelled old run', async () => {
    let oldReleaseNext: ((step: IteratorResult<string>) => void) | null = null
    const oldStream = {
      streamId: 'old',
      toolDeltas: [],
      cancel: vi.fn(async () => {
        oldReleaseNext?.({ done: true, value: '' })
      }),
      result: vi.fn(async () => ({ content: 'old', model: 'm' })),
      [Symbol.asyncIterator]() {
        return {
          next: () =>
            new Promise<IteratorResult<string>>((resolve) => {
              oldReleaseNext = resolve
            }),
          [Symbol.asyncIterator]() {
            return this
          }
        }
      }
    } as unknown as PluginAIStream
    let completeCalls = 0
    const ctx = mockCtx(() => {
      completeCalls++
      return completeCalls === 1
        ? oldStream
        : mockStream({ content: 'new answer', model: 'm' })
    })
    const session = createAgentSession(ctx)
    const oldRun = session.run('old', [])
    await vi.waitFor(() => expect(oldReleaseNext).not.toBeNull())

    const newRun = session.run('new', [])
    const newResult = await newRun
    const oldResult = await oldRun

    expect(newResult.cancelled).toBe(false)
    expect(newResult.text).toBe('new answer')
    expect(oldResult.cancelled).toBe(true)
  })

  it('surfaces a failed parallel tool and its successful sibling', async () => {
    registerTool({
      name: 'fails',
      description: 'fails',
      parameters: { type: 'object', properties: {} },
      handler: async () => {
        throw new Error('failed deliberately')
      }
    })
    registerTool({
      name: 'succeeds',
      description: 'succeeds',
      parameters: { type: 'object', properties: {} },
      handler: async () => ({ content: 'sibling result' })
    })
    const messages: { id: string; content: string; error?: string }[] = []
    const ctx = mockCtx((n) =>
      n === 1
        ? mockStream({
            content: '',
            model: 'm',
            tool_calls: [
              { id: 'bad', name: 'fails', arguments: {} },
              { id: 'good', name: 'succeeds', arguments: {} }
            ]
          })
        : mockStream({ content: 'done', model: 'm' })
    )

    const res = await runAgent(ctx, 'run both', [], {
      onToolMessage: (message) => messages.push(message)
    })

    expect(res.cancelled).toBe(false)
    expect(messages).toEqual([
      expect.objectContaining({
        id: 'bad',
        content: 'Error: failed deliberately'
      }),
      expect.objectContaining({ id: 'good', content: 'sibling result' })
    ])
  })

  it('reports partial results and returns promptly when parallel dispatch is cancelled', async () => {
    registerTool({
      name: 'slow-a',
      description: 'slow a',
      parameters: { type: 'object', properties: {} },
      handler: async () => new Promise(() => {})
    })
    registerTool({
      name: 'slow-b',
      description: 'slow b',
      parameters: { type: 'object', properties: {} },
      handler: async () => new Promise(() => {})
    })
    const controller = new AbortController()
    const partial: { id: string; content: string; error?: string }[] = []
    const ctx = mockCtx(() =>
      mockStream({
        content: '',
        model: 'm',
        tool_calls: [
          { id: 'a', name: 'slow-a', arguments: {} },
          { id: 'b', name: 'slow-b', arguments: {} }
        ]
      })
    )

    const run = runAgent(ctx, 'cancel both', [], {
      signal: controller.signal,
      onToolCall: (call) => {
        if (call.id === 'b') controller.abort()
      },
      onToolMessage: (message) => partial.push(message)
    })
    const res = await run

    expect(res.cancelled).toBe(true)
    expect(partial).toHaveLength(2)
    expect(partial.every((message) => message.error)).toBe(true)
  })
})

// Phase 5 staging integration (#605): a tool that returns isStaged pauses the
// loop until the UX resolves the token. On confirm, the tool's commit runs
// against the (unmodified) stored params; on reject, "rejected by user" is
// fed back to the model.
describe('agent-loop staging', () => {
  afterEach(() => {
    clearTools()
    vi.useRealTimers()
  })

  /** PluginDb mock that backs staging_tokens with a real in-memory table. */
  function mockPluginDb() {
    const rows = new Map<
      string,
      {
        token: string
        plugin_id: string
        operation: string
        expires_at: number
        used: number
      }
    >()
    return {
      rows,
      db: {
        exec: vi.fn(async (sql: string, params: unknown[] = []) => {
          const upper = sql.trim().toUpperCase()
          if (upper.startsWith('INSERT')) {
            const [token, pluginId, operation, _created, expiresAt] =
              params as [string, string, string, number, number]
            rows.set(token, {
              token,
              plugin_id: pluginId,
              operation,
              expires_at: expiresAt,
              used: 0
            })
          } else if (upper.startsWith('UPDATE')) {
            const tok = String(params[0])
            const row = rows.get(tok)
            if (!row) return
            if (upper.includes('USED = 0') && row.used !== 0) return
            row.used = 1
          } else if (upper.startsWith('DELETE')) {
            const cutoff = Number(params[0])
            for (const [tok, r] of rows) {
              if (r.expires_at < cutoff) rows.delete(tok)
            }
          }
        }),
        query: vi.fn(async (sql: string, params: unknown[] = []) => {
          const upper = sql.trim().toUpperCase()
          if (upper.startsWith('SELECT')) {
            const tok = String(params[0])
            const row = rows.get(tok)
            if (!row) return { rows: [], truncated: false }
            if (upper.includes('USED = 0') && row.used !== 0) {
              return { rows: [], truncated: false }
            }
            return { rows: [{ ...row }], truncated: false }
          }
          return { rows: [], truncated: false }
        }),
        migrate: vi.fn(async () => {})
      }
    }
  }

  /** Stage via the real staging module so the token round-trips through
   *  confirmOperation in the loop. */
  function stageOpForTest(
    ctx: PluginContext,
    kind: string,
    params: Record<string, unknown>
  ): Promise<string> {
    return stageOperation(ctx, kind, params)
  }

  function mockCtxWithDb(
    completeImpl: (calls: number) => PluginAIStream
  ): PluginContext {
    const db = mockPluginDb()
    const baseCtx = mockCtx(completeImpl)
    return { ...baseCtx, pluginDb: db.db } as unknown as PluginContext
  }

  it('on confirm: pauses via onStaging, runs commit, feeds result to model', async () => {
    // The destructive tool's handler stages a delete; commit runs the real
    // write after the UX confirms. We assert that commit sees the stored
    // params (not the model's args — the model cannot mutate the staged op).
    const commitCalls: Record<string, unknown>[] = []
    registerTool({
      name: 'delete_blocks',
      description: 'stage then delete',
      parameters: {
        type: 'object',
        required: ['ids'],
        properties: { ids: { type: 'array', items: { type: 'string' } } }
      },
      async handler(_ctx, args) {
        // Stage and return a preview; the loop intercepts.
        const token = await stageOpForTest(_ctx, 'delete_blocks', args)
        return {
          content: '',
          isStaged: true,
          stagedToken: token,
          stagedPreview: {
            kind: 'delete_blocks',
            summary: 'Delete 2 blocks',
            affectedCount: 2
          }
        }
      },
      async commit(_ctx, params) {
        commitCalls.push(params)
        const ids = (params as { ids: string[] }).ids
        return { content: `Deleted ${ids.length} block(s).` }
      }
    })

    const stagingEvents: StagingEvent[] = []
    let callN = 0
    const ctx = mockCtxWithDb((n) => {
      callN = n
      if (n === 1) {
        return mockStream({
          content: '',
          model: 'm',
          tool_calls: [
            {
              id: 'tc1',
              name: 'delete_blocks',
              arguments: { ids: ['b1', 'b2'] }
            }
          ]
        })
      }
      return mockStream({ content: 'OK done.', model: 'm' })
    })

    // Capture tool messages by intercepting the second iteration's messages.
    // We approximate by reading the staging events and asserting the flow.
    const session = createAgentSession(ctx)
    const p = session.run('delete b1 and b2', [], {
      onStaging: (e) => {
        stagingEvents.push(e)
        // Simulate the UX confirming immediately.
        queueMicrotask(() => session.resolveStaging(e.token, true))
      }
    })
    const res = await p

    expect(callN).toBe(2) // staging turn + final answer turn
    expect(res.text).toBe('OK done.')
    expect(stagingEvents).toHaveLength(1)
    expect(stagingEvents[0].preview.summary).toBe('Delete 2 blocks')
    // Commit ran with the staged params (the model's args were captured at
    // stage time and replayed verbatim — the model cannot mutate them).
    expect(commitCalls).toEqual([{ ids: ['b1', 'b2'] }])
  })

  it('on reject: surfaces "rejected by user" to the model', async () => {
    const commitCalls: unknown[] = []
    registerTool({
      name: 'delete_blocks',
      description: 'stage then delete',
      parameters: {
        type: 'object',
        required: ['ids'],
        properties: { ids: { type: 'array', items: { type: 'string' } } }
      },
      async handler(_ctx, args) {
        const token = await stageOpForTest(_ctx, 'delete_blocks', args)
        return {
          content: '',
          isStaged: true,
          stagedToken: token,
          stagedPreview: {
            kind: 'delete_blocks',
            summary: 'Delete 1 block'
          }
        }
      },
      async commit(_ctx, params) {
        commitCalls.push(params)
        return { content: 'should not run on reject' }
      }
    })

    const ctx = mockCtxWithDb((n) => {
      if (n === 1) {
        return mockStream({
          content: '',
          model: 'm',
          tool_calls: [
            {
              id: 'tc1',
              name: 'delete_blocks',
              arguments: { ids: ['bx'] }
            }
          ]
        })
      }
      // Capture the second-turn message list to assert the tool body.
      const stream = mockStream({ content: 'acknowledged.', model: 'm' })
      return stream
    })

    // Patch dispatchTool path is hard; instead, inspect by capturing the
    // messages the second iteration saw via a spy on ctx.ai.complete.
    const completeSpy = ctx.ai.complete as ReturnType<typeof vi.fn>
    const session = createAgentSession(ctx)
    await session.run('delete bx', [], {
      onStaging: (e) => {
        queueMicrotask(() => session.resolveStaging(e.token, false))
      }
    })

    expect(commitCalls).toHaveLength(0)
    // Second complete() call's messages should include a tool role with the
    // "rejected by user" body.
    const secondCallMessages = (
      completeSpy.mock.calls[1][0] as { messages: PluginAIChatMessage[] }
    ).messages
    const toolMsg = secondCallMessages.find((m) => m.role === 'tool')
    expect(toolMsg?.content).toMatch(/rejected by the user/i)
  })

  it('auto-rejects when no awaitStaging/onStaging is wired', async () => {
    // The default (no UX) treats staging as auto-rejected — important so a
    // stray staged tool result in a test/headless context cannot hang the
    // loop forever waiting for a confirmation that never comes.
    registerTool({
      name: 'delete_blocks',
      description: 'stage then delete',
      parameters: {
        type: 'object',
        required: ['ids'],
        properties: { ids: { type: 'array', items: { type: 'string' } } }
      },
      async handler(_ctx, args) {
        const token = await stageOpForTest(_ctx, 'delete_blocks', args)
        return {
          content: '',
          isStaged: true,
          stagedToken: token,
          stagedPreview: { kind: 'delete_blocks', summary: 'Delete 1 block' }
        }
      },
      async commit() {
        return { content: 'ran' }
      }
    })

    const ctx = mockCtxWithDb((n) => {
      if (n === 1) {
        return mockStream({
          content: '',
          model: 'm',
          tool_calls: [
            { id: 'tc1', name: 'delete_blocks', arguments: { ids: ['bx'] } }
          ]
        })
      }
      return mockStream({ content: 'done.', model: 'm' })
    })
    const completeSpy = ctx.ai.complete as ReturnType<typeof vi.fn>

    await runAgent(ctx, 'delete bx', [])

    const secondCallMessages = (
      completeSpy.mock.calls[1][0] as { messages: PluginAIChatMessage[] }
    ).messages
    const toolMsg = secondCallMessages.find((m) => m.role === 'tool')
    expect(toolMsg?.content).toMatch(/rejected by the user/i)
  })
})
