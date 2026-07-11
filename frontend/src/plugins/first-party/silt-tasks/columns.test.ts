// Unit tests for BoardColumn normalize/equal helpers (#437).
import { describe, expect, it } from 'vitest'
import {
  DEFAULT_COLUMNS,
  cloneColumns,
  columnNames,
  columnsEqual,
  normalizeColumns
} from './columns'

describe('normalizeColumns (#437)', () => {
  it('returns default TODO/DOING/DONE unlimited when input is empty/invalid', () => {
    expect(normalizeColumns(undefined)).toEqual(DEFAULT_COLUMNS)
    expect(normalizeColumns(null)).toEqual(DEFAULT_COLUMNS)
    expect(normalizeColumns('TODO')).toEqual(DEFAULT_COLUMNS)
    expect(normalizeColumns([])).toEqual(DEFAULT_COLUMNS)
    expect(normalizeColumns([1, 2, 3])).toEqual(DEFAULT_COLUMNS)
  })

  it('accepts legacy string[]', () => {
    expect(normalizeColumns(['Backlog', 'TODO', 'DONE'])).toEqual([
      { name: 'Backlog' },
      { name: 'TODO' },
      { name: 'DONE' }
    ])
  })

  it('accepts structured {name, wipLimit?}[]', () => {
    expect(
      normalizeColumns([
        { name: 'TODO', wipLimit: 3 },
        { name: 'DOING' },
        { name: 'DONE', wipLimit: null }
      ])
    ).toEqual([
      { name: 'TODO', wipLimit: 3 },
      { name: 'DOING' },
      { name: 'DONE', wipLimit: null }
    ])
  })

  it('mixes legacy strings and structured objects', () => {
    expect(
      normalizeColumns(['TODO', { name: 'DOING', wipLimit: 2 }, 'DONE'])
    ).toEqual([
      { name: 'TODO' },
      { name: 'DOING', wipLimit: 2 },
      { name: 'DONE' }
    ])
  })

  it('floors wipLimit and rejects values < 1', () => {
    expect(
      normalizeColumns([
        { name: 'A', wipLimit: 2.9 },
        { name: 'B', wipLimit: 0 },
        { name: 'C', wipLimit: -1 },
        { name: 'D', wipLimit: NaN }
      ])
    ).toEqual([
      { name: 'A', wipLimit: 2 },
      { name: 'B' },
      { name: 'C' },
      { name: 'D' }
    ])
  })

  it('trims names and skips empty/invalid entries', () => {
    expect(
      normalizeColumns(['  TODO  ', '', { name: '  ' }, { name: 'DOING' }])
    ).toEqual([{ name: 'TODO' }, { name: 'DOING' }])
  })

  it('caps at 50 entries', () => {
    const many = Array.from({ length: 60 }, (_, i) => `COL${i}`)
    const cols = normalizeColumns(many)
    expect(cols).toHaveLength(50)
    expect(cols[0].name).toBe('COL0')
    expect(cols[49].name).toBe('COL49')
  })

  it('returns independent copies (caller mutation safe)', () => {
    const a = normalizeColumns(['TODO'])
    a.push({ name: 'X' })
    a[0].name = 'mutated'
    expect(normalizeColumns(['TODO'])).toEqual([{ name: 'TODO' }])
  })
})

describe('columnNames / columnsEqual / cloneColumns (#437)', () => {
  it('columnNames extracts names in order', () => {
    expect(
      columnNames([{ name: 'TODO', wipLimit: 1 }, { name: 'DOING' }])
    ).toEqual(['TODO', 'DOING'])
  })

  it('columnsEqual compares name + wipLimit (null ≈ undefined)', () => {
    expect(
      columnsEqual(
        [{ name: 'TODO' }, { name: 'DOING', wipLimit: 2 }],
        [
          { name: 'TODO', wipLimit: null },
          { name: 'DOING', wipLimit: 2 }
        ]
      )
    ).toBe(true)
    expect(
      columnsEqual(
        [{ name: 'TODO', wipLimit: 1 }],
        [{ name: 'TODO', wipLimit: 2 }]
      )
    ).toBe(false)
    expect(columnsEqual([{ name: 'TODO' }], [{ name: 'DOING' }])).toBe(false)
    expect(
      columnsEqual([{ name: 'TODO' }], [{ name: 'TODO' }, { name: 'X' }])
    ).toBe(false)
  })

  it('cloneColumns deep-clones entries', () => {
    const src = [{ name: 'TODO', wipLimit: 3 }]
    const copy = cloneColumns(src)
    copy[0].name = 'X'
    copy[0].wipLimit = 9
    expect(src[0]).toEqual({ name: 'TODO', wipLimit: 3 })
  })
})
