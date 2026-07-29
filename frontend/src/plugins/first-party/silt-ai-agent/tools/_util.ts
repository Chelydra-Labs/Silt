// Small helpers shared across the agent tools.

import type { PluginContext } from '../../../sdk'

/**
 * Emit one consistent agent-write-tool audit event (#811). The payload carries
 * ONLY allowlist-safe keys (tool / status / block_id) so the Go redactor
 * (`plugin_audit.go` `allowedAIAuditDetailKeys`) preserves it: a block UUID
 * identifies WHAT was mutated without leaking the block's prose body. Best-effort
 * and fire-and-forget — failures are swallowed so audit can never break a tool
 * handler's own return shape.
 *
 * The agent loop already emits a coarse `tool_call` start/ok/error event per
 * dispatch; this is the complementary fine-grained "what it mutated" record that
 * makes agent-initiated vault changes traceable in the per-plugin `ai.log`.
 */
export function auditWrite(
  ctx: PluginContext,
  tool: string,
  status: 'ok' | 'error',
  blockId?: string
): void {
  try {
    void ctx.ai.auditEvent?.({
      kind: 'tool_result',
      tool,
      status,
      ...(blockId ? { block_id: blockId } : {})
    })
  } catch {
    // Defensive: a test double cast through `unknown` may omit `ai`. Audit is
    // best-effort and must never throw inside a tool handler.
  }
}

/** Clamp a value into [min, max], defaulting when absent/non-finite. */
export function clampInt(
  v: unknown,
  def: number,
  min: number,
  max: number
): number {
  const n = typeof v === 'number' && Number.isFinite(v) ? v : def
  return Math.min(max, Math.max(min, Math.floor(n)))
}

/** Render a `notebook > section > page` breadcrumb, skipping empty segments. */
export function breadcrumb(
  notebook: string,
  section: string,
  page: string
): string {
  return [notebook, section, page].filter((s) => s && s.length > 0).join(' > ')
}

/**
 * Validate a YYYY-MM-DD string is a real calendar date, not just well-formed.
 * Agent tools route due dates to Go setters / atomic writes after the task may
 * already exist, so rejecting impossible dates (e.g. 2026-13-40, Feb 30) up
 * front avoids a partial-failure where the block is created but its due setter
 * then rejects. Round-trips the parsed value through Date to catch month/day
 * overflow.
 */
export function isValidYMD(s: string): boolean {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s)
  if (!m) return false
  const y = Number(m[1])
  const mo = Number(m[2])
  const d = Number(m[3])
  const dt = new Date(Date.UTC(y, mo - 1, d))
  return (
    dt.getUTCFullYear() === y &&
    dt.getUTCMonth() === mo - 1 &&
    dt.getUTCDate() === d
  )
}
