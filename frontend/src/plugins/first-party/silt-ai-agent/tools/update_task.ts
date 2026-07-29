// Agent tool #802 — update_task (structured metadata + state transitions).
//
// The complement of update_block: update_block rewrites prose only (it strips
// checkboxes + [key:: value] tokens from TASK bodies), while update_task
// touches the structured fields the backend owns (status, due, owner,
// priority, tags, recurrence, estimate, blocked_by, title). One tool with
// optional fields mirrors query_tasks' filter-bag shape; only supplied fields
// are dispatched (partial update), every other field + the prose are left
// untouched.
//
// status routes through ctx.updateBlockState so DONE preserves the server-side
// recurrence auto-spawn (a fresh TODO is spliced below the completed line).
// Because updateBlockState returns only a boolean, the spawned instance's UUID
// is not surfaced through the binding — on a recurring DONE the tool reports
// the transition + the spawn and best-effort resolves the new sibling's id.

import type { PluginContext, TaskStatus } from '../../../sdk'
import { asString } from '../../../../lib/asString'
import { isValidYMD } from './_util'
import type { ToolResult } from '../tool-registry'

const TASK_STATUSES: TaskStatus[] = ['TODO', 'DOING', 'DONE']

export const updateTaskToolDef = {
  name: 'update_task',
  description:
    "Update a task's structured metadata and/or status by UUID. Only " +
    'supplied fields change; prose is never altered (use update_block for ' +
    'prose). status transitions go through the status-transition path so a ' +
    'DONE on a recurring task spawns the next instance as usual; the spawned ' +
    "instance's id is best-effort (if not returned, call query_tasks to find " +
    'it). Pass an empty string/array (or null) for due/owner/tags/' +
    'recurrence/estimate/blocked_by to clear the field. priority and status ' +
    'cannot be cleared.',
  parameters: {
    type: 'object',
    required: ['task_id'],
    properties: {
      task_id: { type: 'string', description: 'Task block UUID to update.' },
      status: {
        type: 'string',
        description: "'TODO', 'DOING', or 'DONE'."
      },
      due: {
        type: 'string',
        description: 'Due date as YYYY-MM-DD; empty string clears it.'
      },
      owner: {
        type: 'string',
        description: 'Assignee name; empty string clears it.'
      },
      priority: {
        type: 'integer',
        description: '1=Critical, 2=Normal, 3=Low.',
        minimum: 1,
        maximum: 3
      },
      tags: {
        type: 'array',
        description: 'Full new tag set; empty array clears all tags.',
        items: { type: 'string' }
      },
      recurrence: {
        type: 'string',
        description:
          'Recurrence rule (e.g. "every week"); empty string clears it. ' +
          'Requires a due-date anchor.'
      },
      estimate: {
        type: 'string',
        description: 'Duration estimate (e.g. "30m", "2h"); empty clears it.'
      },
      blocked_by: {
        type: 'array',
        description:
          'Blocker task UUIDs ([blocked_by::] edges); empty array clears.',
        items: { type: 'string' }
      },
      title: {
        type: 'string',
        description: 'New prose title (preserves tags + metadata tokens).'
      }
    }
  }
}

interface TaskSnapshot {
  notebook: string
  section: string
  page: string
  /** Pre-transition line number — the recurrence spawn is spliced directly
   *  below the completed line, so this anchors the spawn lookup. */
  lineNumber: number
  recur: string | null
}

