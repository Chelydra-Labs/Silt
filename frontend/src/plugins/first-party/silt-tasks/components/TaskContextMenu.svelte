<script lang="ts">
  import ContextMenu from '../../../../components/ContextMenu.svelte'
  import ConfirmModal from './ConfirmModal.svelte'
  import BlockedDoneGuard from './BlockedDoneGuard.svelte'
  import ErrorBanner from './ErrorBanner.svelte'
  import { buildDueDatePresets } from './dueDatePresets'
  import { friendlyCaughtError } from '../errors'
  import { useBlockedDoneGuard } from '../shared.svelte'
  import { trackTaskUndoWrite } from '../undo.svelte'
  import { PRIORITY_LABELS, type TaskDetail } from '../types'
  import { getTaskWeekStart } from '../../../../lib/taskWeekStart.svelte'
  import { onDestroy } from 'svelte'
  import type { PluginContext, TaskStatus } from '../../../sdk'
  import {
    neighborTaskHit,
    rescueFocusAfterWrite,
    type TaskContextMenuState
  } from './useTaskContextMenu.svelte'

  /**
   * Shared task context menu (#1116) — one instance hosted per view (List
   * rows, Board cards, Calendar chips). Every mutation goes through the
   * PluginContext SDK exactly as TaskMetadataSidebar does and is wrapped in
   * trackTaskUndoWrite so the hub's undo affordance tracks the write. The
   * host keeps ownership of navigation state: sub-editor opens are pre-bound
   * callbacks, so this component stays stateless about views.
   */
  interface Props {
    ctx: PluginContext
    /** Headless menu state from useTaskContextMenu; null = closed. */
    menu: TaskContextMenuState | null
    onClose: () => void
    /** Shift+Enter parity: open the host's sub-editor for the task. */
    onOpenSubEditor: (task: TaskDetail) => void
    /**
     * Write-failure reporting into the host's top error strip. Called with
     * the full message on failure and with '' after the 8s auto-dismiss
     * (markDownError parity). Without it the banner renders in-flow here.
     */
    onError?: (message: string) => void
  }

  let { ctx, menu, onClose, onOpenSubEditor, onError }: Props = $props()

  let writeError = $state('')
  let writeErrorTimer: ReturnType<typeof setTimeout> | null = null
  // Delete is two-step on purpose (issue: no silent delete) — the ConfirmModal
  // target outlives the menu close so the dialog opens after dismissal.
  let deleteTarget = $state<TaskDetail | null>(null)
  // Anchor snapshot for writes that confirm later than the menu click
  // (blocked-DONE dialog, delete modal) — set on every activation.
  let lastAnchor: HTMLElement | null = null

  onDestroy(() => {
    if (writeErrorTimer) clearTimeout(writeErrorTimer)
  })

  function reportWriteError(e: unknown): void {
    const message = `Couldn't update task: ${friendlyCaughtError(e)}`
    writeError = message
    onError?.(message)
    if (writeErrorTimer) clearTimeout(writeErrorTimer)
    writeErrorTimer = setTimeout(() => {
      writeError = ''
      onError?.('')
      writeErrorTimer = null
    }, 8_000)
  }

  // ctx is a stable plugin-context singleton — see BoardView for rationale.
  // svelte-ignore state_referenced_locally
  const blockedGuard = useBlockedDoneGuard<TaskDetail>(ctx, (e) => {
    reportWriteError(e)
  })

  let weekStart = $derived(getTaskWeekStart())
  // ctx.today is a plain-object getter — untracked inside $derived — and the
  // view hosts mount this component once for their lifetime, unlike the
  // drawer which remounts per open. Keying on `menu` rebuilds the presets on
  // every open so crossing local midnight never offers yesterday as "Today".
  let duePresets = $derived(
    menu ? buildDueDatePresets(ctx.today, weekStart) : []
  )

  const PRIORITIES = [1, 2, 3]

  /** Snapshot the menu's anchor before onClose() clears the menu state. */
  function claimAnchor(): HTMLElement | null {
    lastAnchor = menu?.anchorEl ?? null
    return lastAnchor
  }

  /**
   * Fire-and-forget journaled write. Failures surface in the error strip; on
   * success the focus rescue keeps keyboard focus inside the view when the
   * debounced block:changed reload removes the anchored row/card/chip (the
   * hub's Ctrl+Z scope dies with a fall to <body>).
   */
  function runWrite(
    op: () => Promise<unknown>,
    anchor: HTMLElement | null
  ): void {
    writeError = ''
    const neighbor = anchor ? neighborTaskHit(anchor) : null
    void trackTaskUndoWrite(op())
      .then(() => {
        if (anchor) rescueFocusAfterWrite(anchor, neighbor)
      })
      .catch((e) => {
        reportWriteError(e)
      })
  }

  async function applyStatus(
    task: TaskDetail,
    next: TaskStatus
  ): Promise<void> {
    const anchor = claimAnchor()
    onClose()
    // DONE-on-blocked parity with the row checkbox / drawer status control:
    // blocked tasks confirm through the dialog before the write lands.
    if (next === 'DONE' && task.is_blocked) {
      const result = await blockedGuard.check(task.id, task.is_blocked, task)
      if (result !== 'clear') return
    }
    runWrite(() => ctx.updateBlockState(task.id, next), anchor)
  }

  // Item handlers take the task as a parameter: reading the template's
  // {@const task} after onClose() nulls `menu` would re-evaluate menu.task
  // on a closed menu — capturing by value keeps every activation safe.

  function togglePin(task: TaskDetail): void {
    const anchor = claimAnchor()
    onClose()
    runWrite(
      () => ctx.updateTaskMeta(task.id, { pinned: !task.pinned }),
      anchor
    )
  }

  function setDueDate(task: TaskDetail, ymd: string): void {
    const anchor = claimAnchor()
    onClose()
    runWrite(() => ctx.setTaskDueDate(task.id, ymd), anchor)
  }

  function setPriority(task: TaskDetail, p: number): void {
    const anchor = claimAnchor()
    onClose()
    runWrite(() => ctx.setTaskPriority(task.id, p), anchor)
  }

  function openSubEditorFor(task: TaskDetail): void {
    onClose()
    onOpenSubEditor(task)
  }

  function requestDelete(task: TaskDetail): void {
    claimAnchor()
    onClose()
    deleteTarget = task
  }

  function confirmBlockedDone(): void {
    const pending = blockedGuard.pending
    blockedGuard.dismiss()
    if (pending)
      runWrite(
        () => ctx.updateBlockState(pending.context.id, 'DONE'),
        lastAnchor
      )
  }

  function confirmDelete(): void {
    const task = deleteTarget
    deleteTarget = null
    if (!task) return
    // ctx.deleteBlock is NOT journaled by the Go undo surface (applyBlocksOps
    // never calls recordTaskUndo); tracking still gates the undo affordance
    // for the write's duration. Noted in the PR body for #1116.
    runWrite(() => ctx.deleteBlock(task.id), lastAnchor)
  }
