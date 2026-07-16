// Stage/confirm token protocol (#605).
//
// Destructive agent operations (delete, merge, rename, bulk update) cannot be
// undone from the model's side, so the agent stages them: a tool returns a
// preview + a single-use token. The UX prompts the user; on Confirm the token
// is redeemed (marked used) and the staged op executes; on Reject the token is
// marked used (so it cannot be replayed later) without executing. Tokens
// expire after STAGING_TTL_MS so a leaked or forgotten stage does not linger.
//
// Tokens are 32 hex chars (128 bits of crypto-random) generated via the
// WebCrypto-derived getRandomValues. The DB lookup is parameterized — the
// token is never interpolated into SQL — so a malformed token cannot inject.
//
// plugin_id is stamped at stage time and re-checked at confirm time. The DB
// is this plugin's own per-plugin store, so the check is defense-in-depth
// (an attacker who can write rows here is already inside the plugin); the
// format check (`/^[0-9a-f]{32}$/`) is the real injection guard.

import type { PluginContext } from '../../sdk'

const PLUGIN_ID = 'silt-ai-agent'

/** Stage lifetime: 5 minutes. Replays after this are rejected as expired. */
export const STAGING_TTL_MS = 5 * 60 * 1000

/** 128 bits of entropy encoded as 32 lowercase hex chars. */
const TOKEN_HEX_LEN = 32
const TOKEN_RE = /^[0-9a-f]{32}$/

/** Shape persisted in staging_tokens.operation (JSON-encoded). */
export interface StagedOperation {
  kind: string
  params: Record<string, unknown>
}

/** Validate the token format before any DB hit (cheap-fail injection guard). */
export function isValidTokenFormat(token: unknown): token is string {
  return typeof token === 'string' && TOKEN_RE.test(token)
}

/**
 * Generate a 32-char lowercase-hex token from crypto.getRandomValues. Uses
 * 16 bytes → 32 hex chars (128 bits of entropy). globalThis.crypto is
 * available in the Wails webview, jsdom, and Node 19+, so the WebCrypto
 * path is the only one exercised at runtime.
 */
export function generateToken(): string {
  const bytes = new Uint8Array(TOKEN_HEX_LEN / 2)
  const cryptoObj = (globalThis as { crypto?: Crypto }).crypto
  if (cryptoObj?.getRandomValues) {
    cryptoObj.getRandomValues(bytes)
  } else {
    // Defensive only — Silt's webview always exposes globalThis.crypto.
    for (let i = 0; i < bytes.length; i++) {
      bytes[i] = Math.floor(Math.random() * 256)
    }
  }
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}

/**
 * Stage an operation for user confirmation. Returns the single-use token the
 * UX submits to apply or reject it. The operation record persists for
 * STAGING_TTL_MS; a background sweep (cleanupExpired) GCs stale rows.
 */
export async function stageOperation(
  ctx: PluginContext,
  kind: string,
  params: Record<string, unknown>
): Promise<string> {
  const token = generateToken()
  const now = Date.now()
  const expiresAt = now + STAGING_TTL_MS
  const operation = JSON.stringify({ kind, params })
  await ctx.pluginDb.exec(
    `INSERT INTO staging_tokens
       (token, plugin_id, operation, created_at, expires_at, used)
     VALUES (?, ?, ?, ?, ?, 0)`,
    [token, PLUGIN_ID, operation, now, expiresAt]
  )
  return token
}

/** Reject reasons surfaced to the model + UX. */
export type ConfirmFailure =
  | 'invalid_format'
  | 'not_found'
  | 'expired'
  | 'already_used'
  | 'plugin_mismatch'

export class StagingError extends Error {
  constructor(
    public readonly code: ConfirmFailure,
    message: string
  ) {
    super(message)
    this.name = 'StagingError'
  }
}

/**
 * Redeem a token: look it up, validate expiry + unused + plugin_id, mark used,
 * and return the stored operation. Throws StagingError on any failure so the
 * caller can branch on `code` (e.g. expired vs. replayed).
 *
 * The conditional UPDATE is the database claim. The SDK currently exposes no
 * affected-row count, so calls in this webview are serialized per token and
 * the row is read back immediately after the claim. A future SDK result with
 * `changes`/`rowsAffected` is also honored when available.
 */
export async function confirmOperation(
  ctx: PluginContext,
  token: string
): Promise<StagedOperation> {
  if (!isValidTokenFormat(token)) {
    throw new StagingError('invalid_format', 'invalid staging token format')
  }
  return withTokenLock(token, async () => {
    const now = Date.now()
    const row = await readToken(ctx, token)
    validateClaimable(row, now)

    // The expiry predicate belongs on the UPDATE, not only on the preceding
    // SELECT. It prevents a token that expires between those statements from
    // being redeemed.
    const execResult = await ctx.pluginDb.exec(
      `UPDATE staging_tokens SET used = 1
         WHERE token = ? AND used = 0 AND expires_at > ? AND plugin_id = ?`,
      [token, now, PLUGIN_ID]
    )
    const changes = affectedRows(execResult)
    if (changes === 0) {
      throwClaimFailure(await readToken(ctx, token), now)
    }

    // Older SDKs return void from exec. Under the per-token lock, a read-back
    // confirms that this call owns the claim; SDKs that expose changes have
    // already provided the stronger direct confirmation above.
    const claimed = await readToken(ctx, token)
    if (Number(claimed?.used) !== 1) {
      throwClaimFailure(claimed, now)
    }
    return decodeOperation(claimed?.operation)
  })
}

