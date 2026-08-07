import { describe, expect, it } from 'vitest'
import {
  evidenceGroupSummaryLabel,
  filterTranscriptForBusyDisplay,
  groupTranscript,
  toolActivitySummaryLabel
} from './groupTranscript'
import {
  evidenceEntry,
  textEntry,
  toolCallEntry,
  toolResultEntry
} from './types'

describe('groupTranscript', () => {
  it('groups adjacent tool entries and leaves text alone', () => {
    const transcript = [
      textEntry({ id: 'u', role: 'user', content: 'hi' }),
      toolCallEntry({
        id: 'c1',
        role: 'assistant',
        toolCallId: 't1',
        toolName: 'a',
        args: {}
      }),
      toolResultEntry({
        id: 'r1',
        role: 'system',
        toolCallId: 't1',
        toolName: 'a',
        output: 'ok'
      }),
      textEntry({ id: 'a', role: 'assistant', content: 'done' })
    ]
    const segs = groupTranscript(transcript)
    expect(segs).toHaveLength(3)
    expect(segs[0]).toMatchObject({ kind: 'entry' })
    expect(segs[1]).toMatchObject({
      kind: 'tool-activity',
      callCount: 1,
      resultCount: 1
    })
    expect(segs[2]).toMatchObject({ kind: 'entry' })
  })

  it('returns empty for empty transcript', () => {
    expect(groupTranscript([])).toEqual([])
  })

  it('keeps one activity group when evidence sits between call and result', () => {
    const transcript = [
      textEntry({ id: 'u', role: 'user', content: 'search' }),
      toolCallEntry({
        id: 'c1',
        role: 'assistant',
        toolCallId: 't1',
        toolName: 'search_notes',
        args: { q: 'x' }
      }),
      evidenceEntry({
        id: 'e1',
        role: 'assistant',
        citationIndex: 1,
        title: 'Hit',
        target: { blockId: 'b1' }
      }),
      toolResultEntry({
        id: 'r1',
        role: 'system',
        toolCallId: 't1',
        toolName: 'search_notes',
        output: 'ok'
      }),
      textEntry({ id: 'a', role: 'assistant', content: 'done' })
    ]
    const segs = groupTranscript(transcript)
    // Single evidence stays a lone card (not a group).
    expect(segs.map((s) => s.kind)).toEqual([
      'entry',
      'tool-activity',
      'entry',
      'entry'
    ])
    expect(segs[1]).toMatchObject({
      kind: 'tool-activity',
      callCount: 1,
      resultCount: 1
    })
    if (segs[1].kind === 'tool-activity') {
      expect(segs[1].items.map((i) => i.id)).toEqual(['c1', 'r1'])
    }
    expect(segs[2]).toMatchObject({ kind: 'entry' })
    if (segs[2].kind === 'entry') {
      expect(segs[2].entry.kind).toBe('evidence')
    }
  })

  it('collapses multi-source evidence into one evidence-group (#915)', () => {
    const evidence = Array.from({ length: 10 }, (_, i) =>
      evidenceEntry({
        id: `e${i + 1}`,
        role: 'assistant',
        citationIndex: i + 1,
        title: `Hit ${i + 1}`,
        target: { blockId: `b${i + 1}` }
      })
    )
    const transcript = [
      textEntry({ id: 'u', role: 'user', content: 'search' }),
      toolCallEntry({
        id: 'c1',
        role: 'assistant',
        toolCallId: 't1',
        toolName: 'search_notes',
        args: { q: 'x' }
      }),
      ...evidence,
      toolResultEntry({
        id: 'r1',
        role: 'system',
        toolCallId: 't1',
        toolName: 'search_notes',
        output: 'ok'
      })
    ]
    const segs = groupTranscript(transcript)
    expect(segs.map((s) => s.kind)).toEqual([
      'entry',
      'tool-activity',
      'evidence-group'
    ])
    if (segs[2].kind === 'evidence-group') {
      expect(segs[2].items).toHaveLength(10)
      expect(segs[2].id).toBe('evidence-group-e1')
    }
  })

  it('groups consecutive standalone evidence when count >= 2', () => {
    const segs = groupTranscript([
      evidenceEntry({
        id: 'e1',
        role: 'assistant',
        citationIndex: 1,
        title: 'A',
        target: { blockId: 'a' }
      }),
      evidenceEntry({
        id: 'e2',
        role: 'assistant',
        citationIndex: 2,
        title: 'B',
        target: { blockId: 'b' }
      })
    ])
    expect(segs).toHaveLength(1)
    expect(segs[0]).toMatchObject({ kind: 'evidence-group' })
  })
})

describe('toolActivitySummaryLabel', () => {
  it('formats counts', () => {
    expect(toolActivitySummaryLabel(1, 1)).toBe(
      'Tool activity · 1 tool call, 1 result'
    )
    expect(toolActivitySummaryLabel(2, 0)).toBe('Tool activity · 2 tool calls')
  })
})

describe('evidenceGroupSummaryLabel', () => {
  it('formats source counts', () => {
    expect(evidenceGroupSummaryLabel(1)).toBe('1 source')
    expect(evidenceGroupSummaryLabel(10)).toBe('10 sources')
  })
})

describe('filterTranscriptForBusyDisplay', () => {
  it('keeps prior-turn tools while hiding current-turn tools when busy', () => {
    const transcript = [
      textEntry({ id: 'u1', role: 'user', content: 'first' }),
      toolCallEntry({
        id: 'c1',
        role: 'assistant',
        toolCallId: 't1',
        toolName: 'old',
        args: {}
      }),
      toolResultEntry({
        id: 'r1',
        role: 'system',
        toolCallId: 't1',
        toolName: 'old',
        output: 'ok'
      }),
      textEntry({ id: 'a1', role: 'assistant', content: 'done1' }),
      textEntry({ id: 'u2', role: 'user', content: 'second' }),
      toolCallEntry({
        id: 'c2',
        role: 'assistant',
        toolCallId: 't2',
        toolName: 'new',
        args: {}
      }),
      toolResultEntry({
        id: 'r2',
        role: 'system',
        toolCallId: 't2',
        toolName: 'new',
        output: 'ok2'
      })
    ]
    const filtered = filterTranscriptForBusyDisplay(transcript, true)
    expect(filtered.map((e) => e.id)).toEqual(['u1', 'c1', 'r1', 'a1', 'u2'])
    expect(filterTranscriptForBusyDisplay(transcript, false)).toEqual(
      transcript
    )
  })
})
