// Single source of truth for per-dimension task dispatch.
//
// Both the Board DnD engine (useBoardDnd) and the Board per-column quick-add
// (BoardView.quickAddFor) route a groupBy dimension to the SDK setter that
// owns the binned field. Collapsing the parallel `switch (groupBy)` ladders
// that previously lived in both files into one Record keeps the
// dimension→setter mapping in exactly one place, so a new dimension or a
// setter change lands once and both consumers stay consistent.
//
// The five DnD-enabled dimensions (status/owner/priority/dueDate/tag) carry
// real setters + move announcements + (where supported) a create callback.
// 'none' and the location dimensions (notebook/section/page) are no-ops here:
// DnD is disabled for them (a task's file location is not mutable metadata),
// so their entries exist only to satisfy the Record<GroupBy, …> exhaustiveness.

import type { PluginContext, TaskStatus } from '../../sdk'
import { plusDaysISO } from '../../sdk'
import type { GroupBy } from './state.svelte'
import type { TaskDetail } from './types'

// The slice of a rendered column the dispatch reads. Declared structurally so
// this module does not depend on the Board's Lane type (avoids a circular
// import with useBoardDnd); both Lane and any future consumer that exposes
// `{ value, label }` satisfy it.
export interface DispatchCol {
  value: string
  label: string
}

// Deterministic anchor per due-date bucket so a drop (or quick-add) produces a
// stable date the next query re-bins into the same column. Overdue/today → the
// local day nudges an already-late task forward; upcoming/later pick a finite
// horizon rather than leaving the task undated.
export function dueDateAnchor(bucketKey: string, iso: string): string {
  switch (bucketKey) {
    case 'overdue':
    case 'today':
      return iso
    case 'upcoming':
      return plusDaysISO(iso, 3)
    case 'later':
      return plusDaysISO(iso, 30)
    case 'undated':
    default:
      return ''
  }
}

// Multi-membership: a task can carry several tags. Dropping into a tag column
// ADDS that tag without removing the others. Exported because the DnD engine's
// optimistic update reads the same union so the in-memory patch matches the
// persist.
export function unionTags(card: TaskDetail, tag: string): string[] {
  const existing = (card.tags ?? '')
    .split('|')
    .map((t) => t.trim())
    .filter(Boolean)
  return existing.includes(tag) ? existing : [...existing, tag]
}

export interface DimensionDispatch {
  /** Persist a card dropped/moved into a column of this dimension. */
  setter: (
    ctx: PluginContext,
    card: TaskDetail,
    toCol: DispatchCol,
    today: string
  ) => Promise<unknown>
  /** a11y live-region message after a move into `toCol`. */
  moveLabel: (toCol: DispatchCol) => string
  /** Set this dimension on a freshly-created task (per-column quick-add). */
  onCreate?: (ctx: PluginContext, id: string, value: string) => void
}

// Dimensions whose value is immutable file location, plus the explicit "no
// grouping" mode. DnD is disabled for these, so the setter/label are inert —
// they exist only to make the Record exhaustive.
const inertDispatch: DimensionDispatch = {
  setter: () => Promise.resolve(true),
  moveLabel: () => 'Task moved'
}

export const groupByDispatch: Record<GroupBy, DimensionDispatch> = {
  status: {
    setter: (ctx, card, toCol) =>
      ctx.updateBlockState(card.id, toCol.value as TaskStatus),
    moveLabel: (toCol) => `Task moved to ${toCol.label}`
    // No onCreate: the per-column quick-add seeds the `status` field directly
    // (and runs the board-specific WIP guard), not a metadata setter.
  },
  owner: {
    // '' clears the owner (the Unassigned column).
    setter: (ctx, card, toCol) => ctx.setTaskOwner(card.id, toCol.value),
    moveLabel: (toCol) => `Task reassigned to ${toCol.label}`,
    onCreate: (ctx, id, value) => void ctx.setTaskOwner(id, value)
  },
  priority: {
    setter: (ctx, card, toCol) =>
      ctx.setTaskPriority(card.id, Number(toCol.value)),
    moveLabel: (toCol) => `Task priority set to ${toCol.label}`,
    onCreate: (ctx, id, value) => void ctx.setTaskPriority(id, Number(value))
  },
  dueDate: {
    setter: (ctx, card, toCol, today) =>
      ctx.setTaskDueDate(card.id, dueDateAnchor(toCol.value, today)),
    moveLabel: (toCol) => `Task due date set to ${toCol.label}`
    // No onCreate: the quick-add seeds the `dueDate` field via dueDateAnchor,
    // not a metadata setter.
  },
  tag: {
    // No-Tag column (value='') is a no-op on drop — we don't strip tags.
    setter: (ctx, card, toCol) =>
      toCol.value
        ? ctx.setTaskTags(card.id, unionTags(card, toCol.value))
        : Promise.resolve(true),
    moveLabel: (toCol) =>
      toCol.value ? `Tag ${toCol.label} added` : 'No change',
    // A freshly-created task has no existing tags, so the create path seeds a
    // single-element set (NOT the union the drop path uses).
    onCreate: (ctx, id, value) => {
      if (value) void ctx.setTaskTags(id, [value])
    }
  },
  none: inertDispatch,
  notebook: inertDispatch,
  section: inertDispatch,
  page: inertDispatch
}
