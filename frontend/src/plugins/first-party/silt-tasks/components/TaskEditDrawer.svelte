<script lang="ts">
  import { fly } from 'svelte/transition'
  import { tick } from 'svelte'
  import type { PluginContext } from '../../../sdk'
  import { STANDALONE_TASKS_NOTEBOOK } from '../../../../lib/standaloneTasksNav'

  import type { TaskDetail } from '../types'
  import TaskMetadataSidebar from './TaskMetadataSidebar.svelte'
  import { motionDuration } from '../motion'

  /**
   * Shared task inspector/edit drawer — the single metadata surface for every
   * task-editing view (Tasks list, Kanban board, Calendar/Agenda).
   *
   * Two presentation variants:
   * - overlay (default): fixed right pane, light-dismiss, focus into panel on open
   * - pane: in-flow list split; region semantics, no light-dismiss, no autofocus
   *
   * All metadata controls live in TaskMetadataSidebar. This component owns
   * chrome, focus lifecycle, and host affordances (sub-editor / source page).
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
    /** overlay = fixed drawer; pane = in-flow list inspector. */
    variant?: 'overlay' | 'pane'
    /** Selected task no longer appears in the host filtered set. */
    filteredOut?: boolean
    onPrevTask?: () => void
    onNextTask?: () => void
    hasPrevTask?: boolean
    hasNextTask?: boolean
    /**
     * Mirrors sidebar popover/dialog busy upward so list J/K can pause while
     * a nested control is open (same signal Esc already uses).
     */
    busy?: boolean
  }

  let {
    task,
    ctx,
    onClose,
    onMetaChanged,
    onOpenSubEditor,
    variant = 'overlay',
    filteredOut = false,
    onPrevTask,
    onNextTask,
    hasPrevTask = false,
    hasNextTask = false,
    // eslint-disable-next-line no-useless-assignment
    busy = $bindable(false)
  }: Props = $props()

  let isStandalone = $derived(
    !!task && task.notebook === STANDALONE_TASKS_NOTEBOOK
  )
  let isPane = $derived(variant === 'pane')
  let showTraversal = $derived(!!onPrevTask || !!onNextTask)

  // Focus management: overlay moves focus into the panel on open; pane does
  // not (list keyboard triage stays on the master). Both restore on close.
  let panelRef = $state<HTMLDivElement | null>(null)
  let scrollBodyRef = $state<HTMLDivElement | null>(null)
  let previouslyFocused: HTMLElement | null = null
  let drawerOpen = false
  let lastTaskId = ''
  $effect(() => {
    const tid = task?.id ?? ''
    if (task && !drawerOpen) {
      drawerOpen = true
      previouslyFocused = document.activeElement as HTMLElement
      if (!isPane) {
        void tick().then(() => panelRef?.focus())
      }
    } else if (task && drawerOpen && tid !== lastTaskId) {
      const active = document.activeElement as HTMLElement | null
      if (active && !panelRef?.contains(active)) previouslyFocused = active
      // Switching tasks mid-scroll strands the reader; snap body to header.
      // Direct scrollTop so jsdom tests can observe the reset.
      if (scrollBodyRef) scrollBodyRef.scrollTop = 0
    } else if (!task && drawerOpen) {
      drawerOpen = false
      if (previouslyFocused?.isConnected) {
        previouslyFocused.focus?.()
      }
      previouslyFocused = null
    }
    lastTaskId = tid
  })

  let sidebarBusy = $state(false)
  $effect(() => {
    busy = sidebarBusy
  })

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
    if (e.key === 'Escape' && task && !sidebarBusy) {
      const active = document.activeElement as HTMLElement | null
      if (active?.closest?.('[data-testid="reply-composer"]')) return
      e.preventDefault()
      onClose()
    }
  }

  function onDocumentMouseDown(e: MouseEvent) {
    if (!task || sidebarBusy || isPane) return
    const target = e.target
    if (!(target instanceof Node) || panelRef?.contains(target)) return
    if (target instanceof Element) {
      // Host hit-targets swap selection; do not flash-close the inspector.
      if (target.closest('[data-task-hit]')) return
      if (target.closest('[role="dialog"], [role="listbox"], [role="menu"]'))
        return
    }
    onClose()
  }

  $effect(() => {
    if (!task) return
    window.addEventListener('keydown', onWindowKeydown)
    if (!isPane) {
      document.addEventListener('mousedown', onDocumentMouseDown, true)
    }
    return () => {
      window.removeEventListener('keydown', onWindowKeydown)
      document.removeEventListener('mousedown', onDocumentMouseDown, true)
    }
  })
</script>

