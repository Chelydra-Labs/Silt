// SDK-level coverage for ctx.ai.complete / ctx.ai.embed (#216). Verifies the
// bridge threads pluginID + session token, maps the ergonomic SDK shape onto
// the Go binding's snake_case fields (max_tokens), defaults stream=false, and
// NEVER includes an API key in the plugin-facing payload (credentials live
// server-side). Never hits real IPC — mocks the Wails bindings (AGENTS.md
// canonical vi.mock + vi.hoisted pattern).

import { describe, expect, it, beforeEach, vi } from 'vitest'
import { AIErrorKind } from '../generated/enums'

const mocks = vi.hoisted(() => {
  const eventHandlers = new Map<string, Set<(ev: { data?: unknown }) => void>>()
  return {
    pluginAIComplete: vi.fn(
      (_pluginID: string, _token: string, _input: unknown) =>
        Promise.resolve({
          content: 'pong',
          model: 'qwen3:30b-a3b',
          usage: { promptTokens: 3, completionTokens: 1, totalTokens: 4 }
        })
    ),
    pluginAIEmbed: vi.fn((_pluginID: string, _token: string, _input: unknown) =>
      Promise.resolve({
        embeddings: [[0.1, 0.2, 0.3]],
        model: 'nomic-embed-text',
        dimensions: 3,
        usage: { promptTokens: 1, totalTokens: 1 }
      })
    ),
    pluginAICancelStream: vi.fn(() => Promise.resolve()),
    pluginAIStreamReady: vi.fn(() => Promise.resolve()),
    getActiveLocation: vi.fn(() => ({
      notebook: 'Work',
      section: '',
      page: ''
    })),
    eventHandlers,
    eventsOn: vi.fn((name: string, cb: (ev: { data?: unknown }) => void) => {
      if (!eventHandlers.has(name)) eventHandlers.set(name, new Set())
      eventHandlers.get(name)!.add(cb)
      return () => eventHandlers.get(name)?.delete(cb)
    }),
    emitEvent(name: string, data: unknown) {
      for (const cb of eventHandlers.get(name) ?? []) {
        cb({ data })
      }
    }
  }
})

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    PluginAIComplete: mocks.pluginAIComplete,
    PluginAIEmbed: mocks.pluginAIEmbed,
    PluginAICancelStream: mocks.pluginAICancelStream,
    PluginAIStreamReady: mocks.pluginAIStreamReady
  })
)

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: mocks.eventsOn
  }
}))

vi.mock('./location.svelte', () => ({
  getActiveLocation: mocks.getActiveLocation
}))

import { makePluginContext } from './context'
import type { PluginAIChatMessage } from './sdk'

describe('ctx.ai.complete tool-calling (#595)', () => {
  beforeEach(() => mocks.pluginAIComplete.mockClear())

  it('threads tools + tool_choice (camelCase → snake_case) to the binding', async () => {
    const ctx = makePluginContext('p')
    await ctx.ai.complete({
      messages: [{ role: 'user', content: 'find meetings' }],
      tools: [
        {
          name: 'search_notes',
          description: 'Search the vault',
          parameters: { type: 'object', properties: { q: { type: 'string' } } }
        }
      ],
      toolChoice: { mode: 'force', toolName: 'search_notes' }
    })
    const input = mocks.pluginAIComplete.mock.calls[0][2] as Record<
      string,
      unknown
    >
    expect(input.tools).toEqual([
      {
        name: 'search_notes',
        description: 'Search the vault',
        parameters: { type: 'object', properties: { q: { type: 'string' } } }
      }
    ])
    // camelCase toolChoice maps to snake_case tool_choice with tool_name.
    expect(input.tool_choice).toEqual({
      mode: 'force',
      tool_name: 'search_notes'
    })
  })

  it('omits tool_choice when the caller does not set it', async () => {
    const ctx = makePluginContext('p')
    await ctx.ai.complete({
      messages: [{ role: 'user', content: 'x' }],
      tools: [{ name: 'noop', parameters: { type: 'object' } }]
    })
    const input = mocks.pluginAIComplete.mock.calls[0][2] as Record<
      string,
      unknown
    >
    expect(input.tool_choice).toBeUndefined()
  })

  it('maps tool_calls from the result envelope', async () => {
    mocks.pluginAIComplete.mockResolvedValueOnce({
      content: '',
      model: 'm',
      tool_calls: [
        { id: 'call_1', name: 'search_notes', arguments: { q: 'x' } }
      ]
    } as never)
    const ctx = makePluginContext('p')
    const res = await ctx.ai.complete({
      messages: [{ role: 'user', content: 'x' }],
      tools: [{ name: 'search_notes', parameters: { type: 'object' } }]
    })
    expect(res.tool_calls).toEqual([
      { id: 'call_1', name: 'search_notes', arguments: { q: 'x' } }
    ])
  })

  it('threads tool_calls + tool_call_id on messages for multi-turn replay', async () => {
    const ctx = makePluginContext('p')
    const messages = [
      { role: 'user', content: 'find meetings' },
      {
        role: 'assistant',
        content: '',
        tool_calls: [
          { id: 'call_1', name: 'search_notes', arguments: { q: 'x' } }
        ]
      },
      { role: 'tool', content: '3 matches', tool_call_id: 'call_1' }
    ] as unknown as PluginAIChatMessage[]
    await ctx.ai.complete({ messages })
    const input = mocks.pluginAIComplete.mock.calls[0][2] as Record<
      string,
      unknown
    >
    expect(input.messages).toEqual(messages)
  })
})

