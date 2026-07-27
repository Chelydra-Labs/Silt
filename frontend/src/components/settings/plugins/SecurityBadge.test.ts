/**
 * Isolated unit tests for SecurityBadge (#765). Pure presentational component;
 * no IPC, so no binding mocks. Covers the render guard (empty → nothing) and
 * the accessible name built from denial/rate-limit counts.
 */
import { describe, it, expect, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/svelte'
import SecurityBadge from './SecurityBadge.svelte'
import type { SecurityStats } from './types'

const stats = (over: Partial<SecurityStats> = {}): SecurityStats => ({
  pluginId: 'p',
  denials: 0,
  rateLimited: 0,
  ...over
})

describe('SecurityBadge', () => {
  afterEach(() => cleanup())

  it('renders a status badge with the denial count when denials > 0', () => {
    render(SecurityBadge, { stats: stats({ denials: 3 }) })

    const badge = screen.getByRole('status', {
      name: /3 capability denials/i
    })
    expect(badge).toBeTruthy()
    expect(badge.textContent).toMatch(/3 denied/i)
  })

  it('renders the rate-limit count when only rate-limiting occurred', () => {
    render(SecurityBadge, { stats: stats({ rateLimited: 2 }) })

    const badge = screen.getByRole('status', {
      name: /2 rate-limit hits/i
    })
    expect(badge).toBeTruthy()
    expect(badge.textContent).toMatch(/2 limited/i)
  })

  it('shows both counts when denials and rate-limits are non-zero', () => {
    render(SecurityBadge, {
      stats: stats({ denials: 1, rateLimited: 4 })
    })

    const badge = screen.getByRole('status')
    expect(badge.textContent).toMatch(/1 denied/i)
    expect(badge.textContent).toMatch(/4 limited/i)
  })

  it('renders nothing when stats are undefined', () => {
    render(SecurityBadge, { stats: undefined })
    expect(screen.queryByRole('status')).toBeNull()
  })

  it('renders nothing when both counts are zero', () => {
    render(SecurityBadge, { stats: stats() })
    expect(screen.queryByRole('status')).toBeNull()
  })

  it('includes the last capability in the accessible name when present', () => {
    render(SecurityBadge, {
      stats: stats({ denials: 1, lastCapability: 'network' })
    })
    expect(screen.getByRole('status', { name: /last: network/i })).toBeTruthy()
  })
})