</script>

{#if menu}
  {@const task = menu.task}
  <ContextMenu
    open={true}
    anchor={{ x: menu.x, y: menu.y }}
    anchorEl={menu.anchorEl}
    {onClose}
    ariaLabel={`Actions for ${task.clean_content}`}
    backdropTestId="task-menu-backdrop"
    menuTestId="task-context-menu"
  >
    <button
      type="button"
      role="menuitem"
      onclick={() =>
        void applyStatus(task, task.status === 'DONE' ? 'TODO' : 'DONE')}
      data-testid="task-menu-toggle-status"
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >{task.status === 'DONE' ? 'restart_alt' : 'check_circle'}</span
      >
      <span>{task.status === 'DONE' ? 'Reopen' : 'Mark done'}</span>
    </button>
    {#if task.status !== 'DONE'}
      <!-- PinToggle parity: DONE cards suppress the pin affordance. -->
      <button
        type="button"
        role="menuitem"
        onclick={() => togglePin(task)}
        data-testid="task-menu-pin"
      >
        <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
          >push_pin</span
        >
        <span>{task.pinned ? 'Unpin' : 'Pin'}</span>
      </button>
    {/if}
    <button
      type="button"
      role="menuitem"
      aria-haspopup="menu"
      data-testid="task-menu-due"
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >event</span
      >
      <span>Due date</span>
    </button>
    <div role="menu" aria-label="Due date presets">
      {#each duePresets as preset (preset.value)}
        <button
          type="button"
          role="menuitem"
          onclick={() => setDueDate(task, preset.value)}
          data-testid={`task-menu-due-${preset.value}`}
        >
          <span class="w-4 shrink-0" aria-hidden="true"
            >{#if task.due_date === preset.value}<span
                class="material-symbols-outlined text-icon-md">check</span
              >{/if}</span
          >
          <span>{preset.label}</span>
          <span class="ml-auto text-type-2xs text-text-muted"
            >{preset.value}</span
          >
          {#if task.due_date === preset.value}
            <span class="sr-only">(current)</span>
          {/if}
        </button>
      {/each}
      {#if task.due_date}
        <button
          type="button"
          role="menuitem"
          onclick={() => setDueDate(task, '')}
          data-testid="task-menu-due-clear"
        >
          <span class="w-4 shrink-0" aria-hidden="true"></span>
          <span>Clear due date</span>
        </button>
      {/if}
    </div>
    <button
      type="button"
      role="menuitem"
      aria-haspopup="menu"
      data-testid="task-menu-priority"
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >flag</span
      >
      <span>Priority</span>
    </button>
    <div role="menu" aria-label="Priority">
      {#each PRIORITIES as p (p)}
        <button
          type="button"
          role="menuitem"
          onclick={() => setPriority(task, p)}
          data-testid={`task-menu-priority-${p}`}
        >
          <span class="w-4 shrink-0" aria-hidden="true"
            >{#if task.priority === p}<span
                class="material-symbols-outlined text-icon-md">check</span
              >{/if}</span
          >
          <span>{PRIORITY_LABELS[p]}</span>
          {#if task.priority === p}
            <span class="sr-only">(current)</span>
          {/if}
        </button>
      {/each}
    </div>
    <div class="context-menu-separator" aria-hidden="true"></div>
    <button
      type="button"
      role="menuitem"
      onclick={() => openSubEditorFor(task)}
      data-testid="task-menu-sub-editor"
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >edit_note</span
      >
      <span>Open in sub-editor</span>
    </button>
    <button
      type="button"
      role="menuitem"
      class="text-status-danger"
      onclick={() => requestDelete(task)}
      data-testid="task-menu-delete"
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >delete</span
      >
      <span>Delete task</span>
    </button>
  </ContextMenu>
{/if}

{#if writeError && !onError}
  <!-- Fallback for hosts that don't wire onError; the views hoist this into
       their top error strip with the 8s auto-dismiss parity. -->
  <ErrorBanner
    message={writeError}
    dataTestId="task-menu-error"
    onDismiss={() => (writeError = '')}
  />
{/if}

<BlockedDoneGuard
  pending={blockedGuard.pending}
  cardText={blockedGuard.pending?.context.clean_content}
  onConfirm={confirmBlockedDone}
  onCancel={() => blockedGuard.dismiss()}
/>

{#if deleteTarget}
  <ConfirmModal
    title="Delete task?"
    message={`Delete “${deleteTarget.clean_content}”? The task will be removed from its page.`}
    confirmLabel="Delete"
    cancelLabel="Cancel"
    destructive={true}
    dataTestId="task-menu-delete-confirm"
    onConfirm={confirmDelete}
    onCancel={() => (deleteTarget = null)}
  />
{/if}
