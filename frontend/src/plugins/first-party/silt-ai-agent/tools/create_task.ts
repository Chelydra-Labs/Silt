// Agent tool #801 — create_task.
//
// Closes the read→write loop for tasks: the agent could already query tasks
// (#600) but had no way to create them. Creates a TASK block and applies the
// common capture-set metadata (due, owner, priority, tags) via the same
// ctx.setTask* setters the UI uses. Two placement modes:
//   - standalone (default): a GFM checkbox in <vault>/.silt/tasks.md via
//     ctx.createTask — the quick-add capture surface.
//   - page-scoped: a TASK block on a specific page via ctx.createBlock
//     ({type:'TASK', after}), when notebook/section/page are supplied.
// Returns the new block id so the model can query_tasks / read_blocks on it.
//
// Metadata is applied with post-creation setters rather than folded into the
// prose: structured fields live in [key:: value] tokens / structured columns,
// not the body, so the dedicated setters are the only correct write path
// (mutateBlock/createBlock text is prose). If a setter fails after the block
// was created, the error reports the created id so the model does not retry
// creation and produce a duplicate.

import type { PluginContext } from '../../../sdk'
import { asString } from '../../../../lib/asString'
import type { ToolResult } from '../tool-registry'

export const createTaskToolDef = {
  name: 'create_task',
  description:
    'Create a task (TASK block, initial state TODO). By default the task is ' +
    'appended to the standalone tasks list (.silt/tasks.md); pass notebook + ' +
    'page to place it on a specific page. Optional metadata (due, owner, ' +
    'priority, tags) is applied after creation. Returns the new block id.',
  parameters: {
    type: 'object',
    required: ['text'],
    properties: {
      text: {
        type: 'string',
        description: 'Task description / title prose.'
      },
      due: {
        type: 'string',
        description: 'Due date as YYYY-MM-DD (optional).'
      },
      owner: {
        type: 'string',
        description: 'Assignee name (optional).'
      },
      priority: {
        type: 'integer',
        description: '1=Critical, 2=Normal, 3=Low (optional).',
        minimum: 1,
        maximum: 3
      },
      tags: {
        type: 'array',
        description: 'Tag paths to attach (optional).',
        items: { type: 'string' }
      },
      notebook: {
        type: 'string',
        description:
          'Notebook for a page-scoped task. Omit for a standalone task ' +
          '(the default), or to use the active notebook.'
      },
      section: {
        type: 'string',
        description: 'Section for a page-scoped task (optional).'
      },
      page: {
        type: 'string',
        description:
          'Page for a page-scoped task. When set (with a notebook), the ' +
          'task is created on that page instead of the standalone list.'
      },
      after: {
        type: 'string',
        description:
          'Block UUID to insert the page-scoped task after (optional).'
      }
    }
  }
}

const DATE_RE = /^\d{4}-\d{2}-\d{2}$/