type TokenRow = Record<string, unknown> | undefined

const tokenLocks = new Map<string, Promise<void>>()

async function withTokenLock<T>(
  token: string,
  fn: () => Promise<T>
): Promise<T> {
  const previous = tokenLocks.get(token) ?? Promise.resolve()
  let release!: () => void
  const current = new Promise<void>((resolve) => {
    release = resolve
  })
  tokenLocks.set(token, current)
  await previous
  try {
    return await fn()
  } finally {
    release()
    if (tokenLocks.get(token) === current) tokenLocks.delete(token)
  }
}

async function readToken(ctx: PluginContext, token: string): Promise<TokenRow> {
  const { rows } = await ctx.pluginDb.query(
    `SELECT operation, plugin_id, expires_at, used
       FROM staging_tokens
      WHERE token = ?
      LIMIT 1`,
    [token]
  )
  return rows[0]
}

function validateClaimable(row: TokenRow, now: number): void {
  if (!row) throw new StagingError('not_found', 'staging token not found')
  // Check used BEFORE expiry so a redeemed-then-aged token reports as replay.
  if (Number(row.used) !== 0) {
    throw new StagingError(
      'already_used',
      'staging token has already been used'
    )
  }
  if (Number(row.expires_at) <= now) {
    throw new StagingError('expired', 'staging token has expired')
  }
  if (String(row.plugin_id) !== PLUGIN_ID) {
    throw new StagingError(
      'plugin_mismatch',
      'staging token does not belong to this plugin'
    )
  }
}

function throwClaimFailure(row: TokenRow, now: number): never {
  if (!row) throw new StagingError('not_found', 'staging token not found')
  if (Number(row.used) !== 0) {
    throw new StagingError(
      'already_used',
      'staging token has already been used'
    )
  }
  if (Number(row.expires_at) <= now) {
    throw new StagingError('expired', 'staging token has expired')
  }
  throw new StagingError('already_used', 'staging token claim was lost')
}

/** Accept the common affected-row envelopes used by SQLite bridges. */
function affectedRows(result: unknown): number | undefined {
  if (typeof result === 'number') return result
  if (!result || typeof result !== 'object') return undefined
  const r = result as Record<string, unknown>
  for (const key of ['changes', 'rowsAffected', 'affectedRows']) {
    if (typeof r[key] === 'number') return r[key] as number
  }
  return undefined
}

function decodeOperation(raw: unknown): StagedOperation {
  if (typeof raw !== 'string' || raw.length === 0) {
    throw new StagingError('not_found', 'staging token had no operation')
  }
  try {
    const parsed = JSON.parse(raw) as { kind?: unknown; params?: unknown }
    if (typeof parsed.kind !== 'string') {
      throw new Error('missing kind')
    }
    return {
      kind: parsed.kind,
      params:
        parsed.params && typeof parsed.params === 'object'
          ? (parsed.params as Record<string, unknown>)
          : {}
    }
  } catch {
    throw new StagingError('not_found', 'staging token operation was malformed')
  }
}

/**
 * Reject a token: mark used without executing. The token becomes unusable for
 * any later confirm. No-op (returns false) when the token does not exist or
 * was already consumed — the UX may have raced a confirm vs. a timeout-driven
 * reject, and either order leaves the token safely consumed.
 */
export async function rejectOperation(
  ctx: PluginContext,
  token: string
): Promise<boolean> {
  if (!isValidTokenFormat(token)) return false
  return withTokenLock(token, async () => {
    const now = Date.now()
    const row = await readToken(ctx, token)
    if (!row || Number(row.used) !== 0 || Number(row.expires_at) <= now) {
      return false
    }
    const execResult = await ctx.pluginDb.exec(
      `UPDATE staging_tokens SET used = 1
         WHERE token = ? AND used = 0 AND expires_at > ? AND plugin_id = ?`,
      [token, now, PLUGIN_ID]
    )
    const changes = affectedRows(execResult)
    if (changes === 0) return false
    const claimed = await readToken(ctx, token)
    return Number(claimed?.used) === 1
  })
}

/**
 * Delete expired rows. Called on agent init (onVaultOpen) so the table does
 * not accumulate dead tokens across sessions. Safe to run on every vault
 * open; cost is a single range DELETE on the indexed expires_at column.
 */
export async function cleanupExpired(ctx: PluginContext): Promise<void> {
  const now = Date.now()
  await ctx.pluginDb.exec(`DELETE FROM staging_tokens WHERE expires_at < ?`, [
    now
  ])
}
