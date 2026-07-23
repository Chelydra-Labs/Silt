import { afterEach, describe, expect, it } from 'vitest'
import type { PluginContext } from '../../sdk'
import {
  buildToolCatalog,
  clearTools,
  dispatchTool,
  getTools,
  registerTool,
  unregisterTool,
  validateArgs
} from './tool-registry'

const noopCtx = {} as PluginContext

afterEach(() => {
  clearTools()
})

describe('tool-registry', () => {
  it('registers, lists, and unregisters a tool', () => {
    registerTool({
      name: 'echo',
      description: 'echoes args',
      parameters: { type: 'object', properties: {} },
      handler: async () => ({ content: '' })
    })
    expect(getTools().map((t) => t.name)).toEqual(['echo'])
    unregisterTool('echo')
    expect(getTools()).toHaveLength(0)
  })

  it('buildToolCatalog maps to PluginAIToolDef shape (no handler)', () => {
    registerTool({
      name: 'echo',
      description: 'echoes args',
      parameters: { type: 'object', properties: { q: { type: 'string' } } },
      handler: async () => ({ content: '' })
    })
    const catalog = buildToolCatalog()
    expect(catalog).toHaveLength(1)
    expect(catalog[0]).toEqual({
      name: 'echo',
      description: 'echoes args',
      parameters: { type: 'object', properties: { q: { type: 'string' } } }
    })
    // handler must NOT leak into the catalog sent to the model
    expect('handler' in catalog[0]).toBe(false)
  })

  it('dispatches a no-op tool and returns the result shape', async () => {
    registerTool({
      name: 'noop',
      description: 'does nothing',
      parameters: { type: 'object', properties: {} },
      handler: async () => ({ content: 'ok' })
    })
    const res = await dispatchTool(noopCtx, 'noop', {})
    expect(res).toEqual({ content: 'ok' })
    expect(res.error).toBeUndefined()
  })

  it('returns an error result for an unknown tool', async () => {
    const res = await dispatchTool(noopCtx, 'ghost', {})
    expect(res.content).toBe('')
    expect(res.error).toContain('unknown tool')
  })

  it('validates required fields and rejects missing args', async () => {
    registerTool({
      name: 'needq',
      description: 'needs q',
      parameters: {
        type: 'object',
        required: ['q'],
        properties: { q: { type: 'string' } }
      },
      handler: async () => ({ content: 'never' })
    })
    const res = await dispatchTool(noopCtx, 'needq', {})
    expect(res.content).toBe('')
    expect(res.error).toContain('missing required parameter "q"')
  })

  it('validates types and rejects wrong types', async () => {
    registerTool({
      name: 'typed',
      description: 'typed param',
      parameters: {
        type: 'object',
        properties: { n: { type: 'number' } }
      },
      handler: async (_ctx, args) => ({ content: String(args.n) })
    })
    const bad = await dispatchTool(noopCtx, 'typed', { n: 'not-a-number' })
    expect(bad.error).toMatch(/expected number/)
    const good = await dispatchTool(noopCtx, 'typed', { n: 3 })
    expect(good.content).toBe('3')
  })

  it('catches handler throws into an error result', async () => {
    registerTool({
      name: 'boom',
      description: 'throws',
      parameters: { type: 'object', properties: {} },
      handler: async () => {
        throw new Error('kaboom')
      }
    })
    const res = await dispatchTool(noopCtx, 'boom', {})
    expect(res.content).toBe('')
    expect(res.error).toBe('kaboom')
  })

  it('validateArgs accepts integer for integer type and rejects mismatch', () => {
    const schema = {
      type: 'object',
      required: ['x'],
      properties: { x: { type: 'integer' } }
    }
    expect(validateArgs(schema, { x: 5 }).ok).toBe(true)
    expect(validateArgs(schema, { x: 5.5 }).ok).toBe(false)
    expect(validateArgs(schema, {}).ok).toBe(false)
  })
})