describe('ctx.ai.complete', () => {
  beforeEach(() => mocks.pluginAIComplete.mockClear())

  it('threads pluginID + session token and maps maxTokens → max_tokens', async () => {
    const ctx = makePluginContext('my-plugin', 'sess-token-123')
    await ctx.ai.complete({
      messages: [{ role: 'user', content: 'ping' }],
      maxTokens: 64,
      temperature: 0.7
    })
    expect(mocks.pluginAIComplete).toHaveBeenCalledTimes(1)
    const [pluginID, token, input] = mocks.pluginAIComplete.mock.calls[0] as [
      string,
      string,
      Record<string, unknown>
    ]
    expect(pluginID).toBe('my-plugin')
    expect(token).toBe('sess-token-123')
    expect(input.messages).toEqual([{ role: 'user', content: 'ping' }])
    // The ergonomic camelCase SDK field must reach the Go binding as snake_case.
    expect(input.max_tokens).toBe(64)
    expect(input.temperature).toBe(0.7)
    // stream defaults to false so the Sprint 22 additive signature is safe.
    expect(input.stream).toBe(false)
  })

  it('defaults model to "" when omitted (provider config supplies it server-side)', async () => {
    const ctx = makePluginContext('p')
    await ctx.ai.complete({ messages: [{ role: 'user', content: 'x' }] })
    const input = mocks.pluginAIComplete.mock.calls[0][2] as Record<
      string,
      unknown
    >
    expect(input.model).toBe('')
  })

  it('maps the result envelope (content/model/usage)', async () => {
    const ctx = makePluginContext('p')
    const res = await ctx.ai.complete({
      messages: [{ role: 'user', content: 'x' }]
    })
    expect(res.content).toBe('pong')
    expect(res.model).toBe('qwen3:30b-a3b')
    expect(res.usage?.totalTokens).toBe(4)
  })

  it('NEVER sends an API key in the plugin payload (credentials are server-side)', async () => {
    const ctx = makePluginContext('p')
    await ctx.ai.complete({ messages: [{ role: 'user', content: 'x' }] })
    const input = mocks.pluginAIComplete.mock.calls[0][2] as Record<
      string,
      unknown
    >
    // No key field of any common spelling reaches the binding from the SDK.
    expect(input).not.toHaveProperty('apiKey')
    expect(input).not.toHaveProperty('api_key')
    expect(input).not.toHaveProperty('key')
    expect(JSON.stringify(input)).not.toMatch(/secret|bearer/i)
  })

  it('normalizes a legacy `kind`-keyed rejection to PluginAIError shape', async () => {
    mocks.pluginAIComplete.mockRejectedValueOnce({
      // The Go AIError serializes as a JSON error over IPC; the SDK wrapper
      // coerces it into the documented PluginAIError shape so plugin catch
      // blocks can branch on `code` regardless of IPC transport quirks.
      kind: AIErrorKind.ErrModelMissing,
      status: 404,
      message: 'no such model'
    })
    const ctx = makePluginContext('p')
    await expect(
      ctx.ai.complete({ messages: [{ role: 'user', content: 'x' }] })
    ).rejects.toMatchObject({
      code: AIErrorKind.ErrModelMissing,
      status: 404,
      message: 'no such model'
    })
  })

  it('prefers `code` over legacy `kind` when both shapes are possible', async () => {
    mocks.pluginAIComplete.mockRejectedValueOnce({
      code: AIErrorKind.ErrRateLimited,
      status: 429,
      message: 'slow down'
    })
    const ctx = makePluginContext('p')
    await expect(
      ctx.ai.complete({ messages: [{ role: 'user', content: 'x' }] })
    ).rejects.toMatchObject({
      code: AIErrorKind.ErrRateLimited,
      status: 429,
      message: 'slow down'
    })
  })

  it('coerces a bare string rejection to a PluginAIError with code "unknown"', async () => {
    mocks.pluginAIComplete.mockRejectedValueOnce('boom')
    const ctx = makePluginContext('p')
    await expect(
      ctx.ai.complete({ messages: [{ role: 'user', content: 'x' }] })
    ).rejects.toMatchObject({ code: 'unknown', message: 'boom' })
  })

  // --- Reasoning-tag normalization (#483) -----------------------------------
  // ctx.ai.complete is the single boundary that strips <thought>/<think>/…
  // reasoning blocks leaking from OpenAI-compatible servers without a reasoning
  // parser, so every plugin consumer receives clean content.
  it('strips a <thought> reasoning block from the returned content', async () => {
    mocks.pluginAIComplete.mockResolvedValueOnce({
      content:
        '<thought>* Draft 1: …</thought>The current method of mapping backend errors…',
      model: 'qwen3:30b',
      usage: { promptTokens: 5, completionTokens: 9, totalTokens: 14 }
    })
    const ctx = makePluginContext('p')
    const res = await ctx.ai.complete({
      messages: [{ role: 'user', content: 'x' }]
    })
    expect(res.content).toBe('The current method of mapping backend errors…')
    // model + usage pass through untouched.
    expect(res.model).toBe('qwen3:30b')
    expect(res.usage?.totalTokens).toBe(14)
  })

  it('is a no-op on reasoning-tag-free content', async () => {
    // The default mock returns clean 'pong'; it must survive unchanged.
    const ctx = makePluginContext('p')
    const res = await ctx.ai.complete({
      messages: [{ role: 'user', content: 'x' }]
    })
    expect(res.content).toBe('pong')
  })

  it('strips only the response content, never the request fields', async () => {
    mocks.pluginAIComplete.mockResolvedValueOnce({
      content: '<think>plan</think>answer',
      model: 'm',
      usage: { promptTokens: 1, completionTokens: 1, totalTokens: 2 }
    })
    const ctx = makePluginContext('p')
    const messages: PluginAIChatMessage[] = [
      { role: 'user', content: 'do not touch <think> me' }
    ]
    await ctx.ai.complete({
      messages,
      maxTokens: 32,
      temperature: 0.4,
      reasoningEffort: 'low',
      responseSchema: { type: 'object' }
    })
    const input = mocks.pluginAIComplete.mock.calls[0][2] as Record<
      string,
      unknown
    >
    // The request payload preserves the user's literal "<think>" verbatim — only
    // the model's RESPONSE is normalized.
    expect(input.messages).toEqual(messages)
    expect(input.max_tokens).toBe(32)
    expect(input.temperature).toBe(0.4)
    expect(input.reasoning_effort).toBe('low')
    expect(input.stream).toBe(false)
    expect(input.response_schema).toEqual({ type: 'object' })
  })
})

