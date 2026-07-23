// Agent tool #600 — query_tasks.
//
// Filters the task projection (blocks JOIN tasks) by status, owner, priority,
// due-date range, tags, notebook, and blocked state. All user values are bound
// as SQL parameters (never interpolated), and limit is clamped 1–50. A task is
// "blocked" when it has a task_dependencies edge to a prerequisite whose status
// is not DONE (the same definition the DONE-confirm dialog uses).

import type { PluginContext, TaskStatus } from '../../../sdk'
import { asString } from '../../../../lib/asString'
import type { ToolResult } from '../tool-registry'
import { breadcrumb, clampInt } from './_util'

export const queryTasksToolDef = {
  name: 'query_tasks',
  description:
    'Query tasks with filters. Returns id, title, status, due date, owner, ' +
    'location, and a blocked flag. All filters are optional and combine with ' +
    'AND; limit defaults to 20 (max 50).',
  parameters: {
    type: 'object',
    properties: {
      status: {
        type: 'string',
        description: "'TODO', 'DOING', or 'DONE'."
      },
      owner: { type: 'string', description: 'Exact owner name match.' },
      priority_min: {
        type: 'integer',
        description:
          'Only tasks at this priority or higher (1=Critical, 2=Normal, 3=Low).',
        minimum: 1,
        maximum: 3
      },
      due_before: {
        type: 'string',
        description: 'YYYY-MM-DD inclusive upper bound.'
      },
      due_after: {
        type: 'string',
        description: 'YYYY-MM-DD inclusive lower bound.'
      },
      tags: {
        type: 'array',
        description: 'Tag paths to match (task has at least one).',
        items: { type: 'string' }
      },
      notebook: { type: 'string', description: 'Restrict to a notebook.' },
      is_blocked: {
        type: 'boolean',
        description: 'Filter by blocked state (has an open prerequisite).'
      },
      limit: {
        type: 'integer',
        description: 'Max results (default 20, clamped to 1–50).',
        minimum: 1,
        maximum: 50
      }
    }
  }
}

const TASK_STATUSES: TaskStatus[] = ['TODO', 'DOING', 'DONE']

// EXISTS subquery shared by the is_blocked SELECT column + WHERE filter. The
// 'DONE' literal is a hardcoded constant (not user input), so it is inlined
// rather than bound — this keeps the parameter list ordered by user filters
// only, with limit trailing.
const BLOCKED_EXISTS =
  'EXISTS (SELECT 1 FROM task_dependencies td ' +
  'JOIN tasks bt ON bt.block_id = td.blocked_by_id ' +
  "WHERE td.block_id = t.block_id AND bt.status != 'DONE')"

interface TaskRow {
  id: string
  clean_content: string | null
  status: string
  due_date: string | null
  owner: string | null
  notebook: string
  section: string
  page: string
  priority: number | null
  is_blocked: number
}

export async function handleQueryTasks(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const limit = clampInt(args.limit, 20, 1, 50)

  const where: string[] = []
  const params: unknown[] = []

  if (args.status !== undefined && args.status !== null) {
    const status = asString(args.status).toUpperCase()
    if (!TASK_STATUSES.includes(status as TaskStatus)) {
      return {
        content: '',
        error: `status must be one of ${TASK_STATUSES.join(', ')} (got "${asString(args.status)}")`
      }
    }
    where.push('t.status = ?')
    params.push(status)
  }
  if (args.owner !== undefined && args.owner !== null) {
    where.push('t.owner = ?')
    params.push(asString(args.owner))
  }
  if (args.priority_min !== undefined && args.priority_min !== null) {
    const p = clampInt(args.priority_min, 1, 1, 3)
    // Lower number = higher priority. "at least this priority" ⇒ <= bound.
    where.push('t.priority IS NOT NULL AND t.priority <= ?')
    params.push(p)
  }
  if (args.due_before !== undefined && args.due_before !== null) {
    where.push('t.due_date IS NOT NULL AND t.due_date <= ?')
    params.push(asString(args.due_before))
  }
  if (args.due_after !== undefined && args.due_after !== null) {
    where.push('t.due_date IS NOT NULL AND t.due_date >= ?')
    params.push(asString(args.due_after))
  }
  if (args.notebook !== undefined && args.notebook !== null) {
    where.push('b.notebook = ?')
    params.push(asString(args.notebook))
  }
  if (Array.isArray(args.tags) && args.tags.length > 0) {
    const tags = args.tags.map((t) => asString(t)).filter((s) => s.length > 0)
    if (tags.length > 0) {
      where.push(
        `EXISTS (SELECT 1 FROM tags tg WHERE tg.block_id = b.id AND tg.raw_path IN (${tags
          .map(() => '?')
          .join(',')}))`
      )
      params.push(...tags)
    }
  }
  if (args.is_blocked !== undefined && args.is_blocked !== null) {
    where.push(args.is_blocked ? BLOCKED_EXISTS : `NOT ${BLOCKED_EXISTS}`)
  }

  const sql =
    `SELECT b.id, b.clean_content, t.status, t.due_date, t.owner, ` +
    `b.notebook, b.section, b.page, t.priority, ` +
    `${BLOCKED_EXISTS} AS is_blocked ` +
    `FROM tasks t JOIN blocks b ON b.id = t.block_id ` +
    (where.length > 0 ? `WHERE ${where.join(' AND ')} ` : '') +
    `ORDER BY t.due_date IS NULL, t.due_date, b.id ` +
    `LIMIT ?`
  const { rows } = await ctx.sqliteQuery(sql, [...params, limit])

  const tasks = rows.map(rowToTask)
  if (tasks.length === 0) {
    return { content: 'No tasks match the given filters.' }
  }

  const lines = tasks.map((t, i) => formatTask(i + 1, t))
  return {
    content: `${tasks.length} task(s):\n\n${lines.join('\n\n')}`
  }
}

function rowToTask(r: Record<string, unknown>): TaskRow {
  return {
    id: asString(r.id),
    clean_content: r.clean_content == null ? null : asString(r.clean_content),
    status: asString(r.status),
    due_date: r.due_date == null ? null : asString(r.due_date),
    owner: r.owner == null ? null : asString(r.owner),
    notebook: asString(r.notebook),
    section: asString(r.section),
    page: asString(r.page),
    priority: r.priority == null ? null : Number(r.priority),
    is_blocked: Number(r.is_blocked ?? 0)
  }
}

function formatTask(index: number, t: TaskRow): string {
  const title = (t.clean_content ?? '').trim().replace(/\n/g, ' ')
  const meta = [
    `status: ${t.status}`,
    t.due_date ? `due: ${t.due_date}` : 'no due date',
    t.owner ? `owner: ${t.owner}` : 'unassigned',
    t.priority != null ? `priority: ${t.priority}` : 'no priority',
    t.is_blocked ? 'blocked: yes' : 'blocked: no'
  ].join(' | ')
  return [
    `[${index}] ${title || '(untitled task)'}`,
    `    id: ${t.id}`,
    `    ${meta}`,
    `    location: ${breadcrumb(t.notebook, t.section, t.page)}`
  ].join('\n')
}
