import { describe, it, expect } from 'vitest'
import { friendlyPackError } from './dictionaryStatus.svelte'

describe('friendlyPackError', () => {
  it('maps network failures', () => {
    expect(friendlyPackError(new Error('network timeout'))).toMatch(/network/i)
  })
  it('maps vault-not-loaded', () => {
    expect(friendlyPackError(new Error('vault not loaded'))).toMatch(/vault/i)
  })
  it('maps size limits', () => {
    expect(friendlyPackError(new Error('exceeds 2 byte limit'))).toMatch(
      /too large/i
    )
  })
  it('passes through short messages', () => {
    expect(friendlyPackError(new Error('boom'))).toBe('boom')
  })
})
