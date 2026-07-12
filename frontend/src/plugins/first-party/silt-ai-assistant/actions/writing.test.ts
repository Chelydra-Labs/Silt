import { describe, expect, it, vi } from 'vitest'
import { v2CtxStubs } from '../../../test-helpers'
import type { PluginContext } from '../../../sdk'
import { resolveSettings } from '../settings'
import {
  buildWritingMessages,
  parseWritingOutput,
  runWritingAction
} from './writing'

describe('writing actions', () => {
  it('builds draft messages with instruction', () => {
    const msgs = buildWritingMessages(
      'draft-expand',
      'context',
      resolveSettings(null),
      'Write about cats'
    )
    expect(msgs[0].role).toBe('system')
    expect(msgs[1].content).toContain('Write about cats')
    expect(msgs[1].content).toContain('context')
  })

  it('parses fenced markdown output', () => {
    expect(parseWritingOutput('```\n- a\n```')).toBe('- a')
  })

  it('runs improve-clarity with mocked complete (no write)', async () => {
    const complete = vi.fn(async () => ({
      content: 'Clearer text.',
      model: 'test'
    }))
    const mutateBlock = vi.fn()
    const ctx = {
      ...v2CtxStubs,
      ai: { complete, embed: vi.fn() },
      mutateBlock
    } as unknown as PluginContext

    const proposal = await runWritingAction(
      ctx,
      'improve-clarity',
      {
        notebook: 'n',
        section: 's',
        page: 'p',
        inputText: 'messy text',
        truncated: false,
        targetBlockId: 'b1'
      },
      resolveSettings(null)
    )

    expect(proposal.status).toBe('ready')
    expect(proposal.proposedMarkdown).toBe('Clearer text.')
    expect(proposal.kind).toBe('replace-selection')
    expect(mutateBlock).not.toHaveBeenCalled()
    expect(complete).toHaveBeenCalled()
  })
})
