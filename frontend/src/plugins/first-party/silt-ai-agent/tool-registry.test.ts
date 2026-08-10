import { afterEach, describe, expect, it, vi } from 'vitest'
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

const stageOperation = vi.hoisted(() =>
  vi.fn(async () => 'tok1234567890abcdef1234567890ab')
)

vi.mock('./staging', () => ({
  stageOperation
}))

const noopCtx = {
  pluginDb: { exec: vi.fn(), query: vi.fn() }
} as unknown as PluginContext

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

  it('validateArgs enforces enum, min/max, and nested required', () => {
    const schema = {
      type: 'object',
      required: ['mode', 'n', 'target'],
      properties: {
        mode: { type: 'string', enum: ['a', 'b'] },
        n: { type: 'integer', minimum: 1, maximum: 10 },
        target: {
          type: 'object',
          properties: {
            page: { type: 'string' }
          },
          required: ['page']
        }
      }
    }
    expect(
      validateArgs(schema, {
        mode: 'a',
        n: 5,
        target: { page: 'p' }
      }).ok
    ).toBe(true)
    expect(
      validateArgs(schema, { mode: 'z', n: 5, target: { page: 'p' } }).ok
    ).toBe(false)
    expect(
      validateArgs(schema, { mode: 'a', n: 0, target: { page: 'p' } }).ok
    ).toBe(false)
    expect(validateArgs(schema, { mode: 'a', n: 5, target: {} }).ok).toBe(false)
  })

  it('validateArgs rejects array elements that fail items schema', () => {
    const schema = {
      type: 'object',
      properties: {
        source_block_ids: { type: 'array', items: { type: 'string' } }
      }
    }
    expect(validateArgs(schema, { source_block_ids: ['a', 'b'] }).ok).toBe(true)
    const bad = validateArgs(schema, {
      source_block_ids: ['ok', 123, null]
    })
    expect(bad.ok).toBe(false)
    if (!bad.ok) {
      expect(bad.error).toMatch(/source_block_ids\[1\]/)
      expect(bad.error).toMatch(/expected string/)
    }
  })

  it('refuses mutators in read_only mode', async () => {
    const handler = vi.fn(async () => ({ content: 'wrote' }))
    registerTool({
      name: 'create_note',
      description: 'write',
      parameters: { type: 'object', properties: {} },
      handler,
      commit: handler
    })
    const res = await dispatchTool(
      noopCtx,
      'create_note',
      {},
      {
        mode: 'read_only'
      }
    )
    expect(res.error).toMatch(/Vault writes are disabled/)
    expect(handler).not.toHaveBeenCalled()
  })

  it('stages mutators with commit in confirm mode without calling handler', async () => {
    stageOperation.mockClear()
    const handler = vi.fn(async () => ({ content: 'wrote' }))
    registerTool({
      name: 'create_note',
      description: 'write',
      parameters: { type: 'object', properties: {} },
      handler,
      commit: handler
    })
    const res = await dispatchTool(
      noopCtx,
      'create_note',
      { page: 'P' },
      { mode: 'confirm' }
    )
    expect(res.isStaged).toBe(true)
    expect(res.stagedToken).toBeTruthy()
    expect(handler).not.toHaveBeenCalled()
    expect(stageOperation).toHaveBeenCalled()
    expect(res.stagedPreview?.severity).toBe('normal')
  })

  it('auto mode runs create_note directly but stages extract_and_save', async () => {
    stageOperation.mockClear()
    const createHandler = vi.fn(async () => ({ content: 'created' }))
    const extractHandler = vi.fn(async () => ({ content: 'extracted' }))
    registerTool({
      name: 'create_note',
      description: 'write',
      parameters: { type: 'object', properties: {} },
      handler: createHandler,
      commit: createHandler
    })
    registerTool({
      name: 'extract_and_save',
      description: 'extract',
      parameters: { type: 'object', properties: {} },
      handler: extractHandler,
      commit: extractHandler
    })
    const direct = await dispatchTool(
      noopCtx,
      'create_note',
      { page: 'P' },
      { mode: 'auto' }
    )
    expect(direct.isStaged).toBeFalsy()
    expect(direct.content).toBe('created')
    expect(createHandler).toHaveBeenCalled()
    expect(stageOperation).not.toHaveBeenCalled()

    stageOperation.mockClear()
    const staged = await dispatchTool(
      noopCtx,
      'extract_and_save',
      { mode: 'summary' },
      { mode: 'auto' }
    )
    expect(staged.isStaged).toBe(true)
    expect(extractHandler).not.toHaveBeenCalled()
    expect(stageOperation).toHaveBeenCalled()
    expect(staged.stagedPreview?.severity).toBe('danger')
  })
})
