import { describe, expect, it } from 'vitest'
import {
  confirmationEntry,
  evidenceEntry,
  proposalEntry,
  statusEntry,
  textEntry,
  toolCallEntry,
  toolResultEntry,
  type AIChatEntry
} from './types'

describe('AI chat transcript entries', () => {
  it('constructs every discriminated result with identity metadata', () => {
    const createdAt = 123
    const entries: AIChatEntry[] = [
      textEntry({ id: 'text', createdAt, role: 'user', content: 'Hello' }),
      evidenceEntry({
        id: 'evidence',
        createdAt,
        role: 'assistant',
        citationIndex: 1,
        title: 'Plan',
        target: { blockId: 'b1' }
      }),
      toolCallEntry({
        id: 'call',
        createdAt,
        role: 'assistant',
        toolCallId: 'tc1',
        toolName: 'search_notes',
        args: { query: 'plan' }
      }),
      toolResultEntry({
        id: 'result',
        createdAt,
        role: 'system',
        toolCallId: 'tc1',
        toolName: 'search_notes',
        output: 'Found one note'
      }),
      proposalEntry({
        id: 'proposal',
        createdAt,
        role: 'assistant',
        title: 'Tighten introduction',
        content: 'A clearer opening.'
      }),
      confirmationEntry({
        id: 'confirmation',
        createdAt,
        role: 'system',
        token: 'token',
        operation: 'delete blocks',
        summary: 'Delete two blocks'
      }),
      statusEntry({
        id: 'status',
        createdAt,
        role: 'system',
        status: 'thinking',
        message: 'Thinking…'
      })
    ]

    expect(entries.map((entry) => entry.kind)).toEqual([
      'text',
      'evidence',
      'tool-call',
      'tool-result',
      'proposal',
      'confirmation',
      'status'
    ])
    expect(entries.every((entry) => entry.createdAt === createdAt)).toBe(true)
  })

  it('narrows payloads from the kind discriminant', () => {
    const describeEntry = (entry: AIChatEntry): string => {
      switch (entry.kind) {
        case 'text':
          return entry.content
        case 'evidence':
          return entry.target.blockId
        case 'tool-call':
          return entry.toolName
        case 'tool-result':
          return entry.output
        case 'proposal':
          return entry.title
        case 'confirmation':
          return entry.token
        case 'status':
          return entry.status
      }
    }

    expect(
      describeEntry(textEntry({ role: 'assistant', content: 'Answer' }))
    ).toBe('Answer')
  })
})