{#snippet drawerBody()}
  {#if filteredOut}
    <div
      class="flex-shrink-0 border-b border-surface-card-border bg-surface-panel px-5 py-2 text-type-xs font-label-sm text-text-muted"
      data-testid="task-filtered-out-banner"
      role="status"
    >
      Hidden by current filters — edits still save
    </div>
  {/if}

  {#if showTraversal}
    <div
      class="flex flex-shrink-0 items-center justify-between gap-2 border-b border-surface-card-border px-3 py-1.5"
      data-testid="task-inspector-traversal"
    >
      <button
        type="button"
        class="inline-flex items-center gap-1 rounded px-2 py-1 text-type-xs font-label-sm text-text-muted transition-colors hover:bg-hover hover:text-text-primary disabled:cursor-not-allowed disabled:opacity-40"
        aria-label="Previous task"
        title="Previous task (K)"
        aria-keyshortcuts="K"
        disabled={!hasPrevTask}
        onclick={() => onPrevTask?.()}
      >
        <span class="material-symbols-outlined text-icon-sm" aria-hidden="true"
          >chevron_left</span
        >
        Previous
      </button>
      <button
        type="button"
        class="inline-flex items-center gap-1 rounded px-2 py-1 text-type-xs font-label-sm text-text-muted transition-colors hover:bg-hover hover:text-text-primary disabled:cursor-not-allowed disabled:opacity-40"
        aria-label="Next task"
        title="Next task (J)"
        aria-keyshortcuts="J"
        disabled={!hasNextTask}
        onclick={() => onNextTask?.()}
      >
        Next
        <span class="material-symbols-outlined text-icon-sm" aria-hidden="true"
          >chevron_right</span
        >
      </button>
    </div>
  {/if}

  <div
    bind:this={scrollBodyRef}
    class="min-h-0 flex-1 overflow-y-auto custom-scrollbar"
    data-testid="task-edit-drawer-scroll"
  >
    {#if task}
      <!-- Remount on task identity change so open popovers / blocked-done UI
           cannot stick to the newly selected task. -->
      {#key task.id}
        <TaskMetadataSidebar
          {task}
          {ctx}
          {onMetaChanged}
          {onClose}
          stickyPrimary
          commentLayout="drawer"
          headingId="task-edit-drawer-title"
          bind:busy={sidebarBusy}
        />
      {/key}
    {/if}
  </div>

  <div
    class="flex-shrink-0 space-y-3 border-t border-surface-card-border px-5 py-4"
  >
    {#if onOpenSubEditor}
      <section>
        <button
          type="button"
          onclick={onOpenSubEditor}
          class="w-full flex items-center justify-center gap-2 px-3 py-2 rounded border border-surface-card-border bg-surface-card text-text-primary hover:bg-hover transition-all font-label-sm-bold"
        >
          <span class="material-symbols-outlined text-icon-md">edit_note</span>
          Open sub-editor
        </button>
      </section>
    {/if}

    {#if task && !isStandalone}
      <section>
        <button
          type="button"
          onclick={openSourcePage}
          class="w-full flex items-center justify-center gap-2 px-3 py-2 rounded border border-accent-primary-start/30 bg-accent-primary-glow text-accent-primary-start hover:brightness-110 transition-all font-label-sm-bold"
        >
          <span class="material-symbols-outlined text-icon-md">open_in_new</span
          >
          Open source page
        </button>
      </section>
      <section class="pt-2 border-t border-surface-card-border">
        <p class="text-type-2xs font-label-sm text-text-muted break-all">
          {task.notebook} › {task.section} › {task.page}
        </p>
      </section>
    {/if}
  </div>
{/snippet}

{#if task}
  {#if isPane}
    <!-- In-flow pane: no enter/exit transition (list stays interactive). -->
    <div
      bind:this={panelRef}
      class="flex h-full w-full flex-col overflow-hidden border-l border-surface-card-border bg-surface-card shadow-none focus:outline-none"
      role="region"
      aria-labelledby="task-edit-drawer-title"
      aria-keyshortcuts={showTraversal ? 'J K' : undefined}
      tabindex="-1"
      data-testid="task-edit-drawer"
      data-variant="pane"
    >
      {@render drawerBody()}
    </div>
  {:else}
    <div
      bind:this={panelRef}
      transition:fly={{ x: 320, duration: motionDuration(200) }}
      class="fixed right-0 top-12 z-40 flex h-[calc(100vh-48px)] w-full flex-col overflow-hidden border-l border-surface-card-border bg-surface-card shadow-2xl focus:outline-none sm:w-[480px] lg:w-[540px] lg:max-w-xl"
      role="dialog"
      aria-modal="false"
      aria-labelledby="task-edit-drawer-title"
      aria-keyshortcuts={showTraversal ? 'J K' : undefined}
      tabindex="-1"
      data-testid="task-edit-drawer"
      data-variant="overlay"
    >
      {@render drawerBody()}
    </div>
  {/if}
{/if}
