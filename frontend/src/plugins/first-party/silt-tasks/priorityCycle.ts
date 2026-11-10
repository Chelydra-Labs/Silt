// Card-pill priority cycling: the single write path shared by the Board and
// List surfaces so the undo-journal wrap and the confirmation toast (with its
// Undo affordance) stay identical wherever the pill renders.
import type { PluginContext } from '../../sdk'
import { pushNotification } from '../../../notifications/store.svelte'
import { friendlyCaughtError } from './errors'
import { priorityLabel, type TaskDetail } from './types'
import { runTaskUndo, trackTaskUndoWrite } from './undo.svelte'

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
  pushNotification({
    kind: 'success',
    message: `Task priority set to ${priorityLabel(next)}`,
    action: { label: 'Undo', run: () => void runTaskUndo() }
  })
}
