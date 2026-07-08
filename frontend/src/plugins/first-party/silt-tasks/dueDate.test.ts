import { describe, expect, it } from 'vitest'
import { dueDateClass, dueDateTextClass } from './dueDate'

describe('dueDateClass', () => {
  const today = '2026-07-07'

  it('classifies an earlier date as overdue', () => {
    expect(dueDateClass('2026-07-06', today)).toBe('overdue')
    expect(dueDateClass('2025-12-31', today)).toBe('overdue')
  })

  it('classifies today as today (not overdue)', () => {
    expect(dueDateClass(today, today)).toBe('today')
  })

  it('classifies a later date as upcoming', () => {
    expect(dueDateClass('2026-07-08', today)).toBe('upcoming')
    expect(dueDateClass('2027-01-01', today)).toBe('upcoming')
  })

  it('treats null / empty / whitespace-only as none', () => {
    expect(dueDateClass(null, today)).toBe('none')
    expect(dueDateClass('', today)).toBe('none')
    expect(dueDateClass('   ', today)).toBe('none')
    expect(dueDateClass(undefined, today)).toBe('none')
  })

  it('uses pure lexicographic YYYY-MM-DD comparison (no timezone drift)', () => {
    // The whole point of date-only string compare: a date "before" today in
    // calendar terms is lexicographically smaller, regardless of tz offset.
    expect(dueDateClass('2026-07-06', '2026-07-07')).toBe('overdue')
    expect(dueDateClass('2026-07-07', '2026-07-06')).toBe('upcoming')
  })
})

describe('dueDateTextClass', () => {
  it('maps overdue → error tone', () => {
    expect(dueDateTextClass('overdue')).toBe('text-error')
  })

  it('maps today → accent tone', () => {
    expect(dueDateTextClass('today')).toBe('text-accent-primary-start')
  })

  it('maps upcoming and none → muted tone', () => {
    expect(dueDateTextClass('upcoming')).toBe('text-text-muted')
    expect(dueDateTextClass('none')).toBe('text-text-muted')
  })
})
