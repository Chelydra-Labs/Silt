// SDK-level coverage for ctx.ai.complete / ctx.ai.embed (#216). Verifies the
// bridge threads pluginID + session token, maps the ergonomic SDK shape onto
// the Go binding's snake_case fields (max_tokens), defaults stream=false, and
// NEVER includes an API key in the plugin-facing payload (credentials live
// server-side). Never hits real IPC — mocks the Wails bindings (AGENTS.md
// canonical vi.mock + vi.hoisted pattern).

import { describe, expect, it, beforeEach, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
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
  getActiveLocation: vi.fn(() => ({
    notebook: 'Work',
    section: '',
    page: ''
  }))
}))

vi.mock('../../wailsjs/go/main/App.js', () => ({
  PluginAIComplete: mocks.pluginAIComplete,
  PluginAIEmbed: mocks.pluginAIEmbed
}))

vi.mock('./location.svelte', () => ({
  getActiveLocation: mocks.getActiveLocation
}))

import { makePluginContext } from './context'

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
      kind: 'model-missing',
      status: 404,
      message: 'no such model'
    })
    const ctx = makePluginContext('p')
    await expect(
      ctx.ai.complete({ messages: [{ role: 'user', content: 'x' }] })
    ).rejects.toMatchObject({
      code: 'model-missing',
      status: 404,
      message: 'no such model'
    })
  })

  it('prefers `code` over legacy `kind` when both shapes are possible', async () => {
    mocks.pluginAIComplete.mockRejectedValueOnce({
      code: 'rate-limited',
      status: 429,
      message: 'slow down'
    })
    const ctx = makePluginContext('p')
    await expect(
      ctx.ai.complete({ messages: [{ role: 'user', content: 'x' }] })
    ).rejects.toMatchObject({
      code: 'rate-limited',
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
})

describe('ctx.ai.embed', () => {
  beforeEach(() => mocks.pluginAIEmbed.mockClear())

  it('threads pluginID + token and forwards the batch', async () => {
    const ctx = makePluginContext('emb-plugin', 'tok')
    await ctx.ai.embed({ texts: ['a', 'b'], dimensions: 768 })
    expect(mocks.pluginAIEmbed).toHaveBeenCalledWith('emb-plugin', 'tok', {
      texts: ['a', 'b'],
      model: '',
      dimensions: 768
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
      kind: 'unauthorized',
      status: 401,
      message: 'no key'
    })
    const ctx = makePluginContext('p')
    await expect(ctx.ai.embed({ texts: ['x'] })).rejects.toMatchObject({
      code: 'unauthorized',
      status: 401,
      message: 'no key'
    })
  })
})
