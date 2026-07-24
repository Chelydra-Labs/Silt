import { describe, expect, it } from 'vitest'
import {
  formatDate,
  resolveDateFormat,
  DATE_FORMATS,
  DATE_FORMAT_IDS
} from './dateFormat'

// July 24, 2026 is a Friday.
const d = new Date(2026, 6, 24)
// Jan 5, 2027 (single-digit day, different year).
const d2 = new Date(2027, 0, 5)

describe('formatDate', () => {
  it('formats YYYY-MM-DD (ISO)', () => {
    expect(formatDate(d, 'YYYY-MM-DD')).toBe('2026-07-24')
  })
  it('formats DD-MMM-YY', () => {
    expect(formatDate(d, 'DD-MMM-YY')).toBe('24-Jul-26')
  })
  it('formats MM/DD/YYYY (US)', () => {
    expect(formatDate(d, 'MM/DD/YYYY')).toBe('07/24/2026')
  })
  it('formats DD/MM/YYYY (EU)', () => {
    expect(formatDate(d, 'DD/MM/YYYY')).toBe('24/07/2026')
  })
  it('formats MMM D, YYYY', () => {
    expect(formatDate(d, 'MMM D, YYYY')).toBe('Jul 24, 2026')
  })
  it('formats long weekday', () => {
    expect(formatDate(d, 'long')).toBe('Friday, July 24, 2026')
  })
  it('formats D MMM YYYY', () => {
    expect(formatDate(d, 'D MMM YYYY')).toBe('24 Jul 2026')
  })
  it('formats MM/DD/YY (US 2-digit year)', () => {
    expect(formatDate(d, 'MM/DD/YY')).toBe('07/24/26')
  })
  it('formats DD/MM/YY (EU 2-digit year)', () => {
    expect(formatDate(d, 'DD/MM/YY')).toBe('24/07/26')
  })

  it('pads single-digit days/months where the format requires it', () => {
    expect(formatDate(d2, 'YYYY-MM-DD')).toBe('2027-01-05')
    expect(formatDate(d2, 'MM/DD/YYYY')).toBe('01/05/2027')
    expect(formatDate(d2, 'MMM D, YYYY')).toBe('Jan 5, 2027')
  })

  it('falls back to ISO for an unknown format string', () => {
    expect(formatDate(d, 'garbage')).toBe('2026-07-24')
    expect(formatDate(d, '')).toBe('2026-07-24')
  })

  it('every DATE_FORMATS id produces a non-empty string', () => {
    for (const fmt of DATE_FORMATS) {
      const out = formatDate(d, fmt.id)
      expect(out.length).toBeGreaterThan(0)
      expect(out).not.toContain('undefined')
      expect(out).not.toContain('NaN')
    }
  })
})

describe('resolveDateFormat', () => {
  it('returns the configured format when valid', () => {
    expect(resolveDateFormat('DD-MMM-YY')).toBe('DD-MMM-YY')
    expect(resolveDateFormat('long')).toBe('long')
  })
  it('falls back to ISO for nil/empty/unknown', () => {
    expect(resolveDateFormat(undefined)).toBe('YYYY-MM-DD')
    expect(resolveDateFormat(null)).toBe('YYYY-MM-DD')
    expect(resolveDateFormat('')).toBe('YYYY-MM-DD')
    expect(resolveDateFormat('totally-made-up')).toBe('YYYY-MM-DD')
  })
  it('every DATE_FORMAT_IDS entry resolves to itself', () => {
    for (const id of DATE_FORMAT_IDS) {
      expect(resolveDateFormat(id)).toBe(id)
    }
  })
})
