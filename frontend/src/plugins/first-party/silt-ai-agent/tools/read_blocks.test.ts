import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { clearTools } from '../tool-registry'
import { handleReadBlocks, readBlocksToolDef } from './read_blocks'

interface B {
  id: string
  clean_content: string
  notebook: string
  section: string
  page: string
  type: string
  parent_id: string | null
  depth: number
  line_number: number
}

function makeCtx(blocks: B[]): PluginContext {
  const byId = new Map(blocks.map((b) => [b.id, b]))
  return {
    sqliteQuery: vi.fn(async (sql: string, params?: unknown[]) => {
      if (sql.includes('parent_id IN')) {
        // Split params into (parentIds, excludeIds) by counting placeholders
        // in the parent_id IN(...) clause.
        const parentIn = sql.match(/parent_id IN \(([^)]*)\)/)
        const nParent = parentIn ? parentIn[1].split(',').length : 0
        const parents = (params ?? []).slice(0, nParent) as string[]
        const excludes = (params ?? []).slice(nParent, -1) as string[]
        const rows = [...byId.values()].filter(
          (b) =>
            b.parent_id !== null &&
            parents.includes(b.parent_id) &&
            !excludes.includes(b.id)
        )
        return {
          rows: rows as unknown as Record<string, unknown>[],
          truncated: false
        }
      }
      if (sql.includes('id IN')) {
        const ids = (params ?? []) as string[]
        const rows = ids.filter((id) => byId.has(id)).map((id) => byId.get(id)!)
        return {
          rows: rows as unknown as Record<string, unknown>[],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })
  } as unknown as PluginContext
}

beforeEach(() => clearTools())
afterEach(() => clearTools())

describe('read_blocks', () => {
  it('rejects more than 20 ids', async () => {
    const ctx = makeCtx([])
    const ids = Array.from({ length: 21 }, (_, i) => `id-${i}`)
    const res = await handleReadBlocks(ctx, { block_ids: ids })
    expect(res.error).toMatch(/exceeds the 20-id limit/)
    expect(res.content).toBe('')
  })

  it('skips unknown UUIDs with a warning and returns the rest', async () => {
    const ctx = makeCtx([
      {
        id: 'b1',
        clean_content: 'real block',
        notebook: 'N',
        section: 'S',
        page: 'P',
        type: 'NOTE',
        parent_id: null,
        depth: 0,
        line_number: 1
      }
    ])
    const res = await handleReadBlocks(ctx, {
      block_ids: ['b1', 'ghost'],
      include_context: false
    })
    expect(res.error).toBeUndefined()
    expect(res.content).toContain('warning: block ghost not found')
    expect(res.content).toContain('real block')
  })

  it('fetches parent and siblings when include_context is true', async () => {
    const ctx = makeCtx([
      {
        id: 'child',
        clean_content: 'child note',
        notebook: 'N',
        section: 'S',
        page: 'P',
        type: 'NOTE',
        parent_id: 'parent',
        depth: 1,
        line_number: 2
      },
      {
        id: 'parent',
        clean_content: 'parent heading',
        notebook: 'N',
        section: 'S',
        page: 'P',
        type: 'HEADER',
        parent_id: null,
        depth: 0,
        line_number: 1
      },
      {
        id: 'sibling',
        clean_content: 'sibling note',
        notebook: 'N',
        section: 'S',
        page: 'P',
        type: 'NOTE',
        parent_id: 'parent',
        depth: 1,
        line_number: 3
      }
    ])
    const res = await handleReadBlocks(ctx, {
      block_ids: ['child'],
      include_context: true
    })
    expect(res.content).toContain('child note')
    expect(res.content).toContain('parent heading')
    expect(res.content).toContain('sibling note')
    // Breadcrumb location present.
    expect(res.content).toContain('N > S > P')
  })

  it('formats breadcrumbs and skips context when include_context is false', async () => {
    const ctx = makeCtx([
      {
        id: 'b1',
        clean_content: 'standalone',
        notebook: 'Work',
        section: 'Notes',
        page: 'Decisions',
        type: 'NOTE',
        parent_id: 'p1',
        depth: 1,
        line_number: 5
      }
    ])
    const res = await handleReadBlocks(ctx, {
      block_ids: ['b1'],
      include_context: false
    })
    expect(res.content).toContain('Work > Notes > Decisions')
    // No context fetched → parent p1 never resolved.
    expect(res.content).not.toContain('Context:')
    expect(res.content).not.toContain('parent [p1]')
  })

  it('truncates oversized output with a visible marker', async () => {
    const ctx = makeCtx([
      {
        id: 'huge',
        clean_content: 'x'.repeat(40_000),
        notebook: 'N',
        section: 'S',
        page: 'P',
        type: 'NOTE',
        parent_id: null,
        depth: 0,
        line_number: 1
      }
    ])
    const res = await handleReadBlocks(ctx, {
      block_ids: ['huge'],
      include_context: false
    })
    expect(res.content.length).toBeLessThanOrEqual(32_000)
    expect(res.content).toContain('[output truncated: size limit reached]')
  })

  it('exposes the tool def shape', () => {
    expect(readBlocksToolDef.name).toBe('read_blocks')
    expect(readBlocksToolDef.parameters.required).toContain('block_ids')
  })
})
