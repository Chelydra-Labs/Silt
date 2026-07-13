import { describe, expect, it, vi } from 'vitest'
import { v2CtxStubs } from '../../../test-helpers'
import type { PluginContext } from '../../../sdk'
import { resolveSettings } from '../settings'
import { dedupeTasks, parseExtractTasks, runExtractTasks } from './extractTasks'

describe('extract tasks', () => {
  it('parses task JSON', () => {
    expect(parseExtractTasks('{"tasks":["Ship v1","Write docs"]}')).toEqual([
      'Ship v1',
      'Write docs'
    ])
  })

  it('dedupes against existing', () => {
    expect(
      dedupeTasks(['Ship v1', 'New one'], ['- [ ] Ship v1', 'Other'])
    ).toEqual(['New one'])
  })

  it('produces insert-tasks proposal without writing', async () => {
    const complete = vi.fn(async () => ({
      content: '{"tasks":["Follow up with Alice"]}',
      model: 't'
    }))
    const createBlock = vi.fn()
    const ctx = {
      ...v2CtxStubs,
      ai: { complete, embed: vi.fn() },
      createBlock
    } as unknown as PluginContext

    const p = await runExtractTasks(
      ctx,
      {
        notebook: 'n',
        section: '',
        page: 'p',
        inputText: 'Alice will follow up next week.',
        truncated: false,
        existingTaskTitles: []
      },
      resolveSettings(null)
    )
    expect(p.kind).toBe('insert-tasks')
    expect(p.tasks).toEqual(['Follow up with Alice'])
    expect(createBlock).not.toHaveBeenCalled()
  })
})
