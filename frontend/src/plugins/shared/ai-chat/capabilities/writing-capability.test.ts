import { describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import type { AIChatCapabilityContext } from '../ai-chat-controller.svelte'
import { createProposal } from '../../../first-party/silt-ai-assistant/proposal/model'
import type { AIChatEntry, ProposalEntry } from '../types'

const mocks = vi.hoisted(() => ({
  runWritingAction: vi.fn(),
  applyProposal: vi.fn()
}))

vi.mock('../../../first-party/silt-ai-assistant/actions/writing', () => ({
  runWritingAction: mocks.runWritingAction
}))
vi.mock('../../../first-party/silt-ai-assistant/proposal/apply', () => ({
  applyProposal: mocks.applyProposal
}))

import {
  createWritingCapability,
  parseWritingCommand
} from './writing-capability'

function makeContext(selectionText = 'selected text') {
  const entries: AIChatEntry[] = []
  const pluginContext = {
    activeNotebook: 'Work',
    activeSection: 'Notes',
    activePage: 'Plan',
    sqliteQuery: vi.fn(async () => ({
      rows: [{ id: 'block-1', clean_content: selectionText, type: 'NOTE' }],
      truncated: false
    }))
  } as unknown as PluginContext
  const context = {
    pluginContext,
    request: { selectionText },
    get transcript() {
      return entries
    },
    append(entry: AIChatEntry) {
      entries.push(entry)
    },
    update(id: string, updater: (entry: AIChatEntry) => AIChatEntry) {
      const index = entries.findIndex((entry) => entry.id === id)
      if (index >= 0) entries[index] = updater(entries[index])
    },
    remove(id: string) {
      const index = entries.findIndex((entry) => entry.id === id)
      if (index >= 0) entries.splice(index, 1)
    }
  } satisfies AIChatCapabilityContext
  return { context, entries, pluginContext }
}

function readyProposal(scope: any) {
  return createProposal({
    actionId: 'improve-clarity',
    kind: 'replace-selection',
    scope,
    proposedMarkdown: 'clearer text',
    status: 'ready'
  })
}

describe('writing capability', () => {
  it('routes only explicit slash writing commands', () => {
    expect(parseWritingCommand('/rewrite-succinct')).toEqual({
      actionId: 'rewrite-succinct',
      instruction: undefined
    })
    expect(parseWritingCommand('/draft-expand a launch outline')).toEqual({
      actionId: 'draft-expand',
      instruction: 'a launch outline'
    })
    expect(parseWritingCommand('rewrite this')).toBeNull()
  })

  it('forwards selection scope and creates a proposal', async () => {
    mocks.runWritingAction.mockReset()
    const { context, entries } = makeContext()
    mocks.runWritingAction.mockImplementation(async (_ctx, _action, scope) =>
      readyProposal(scope)
    )

    const capability = createWritingCapability()
    capability.attach?.(context.pluginContext)
    await capability.run('/improve-clarity', context)

    expect(mocks.runWritingAction).toHaveBeenCalledWith(
      context.pluginContext,
      'improve-clarity',
      expect.objectContaining({
        selectionText: 'selected text',
        targetBlockId: 'block-1',
        inputText: 'selected text'
      }),
      expect.anything(),
      expect.anything()
    )
    expect(entries.find((entry) => entry.kind === 'proposal')).toBeTruthy()
  })

  it('applies an accepted proposal exactly once and never applies discard', async () => {
    mocks.runWritingAction.mockReset()
    mocks.applyProposal.mockReset()
    mocks.applyProposal.mockResolvedValue({ ok: true })
    const first = makeContext()
    mocks.runWritingAction.mockImplementation(async (_ctx, _action, scope) =>
      readyProposal(scope)
    )
    const capability = createWritingCapability()
    capability.attach?.(first.pluginContext)
    await capability.run('/improve-clarity', first.context)
    const accepted = first.entries.find(
      (entry): entry is ProposalEntry => entry.kind === 'proposal'
    )!

    await Promise.all([
      capability.acceptProposal!(accepted),
      capability.acceptProposal!(accepted)
    ])
    expect(mocks.applyProposal).toHaveBeenCalledOnce()

    const second = makeContext()
    await capability.run('/improve-clarity', second.context)
    const discarded = second.entries.find(
      (entry): entry is ProposalEntry => entry.kind === 'proposal'
    )!
    capability.discardProposal?.(discarded)
    expect(mocks.applyProposal).toHaveBeenCalledOnce()
  })
})
