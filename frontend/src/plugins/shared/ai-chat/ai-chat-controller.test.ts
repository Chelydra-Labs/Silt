import { describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../sdk'

// Keep the controller hermetic: stub the default capabilities + settings so
// construction does not pull in the agent loop or writing-action graph.
vi.mock('../../../settings/ai-setup', () => ({
  aiProviderNeedsSetup: () => false
}))
vi.mock('../../../settings/store.svelte', () => ({
  settings: { config: { ai: { chat: { provider_type: 'openai-compatible' } } } }
}))

// Shared so the hoisted default mock and the mockImplementationOnce override stay in sync.
type AgentRunOptions = {
  onToolCall?: (c: {
    id: string
    name: string
    args: Record<string, unknown>
  }) => void
  onToolResult?: (r: {
    id: string
    name: string
    result: { content: string }
  }) => void
  onStaging?: (e: {
    token: string
    preview: {
      kind: string
      summary: string
      details: string
      affectedCount: number
    }
  }) => void
  onStagingOutcome?: (token: string, outcome: string) => void
  onDone?: (text: string) => void
}

const agentMocks = vi.hoisted(() => ({
  run: vi.fn(
    async (_text: string, _history: unknown, _opts: AgentRunOptions) => ({
      text: 'done'
    })
  ),
  cancel: vi.fn(),
  resolveStaging: vi.fn()
}))
vi.mock('../../first-party/silt-ai-agent/agent-loop', () => ({
  createAgentSession: () => ({
    run: agentMocks.run,
    cancel: agentMocks.cancel,
    resolveStaging: agentMocks.resolveStaging
  })
}))
vi.mock('./capabilities/writing-capability', () => ({
  createWritingCapability: () => ({ id: 'writing-proposals' })
}))

import { createAIChatController } from './ai-chat-controller.svelte'
import type { AIChatCapability } from './ai-chat-controller.svelte'
import { proposalEntry, textEntry, type ProposalEntry } from './types'

function pluginContextStub(): PluginContext {
  return { activeNotebook: 'Work' } as unknown as PluginContext
}

describe('AI chat controller — proposal accept/discard failure handling', () => {
  it('surfaces a failed apply as an error entry and leaves the proposal pending', async () => {
    const controller = createAIChatController()
    controller.attach(pluginContextStub())

    let proposalId = ''
    const accept = vi.fn(async () => {
      throw new Error('apply exploded')
    })
    const stub: AIChatCapability = {
      id: 'stub',
      run: async (_text, context) => {
        const entry = proposalEntry({
          role: 'assistant',
          title: 'Draft',
          content: 'body'
        })
        proposalId = entry.id
        context.append(entry)
      },
      acceptProposal: accept
    }
    controller.registerCapability(stub, { makeDefault: true })

    await controller.send('/stub')
    expect(proposalId).not.toBe('')

    await controller.acceptProposal(proposalId)
    expect(accept).toHaveBeenCalledOnce()

    // The failed apply must not flip the proposal to accepted.
    const proposal = controller.transcript.find(
      (entry): entry is ProposalEntry =>
        entry.id === proposalId && entry.kind === 'proposal'
    )
    expect(proposal?.state ?? 'pending').toBe('pending')

    // And an error status must be surfaced to the transcript.
    expect(
      controller.transcript.some(
        (entry) => entry.kind === 'status' && entry.status === 'error'
      )
    ).toBe(true)

    controller.dispose()
  })

  it('clears transcript and capability state when attached to a new context (vault switch)', async () => {
    const controller = createAIChatController()
    controller.attach({} as PluginContext)

    // Populate the transcript via a stub default capability.
    const stub: AIChatCapability = {
      id: 'stub',
      run: async (_text, context) => {
        context.append(
          proposalEntry({ role: 'assistant', title: 'x', content: 'y' })
        )
      }
    }
    controller.registerCapability(stub, { makeDefault: true })
    await controller.send('hello')
    expect(controller.transcript.length).toBeGreaterThan(0)

    // A genuinely new context (vault switch) must not carry the prior vault's
    // transcript or protocol history forward.
    controller.attach({ activeNotebook: 'Other' } as unknown as PluginContext)
    expect(controller.transcript).toEqual([])
    expect(controller.busy).toBe(false)

    controller.dispose()
  })

  it('leaves a done status entry after a successful run', async () => {
    const controller = createAIChatController()
    controller.attach(pluginContextStub())
    const stub: AIChatCapability = {
      id: 'stub',
      run: async () => {
        /* no-op success */
      }
    }
    controller.registerCapability(stub, { makeDefault: true })
    await controller.send('hello')
    expect(controller.lastOutcome).toBe('complete')
    expect(
      controller.transcript.some(
        (e) =>
          e.kind === 'status' && e.status === 'done' && e.message === 'Done'
      )
    ).toBe(true)
    controller.dispose()
  })

  it('ignores transcript mutations from a stale run after clear()', async () => {
    const controller = createAIChatController()
    controller.attach({} as PluginContext)
    let resolveRun!: () => void
    const runPromise = new Promise<void>((r) => {
      resolveRun = r
    })
    const stub: AIChatCapability = {
      id: 'stub',
      run: async (_text, context) => {
        await runPromise
        // Late callback arriving after clear(): must be fenced out.
        context.append(
          proposalEntry({ role: 'assistant', title: 'stale', content: 'x' })
        )
      }
    }
    controller.registerCapability(stub, { makeDefault: true })

    const sendPromise = controller.send('a') // suspends inside the stub
    controller.clear() // bumps run id + wipes transcript
    resolveRun() // stale run completes and attempts a late append
    await sendPromise

    expect(controller.transcript).toEqual([])
    expect(controller.busy).toBe(false)
    controller.dispose()
  })

  it('finalizes a streaming assistant entry when the run is stopped', async () => {
    const controller = createAIChatController()
    controller.attach({} as PluginContext)
    let resolveRun!: () => void
    const runPromise = new Promise<void>((r) => {
      resolveRun = r
    })
    const stub: AIChatCapability = {
      id: 'stub',
      run: async (_text, context) => {
        context.append(
          textEntry({
            id: 'streaming-entry',
            role: 'assistant',
            content: 'partial',
            streaming: true
          })
        )
        await runPromise
      }
    }
    controller.registerCapability(stub, { makeDefault: true })

    const sendPromise = controller.send('x') // stub appends streaming entry
    controller.stop() // must drop the streaming caret
    resolveRun()
    await sendPromise

    const entry = controller.transcript.find((e) => e.id === 'streaming-entry')
    expect(entry?.kind).toBe('text')
    expect((entry as { streaming?: boolean })?.streaming).toBeFalsy()
    expect(controller.lastOutcome).toBe('stopped')
    controller.dispose()
  })

  it('sets lastOutcome to error when the capability run rejects', async () => {
    const controller = createAIChatController()
    controller.attach(pluginContextStub())
    const stub: AIChatCapability = {
      id: 'stub',
      run: async () => {
        throw new Error('provider blew up')
      }
    }
    controller.registerCapability(stub, { makeDefault: true })

    await controller.send('hello')

    expect(controller.lastOutcome).toBe('error')
    expect(
      controller.transcript.some(
        (entry) => entry.kind === 'status' && entry.status === 'error'
      )
    ).toBe(true)
    expect(controller.busy).toBe(false)
    controller.dispose()
  })

  it('sets lastOutcome to complete on a successful run', async () => {
    const controller = createAIChatController()
    controller.attach(pluginContextStub())
    const stub: AIChatCapability = {
      id: 'stub',
      run: async (_text, context) => {
        context.append(
          textEntry({ role: 'assistant', content: 'done', streaming: false })
        )
      }
    }
    controller.registerCapability(stub, { makeDefault: true })

    await controller.send('hello')

    expect(controller.lastOutcome).toBe('complete')
    expect(controller.busy).toBe(false)
    controller.dispose()
  })

  it('clear() nulls lastOutcome so a11y does not announce a stale run', async () => {
    const controller = createAIChatController()
    controller.attach(pluginContextStub())
    const stub: AIChatCapability = {
      id: 'stub',
      run: async () => {
        throw new Error('boom')
      }
    }
    controller.registerCapability(stub, { makeDefault: true })
    await controller.send('hello')
    expect(controller.lastOutcome).toBe('error')

    controller.clear()
    expect(controller.lastOutcome).toBeNull()
    expect(controller.transcript).toEqual([])
    controller.dispose()
  })

  it('promotes the live status line across multi-step tools and staging reject', async () => {
    agentMocks.run.mockImplementationOnce(
      async (_text: string, _history: unknown, opts: AgentRunOptions) => {
        opts.onToolCall?.({ id: 't1', name: 'search_notes', args: {} })
        opts.onToolResult?.({
          id: 't1',
          name: 'search_notes',
          result: { content: 'hits' }
        })
        opts.onToolCall?.({ id: 't2', name: 'read_blocks', args: {} })
        opts.onToolResult?.({
          id: 't2',
          name: 'read_blocks',
          result: { content: 'body' }
        })
        opts.onStaging?.({
          token: 'stg-1',
          preview: {
            kind: 'update_block',
            summary: 'Update note',
            details: '',
            affectedCount: 1
          }
        })
        opts.onStagingOutcome?.('stg-1', 'rejected')
        opts.onToolCall?.({ id: 't3', name: 'list_tags', args: {} })
        opts.onDone?.('all set')
        return { text: 'all set' }
      }
    )

    const controller = createAIChatController()
    controller.attach(pluginContextStub())
    // Default capability is agent-tools (createAgentCapability).
    await controller.send('multi step')

    const statuses = controller.transcript
      .filter((e) => e.kind === 'status')
      .map((e) =>
        e.kind === 'status' ? { status: e.status, message: e.message } : null
      )
    // Final terminal status is done; intermediate promotions must have updated
    // the single live line (not stuck on the first tool's "thinking" only).
    expect(statuses.some((s) => s?.status === 'done')).toBe(true)
    // Confirmation card reflects reject.
    expect(
      controller.transcript.some(
        (e) => e.kind === 'confirmation' && e.state === 'rejected'
      )
    ).toBe(true)
    // At least two tool-call entries were recorded for the multi-step run.
    expect(
      controller.transcript.filter((e) => e.kind === 'tool-call').length
    ).toBeGreaterThanOrEqual(2)
    // Reject path must not leave the line stuck on waiting_confirmation.
    expect(
      controller.transcript.some(
        (e) => e.kind === 'status' && e.status === 'waiting_confirmation'
      )
    ).toBe(false)
    controller.dispose()
  })

  it('send returns false when busy so callers can keep the command', async () => {
    const controller = createAIChatController()
    controller.attach(pluginContextStub())
    let resolveRun!: () => void
    const runPromise = new Promise<void>((r) => {
      resolveRun = r
    })
    const stub: AIChatCapability = {
      id: 'stub',
      run: async () => {
        await runPromise
      }
    }
    controller.registerCapability(stub, { makeDefault: true })

    const first = controller.send('a')
    expect(controller.busy).toBe(true)
    const second = await controller.send('b')
    expect(second).toBe(false)

    resolveRun()
    expect(await first).toBe(true)
    controller.dispose()
  })
})
