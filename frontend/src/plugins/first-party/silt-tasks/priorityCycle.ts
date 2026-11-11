// Card-pill priority cycling: the single write path shared by the Board and
// List surfaces so the undo-journal wrap and the confirmation toast (with its
// Undo affordance) stay identical wherever the pill renders.
import type { PluginContext } from '../../sdk'
import { pushNotification } from '../../../notifications/store.svelte'
import { friendlyCaughtError } from './errors'
import { priorityLabel, type TaskDetail } from './types'
import {
  getTasksUndoView,
  refreshTasksUndo,
  runTaskUndo,
  trackTaskUndoWrite
} from './undo.svelte'

export async function applyPriorityCycle(
  ctx: PluginContext,
  task: TaskDetail,
  next: number
): Promise<void> {
  try {
    await trackTaskUndoWrite(ctx.setTaskPriority(task.id, next))
  } catch (e) {
    pushNotification({
      kind: 'error',
      message: `Couldn't set priority: ${friendlyCaughtError(e)}`
    })
    // Re-throw so the pill's optimistic value reverts alongside the toast.
    throw e
  }
  // Capture this write's journal identity now: the toast outlives the write,
  // and runTaskUndo() is global LIFO — unguarded, its Undo would reverse
  // whatever mutation is newest by click time, not the one this toast names.
  await refreshTasksUndo()
  const head = getTasksUndoView()
  const headMatched =
    head.changedFields.includes('priority') && head.blockIds.includes(task.id)
  pushNotification({
    kind: 'success',
    message: `Task priority set to ${priorityLabel(next)}`,
    action: {
      label: 'Undo',
      run: () => guardedUndo(task.id, headMatched ? head.remaining : null)
    }
  })
}

/** Undo this toast's write only while it is still the journal head. */
async function guardedUndo(
  taskId: string,
  capturedDepth: number | null
): Promise<void> {
  await refreshTasksUndo()
  const head = getTasksUndoView()
  // The depth seal settles what fields+blocks cannot: a stacked re-cycle of
  // the same task wears the same head disclosure but sits deeper in the
  // journal, so an older toast must not undo it. A null capture (the
  // toast-time peek failed) degrades to the fields+blocks check alone.
  const stillThisWrite =
    head.changedFields.includes('priority') &&
    head.blockIds.includes(taskId) &&
    (capturedDepth === null || head.remaining === capturedDepth)
  if (!stillThisWrite) {
    pushNotification({
      kind: 'info',
      message: 'Undo skipped — a newer change was made'
    })
    return
  }
  await runTaskUndo()
}
