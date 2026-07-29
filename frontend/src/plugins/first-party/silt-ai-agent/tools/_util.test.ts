import { describe, expect, it } from 'vitest'
import { breadcrumb, clampInt, isValidYMD } from './_util'

describe('clampInt', () => {
  it('clamps into range and floors non-integers', () => {
    expect(clampInt(5, 1, 1, 3)).toBe(3)
    expect(clampInt(0, 1, 1, 3)).toBe(1)
    expect(clampInt(2.9, 1, 1, 3)).toBe(2)
    expect(clampInt(undefined, 2, 1, 3)).toBe(2)
    expect(clampInt(NaN, 2, 1, 3)).toBe(2)
  })
})

describe('breadcrumb', () => {
  it('joins non-empty segments with " > "', () => {
    expect(breadcrumb('Work', 'Sprint', 'Plan')).toBe('Work > Sprint > Plan')
    expect(breadcrumb('Work', '', 'Diary')).toBe('Work > Diary')
  })
})

describe('isValidYMD', () => {
  it('accepts well-formed real calendar dates', () => {
    expect(isValidYMD('2026-07-29')).toBe(true)
    expect(isValidYMD('2024-02-29')).toBe(true) // leap year
    expect(isValidYMD('1999-12-31')).toBe(true)
  })

  it('rejects non YYYY-MM-DD shapes', () => {
    expect(isValidYMD('Aug 1')).toBe(false)
    expect(isValidYMD('2026/07/29')).toBe(false)
    expect(isValidYMD('2026-7-9')).toBe(false) // no zero-padding
    expect(isValidYMD('')).toBe(false)
  })

  it('rejects well-formed but impossible calendar dates', () => {
    expect(isValidYMD('2026-13-01')).toBe(false) // month overflow
    expect(isValidYMD('2026-00-10')).toBe(false) // month 0
    expect(isValidYMD('2026-04-31')).toBe(false) // Apr has 30 days
    expect(isValidYMD('2026-02-30')).toBe(false) // Feb overflow
    expect(isValidYMD('2023-02-29')).toBe(false) // not a leap year
    expect(isValidYMD('2026-01-00')).toBe(false) // day 0
    expect(isValidYMD('2026-01-32')).toBe(false) // day overflow
  })
})
