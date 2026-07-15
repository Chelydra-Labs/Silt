// Agent tool registry (#596).
//
// Tools the agent loop can dispatch. A tool is a named handler with a JSON
// Schema for its arguments. registerTool stores defs in a module-scoped Map;
// buildToolCatalog maps them to the PluginAIToolDef shape ctx.ai.complete
// expects. dispatchTool validates arguments structurally (required fields
// exist + types match) before invoking the handler, so a malformed call from
// the model never reaches tool code.

import type { PluginAIToolDef, PluginContext } from '../../sdk'

/** Result returned by an agent tool handler. */
export interface ToolResult {
  content: string
  /** Set when the tool rejected its inputs or failed; surfaces to the model. */
  error?: string
  /**
   * Phase 5 staging-token support. When a destructive tool stages a write for
   * user confirmation, isStaged marks the result and stagedToken carries the
   * token the UX submits to apply or reject it. stagedPreview is a short
   * human-readable description of what would happen ("Delete 3 blocks"),
   * surfaced in the confirm dialog. The agent loop intercepts staged results
   * and does NOT feed them to the model — the model only sees the post-confirm
   * outcome (commit result or "rejected by user").
   */
  isStaged?: boolean
  stagedToken?: string
  stagedPreview?: StagedPreview
}

/**
 * Preview data for a staged operation. `kind` is the operation category
 * (delete_blocks, merge_pages, rename_tag, bulk_update, …); `summary` is a
 * one-line description for the confirm dialog; `details` is an optional
 * longer breakdown (e.g. per-target breadcrumbs). The agent loop does not
 * interpret these — it forwards them to the UX via onStaging.
 */
export interface StagedPreview {
  kind: string
  /** Short imperative summary, e.g. "Delete 3 blocks in Work/Notes/Decisions". */
  summary: string
  /** Optional multi-line detail for the confirm dialog body. */
  details?: string
  /** Optional count of affected targets, for the dialog headline. */
  affectedCount?: number
}

/**
 * The commit half of a staged tool. After confirmOperation redeems the token,
 * the agent loop calls commit(ctx, params) to execute the real write.
 * `params` is the operation payload stored alongside the token at stage time,
 * NOT the model's args — so the model cannot mutate the staged op between
 * staging and confirmation.
 */
export type StagedCommit = (
  ctx: PluginContext,
  params: Record<string, unknown>
) => Promise<ToolResult>

/**
 * A tool the agent can call. `parameters` is a raw JSON Schema object
 * (lowercase type strings) describing the handler's `args`.
 *
 * Destructive tools register BOTH `handler` (which stages the op and returns
 * a preview + token) and `commit` (which executes after the user confirms).
 * Read-only tools omit `commit`.
 */
export interface AgentToolDef {
  name: string
  description: string
  parameters: Record<string, unknown>
  handler: (
    ctx: PluginContext,
    args: Record<string, unknown>
  ) => Promise<ToolResult>
  /** Optional: present on destructive (staged) tools. */
  commit?: StagedCommit
}

// Module-scoped registry: one Map per process. clearTools() resets it so tests
// start from a known state and onVaultClose can tear down.
const tools = new Map<string, AgentToolDef>()

/** Register (or replace) a tool by name. */
export function registerTool(tool: AgentToolDef): void {
  tools.set(tool.name, tool)
}

/** Remove a single tool by name (no-op if absent). */
export function unregisterTool(name: string): void {
  tools.delete(name)
}

/** All registered tool defs, in insertion order. */
export function getTools(): AgentToolDef[] {
  return [...tools.values()]
}

/** Reset the registry (test/vault-close teardown). */
export function clearTools(): void {
  tools.clear()
}

/**
 * Map the registry to the PluginAIToolDef shape ctx.ai.complete consumes
 * (name/description/parameters only — the handler stays host-side).
 */
export function buildToolCatalog(): PluginAIToolDef[] {
  return getTools().map((t) => ({
    name: t.name,
    description: t.description,
    parameters: t.parameters
  }))
}

/**
 * Structural JSON-Schema-ish validation. For sprint 41 a lightweight check
 * suffices (per PLAN.md): verify every entry in `schema.required` is present,
 * and every property declared in `schema.properties` has a matching JS type.
 * A full validator can replace this later without changing the call sites.
 */
export function validateArgs(
  schema: Record<string, unknown>,
  args: Record<string, unknown>
): { ok: true } | { ok: false; error: string } {
  const required = Array.isArray(schema.required) ? schema.required : []
  for (const r of required) {
    if (typeof r !== 'string') continue
    if (!(r in args)) {
      return { ok: false, error: `missing required parameter "${r}"` }
    }
  }
  const props =
    (schema.properties as
      Record<string, { type?: string | string[] }> | undefined) ?? {}
  for (const [key, decl] of Object.entries(props)) {
    if (!(key in args)) continue
    const value = args[key]
    const expected = decl?.type
    if (!expected) continue
    const types = Array.isArray(expected) ? expected : [expected]
    const actual = jsTypeOf(value)
    // JSON Schema's "number" subsumes "integer": an integer value satisfies a
    // number param. "integer" stays strict (rejects floats).
    const matches =
      types.includes(actual) ||
      (actual === 'integer' && types.includes('number'))
    if (!matches) {
      return {
        ok: false,
        error: `parameter "${key}" expected ${types.join('|')}, got ${actual}`
      }
    }
  }
  return { ok: true }
}

// JSON-Schema type name → JS type name. JSON Schema's "integer" is a Number
// in JS; the registry accepts either an int or float for integer params.
function jsTypeOf(v: unknown): string {
  if (Array.isArray(v)) return 'array'
  if (v === null) return 'null'
  const t = typeof v
  if (t === 'number') return Number.isInteger(v) ? 'integer' : 'number'
  return t
}

/**
 * Look up `name`, validate `argsJson` against its schema, and invoke the
 * handler. Any failure (unknown tool, malformed args, handler throw) is
 * caught and returned as `{ content: '', error }` so the loop can feed the
 * error back to the model instead of crashing.
 */
export async function dispatchTool(
  ctx: PluginContext,
  name: string,
  argsJson: Record<string, unknown>
): Promise<ToolResult> {
  const tool = tools.get(name)
  if (!tool) {
    return { content: '', error: `unknown tool "${name}"` }
  }
  const v = validateArgs(tool.parameters, argsJson)
  if (!v.ok) {
    return { content: '', error: v.error }
  }
  try {
    return await tool.handler(ctx, argsJson)
  } catch (e: unknown) {
    const message = e instanceof Error ? e.message : String(e)
    return { content: '', error: message }
  }
}
