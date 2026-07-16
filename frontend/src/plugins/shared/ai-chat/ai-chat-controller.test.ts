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
vi.mock('../../first-party/silt-ai-agent/agent-loop', () => ({
  createAgentSession: () => ({
    run: vi.fn(),
    cancel: vi.fn(),
    resolveStaging: vi.fn()
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
})
