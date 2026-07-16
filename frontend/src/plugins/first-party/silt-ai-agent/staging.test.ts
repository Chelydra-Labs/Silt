// Phase 5 staging protocol tests (#605).
//
// Mocks the per-plugin SQLite surface (ctx.pluginDb.exec/query/migrate) via a
// small in-memory staging_tokens implementation that honors the real schema
// (used = 0/1, expires_at, plugin_id). This lets us exercise expiry, replay,
// format-validation, and cleanup without spinning up a real SQLite handle.
// The mock captures exec calls and serves query results from the in-memory
// table so SQL semantics (WHERE used = 0, expires_at < ?, etc.) are real.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../sdk'
import {
  cleanupExpired,
  confirmOperation,
  generateToken,
  isValidTokenFormat,
  rejectOperation,
  stageOperation,
  STAGING_TTL_MS,
  StagingError
} from './staging'

interface StagingRow {
  token: string
  plugin_id: string
  operation: string
  created_at: number
  expires_at: number
  used: number
}

/** In-memory mock of pluginDb backed by the real staging_tokens schema. */
function makeCtx(): {
  ctx: PluginContext
  rows: Map<string, StagingRow>
  execCalls: { sql: string; params: unknown[] }[]
  queryCalls: { sql: string; params: unknown[] }[]
} {
  const rows = new Map<string, StagingRow>()
  const execCalls: { sql: string; params: unknown[] }[] = []
  const queryCalls: { sql: string; params: unknown[] }[] = []

  const exec = vi.fn(async (sql: string, params: unknown[] = []) => {
    execCalls.push({ sql, params })
    const upper = sql.trim().toUpperCase()
    if (upper.startsWith('INSERT')) {
      const [token, pluginId, operation, createdAt, expiresAt] = params as [
        string,
        string,
        string,
        number,
        number
      ]
      rows.set(token, {
        token,
        plugin_id: pluginId,
        operation,
        created_at: createdAt,
        expires_at: expiresAt,
        used: 0
      })
      return
    }
    if (upper.startsWith('UPDATE')) {
      const token = String(params[0])
      const row = rows.get(token)
      if (!row) return // 0 rows affected (matches real exec semantics)
      // Real SQL: UPDATE ... SET used = 1 WHERE token = ? [AND used = 0]
      if (upper.includes('USED = 0')) {
        if (row.used !== 0) return // conditional update matches 0 rows
      }
      row.used = 1
      return
    }
    if (upper.startsWith('DELETE')) {
      // DELETE WHERE expires_at < ?
      const cutoff = Number(params[0])
      for (const [tok, r] of rows) {
        if (r.expires_at < cutoff) rows.delete(tok)
      }
      return
    }
  })

  const query = vi.fn(
    async (
      sql: string,
      params: unknown[] = []
    ): Promise<{ rows: Record<string, unknown>[]; truncated: boolean }> => {
      queryCalls.push({ sql, params })
      const upper = sql.trim().toUpperCase()
      if (upper.startsWith('SELECT')) {
        const token = String(params[0])
        const row = rows.get(token)
        if (!row) return { rows: [], truncated: false }
        // Respect the WHERE used = 0 clause if present.
        if (upper.includes('USED = 0') && row.used !== 0) {
          return { rows: [], truncated: false }
        }
        return { rows: [{ ...row }], truncated: false }
      }
      return { rows: [], truncated: false }
    }
  )

  const ctx = {
    pluginDb: { exec, query, migrate: vi.fn(async () => {}) }
  } as unknown as PluginContext
  return { ctx, rows, execCalls, queryCalls }
}

beforeEach(() => {
  vi.useRealTimers()
})
afterEach(() => {
  vi.useRealTimers()
})

describe('generateToken', () => {
  it('produces a 32-char lowercase-hex string', () => {
    const t = generateToken()
    expect(t).toMatch(/^[0-9a-f]{32}$/)
    expect(t.length).toBe(32)
  })

  it('is crypto-random (no two calls collide in a batch)', () => {
    const seen = new Set<string>()
    for (let i = 0; i < 256; i++) seen.add(generateToken())
    // 128 bits of entropy → collisions across 256 draws are astronomically
    // unlikely; this guards against a regression that falls back to a
    // constant or sequential generator.
    expect(seen.size).toBe(256)
  })
})

