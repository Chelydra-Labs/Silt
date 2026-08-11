import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { clearTools, getTools, registerTool } from '../tool-registry'
import { registerP0Tools } from '../tools'
import {
  handleSearchProductDocs,
  searchProductDocsToolDef
} from './search_product_docs'
import { NO_MATCH_MESSAGE } from '../product-docs/searchProductDocs'

vi.mock('../../shared/ai-chat/availability', () => ({
  getAIAvailability: () => ({
    enabled: true,
    ragEnabled: false,
    summariesEnabled: false,
    agentWrites: 'confirm'
  })
}))

// Avoid pulling real extract/embed deps when registering P0.
vi.mock('./extract_and_save', () => ({
  extractAndSaveToolDef: {
    name: 'extract_and_save',
    description: 'd',
    parameters: { type: 'object', properties: {} }
  },
  handleExtractAndSave: vi.fn(),
  commitExtractAndSave: vi.fn()
}))

const noopCtx = {} as PluginContext

beforeEach(() => clearTools())
afterEach(() => clearTools())

describe('search_product_docs tool', () => {
  it('returns hits with product_help evidence', async () => {
    const res = await handleSearchProductDocs(noopCtx, {
      query: 'how to enable AI and semantic search'
    })
    expect(res.error).toBeUndefined()
    expect(res.content).not.toBe(NO_MATCH_MESSAGE)
    expect(res.content).toMatch(/Silt help/i)
    expect(res.evidence?.length).toBeGreaterThan(0)
    expect(res.evidence![0].sourceKind).toBe('product_help')
    expect(res.evidence![0].blockId).toMatch(/^help:/)
    expect(res.evidence![0].title).toMatch(/^Silt help:/)
  })

  it('rejects empty query', async () => {
    const res = await handleSearchProductDocs(noopCtx, { query: '  ' })
    expect(res.error).toMatch(/query must not be empty/)
  })

  it('returns stable no-match copy', async () => {
    const res = await handleSearchProductDocs(noopCtx, {
      query: 'xyzzy quantum flux capacitor zzz999'
    })
    expect(res.error).toBeUndefined()
    expect(res.content).toBe(NO_MATCH_MESSAGE)
    expect(res.evidence).toBeUndefined()
  })

  it('registers as non-RAG P0 tool', () => {
    registerP0Tools()
    const names = getTools().map((t) => t.name)
    expect(names).toContain('search_product_docs')
  })

  it('tool def shape', () => {
    expect(searchProductDocsToolDef.name).toBe('search_product_docs')
    expect(searchProductDocsToolDef.parameters.required).toEqual(['query'])
    registerTool({
      ...searchProductDocsToolDef,
      handler: handleSearchProductDocs
    })
    expect(getTools().some((t) => t.name === 'search_product_docs')).toBe(true)
  })
})
