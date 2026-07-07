import { describe, expect, it } from 'vitest'
import { diffFacets, newItems, normalizeItem, EMPTY_EXTRACTION } from './diff'

describe('normalizeItem', () => {
  it('lowercases, trims, and collapses internal whitespace', () => {
    expect(normalizeItem('  Ship   the  API  ')).toBe('ship the api')
  })
  it('null-coerces to empty string', () => {
    expect(normalizeItem(undefined as unknown as string)).toBe('')
  })
})

describe('newItems', () => {
  it('returns original wording of items absent from prior', () => {
    const prior = ['ship the api', 'hire a designer']
    const current = ['Ship the API', 'Write the brief', 'Hire a designer']
    expect(newItems(current, prior)).toEqual(['Write the brief'])
  })
  it('treats an empty prior as everything-new', () => {
    expect(newItems(['a', 'b'], [])).toEqual(['a', 'b'])
  })
  it('dedupes current duplicates by normalized key (first wins)', () => {
    expect(newItems(['Draft spec', 'draft  spec'], [])).toEqual(['Draft spec'])
  })
  it('skips empty/whitespace items', () => {
    expect(newItems(['real', '   ', ''], [])).toEqual(['real'])
  })
  it('returns empty when nothing is new', () => {
    expect(newItems(['a', 'b'], ['a', 'b', 'c'])).toEqual([])
  })
})

describe('diffFacets', () => {
  it('diffs every facet independently', () => {
    const current = {
      summary: 's',
      tasks: ['New task', 'Old task'],
      risks: ['Old risk'],
      decisions: ['New decision']
    }
    const prior = {
      summary: '',
      tasks: ['Old task'],
      risks: ['Old risk'],
      decisions: []
    }
    expect(diffFacets(current, prior)).toEqual({
      tasks: ['New task'],
      risks: [],
      decisions: ['New decision']
    })
  })
  it('empty prior flags everything as new', () => {
    const cur = { summary: 's', tasks: ['t'], risks: ['r'], decisions: ['d'] }
    expect(diffFacets(cur, EMPTY_EXTRACTION)).toEqual({
      tasks: ['t'],
      risks: ['r'],
      decisions: ['d']
    })
  })
})
