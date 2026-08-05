<script lang="ts">
  import { fly } from 'svelte/transition'
  import { tick } from 'svelte'
  import type { PluginContext } from '../../../sdk'
  import { STANDALONE_TASKS_NOTEBOOK } from '../../../../lib/standaloneTasksNav'

  import type { TaskDetail } from '../types'
  import { PRIORITY_LABELS, priorityClass } from '../types'
  import TaskMetadataSidebar from './TaskMetadataSidebar.svelte'

  /**
   * Shared task inspector/edit drawer — the single metadata surface for every
   * task-editing view (Tasks list, Kanban board, future Calendar/Agenda).
   *
   * NON-BLOCKING inspector pane (GitLab Pajamas / NNGroup pattern): no scrim,
   * `aria-modal="false"`, focus moves to the panel on open + restores on
   * close, but is NOT trapped — the user can click/Tab back to the host list
   * to switch tasks. This is what removes the single/double-click
   * disambiguation the Kanban board used to need.
   *
   * All metadata controls (title, status, due date, pin, progress, estimate,
   * recurrence, owner, priority, timestamps, tags, dependencies, comments)
   * live in TaskMetadataSidebar, shared with TaskSubEditorModal (#780). This
   * component owns only the drawer chrome + focus lifecycle + the "Open
   * sub-editor" / "Open source page" affordances.
   */
  interface Props {
    /** The task to inspect/edit, or null when the drawer is closed. */
    task: TaskDetail | null
    ctx: PluginContext
    onClose: () => void
    /** Called after a successful metadata write so the host can re-query. */
    onMetaChanged?: () => void
    /** When provided, an "Open sub-editor" button renders and calls this. */
    onOpenSubEditor?: () => void
  }

  let { task, ctx, onClose, onMetaChanged, onOpenSubEditor }: Props = $props()

  // Source awareness: standalone (.silt) tasks have no source page, so the
  // breadcrumb shows a friendly label and "Open source page" is hidden.
  let isStandalone = $derived(
    !!task && task.notebook === STANDALONE_TASKS_NOTEBOOK
  )

  // Focus management (non-blocking): move focus into the panel on open and
  // restore it to the trigger on close. NOT trapped — the host list stays
  // interactive so the user can click another task to switch.
  let panelRef = $state<HTMLDivElement | null>(null)
  let previouslyFocused: HTMLElement | null = null
  let drawerOpen = false
  let lastTaskId = ''
  $effect(() => {
    const tid = task?.id ?? ''
    if (task && !drawerOpen) {
      drawerOpen = true
      previouslyFocused = document.activeElement as HTMLElement
      void tick().then(() => panelRef?.focus())
    } else if (task && drawerOpen && tid !== lastTaskId) {
      const active = document.activeElement as HTMLElement | null
      if (active && !panelRef?.contains(active)) previouslyFocused = active
    } else if (!task && drawerOpen) {
      drawerOpen = false
      if (previouslyFocused?.isConnected) {
        previouslyFocused.focus?.()
      }
      previouslyFocused = null
    }
    lastTaskId = tid
  })

  // Mirrors whether the sidebar has a popover/dialog open so the Esc handler
  // can avoid closing mid-interaction.
  let sidebarBusy = $state(false)

  function openSourcePage() {
    if (!task) return
    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: {
          notebook: task.notebook,
          source: task.source,
          section: task.section,
          page: task.page,
          date: task.file_date,
          blockId: task.id
        }
      })
    )
  }

  function onWindowKeydown(e: KeyboardEvent) {
    // Don't close on Escape while a Popover (recurrence/due-date), the
    // BlockedDoneDialog, or the comment reply composer is open — those
    // consume Escape first (the popovers stopPropagation; the sidebar's
    // `busy` flag covers the BlockedDoneDialog which does not).
    if (e.key === 'Escape' && task && !sidebarBusy) {
      const active = document.activeElement as HTMLElement | null
      if (active?.closest?.('[data-testid="reply-composer"]')) return
      e.preventDefault()
      onClose()
    }
  }

  // Esc-to-close listener is bound only while the drawer is open.
  $effect(() => {
    if (!task) return
    window.addEventListener('keydown', onWindowKeydown)
    return () => window.removeEventListener('keydown', onWindowKeydown)
  })
</script>

{#if task}
  <div
    bind:this={panelRef}
    transition:fly={{ x: 320, duration: 200 }}
    class="fixed right-0 top-12 h-[calc(100vh-48px)] w-96 bg-surface-card border-l border-surface-card-border z-40 overflow-y-auto custom-scrollbar focus:outline-none shadow-2xl"
    role="dialog"
    aria-modal="false"
    aria-labelledby="task-edit-drawer-title"
    tabindex="-1"
  >
    <!-- Header -->
    <div
      class="flex items-start justify-between gap-2 px-5 py-4 border-b border-surface-card-border sticky top-0 bg-surface-card"
    >
      <div class="flex flex-col gap-1.5 min-w-0 flex-1">
        {#if task.priority >= 1 && task.priority <= 3}
          <span
            class="self-start px-1.5 py-0.5 border rounded-sm font-label-sm text-type-3xs uppercase tracking-wide w-fit {priorityClass(
              task.priority
            )}"
          >
            {PRIORITY_LABELS[task.priority] ?? 'Normal'}
          </span>
        {/if}
        <h2
          id="task-edit-drawer-title"
          class="font-headline-md text-headline-md text-text-primary break-words flex items-start gap-1"
        >
          {#if task.recurrence}
            <span
              class="material-symbols-outlined text-icon-md text-accent-secondary-start shrink-0 mt-1"
              aria-hidden="true"
              title="Recurring: {task.recurrence}">event_repeat</span
            >
          {/if}
          <span class="break-words">{task.clean_content}</span>
        </h2>
      </div>
      <button
        type="button"
        onclick={onClose}
        aria-label="Close detail panel"
        class="text-text-muted hover:text-text-primary transition-colors shrink-0"
      >
        <span class="material-symbols-outlined">close</span>
      </button>
    </div>

    <div class="px-5 py-4 space-y-6">
      <TaskMetadataSidebar
        {task}
        {ctx}
        {onMetaChanged}
        bind:busy={sidebarBusy}
      />

      {#if onOpenSubEditor}
        <section>
          <button
            type="button"
            onclick={onOpenSubEditor}
            class="w-full flex items-center justify-center gap-2 px-3 py-2 rounded border border-surface-card-border bg-surface-card text-text-primary hover:bg-hover transition-all font-label-sm-bold"
          >
            <span class="material-symbols-outlined text-icon-md">edit_note</span
            >
            Open sub-editor
          </button>
        </section>
      {/if}

      {#if !isStandalone}
        <section>
          <button
            type="button"
            onclick={openSourcePage}
            class="w-full flex items-center justify-center gap-2 px-3 py-2 rounded border border-accent-primary-start/30 bg-accent-primary-glow text-accent-primary-start hover:brightness-110 transition-all font-label-sm-bold"
          >
            <span class="material-symbols-outlined text-icon-md"
              >open_in_new</span
            >
            Open source page
          </button>
        </section>
      {/if}

      <!-- Source breadcrumb — omitted for standalone tasks (no source page). -->
      {#if !isStandalone}
        <section class="pt-2 border-t border-surface-card-border">
          <p class="text-type-2xs font-label-sm text-text-muted break-all">
            {task.notebook} › {task.section} › {task.page}
          </p>
        </section>
      {/if}
    </div>
  </div>
{/if}
