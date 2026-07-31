import { describe, expect, it } from 'vitest'
import {
  filterTranscriptForBusyDisplay,
  groupTranscript,
  toolActivitySummaryLabel
} from './groupTranscript'
import { textEntry, toolCallEntry, toolResultEntry } from './types'

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
})

describe('toolActivitySummaryLabel', () => {
  it('formats counts', () => {
    expect(toolActivitySummaryLabel(1, 1)).toBe(
      'Tool activity · 1 tool call, 1 result'
    )
    expect(toolActivitySummaryLabel(2, 0)).toBe('Tool activity · 2 tool calls')
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
