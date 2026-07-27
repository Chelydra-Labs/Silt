import { describe, expect, it } from 'vitest'
import { formatAIError, isAbortError } from './formatAIError'
import { AIErrorKind, AIErrorKindNames } from '../../generated/enums'

describe('formatAIError', () => {
  it('formats PluginAIError codes without [object Object]', () => {
    expect(
      formatAIError({ code: AIErrorKind.ErrUnreachable, message: 'dial tcp' })
    ).toMatch(/Could not reach/i)
    expect(
      formatAIError({ code: AIErrorKind.ErrTimeout, message: 'deadline' })
    ).toMatch(/timed out/i)
    expect(
      formatAIError({
        code: AIErrorKind.ErrUnknown,
        message: 'provider said no'
      })
    ).toBe('provider said no')
  })

  it('never returns [object Object] for plain objects', () => {
    const out = formatAIError({ code: AIErrorKind.ErrServer, message: '' })
    expect(out).not.toContain('[object Object]')
    expect(out.length).toBeGreaterThan(0)
  })

  it('handles Error and AbortError', () => {
    expect(formatAIError(new Error('boom'))).toBe('boom')
    const abort = new Error('aborted')
    abort.name = 'AbortError'
    expect(formatAIError(abort)).toBe('Cancelled.')
  })

  it('maps Error instances with PluginAIError codes to friendly copy', () => {
    const rateLimited = Object.assign(new Error('rl'), {
      name: 'PluginAIError',
      code: AIErrorKind.ErrRateLimited
    })
    expect(formatAIError(rateLimited)).toMatch(/rate limit/i)

    const unauthorized = Object.assign(new Error('bad key'), {
      name: 'PluginAIError',
      code: AIErrorKind.ErrUnauthorized
    })
    expect(formatAIError(unauthorized)).toMatch(/API key/i)
  })
})

describe('isAbortError', () => {
  it('detects AbortError', () => {
    const e = new Error('x')
    e.name = 'AbortError'
    expect(isAbortError(e)).toBe(true)
    expect(isAbortError(new Error('x'))).toBe(false)
  })
})

// Guards #760: when a new AIErrorKind is added to the Go enum, cmd/genenums
// regenerates AIErrorKindNames and the partition test below fails until the new
// kind is either given a dedicated friendly phrase (a case in messageForCode +
// listed in `mapped`) or explicitly accepted as generic (listed in
// `intentionallyGeneric`). The non-empty check then confirms no branch throws.
describe('formatAIError exhaustiveness over the Go AIErrorKind enum', () => {
  // Kinds with a dedicated friendly phrase in messageForCode. Keep in sync with
  // the switch in formatAIError.ts — the partition test below flags drift in
  // both directions (a mapped kind dropped from messageForCode, or a new kind
  // added without a decision).
  const mapped = new Set<string>([
    'ErrUnauthorized',
    'ErrRateLimited',
    'ErrModelMissing',
    'ErrTimeout',
    'ErrUnreachable',
    'ErrForbidden',
    'ErrBadRequest',
    'ErrServer'
  ])
  // Kinds that intentionally fall through to the generic message path: they
  // surface the underlying message (or "The AI request failed.") rather than a
  // provider-specific phrase. Listed explicitly so a new kind can't join them
  // silently.
  const intentionallyGeneric = new Set<string>(['ErrCanceled', 'ErrUnknown'])

  it('partitions every declared kind as mapped or intentionally generic', () => {
    const declared = new Set<string>(AIErrorKindNames)
    for (const name of declared) {
      expect(
        mapped.has(name) || intentionallyGeneric.has(name),
        `${name} was added to the Go AIErrorKind enum but is neither mapped ` +
          `nor allowlisted. Add a case to messageForCode in formatAIError.ts ` +
          `and list it in \`mapped\` here, or add it to \`intentionallyGeneric\` ` +
          `if the generic message path is intended.`
      ).toBe(true)
    }
    // Stale entries: a kind dropped from the Go enum must not linger in a set.
    for (const name of mapped) {
      expect(
        declared.has(name),
        `\`mapped\` lists ${name}, which is no longer in AIErrorKindNames`
      ).toBe(true)
    }
    for (const name of intentionallyGeneric) {
      expect(
        declared.has(name),
        `\`intentionallyGeneric\` lists ${name}, which is no longer in AIErrorKindNames`
      ).toBe(true)
    }
    // No kind may belong to both sets.
    for (const name of mapped) {
      expect(
        intentionallyGeneric.has(name),
        `${name} appears in both \`mapped\` and \`intentionallyGeneric\``
      ).toBe(false)
    }
  })

  it('produces a non-empty message for every declared kind', () => {
    const values = AIErrorKind as Record<string, string>
    for (const name of AIErrorKindNames) {
      const code = values[name]
      const out = formatAIError({ code, message: `probe ${code}` })
      expect(out.length, `kind ${name} (${code})`).toBeGreaterThan(0)
    }
  })
})