describe('isValidTokenFormat', () => {
  it('accepts 32-char hex and rejects everything else', () => {
    expect(isValidTokenFormat('a'.repeat(32))).toBe(true)
    expect(isValidTokenFormat('0123456789abcdef'.repeat(2))).toBe(true)
    expect(isValidTokenFormat('X'.repeat(32))).toBe(false) // non-hex
    expect(isValidTokenFormat('a'.repeat(31))).toBe(false) // too short
    expect(isValidTokenFormat('a'.repeat(33))).toBe(false) // too long
    expect(isValidTokenFormat(null)).toBe(false)
    expect(isValidTokenFormat(undefined)).toBe(false)
    expect(isValidTokenFormat(12345)).toBe(false)
  })
})

describe('stageOperation + confirmOperation', () => {
  it('stages a token then redeems it for the stored op', async () => {
    const { ctx, rows } = makeCtx()
    const token = await stageOperation(ctx, 'delete_blocks', {
      ids: ['b1', 'b2']
    })
    expect(isValidTokenFormat(token)).toBe(true)
    expect(rows.get(token)?.used).toBe(0)
    expect(rows.get(token)?.plugin_id).toBe('silt-ai-agent')

    const op = await confirmOperation(ctx, token)
    expect(op.kind).toBe('delete_blocks')
    expect(op.params).toEqual({ ids: ['b1', 'b2'] })
    // Token is now consumed.
    expect(rows.get(token)?.used).toBe(1)
  })

  it('redeems a token only once when confirms race', async () => {
    const { ctx, rows } = makeCtx()
    const token = await stageOperation(ctx, 'delete_blocks', { ids: ['b1'] })

    const results = await Promise.allSettled([
      confirmOperation(ctx, token),
      confirmOperation(ctx, token)
    ])
    expect(results.filter((r) => r.status === 'fulfilled')).toHaveLength(1)
    const rejected = results.filter(
      (r): r is PromiseRejectedResult => r.status === 'rejected'
    )
    expect(rejected).toHaveLength(1)
    expect(rejected[0].reason).toMatchObject({ code: 'already_used' })
    expect(rows.get(token)?.used).toBe(1)
  })

  it('serializes reject against confirm through the same claim path', async () => {
    const { ctx, rows } = makeCtx()
    const token = await stageOperation(ctx, 'delete_blocks', { ids: ['b1'] })
    const [confirmed, rejected] = await Promise.allSettled([
      confirmOperation(ctx, token),
      rejectOperation(ctx, token)
    ])

    const confirmWon = confirmed.status === 'fulfilled'
    const rejectWon = rejected.status === 'fulfilled' && rejected.value === true
    expect(Number(confirmWon) + Number(rejectWon)).toBe(1)
    expect(rows.get(token)?.used).toBe(1)
  })

  it('uses parameterized SQL (never interpolates user values)', async () => {
    const { ctx, execCalls, queryCalls } = makeCtx()
    const token = await stageOperation(ctx, 'rename_tag', {
      from: 'foo',
      to: 'bar'
    })
    await confirmOperation(ctx, token)
    for (const { sql, params } of execCalls) {
      expect(sql).not.toContain("'foo'")
      expect(sql).not.toContain(token)
      // Parameters carry the values; SQL has placeholders.
      expect(params).toContain(token)
    }
    for (const { sql, params } of queryCalls) {
      expect(sql).not.toContain(token)
      expect(params).toContain(token)
    }
  })
})