export async function handleCreateTask(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const text = asString(args.text).trim()
  if (!text) {
    return { content: '', error: 'text must not be empty' }
  }

  // Validate scalar metadata up front so we never create a block only to
  // reject a bad field afterward.
  const due =
    args.due === undefined || args.due === null
      ? null
      : asString(args.due).trim()
  if (due && !DATE_RE.test(due)) {
    return { content: '', error: `due must be YYYY-MM-DD (got "${due}")` }
  }
  const owner =
    args.owner === undefined || args.owner === null
      ? null
      : asString(args.owner).trim()
  let priority: number | null = null
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
  const tags = normalizeTags(args.tags)

  // Placement: page-scoped only when a page (and its notebook) resolve.
  const page =
    args.page === undefined || args.page === null ? '' : asString(args.page)
  const notebookArg =
    args.notebook === undefined || args.notebook === null
      ? ''
      : asString(args.notebook)
  const sectionArg =
    args.section === undefined || args.section === null
      ? ''
      : asString(args.section)
  const afterArg =
    args.after === undefined || args.after === null
      ? undefined
      : asString(args.after)

  // notebook/section/after are page-scoped anchors — meaningless without a
  // page (the standalone path ignores them). Fail loudly so the model
  // self-corrects instead of the task silently landing in the wrong place.
  if (!page && (notebookArg || sectionArg || afterArg !== undefined)) {
    return {
      content: '',
      error:
        'notebook/section/after require a page; pass page to place the task ' +
        'on a page, or omit them for a standalone task.'
    }
  }

  let blockId: string
  let placement: string
  if (page) {
    const notebook = notebookArg || ctx.activeNotebook
    if (!notebook) {
      return {
        content: '',
        error:
          'page-scoped task needs a notebook; pass notebook explicitly or ' +
          'open a notebook (omit page for a standalone task).'
      }
    }
    // Ensure the page exists before appending — SaveFileBlocks fails closed on
    // a missing page file (the #691 rename/delete guard), so a task targeted at
    // a new page would otherwise error as "page moved or deleted." createPage
    // is idempotent (a no-op when the page already exists).
    await ctx.createPage(notebook, sectionArg, page)
    blockId = await ctx.createBlock({
      type: 'TASK',
      text,
      notebook,
      section: sectionArg,
      page,
      after: afterArg
    })
    placement = [notebook, sectionArg, page]
      .filter((s) => s.length > 0)
      .join('/')
  } else {
    // ctx.createTask lands a GFM checkbox in .silt/tasks.md.
    blockId = await ctx.createTask({ title: text })
    placement = 'standalone tasks list'
  }

  // Apply metadata via the dedicated setters (structured fields, not prose).
  // A failure here is partial: the task exists, so report the id rather than
  // letting the model retry creation and duplicate it.
  const failed = await applyMetadata(ctx, blockId, {
    due,
    owner,
    priority,
    tags
  })
  if (failed.length > 0) {
    return {
      content: '',
      error: `Task created (block ${blockId} on ${placement}) but metadata failed: ${failed.join('; ')}`
    }
  }

  return {
    content: `Created task "${text}" on ${placement} (block ${blockId}). Use query_tasks to list or filter it, or update_task to change its fields.`
  }
}

interface TaskMetadata {
  due: string | null
  owner: string | null
  priority: number | null
  tags: string[]
}

/**
 * Apply optional metadata fields via the ctx.setTask* setters. Returns the
 * per-field failure reasons (empty if all succeeded). Each setter is an
 * independent atomic write, so a later field failing does not roll back an
 * earlier one — the caller reports the created id so the model can reconcile.
 */
async function applyMetadata(
  ctx: PluginContext,
  blockId: string,
  meta: TaskMetadata
): Promise<string[]> {
  const steps: Array<[string, () => Promise<unknown>]> = []
  if (meta.due)
    steps.push(['due', () => ctx.setTaskDueDate(blockId, meta.due!)])
  if (meta.owner)
    steps.push(['owner', () => ctx.setTaskOwner(blockId, meta.owner!)])
  if (meta.priority !== null)
    steps.push(['priority', () => ctx.setTaskPriority(blockId, meta.priority!)])
  if (meta.tags.length > 0)
    steps.push(['tags', () => ctx.setTaskTags(blockId, meta.tags)])

  const failed: string[] = []
  for (const [field, run] of steps) {
    try {
      await run()
    } catch (e: unknown) {
      failed.push(`${field}: ${e instanceof Error ? e.message : String(e)}`)
    }
  }
  return failed
}

/** Coerce a raw tags array to clean tag paths (drop empties; no leading #). */
function normalizeTags(raw: unknown): string[] {
  if (!Array.isArray(raw)) return []
  return raw
    .map((t) => asString(t).trim())
    .filter((t) => t.length > 0)
    .map((t) => t.replace(/^#+/, ''))
    .filter((t) => t.length > 0)
}
