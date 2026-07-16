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
import { proposalEntry, type ProposalEntry } from './types'

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
})