describe('ctx.ai.complete stream (#226)', () => {
  beforeEach(() => {
    mocks.pluginAIComplete.mockClear()
    mocks.pluginAICancelStream.mockClear()
    mocks.eventHandlers.clear()
    mocks.eventsOn.mockClear()
  })

  it('returns an async-iterable of deltas and aggregates result()', async () => {
    mocks.pluginAIComplete.mockResolvedValueOnce({
      stream_id: 'sid-1',
      model: 'm'
    } as never)
    const ctx = makePluginContext('p', 'tok')
    const stream = await ctx.ai.complete({
      messages: [{ role: 'user', content: 'hi' }],
      stream: true
    })
    expect(stream.streamId).toBe('sid-1')
    // Ready ack must fire after listeners attach so Go can start the producer.
    expect(mocks.pluginAIStreamReady).toHaveBeenCalledWith('p', 'tok', 'sid-1')

    const collected: string[] = []
    const iter = (async () => {
      for await (const d of stream) collected.push(d)
    })()

    // Let listeners attach.
    await Promise.resolve()
    mocks.emitEvent('ai:complete:delta:p', {
      stream_id: 'sid-1',
      delta: 'Hel'
    })
    mocks.emitEvent('ai:complete:delta:p', {
      stream_id: 'sid-1',
      delta: 'lo'
    })
    mocks.emitEvent('ai:complete:done:p', {
      stream_id: 'sid-1',
      content: 'Hello',
      model: 'm'
    })
    await iter
    expect(collected.join('')).toBe('Hello')
    const res = await stream.result()
    expect(res.content).toBe('Hello')
    expect(res.model).toBe('m')
  })

  it('cancel() calls PluginAICancelStream', async () => {
    mocks.pluginAIComplete.mockResolvedValueOnce({
      stream_id: 'sid-2',
      model: 'm'
    } as never)
    const ctx = makePluginContext('p', 'tok')
    const stream = await ctx.ai.complete({
      messages: [{ role: 'user', content: 'hi' }],
      stream: true
    })
    await stream.cancel()
    expect(mocks.pluginAICancelStream).toHaveBeenCalledWith('p', 'tok', 'sid-2')
  })

  it('non-stream path remains buffered', async () => {
    const ctx = makePluginContext('p')
    const res = await ctx.ai.complete({
      messages: [{ role: 'user', content: 'x' }]
    })
    expect(res).toMatchObject({ content: 'pong', model: 'qwen3:30b-a3b' })
    expect('streamId' in res).toBe(false)
  })

  it('accumulates streamed tool-delta events on stream.toolDeltas (#595)', async () => {
    mocks.pluginAIComplete.mockResolvedValueOnce({
      stream_id: 'sid-tool',
      model: 'm'
    } as never)
    const ctx = makePluginContext('p', 'tok')
    const stream = await ctx.ai.complete({
      messages: [{ role: 'user', content: 'hi' }],
      stream: true
    })
    const iter = (async () => {
      for await (const _d of stream) {
        /* drain */
      }
    })()
    await Promise.resolve()
    mocks.emitEvent('ai:complete:tool-delta:p', {
      stream_id: 'sid-tool',
      index: 0,
      id: 'call_1',
      name: 'search_notes'
    })
    mocks.emitEvent('ai:complete:tool-delta:p', {
      stream_id: 'sid-tool',
      index: 0,
      arguments_fragment: '{"q":"x"}'
    })
    mocks.emitEvent('ai:complete:done:p', {
      stream_id: 'sid-tool',
      content: '',
      model: 'm',
      tool_calls: [
        { id: 'call_1', name: 'search_notes', arguments: { q: 'x' } }
      ]
    })
    await iter
    expect(stream.toolDeltas).toHaveLength(2)
    expect(stream.toolDeltas[0]).toMatchObject({
      id: 'call_1',
      name: 'search_notes'
    })
    const res = await stream.result()
    expect(res.tool_calls).toEqual([
      { id: 'call_1', name: 'search_notes', arguments: { q: 'x' } }
    ])
  })
})

