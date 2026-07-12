import { describe, expect, it } from 'vitest'
import { buildRAGMessages, NO_RESULTS_MESSAGE, parseCitations } from './rag'
import type { RetrievedPassage } from './types'

const passages: RetrievedPassage[] = [
  {
    blockId: 'b1',
    notebook: 'Work',
    section: 'Notes',
    page: 'Decisions',
    lineNumber: 1,
    text: 'We chose Postgres.',
    score: 1,
    citeIndex: 1
  },
  {
    blockId: 'b2',
    notebook: 'Work',
    section: 'Notes',
    page: 'Decisions',
    lineNumber: 5,
    text: 'Billing migrates in Q3.',
    score: 0.8,
    citeIndex: 2
  }
]

describe('buildRAGMessages', () => {
  it('includes system prompt, passages, and question', () => {
    const msgs = buildRAGMessages('What about billing?', passages)
    expect(msgs[0].role).toBe('system')
    const user = msgs[msgs.length - 1]
    expect(user.role).toBe('user')
    expect(user.content).toContain('[1]')
    expect(user.content).toContain('We chose Postgres.')
    expect(user.content).toContain('What about billing?')
  })
})

describe('parseCitations', () => {
  it('maps known markers and drops unknown', () => {
    const cites = parseCitations(
      'Billing is Q3 [2] and DB is Postgres [1] and fake [9].',
      passages
    )
    expect(cites.map((c) => c.index)).toEqual([2, 1])
    expect(cites[0].blockId).toBe('b2')
    expect(cites[1].blockId).toBe('b1')
  })

  it('dedupes repeated markers', () => {
    const cites = parseCitations('See [1] and again [1].', passages)
    expect(cites).toHaveLength(1)
  })
})

describe('no-results message', () => {
  it('is non-empty guidance', () => {
    expect(NO_RESULTS_MESSAGE.length).toBeGreaterThan(20)
  })
})