describe('confirmOperation failures', () => {
  it('rejects an invalid-format token before hitting the DB', async () => {
    const { ctx, queryCalls } = makeCtx()
    await expect(
      confirmOperation(ctx, 'not-a-real-token')
    ).rejects.toBeInstanceOf(StagingError)
    await expect(
      confirmOperation(ctx, 'not-a-real-token')
    ).rejects.toMatchObject({ code: 'invalid_format' })
    // No query should have run — the format check is the cheap-fail guard.
    expect(queryCalls).toHaveLength(0)
  })

  it('rejects an unknown (random) token as not_found', async () => {
    const { ctx } = makeCtx()
    await expect(confirmOperation(ctx, 'a'.repeat(32))).rejects.toMatchObject({
      code: 'not_found'
    })
  })

  it('rejects a replayed (already-used) token as already_used', async () => {
    const { ctx } = makeCtx()
    const token = await stageOperation(ctx, 'delete_blocks', { ids: ['b1'] })
    await confirmOperation(ctx, token) // first consume
    await expect(confirmOperation(ctx, token)).rejects.toMatchObject({
      code: 'already_used'
    })
  })

  it('rejects an expired token as expired', async () => {
    const { ctx } = makeCtx()
    const start = 1_700_000_000_000
    vi.useFakeTimers()
    vi.setSystemTime(start)
    const token = await stageOperation(ctx, 'delete_blocks', { ids: ['b1'] })
    // Advance just past the TTL window.
    vi.setSystemTime(start + STAGING_TTL_MS + 1)
    await expect(confirmOperation(ctx, token)).rejects.toMatchObject({
      code: 'expired'
    })
  })

  it('reports already_used (not expired) for a token redeemed then aged', async () => {
    // A replay attempt hours later should call out the replay, not the
    // expiry — replay is the more actionable signal for the user.
    const { ctx } = makeCtx()
    const start = 1_700_000_000_000
    vi.useFakeTimers()
    vi.setSystemTime(start)
    const token = await stageOperation(ctx, 'delete_blocks', { ids: ['b1'] })
    await confirmOperation(ctx, token)
    vi.setSystemTime(start + STAGING_TTL_MS + 10_000)
    await expect(confirmOperation(ctx, token)).rejects.toMatchObject({
      code: 'already_used'
    })
  })

  it('rejects a token whose plugin_id does not match', async () => {
    const { ctx, rows } = makeCtx()
    const token = await stageOperation(ctx, 'delete_blocks', { ids: ['b1'] })
    // Tamper with the row's plugin_id (simulates a cross-plugin token).
    const row = rows.get(token)!
    row.plugin_id = 'some-other-plugin'
    await expect(confirmOperation(ctx, token)).rejects.toMatchObject({
      code: 'plugin_mismatch'
    })
  })
})

describe('rejectOperation', () => {
  it('marks the token used without executing', async () => {
    const { ctx, rows } = makeCtx()
    const token = await stageOperation(ctx, 'delete_blocks', { ids: ['b1'] })
    const ok = await rejectOperation(ctx, token)
    expect(ok).toBe(true)
    expect(rows.get(token)?.used).toBe(1)
    // A subsequent confirm is rejected as already_used.
    await expect(confirmOperation(ctx, token)).rejects.toMatchObject({
      code: 'already_used'
    })
  })

  it('returns false for a malformed token (no DB write)', async () => {
    const { ctx, execCalls } = makeCtx()
    const before = execCalls.length
    const ok = await rejectOperation(ctx, 'bad')
    expect(ok).toBe(false)
    expect(execCalls.length).toBe(before)
  })
})

describe('cleanupExpired', () => {
  it('deletes only expired rows', async () => {
    const { ctx, rows } = makeCtx()
    const start = 1_700_000_000_000
    vi.useFakeTimers()
    // Stage stale first (expiry = start + TTL).
    vi.setSystemTime(start)
    const stale = await stageOperation(ctx, 'delete_blocks', { ids: ['b2'] })
    // Stage fresh 5s later (expiry = start + 5000 + TTL — 5s more headroom).
    vi.setSystemTime(start + 5_000)
    const fresh = await stageOperation(ctx, 'delete_blocks', { ids: ['b1'] })
    // Advance to a point past stale's expiry but before fresh's.
    vi.setSystemTime(start + 5_000 + STAGING_TTL_MS - 1)
    await cleanupExpired(ctx)
    expect(rows.has(fresh)).toBe(true)
    expect(rows.has(stale)).toBe(false)
  })
})

describe('StagingError', () => {
  it('carries a stable code', () => {
    const e = new StagingError('expired', 'old')
    expect(e.code).toBe('expired')
    expect(e.message).toBe('old')
    expect(e.name).toBe('StagingError')
    expect(e instanceof Error).toBe(true)
  })
})