describe('ctx.ai.embed', () => {
  beforeEach(() => mocks.pluginAIEmbed.mockClear())

  it('threads pluginID + token and forwards the batch', async () => {
    const ctx = makePluginContext('emb-plugin', 'tok')
    await ctx.ai.embed({ texts: ['a', 'b'], dimensions: 768 })
    expect(mocks.pluginAIEmbed).toHaveBeenCalledWith('emb-plugin', 'tok', {
      texts: ['a', 'b'],
      model: '',
      dimensions: 768,
      task_type: ''
    })
  })

  it('maps the result envelope (embeddings/model/dimensions/usage)', async () => {
    const ctx = makePluginContext('p')
    const res = await ctx.ai.embed({ texts: ['x'] })
    expect(res.embeddings).toEqual([[0.1, 0.2, 0.3]])
    expect(res.model).toBe('nomic-embed-text')
    expect(res.dimensions).toBe(3)
    expect(res.usage?.totalTokens).toBe(1)
  })

  it('NEVER sends an API key in the plugin payload', async () => {
    const ctx = makePluginContext('p')
    await ctx.ai.embed({ texts: ['x'] })
    const input = mocks.pluginAIEmbed.mock.calls[0][2] as Record<
      string,
      unknown
    >
    expect(input).not.toHaveProperty('apiKey')
    expect(input).not.toHaveProperty('api_key')
    expect(JSON.stringify(input)).not.toMatch(/secret|bearer/i)
  })

  it('normalizes a binding rejection to PluginAIError shape', async () => {
    mocks.pluginAIEmbed.mockRejectedValueOnce({
      kind: AIErrorKind.ErrUnauthorized,
      status: 401,
      message: 'no key'
    })
    const ctx = makePluginContext('p')
    await expect(ctx.ai.embed({ texts: ['x'] })).rejects.toMatchObject({
      code: AIErrorKind.ErrUnauthorized,
      status: 401,
      message: 'no key'
    })
  })
})
