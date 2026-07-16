import { describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../sdk'
import type { AIChatCapabilityContext } from './ai-chat-controller.svelte'
import type { AIChatEntry } from './types'

const mocks = vi.hoisted(() => ({
  createAgentSession: vi.fn()
}))

vi.mock('../../first-party/silt-ai-agent/agent-loop', () => ({
  createAgentSession: mocks.createAgentSession
}))

import { createAgentCapability } from './ai-chat-controller.svelte'

function makeContext() {
  const entries: AIChatEntry[] = []
  const context = {
    pluginContext: { ai: {} } as unknown as PluginContext,
    request: {},
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
  return { context, entries }
}

describe('agent evidence bridge', () => {
  it('emits navigable evidence with citation indices used by the answer', async () => {
    mocks.createAgentSession.mockReturnValue({
      cancel: vi.fn(),
      resolveStaging: vi.fn(),
      run: async (
        _text: string,
        _history: unknown[],
        options: {
          onToolResult?: (value: any) => void
          onDone?: (text: string) => void
        }
      ) => {
        options.onToolResult?.({
          id: 'call-1',
          name: 'search_notes',
          result: {
            content: '[1] block block-1',
            evidence: [
              {
                citationIndex: 1,
                blockId: 'block-1',
                notebook: 'Work',
                section: 'Notes',
                page: 'Plan',
                snippet: 'Ship in August',
                title: 'Work > Notes > Plan'
              }
            ]
          }
        })
        options.onDone?.('The launch is in August [1].')
        return {
          text: 'The launch is in August [1].',
          iterations: 2,
          cancelled: false,
          hitIterationCap: false
        }
      }
    })

    const { context, entries } = makeContext()
    const capability = createAgentCapability()
    capability.attach?.(context.pluginContext)
    await capability.run('When is launch?', context)

    const evidence = entries.filter((entry) => entry.kind === 'evidence')
    expect(evidence).toHaveLength(1)
    expect(evidence[0]).toMatchObject({
      citationIndex: 1,
      target: {
        blockId: 'block-1',
        notebook: 'Work',
        section: 'Notes',
        page: 'Plan'
      },
      excerpt: 'Ship in August'
    })
    expect(
      entries.find(
        (entry) => entry.kind === 'text' && entry.content.includes('[1]')
      )
    ).toBeTruthy()
  })
})