export async function handleUpdateTask(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const taskId = asString(args.task_id).trim()
  if (!taskId) {
    return { content: '', error: 'task_id must not be empty' }
  }

  // Validate scalars up front so a bad field fails before any write.
  let status: TaskStatus | null = null
  if (args.status !== undefined && args.status !== null) {
    const s = asString(args.status).toUpperCase()
    if (!TASK_STATUSES.includes(s as TaskStatus)) {
      return {
        content: '',
        error: `status must be one of ${TASK_STATUSES.join(', ')} (got "${asString(args.status)}")`
      }
    }
    status = s as TaskStatus
  }
  const due = optionalString(args.due)
  if (due.set && due.value && !isValidYMD(due.value)) {
    return {
      content: '',
      error: `due must be a real YYYY-MM-DD date (got "${due.value}")`
    }
  }
  const owner = optionalString(args.owner)
  let priority: number | null | undefined
  if (args.priority !== undefined && args.priority !== null) {
    const p = Number(args.priority)
    if (!Number.isInteger(p) || p < 1 || p > 3) {
      return {
        content: '',
        error: 'priority must be an integer 1–3 (1=Critical, 2=Normal, 3=Low)'
      }
    }
    priority = p
  }
  const recurrence = optionalString(args.recurrence)
  const estimate = optionalString(args.estimate)
  const tags = optionalStringArray(args.tags)
  const blockedBy = optionalStringArray(args.blocked_by)
  // null is treated as "not supplied" (skip), like undefined — title genuinely
  // cannot be cleared, so only an explicit empty/whitespace string errors.
  // Trim before setTaskTitle so model-supplied surrounding whitespace doesn't
  // leak into the prose title (mirrors create_task's asString(text).trim()).
  const title =
    args.title === undefined || args.title === null
      ? undefined
      : asString(args.title).trim()
  if (title !== undefined && !title) {
    return { content: '', error: 'title must not be empty' }
  }

  const hasMutation =
    status !== null ||
    due.set ||
    owner.set ||
    priority !== undefined ||
    tags.set ||
    recurrence.set ||
    estimate.set ||
    blockedBy.set ||
    title !== undefined
  if (!hasMutation) {
    return { content: '', error: 'no fields supplied to update' }
  }

  // Existence pre-check + recurrence/location snapshot for spawn reporting.
  let snap: TaskSnapshot | null
  try {
    snap = await readSnapshot(ctx, taskId)
  } catch (e: unknown) {
    return {
      content: '',
      error: `lookup failed for task ${taskId}: ${e instanceof Error ? e.message : String(e)}`
    }
  }
  if (!snap) {
    return { content: '', error: `task ${taskId} not found` }
  }

  // Apply metadata setters first (so a freshly-set recurrence is in place if
  // the status transition follows). Each setter is an independent atomic
  // write; collect failures rather than aborting so a partial update still
  // lands the fields that can land.
  const failed: string[] = []
  if (due.set)
    await runSetter('due', () => ctx.setTaskDueDate(taskId, due.value!), failed)
  if (owner.set)
    await runSetter(
      'owner',
      () => ctx.setTaskOwner(taskId, owner.value!),
      failed
    )
  if (priority !== undefined)
    await runSetter(
      'priority',
      () => ctx.setTaskPriority(taskId, priority!),
      failed
    )
  if (tags.set)
    await runSetter('tags', () => ctx.setTaskTags(taskId, tags.value!), failed)
  if (recurrence.set)
    await runSetter(
      'recurrence',
      () => ctx.setTaskRecurrence(taskId, recurrence.value!),
      failed
    )
  if (estimate.set)
    await runSetter(
      'estimate',
      () => ctx.setTaskEstimate(taskId, estimate.value!),
      failed
    )
  if (blockedBy.set)
    await runSetter(
      'blocked_by',
      () => ctx.setTaskBlockedBy(taskId, blockedBy.value!),
      failed
    )
  if (title !== undefined)
    await runSetter('title', () => ctx.setTaskTitle(taskId, title), failed)

  // The recurrence rule that governs a DONE spawn: a just-set rule wins over
  // the pre-existing snapshot, but only if the write actually landed — a
  // rejected setTaskRecurrence leaves the on-disk rule unchanged, and the
  // server would spawn against the prior rule (or not at all).
  const recurFailed = failed.some((f) => f.startsWith('recurrence:'))
  const effectiveRecur =
    recurrence.set && !recurFailed ? recurrence.value || null : snap.recur

  // Status transition (routes through updateBlockState → server-side spawn).
  let spawnNote = ''
  if (status !== null) {
    try {
      await ctx.updateBlockState(taskId, status)
    } catch (e: unknown) {
      failed.push(`status: ${e instanceof Error ? e.message : String(e)}`)
      status = null
    }
    if (status === 'DONE' && effectiveRecur) {
      spawnNote = await reportSpawnedInstance(ctx, taskId, snap, effectiveRecur)
    }
  }

  if (failed.length > 0) {
    return {
      content: '',
      error: `Updated task ${taskId} with failures: ${failed.join('; ')}${spawnNote ? ` (${spawnNote})` : ''}`
    }
  }

  return {
    content: `Updated task ${taskId}.${spawnNote ? ` ${spawnNote}` : ''}`
  }
}

