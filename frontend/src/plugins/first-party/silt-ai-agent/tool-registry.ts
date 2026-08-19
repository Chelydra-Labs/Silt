// Agent tool registry (#596).
//
// Tools the agent loop can dispatch. A tool is a named handler with a JSON
// Schema for its arguments. registerTool stores defs in a module-scoped Map;
// buildToolCatalog maps them to the PluginAIToolDef shape ctx.ai.complete
// expects. dispatchTool validates arguments structurally (required fields
// exist + types match) before invoking the handler, so a malformed call from
// the model never reaches tool code.

import type { PluginAIToolDef, PluginContext } from '../../sdk'
import { coerceIPCError } from '../../../lib/ipcError'
import {
  type AgentWritesMode,
  isMutatingTool,
  previewForMutation,
  shouldStageTool
} from './write-policy'
import { stageOperation } from './staging'

/** Result returned by an agent tool handler. */
export interface ToolResult {
  content: string
  /** Set when the tool rejected its inputs or failed; surfaces to the model. */
  error?: string
  /** Vault passages returned by retrieval tools for the shared chat transcript. */
  evidence?: ToolEvidence[]
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

/** Structured retrieval metadata kept alongside the model-facing text. */
export interface ToolEvidence {
  citationIndex: number
  /**
   * Vault block UUID, or a synthetic help id (`help:…`) when
   * sourceKind is product_help.
   */
  blockId: string
  /** Defaults to vault when omitted. */
  sourceKind?: 'vault' | 'product_help'
  notebook?: string
  section?: string
  page?: string
  lineNumber?: number
  snippet?: string
  title?: string
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
  /**
   * UX severity: bulk/irreversible ops use danger styling; single reversible
   * edits use a neutral "approve change" card under default confirm mode.
   */
  severity?: 'normal' | 'danger'
}

/**
 * The commit half of a staged tool. After confirmOperation redeems the token,
 * the agent loop calls commit(ctx, params, signal?) to execute the real write.
 * `params` is the operation payload stored alongside the token at stage time,
 * NOT the model's args — so the model cannot mutate the staged op between
 * staging and confirmation. `signal` is the run AbortSignal so long commits
 * (e.g. extract_and_save applyBlocks batch) honor Stop after confirm.
 * Nested model work for extract runs in the stage handler, not commit.
 */
export type StagedCommit = (
  ctx: PluginContext,
  params: Record<string, unknown>,
  signal?: AbortSignal
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
    args: Record<string, unknown>,
    signal?: AbortSignal
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
 * Map tool defs to the PluginAIToolDef shape ctx.ai.complete consumes
 * (name/description/parameters only — the handler stays host-side).
 */
export function buildToolCatalogFrom(
  toolsList: AgentToolDef[]
): PluginAIToolDef[] {
  return toolsList.map((t) => ({
    name: t.name,
    description: t.description,
    parameters: t.parameters
  }))
}

/**
 * Map the full registry to the PluginAIToolDef shape ctx.ai.complete consumes.
 */
export function buildToolCatalog(): PluginAIToolDef[] {
  return buildToolCatalogFrom(getTools())
}

type PropDecl = {
  type?: string | string[]
  enum?: unknown[]
  minimum?: number
  maximum?: number
  properties?: Record<string, PropDecl>
  required?: string[]
  items?: PropDecl
}

/**
 * Structural JSON-Schema-ish validation: required fields, types, enum,
 * min/max on numbers, and one level of nested object properties/required.
 */
export function validateArgs(
  schema: Record<string, unknown>,
  args: Record<string, unknown>
): { ok: true } | { ok: false; error: string } {
  return validateObjectShape(schema as PropDecl, args, '')
}

function validateObjectShape(
  schema: PropDecl,
  args: Record<string, unknown>,
  pathPrefix: string
): { ok: true } | { ok: false; error: string } {
  const required = Array.isArray(schema.required) ? schema.required : []
  for (const r of required) {
    if (typeof r !== 'string') continue
    if (!(r in args)) {
      const label = pathPrefix ? `${pathPrefix}.${r}` : r
      return { ok: false, error: `missing required parameter "${label}"` }
    }
  }
  const props = schema.properties ?? {}
  for (const [key, decl] of Object.entries(props)) {
    if (!(key in args)) continue
    const label = pathPrefix ? `${pathPrefix}.${key}` : key
    const check = validateValue(decl, args[key], label)
    if (!check.ok) return check
  }
  return { ok: true }
}

function validateValue(
  decl: PropDecl | undefined,
  value: unknown,
  label: string
): { ok: true } | { ok: false; error: string } {
  if (!decl) return { ok: true }

  if (Array.isArray(decl.enum) && decl.enum.length > 0) {
    if (!decl.enum.some((e) => Object.is(e, value))) {
      return {
        ok: false,
        error: `parameter "${label}" must be one of ${decl.enum.map(String).join(', ')} (got ${JSON.stringify(value)})`
      }
    }
  }

  const expected = decl.type
  if (expected) {
    const types = Array.isArray(expected) ? expected : [expected]
    const actual = jsTypeOf(value)
    const matches =
      types.includes(actual) ||
      (actual === 'integer' && types.includes('number'))
    if (!matches) {
      return {
        ok: false,
        error: `parameter "${label}" expected ${types.join('|')}, got ${actual}`
      }
    }
  }

  if (typeof value === 'number' && Number.isFinite(value)) {
    if (typeof decl.minimum === 'number' && value < decl.minimum) {
      return {
        ok: false,
        error: `parameter "${label}" must be >= ${decl.minimum} (got ${value})`
      }
    }
    if (typeof decl.maximum === 'number' && value > decl.maximum) {
      return {
        ok: false,
        error: `parameter "${label}" must be <= ${decl.maximum} (got ${value})`
      }
    }
  }

  // Array element types via items (one level; e.g. source_block_ids: string[]).
  if (Array.isArray(value) && decl.items) {
    for (let i = 0; i < value.length; i++) {
      const check = validateValue(decl.items, value[i], `${label}[${i}]`)
      if (!check.ok) return check
    }
  }

  // One level of nested object properties + required (e.g. create target).
  if (
    value &&
    typeof value === 'object' &&
    !Array.isArray(value) &&
    decl.properties
  ) {
    return validateObjectShape(decl, value as Record<string, unknown>, label)
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

export interface DispatchToolOpts {
  mode?: AgentWritesMode
  signal?: AbortSignal
  /** When set, names outside this turn's catalog are refused. */
  allowed?: ReadonlySet<string>
}

/**
 * Look up `name`, validate `argsJson` against its schema, apply write policy,
 * and invoke the handler (or stage). Failures return `{ content: '', error }`.
 */
export async function dispatchTool(
  ctx: PluginContext,
  name: string,
  argsJson: Record<string, unknown>,
  opts?: DispatchToolOpts
): Promise<ToolResult> {
  if (opts?.allowed && !opts.allowed.has(name)) {
    return { content: '', error: `tool "${name}" is not available this turn` }
  }
  const tool = tools.get(name)
  if (!tool) {
    return { content: '', error: `unknown tool "${name}"` }
  }
  const v = validateArgs(tool.parameters, argsJson)
  if (!v.ok) {
    return { content: '', error: v.error }
  }

  const mode = opts?.mode ?? 'confirm'
  const signal = opts?.signal

  // Mutating tools are refused entirely in read_only (not staged).
  if (mode === 'read_only' && isMutatingTool(name)) {
    return {
      content: '',
      error: 'Vault writes are disabled (Agent vault writes is Read only).'
    }
  }

  try {
    if (shouldStageTool(name, mode)) {
      // Fail closed: a classified mutator without commit must never fall
      // through to a direct handler write under confirm (or always-confirm).
      if (!tool.commit) {
        return {
          content: '',
          error: `tool "${name}" cannot be staged (missing commit handler)`
        }
      }
      // rename_tag / extract_and_save stage inside the handler (richer preview
      // / frozen payload). Other mutators stage raw args at dispatch.
      if (name === 'rename_tag' || name === 'extract_and_save') {
        return await tool.handler(ctx, argsJson, signal)
      }
      // Other mutators: stage at dispatch; commit runs the real handler later.
      const token = await stageOperation(ctx, name, argsJson)
      return {
        content: '',
        isStaged: true,
        stagedToken: token,
        stagedPreview: previewForMutation(name, argsJson)
      }
    }
    return await tool.handler(ctx, argsJson, signal)
  } catch (e: unknown) {
    return { content: '', error: coerceIPCError(e).message }
  }
}
