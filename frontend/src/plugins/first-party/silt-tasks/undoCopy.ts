// User-facing copy for the session undo journal (#1011). Kept pure and
// separate from the reactive store so the wording that links a journal entry
// to a concrete change ("status change in Work › Journal › Daily") is pinned
// by unit tests and reused by every surface.
import type { TaskChangedField } from '../../sdk'
import { STANDALONE_TASKS_NOTEBOOK } from '../../../lib/standaloneTasksNav'

const FIELD_LABELS: Record<TaskChangedField, string> = {
  text: 'text',
  status: 'status',
  completed: 'completion',
  owner: 'owner',
  startDate: 'start date',
  dueDate: 'due date',
  priority: 'priority',
  shortlist: 'shortlist',
  pinned: 'pin',
  progress: 'progress',
  recurrence: 'recurrence',
  blockedBy: 'dependency',
  estimate: 'estimate',
  order: 'order'
}

/**
 * Noun phrase naming what an undo restores. A single mutation can touch
 * several attributes (e.g. TODO→DONE also writes `completed`), so the copy
 * lists them instead of guessing one.
 */
export function describeChangedFields(
  fields: readonly TaskChangedField[]
): string {
  const labels = [...new Set(fields.map((f) => FIELD_LABELS[f] ?? f))]
  if (labels.length === 0) return 'change'
  if (labels.length === 1) return `${labels[0]} change`
  const head = labels.slice(0, -1).join(', ')
  return `${head} and ${labels[labels.length - 1]} changes`
}

/**
 * Where the newest journal entry points. The synthetic standalone-task
 * notebook is hidden everywhere else in the UI, so it must not leak here.
 */
export function undoPageLabel(
  notebook: string,
  section: string,
  page: string
): string {
  if (notebook === STANDALONE_TASKS_NOTEBOOK) return 'your standalone tasks'
  const parts = [notebook, section, page].filter(Boolean)
  return parts.length > 0 ? parts.join(' › ') : 'your vault'
}

/** Honest depth disclosure: silence about remaining entries reads as "done". */
export function undoRemainingClause(remaining: number): string {
  if (remaining <= 0) return 'No more changes to undo.'
  if (remaining === 1) return '1 change left.'
  return `${remaining} changes left.`
}

export interface UndoResultCopy {
  undone: boolean
  skipped: boolean
  changedFields: readonly TaskChangedField[]
  notebook: string
  section: string
  page: string
  remaining: number
}

/**
 * Announcement after an undo attempt. A skip means the entry was dropped
 * (vanished target, permanent fault, exhausted retry budget) — not that the
 * journal is empty — so the remaining count still speaks for itself.
 */
export function undoResultAnnouncement(result: UndoResultCopy): string {
  if (result.undone) {
    const fields = describeChangedFields(result.changedFields)
    const page = undoPageLabel(result.notebook, result.section, result.page)
    return `Undid ${fields} in ${page}. ${undoRemainingClause(result.remaining)}`
  }
  if (result.skipped) {
    return `Undo skipped — that change couldn't be restored and was dropped from the undo history. ${undoRemainingClause(
      result.remaining
    )}`
  }
  return 'Nothing to undo.'
}

export interface UndoButtonCopy {
  canUndo: boolean
  pending: boolean
  writePending: boolean
  remaining: number
  changedFields: readonly TaskChangedField[]
  pageLabel: string
}

/**
 * Accessible name for the undo affordance. It always describes the newest
 * journal entry (undo is global LIFO, not scoped to the gesture the user just
 * made) and spells out the remaining depth so a multi-file reorder's extra
 * entries are not a surprise.
 */
export function undoButtonLabel(copy: UndoButtonCopy): string {
  if (copy.pending) return 'Undoing last change…'
  if (copy.writePending) return 'Undo unavailable while a change is saving'
  if (!copy.canUndo) return 'Nothing to undo'
  const fields = describeChangedFields(copy.changedFields)
  const left = copy.remaining > 1 ? ` — ${copy.remaining} changes left` : ''
  return `Undo ${fields} in ${copy.pageLabel}${left}`
}