/**
 * Read the task's location + recurrence rule. Returns null when the id is
 * absent or not a TASK (the not-found path). recur + location drive the
 * recurrence-spawn report on a DONE transition.
 */
async function readSnapshot(
  ctx: PluginContext,
  taskId: string
): Promise<TaskSnapshot | null> {
  const { rows } = await ctx.sqliteQuery(
    'SELECT b.notebook, b.section, b.page, b.line_number, t.recur ' +
      'FROM blocks b JOIN tasks t ON t.block_id = b.id ' +
      'WHERE b.id = ?',
    [taskId]
  )
  const row = rows[0]
  if (!row) return null
  return {
    notebook: asString(row.notebook),
    section: asString(row.section),
    page: asString(row.page),
    lineNumber: Number(row.line_number),
    recur: row.recur == null ? null : asString(row.recur)
  }
}

/**
 * After a DONE transition on a recurring task, the server splices a fresh TODO
 * directly below the completed line (app_recurrence_test.go: spawnedIdx ==
 * completedIdx + 1), so the first TODO block whose line_number exceeds the
 * completed task's pre-transition line is the spawn — precise even when the
 * page holds other recurring TODOs. updateBlockState returns no id, so resolve
 * it here; if the lookup fails or finds nothing, report the spawn textually
 * with the page path so the model can narrow a query_tasks call. Never guess.
 */
async function reportSpawnedInstance(
  ctx: PluginContext,
  taskId: string,
  snap: TaskSnapshot,
  recur: string
): Promise<string> {
  const pagePath = [snap.notebook, snap.section, snap.page]
    .filter((s) => s.length > 0)
    .join('/')
  let spawnedId: string | null = null
  try {
    const { rows } = await ctx.sqliteQuery(
      'SELECT b.id FROM blocks b JOIN tasks t ON t.block_id = b.id ' +
        'WHERE b.notebook = ? AND b.section = ? AND b.page = ? ' +
        "AND b.line_number > ? AND b.id != ? AND t.status = 'TODO' " +
        'ORDER BY b.line_number ASC LIMIT 1',
      [snap.notebook, snap.section, snap.page, snap.lineNumber, taskId]
    )
    if (rows[0]) spawnedId = asString(rows[0].id)
  } catch {
    spawnedId = null
  }
  if (spawnedId) {
    return `Recurrence "${recur}" spawned a new instance (block ${spawnedId}); the original is now DONE.`
  }
  return `Recurrence "${recur}" spawned a new TODO instance in ${pagePath}; use query_tasks to find it (the id is not returned by the status transition).`
}

/** Run a setter, appending a labeled failure reason on rejection. */
async function runSetter(
  field: string,
  fn: () => Promise<unknown>,
  failed: string[]
): Promise<void> {
  try {
    await fn()
  } catch (e: unknown) {
    failed.push(`${field}: ${e instanceof Error ? e.message : String(e)}`)
  }
}

/**
 * Resolve an optional string field. `set` distinguishes "caller supplied this
 * field" (undefined → not supplied → skip) from an explicit empty/null clear.
 */
function optionalString(raw: unknown): { set: boolean; value: string } {
  if (raw === undefined) return { set: false, value: '' }
  return { set: true, value: raw === null ? '' : asString(raw) }
}

/** Resolve an optional string-array field with the same set/clear semantics.
 *  null clears (matches optionalString); undefined and non-array types are
 *  skipped — the registry's `type: 'array'` check keeps stray types out, so the
 *  non-array branch is just defense-in-depth. */
function optionalStringArray(raw: unknown): {
  set: boolean
  value: string[]
} {
  if (raw === undefined) return { set: false, value: [] }
  if (raw === null) return { set: true, value: [] }
  if (!Array.isArray(raw)) return { set: false, value: [] }
  const arr = raw
    .map((t) => asString(t).trim())
    .filter((t) => t.length > 0)
    .map((t) => t.replace(/^#+/, ''))
    .filter((t) => t.length > 0)
  return { set: true, value: arr }
}
