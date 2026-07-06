<script lang="ts">
  import { fly } from 'svelte/transition'
  import { tick } from 'svelte'
  import type { PluginContext, TaskStatus } from '../../sdk'
  import { plusDaysISO } from '../../sdk'
  import type { TaskDetail } from './types'
  import { PRIORITY_LABELS, laneLabel, priorityClass } from './types'
  import DependencyPicker from './DependencyPicker.svelte'
  import BlockedDoneDialog from './BlockedDoneDialog.svelte'
  import Popover from '../../../components/Popover.svelte'
  import { STANDALONE_TASKS_NOTEBOOK } from '../../../lib/standaloneTasksNav'

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
   * Editable inline: pin, progress, recurrence, due date, status. The DONE
   * transition routes through the shared BlockedDoneDialog so a blocked task
   * confirms first, regardless of which surface opened the drawer.
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
  $effect(() => {
    if (task) {
      previouslyFocused = document.activeElement as HTMLElement
      // tabindex=-1 lets the panel receive focus without joining the tab order.
      tick().then(() => panelRef?.focus())
    } else if (previouslyFocused) {
      previouslyFocused.focus?.()
      previouslyFocused = null
    }
  })

  // Friendly local rendering of the [created::]/[completed::] ISO timestamps
  // (#417) for the Details metadata line. Falls back to the raw string when
  // the value isn't a parseable date so a hand-entered timestamp never renders
  // as "Invalid Date". No shared date formatter exists in the codebase yet
  // (VirtualScrollContainer.formatDate is local + date-only), so this is a
  // small inline helper using Intl.DateTimeFormat.
  const timestampFormatter = new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: 'numeric',
    minute: '2-digit'
  })
  function formatTimestamp(iso: string): string {
    if (!iso) return ''
    const parsed = new Date(iso)
    return isNaN(parsed.getTime()) ? iso : timestampFormatter.format(parsed)
  }

  let blockedByList = $derived(
    task?.blocked_by ? task.blocked_by.split('|').filter(Boolean) : []
  )

  // Local optimistic mirrors. The drawer is the only writer for these while
  // open, so optimistic update + revert-on-error matches the host contract.
  let pinState = $state(false)
  let progressState = $state(0)
  let recurrenceState = $state('')
  let dueDateState = $state('')
  let statusState = $state<TaskStatus>('TODO')
  let metaError = $state('')
  let pinPending = $state(false)
  let progressPending = $state(false)
  let recurrencePending = $state(false)
  let dueDatePending = $state(false)
  let statusPending = $state(false)
  let recurrenceOpen = $state(false)
  let dueDateOpen = $state(false)
  let recurrenceTrigger = $state<HTMLButtonElement | null>(null)
  let dueDateTrigger = $state<HTMLButtonElement | null>(null)
  let recurrenceFocusIdx = $state(-1)
  let customRecurrence = $state('')

  // Local optimistic mirrors for the four #412 metadata editors. Each
  // follows the same optimistic-update + revert-on-error contract as
  // pin/progress/recurrence above.
  let ownerState = $state('')
  let ownerDraft = $state('')
  let ownerPending = $state(false)
  let priorityState = $state(2)
  let priorityPending = $state(false)
  let tagsState = $state<string[]>([])
  let tagsPending = $state(false)
  let tagDraft = $state('')
  // Live-region text announcing tag adds/removes (a11y).
  let tagsAnnouncement = $state('')
  let titleState = $state('')
  let titleDraft = $state('')
  let titlePending = $state(false)

  // Pending DONE-on-blocked confirmation (#302). Picking DONE on a task with
  // open prerequisites pauses here and renders the shared BlockedDoneDialog
  // (the same guard surface Kanban/Agenda use) until the user decides.
  let pendingBlockedDone = $state<{
    blockers: { id: string; clean_content?: string }[]
  } | null>(null)

  const RECURRENCE_PRESETS: { value: string; hint: string }[] = [
    { value: 'every day', hint: '7 days a week' },
    { value: 'every weekday', hint: 'Mon – Fri' },
    { value: 'every week', hint: '' },
    { value: 'every 2 weeks', hint: '' },
    { value: 'every month', hint: '' },
    { value: 'every 3 months', hint: 'Quarterly' },
    { value: 'every year', hint: '' }
  ]

  // The three canonical parser-backed statuses (TODO/DOING/DONE). Custom
  // Kanban lanes (#413) are non-functional today; this control evolves to
  // reflect them when #413 lands.
  const STATUSES: TaskStatus[] = ['TODO', 'DOING', 'DONE']

  // The three parser-backed priorities (#412), matching PRIORITY_LABELS in
  // types.ts: 1=Critical, 2=Normal, 3=Low.
  const PRIORITIES: number[] = [1, 2, 3]

  $effect(() => {
    // Read individual fields so fine-grained reactivity tracks them as deps.
    void task?.pinned
    void task?.progress
    void task?.recurrence
    void task?.due_date
    void task?.status
    void task?.owner
    void task?.priority
    void task?.tags
    void task?.clean_content
    // Coerce at the boundary: `pinned` arrives as INT 0/1/NULL from the SQL
    // projection; force a real boolean so aria-pressed renders "true"/"false"
    // (not "0"/"1", which is out of spec).
    pinState = task?.pinned ? true : false
    progressState = task?.progress ?? 0
    recurrenceState = task?.recurrence ?? ''
    dueDateState = task?.due_date ?? ''
    statusState = task?.status ?? 'TODO'
    ownerState = task?.owner ?? ''
    ownerDraft = task?.owner ?? ''
    priorityState = task?.priority ?? 2
    tagsState = task?.tags ? task.tags.split('|').filter(Boolean) : []
    titleState = task?.clean_content ?? ''
    titleDraft = task?.clean_content ?? ''
    metaError = ''
  })

  async function togglePin() {
    if (!task || pinPending) return
    const prev = pinState
    pinState = !pinState
    pinPending = true
    metaError = ''
    try {
      await ctx.updateTaskMeta(task.id, { pinned: pinState })
      onMetaChanged?.()
    } catch (e) {
      pinState = prev
      metaError = e instanceof Error ? e.message : String(e)
    } finally {
      pinPending = false
    }
  }

  let progressSeq = 0
  function onProgressChange(e: Event) {
    if (!task || progressPending) return
    const v = Number((e.target as HTMLInputElement).value)
    const prev = progressState
    const my = ++progressSeq
    progressState = v
    progressPending = true
    metaError = ''
    void (async () => {
      try {
        await ctx.updateTaskMeta(task.id, { progress: v })
        onMetaChanged?.()
      } catch (err) {
        if (my !== progressSeq) return
        progressState = prev
        metaError = err instanceof Error ? err.message : String(err)
      } finally {
        progressPending = false
      }
    })()
  }

  function openSourcePage() {
    if (!task) return
    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: {
          notebook: task.notebook,
          section: task.section,
          page: task.page,
          date: task.file_date,
          blockId: task.id
        }
      })
    )
  }

  async function commitRecurrence(value: string) {
    if (!task || recurrencePending) return
    const prev = recurrenceState
    recurrenceState = value
    closeRecurrence()
    recurrencePending = true
    metaError = ''
    try {
      await ctx.setTaskRecurrence(task.id, value)
      onMetaChanged?.()
    } catch (e) {
      recurrenceState = prev
      metaError = e instanceof Error ? e.message : String(e)
    } finally {
      recurrencePending = false
    }
  }

  function closeRecurrence() {
    recurrenceOpen = false
    recurrenceFocusIdx = -1
    customRecurrence = ''
  }

  function onRecurrenceKeydown(e: KeyboardEvent) {
    if (!recurrenceOpen) {
      if (e.key === 'ArrowDown' || e.key === 'Enter') {
        e.preventDefault()
        recurrenceOpen = true
        recurrenceFocusIdx = 0
      }
      return
    }
    const optionValues = RECURRENCE_PRESETS.map((p) => p.value)
    if (recurrenceState) optionValues.push('') // "Stop recurring"
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      e.stopPropagation()
      recurrenceFocusIdx = Math.min(
        recurrenceFocusIdx + 1,
        optionValues.length - 1
      )
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      e.stopPropagation()
      recurrenceFocusIdx = Math.max(recurrenceFocusIdx - 1, 0)
    } else if (e.key === 'Enter') {
      e.preventDefault()
      e.stopPropagation()
      const selected = optionValues[recurrenceFocusIdx]
      if (selected !== undefined) void commitRecurrence(selected)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      closeRecurrence()
    }
  }

  // --- Due-date editor (mirrors the recurrence Popover pattern) ---
  async function commitDueDate(value: string) {
    if (!task || dueDatePending) return
    const prev = dueDateState
    dueDateState = value
    closeDueDate()
    dueDatePending = true
    metaError = ''
    try {
      await ctx.setTaskDueDate(task.id, value)
      onMetaChanged?.()
    } catch (e) {
      dueDateState = prev
      metaError = e instanceof Error ? e.message : String(e)
    } finally {
      dueDatePending = false
    }
  }

  function closeDueDate() {
    dueDateOpen = false
  }

  function onDueDateKeydown(e: KeyboardEvent) {
    if (!dueDateOpen) {
      if (e.key === 'ArrowDown' || e.key === 'Enter') {
        e.preventDefault()
        dueDateOpen = true
      }
      return
    }
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      closeDueDate()
    }
  }

  // --- Status radiogroup ---
  // Shared radiogroup arrow-key index helper (WAI-ARIA APG). Returns null for
  // non-arrow keys; otherwise the next index for a radiogroup of `length`
  // options, with wrapping on ArrowLeft/Right and Home/End jumping to bounds.
  // Used by both the Status and Priority radiogroups so they share one
  // WCAG-compliant keyboard-nav implementation.
  function nextRadiogroupIndex(
    key: string,
    currentIdx: number,
    length: number
  ): number | null {
    if (!['ArrowRight', 'ArrowLeft', 'Home', 'End'].includes(key)) return null
    if (key === 'Home') return 0
    if (key === 'End') return length - 1
    const dir = key === 'ArrowLeft' ? -1 : 1
    return (currentIdx + dir + length) % length
  }

  function onStatusKeydown(e: KeyboardEvent) {
    if (statusPending) return
    const idx = nextRadiogroupIndex(
      e.key,
      STATUSES.indexOf(statusState),
      STATUSES.length
    )
    if (idx === null) return
    e.preventDefault()
    void setStatus(STATUSES[idx])
    // Move focus with the arrow immediately — EXCEPT when landing on DONE
    // for a blocked task, where setStatus will pause on the guard. Pre-
    // focusing the (about-to-be-canceled) DONE radio would park focus on an
    // unchecked tabindex=-1 radio; cancelBlockedDone re-points focus to the
    // still-checked radio instead.
    const willGuard = STATUSES[idx] === 'DONE' && !!task?.is_blocked
    if (!willGuard) {
      ;(e.currentTarget as HTMLElement)
        .querySelector<HTMLElement>(`[data-status="${STATUSES[idx]}"]`)
        ?.focus()
    }
  }

  async function setStatus(s: TaskStatus) {
    if (!task || s === statusState || statusPending) return
    // DONE-on-blocked guard (#302): pause and render the shared
    // BlockedDoneDialog before committing. We do NOT optimistically flip to
    // DONE first; statusState stays at the prior value until confirm.
    if (s === 'DONE' && task.is_blocked) {
      try {
        const blockers = await ctx.getTaskBlockers(task.id)
        if (blockers.length > 0) {
          pendingBlockedDone = {
            blockers: blockers.map((b) => ({
              id: b.id,
              clean_content: b.clean_content
            }))
          }
          return
        }
      } catch (e) {
        metaError = e instanceof Error ? e.message : String(e)
        return
      }
    }
    await commitStatus(s)
  }

  async function commitStatus(s: TaskStatus) {
    if (!task) return
    const prev = statusState
    statusState = s
    statusPending = true
    metaError = ''
    try {
      await ctx.updateBlockState(task.id, s)
      onMetaChanged?.()
    } catch (e) {
      statusState = prev
      metaError = e instanceof Error ? e.message : String(e)
    } finally {
      statusPending = false
    }
  }

  function confirmBlockedDone() {
    pendingBlockedDone = null
    void commitStatus('DONE')
  }

  async function cancelBlockedDone() {
    pendingBlockedDone = null
    // statusState unchanged — we never optimistically flipped to DONE. The
    // DONE radio (the click/arrow target) holds focus with tabindex=-1
    // while the checked radio holds tabindex=0. After the guard dialog
    // unmounts (and restores focus to that DONE radio), re-point focus to
    // the still-checked radio so roving tabindex stays consistent.
    await tick()
    panelRef
      ?.querySelector<HTMLElement>(`[data-status="${statusState}"]`)
      ?.focus()
  }

  // --- Owner editor (#412) ---
  // Commit on blur/Enter. Empty string clears the owner. Optimistic +
  // revert-on-error (mirrors pin/progress).
  async function commitOwner() {
    if (!task || ownerPending) return
    const trimmed = ownerDraft.trim()
    if (trimmed === ownerState) return
    const prev = ownerState
    ownerState = trimmed
    ownerPending = true
    metaError = ''
    try {
      await ctx.setTaskOwner(task.id, trimmed)
      onMetaChanged?.()
    } catch (e) {
      ownerState = prev
      ownerDraft = prev
      metaError = e instanceof Error ? e.message : String(e)
    } finally {
      ownerPending = false
    }
  }

  // --- Priority editor (#412) ---
  // Segmented radiogroup; reuses the shared nextRadiogroupIndex helper.
  let priorityCheckedIdx = $derived(PRIORITIES.indexOf(priorityState))

  async function commitPriority(p: number) {
    if (!task || priorityPending || p === priorityState) return
    const prev = priorityState
    priorityState = p
    priorityPending = true
    metaError = ''
    try {
      await ctx.setTaskPriority(task.id, p)
      onMetaChanged?.()
    } catch (e) {
      priorityState = prev
      metaError = e instanceof Error ? e.message : String(e)
    } finally {
      priorityPending = false
    }
  }

  function onPriorityKeydown(e: KeyboardEvent) {
    if (priorityPending) return
    // When no radio is checked (priority unset), roving tabindex starts at
    // the first option so the group is always keyboard-reachable.
    const curIdx = priorityCheckedIdx >= 0 ? priorityCheckedIdx : 0
    const idx = nextRadiogroupIndex(e.key, curIdx, PRIORITIES.length)
    if (idx === null) return
    e.preventDefault()
    void commitPriority(PRIORITIES[idx])
    ;(e.currentTarget as HTMLElement)
      .querySelector<HTMLElement>(`[data-priority="${PRIORITIES[idx]}"]`)
      ?.focus()
  }

  // --- Tags chip editor (#412) ---
  // Each add/remove commits the FULL new tag set via ctx.setTaskTags
  // (the backend rewrites the whole [tags:: a|b|c] token). Optimistic +
  // revert. The aria-live region announces adds/removes for SR users.
  async function commitTags(newTags: string[], announcement: string) {
    if (!task || tagsPending) return
    const prev = tagsState
    tagsState = newTags
    tagDraft = ''
    tagsAnnouncement = announcement
    tagsPending = true
    metaError = ''
    try {
      await ctx.setTaskTags(task.id, newTags)
      onMetaChanged?.()
    } catch (e) {
      tagsState = prev
      // Correct the polite region: the optimistic "Added/Removed" was a lie
      // on failure. Flip it to the matching "Couldn't add/remove" so SR users
      // hear the truth (the visible banner already shows the error).
      tagsAnnouncement = announcement
        .replace('Added tag', "Couldn't add")
        .replace('Removed tag', "Couldn't remove")
      metaError = e instanceof Error ? e.message : String(e)
    } finally {
      tagsPending = false
    }
  }

  function addTag() {
    if (!task || tagsPending) return
    const t = tagDraft.trim()
    if (!t || tagsState.includes(t)) {
      tagDraft = ''
      return
    }
    void commitTags([...tagsState, t], `Added tag ${t}`)
  }

  function removeTag(t: string) {
    if (!task || tagsPending || !tagsState.includes(t)) return
    void commitTags(
      tagsState.filter((x) => x !== t),
      `Removed tag ${t}`
    )
  }

  // --- Title editor (#412) ---
  // Inline editable heading. Commit on blur/Enter; the backend preserves
  // #tags, ((uuid)) refs, and inline tokens during the prose rewrite.
  async function commitTitle() {
    if (!task || titlePending) return
    const trimmed = titleDraft.trim()
    if (!trimmed) {
      // Empty title: the backend rejects it. Snap the visible draft back to
      // the real committed value so the field doesn't lie about state (and
      // the sr-only dialog name stays in sync with what's shown).
      titleDraft = titleState
      return
    }
    if (trimmed === titleState) return
    const prev = titleState
    titleState = trimmed
    titlePending = true
    metaError = ''
    try {
      await ctx.setTaskTitle(task.id, trimmed)
      onMetaChanged?.()
    } catch (e) {
      titleState = prev
      titleDraft = prev
      metaError = e instanceof Error ? e.message : String(e)
    } finally {
      titlePending = false
    }
  }

  // Computed next-occurrence preview. Reads the LOCAL optimistic mirrors
  // (dueDateState / recurrenceState) so an in-drawer edit refreshes the
  // preview without waiting for the host reload.
  let nextOccurrence = $derived.by(() => {
    if (!recurrenceState || !dueDateState) return ''
    const due = new Date(dueDateState + 'T00:00:00')
    if (isNaN(due.getTime())) return ''
    const today = new Date()
    today.setHours(0, 0, 0, 0)
    // Overdue recurring task: the server's skip-missed resolver decides the
    // landing date at completion; a client guess could be wrong.
    if (due <= today) return ''
    const rule = recurrenceState.toLowerCase()
    let step: Date
    if (rule.includes('day') && !rule.includes('weekday')) {
      const n = parseInt(rule.match(/(\d+)\s*day/)?.[1] ?? '1')
      step = new Date(due.getTime() + n * 86400000)
    } else if (rule.includes('weekday')) {
      step = new Date(due.getTime() + 86400000)
      while (step.getDay() === 0 || step.getDay() === 6)
        step.setDate(step.getDate() + 1)
    } else if (rule.includes('week')) {
      const n = parseInt(rule.match(/(\d+)\s*week/)?.[1] ?? '1')
      step = new Date(due.getTime() + n * 7 * 86400000)
    } else if (rule.includes('month')) {
      const n = parseInt(rule.match(/(\d+)\s*month/)?.[1] ?? '1')
      step = new Date(due.getFullYear(), due.getMonth() + n, due.getDate())
    } else if (rule.includes('year')) {
      const n = parseInt(rule.match(/(\d+)\s*year/)?.[1] ?? '1')
      step = new Date(due.getFullYear() + n, due.getMonth(), due.getDate())
    } else {
      return ''
    }
    return step.toISOString().slice(0, 10)
  })

  function onWindowKeydown(e: KeyboardEvent) {
    // Don't close on Escape while a Popover (recurrence/due-date) or the
    // BlockedDoneDialog is open — those consume Escape first.
    if (
      e.key === 'Escape' &&
      task &&
      !recurrenceOpen &&
      !dueDateOpen &&
      !pendingBlockedDone
    ) {
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
    class="fixed right-0 top-14 h-[calc(100vh-56px)] w-96 bg-surface-card border-l border-surface-card-border z-40 overflow-y-auto custom-scrollbar focus:outline-none shadow-2xl"
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
        {#if priorityState >= 1 && priorityState <= 3}
          <span
            class="self-start px-1.5 py-0.5 border rounded-sm font-label-sm text-[9px] uppercase tracking-wide w-fit {priorityClass(
              priorityState
            )}"
          >
            {PRIORITY_LABELS[priorityState] ?? 'Normal'}
          </span>
        {/if}
        <h2
          id="task-edit-drawer-title"
          class="font-headline-md text-headline-md text-text-primary break-words flex items-start gap-1"
        >
          {#if recurrenceState}
            <span
              class="material-symbols-outlined text-[16px] text-accent-secondary-start shrink-0 mt-1"
              aria-hidden="true"
              title="Recurring: {recurrenceState}">event_repeat</span
            >
          {/if}
          <!-- Visually-hidden mirror of the title so the dialog's
               aria-labelledby name resolves (an <input>'s value is a property,
               not textContent, so it alone can't name the dialog) and so
               textContent-based assertions still see the committed title. -->
          <span class="sr-only">{titleState}</span>
          <input
            type="text"
            class="flex-1 min-w-0 bg-transparent border-none outline-none focus:ring-1 focus:ring-accent-primary-start/40 hover:bg-hover rounded -mx-1 px-1 placeholder:text-text-muted {titlePending
              ? 'opacity-50'
              : ''}"
            bind:value={titleDraft}
            readonly={titlePending}
            aria-busy={titlePending}
            aria-label="Task title"
            onblur={commitTitle}
            onkeydown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                void commitTitle()
              }
            }}
          />
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
      {#if metaError}
        <div
          class="flex items-start gap-2 px-3 py-2 rounded border border-error-border bg-error-bg text-error text-[12px] font-body-md"
          role="alert"
        >
          <span class="material-symbols-outlined text-[14px] shrink-0"
            >error</span
          >
          <span>Couldn't save: {metaError}</span>
        </div>
      {/if}

      <!-- Status radiogroup -->
      <section>
        <h3
          class="font-label-sm-bold uppercase tracking-widest text-[10px] text-text-muted mb-2"
        >
          Status
        </h3>
        <!-- svelte-ignore a11y_no_static_element_interactions
             role="radiogroup" is a composite widget that handles arrow-key
             navigation for its radio children per WAI-ARIA APG. -->
        <div
          class="flex items-center gap-0.5 bg-surface-panel border border-surface-panel-border rounded-lg p-0.5"
          role="radiogroup"
          aria-label="Task status"
          tabindex="-1"
          onkeydown={onStatusKeydown}
        >
          {#each STATUSES as s}
            <button
              data-status={s}
              type="button"
              onclick={() => void setStatus(s)}
              role="radio"
              aria-checked={statusState === s}
              tabindex={statusState === s ? 0 : -1}
              disabled={statusPending}
              class="flex-1 px-2.5 py-1 rounded font-label-sm border-none cursor-pointer transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              class:bg-hover={statusState === s}
              class:text-accent-primary-start={statusState === s}
              class:text-text-muted={statusState !== s}
            >
              {laneLabel(s)}
            </button>
          {/each}
        </div>
      </section>

      <!-- Due-date editor -->
      <section>
        <h3
          class="font-label-sm-bold uppercase tracking-widest text-[10px] text-text-muted mb-2"
        >
          Due date
        </h3>
        <button
          bind:this={dueDateTrigger}
          type="button"
          onclick={() => {
            dueDateOpen = !dueDateOpen
          }}
          onkeydown={onDueDateKeydown}
          disabled={dueDatePending}
          aria-haspopup="dialog"
          aria-expanded={dueDateOpen}
          class="w-full flex items-center justify-between px-3 py-2 rounded border border-surface-card-border bg-surface-card hover:bg-hover transition-colors disabled:opacity-50 text-[12px] font-label-sm text-text-primary"
        >
          <span class="flex items-center gap-2">
            <span
              class="material-symbols-outlined text-[16px] {dueDateState
                ? 'text-accent-secondary-start'
                : 'text-text-muted'}"
              aria-hidden="true">event</span
            >
            {dueDateState || 'Set due date…'}
          </span>
          <span
            class="material-symbols-outlined text-[14px] text-text-muted"
            aria-hidden="true">expand_more</span
          >
        </button>
        <Popover
          open={dueDateOpen}
          onClose={closeDueDate}
          anchor={dueDateTrigger}
          matchWidth
          class="rounded border border-surface-popover-border bg-surface-popover shadow-lg"
        >
          {#snippet content()}
            <div
              transition:fly={{ y: -4, duration: 100 }}
              role="dialog"
              aria-label="Due date options"
            >
              <div class="p-2 border-b border-surface-card-border">
                <input
                  type="date"
                  aria-label="Custom due date"
                  class="w-full px-2 py-1 text-[12px] font-label-sm bg-surface-card border border-surface-card-border rounded text-text-primary focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40"
                  value={dueDateState}
                  oninput={(e) =>
                    (dueDateState = (e.currentTarget as HTMLInputElement)
                      .value)}
                  onkeydown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault()
                      e.stopPropagation()
                      if (dueDateState) void commitDueDate(dueDateState)
                    } else if (e.key === 'Escape') {
                      e.preventDefault()
                      e.stopPropagation()
                      closeDueDate()
                    }
                  }}
                />
              </div>
              {#each [{ label: 'Today', value: ctx.today }, { label: 'Tomorrow', value: plusDaysISO(ctx.today, 1) }, { label: 'Next week', value: plusDaysISO(ctx.today, 7) }] as preset}
                <button
                  type="button"
                  class="w-full text-left px-3 py-1.5 text-[12px] font-label-sm hover:bg-hover transition-colors {dueDateState ===
                  preset.value
                    ? 'text-accent-primary-start font-label-sm-bold'
                    : 'text-text-primary'}"
                  onclick={() => void commitDueDate(preset.value)}
                >
                  {preset.label}
                  <span class="text-text-muted text-[10px] ml-1"
                    >{preset.value}</span
                  >
                </button>
              {/each}
              {#if dueDateState}
                <div class="border-t border-surface-card-border">
                  <button
                    type="button"
                    class="w-full text-left px-3 py-1.5 text-[12px] font-label-sm text-text-muted hover:bg-hover transition-colors"
                    onclick={() => void commitDueDate('')}
                  >
                    Clear due date
                  </button>
                </div>
              {/if}
            </div>
          {/snippet}
        </Popover>
      </section>

      <!-- Pin toggle -->
      <section>
        <button
          type="button"
          onclick={togglePin}
          disabled={pinPending}
          class="w-full flex items-center justify-between px-3 py-2 rounded border border-surface-card-border bg-surface-card hover:bg-hover transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          aria-pressed={pinState}
        >
          <span
            class="flex items-center gap-2 text-[12px] font-label-sm text-text-primary"
          >
            <span class="material-symbols-outlined text-[16px]">push_pin</span>
            {pinState ? 'Pinned' : 'Pin'}
          </span>
          {#if pinState}
            <span
              class="material-symbols-outlined text-[16px] text-accent-primary-start"
              >check</span
            >
          {/if}
        </button>
      </section>

      <!-- Progress slider -->
      <section>
        <div class="flex items-center justify-between mb-2">
          <h3
            class="font-label-sm-bold uppercase tracking-widest text-[10px] text-text-muted"
          >
            Progress
          </h3>
          <span class="text-[11px] font-label-sm text-text-primary"
            >{progressState}%</span
          >
        </div>
        <input
          type="range"
          min="0"
          max="100"
          value={progressState}
          oninput={(e) => {
            if (!progressPending)
              progressState = Number(
                (e.currentTarget as HTMLInputElement).value
              )
          }}
          onchange={onProgressChange}
          disabled={progressPending}
          aria-label="Task progress"
          class="w-full accent-accent-secondary-start disabled:opacity-50"
        />
        <div
          class="mt-2 h-1 bg-surface-card border border-surface-card-border rounded overflow-hidden"
        >
          <div
            class="h-full bg-accent-secondary-start transition-all"
            style="width: {progressState}%"
          ></div>
        </div>
      </section>

      <!-- Recurrence editor -->
      <section>
        <h3
          class="font-label-sm-bold uppercase tracking-widest text-[10px] text-text-muted mb-2"
        >
          Recurrence
        </h3>
        {#if dueDateState}
          <button
            bind:this={recurrenceTrigger}
            type="button"
            onclick={() => {
              recurrenceOpen = !recurrenceOpen
              recurrenceFocusIdx = 0
            }}
            onkeydown={onRecurrenceKeydown}
            disabled={recurrencePending}
            aria-haspopup="listbox"
            aria-expanded={recurrenceOpen}
            aria-controls="recurrence-listbox"
            class="w-full flex items-center justify-between px-3 py-2 rounded border border-surface-card-border bg-surface-card hover:bg-hover transition-colors disabled:opacity-50 text-[12px] font-label-sm text-text-primary"
          >
            <span class="flex items-center gap-2">
              <span
                class="material-symbols-outlined text-[16px] {recurrenceState
                  ? 'text-accent-secondary-start'
                  : 'text-text-muted'}"
                aria-hidden="true">event_repeat</span
              >
              {recurrenceState || 'Set recurrence…'}
            </span>
            <span
              class="material-symbols-outlined text-[14px] text-text-muted"
              aria-hidden="true">expand_more</span
            >
          </button>
          <Popover
            open={recurrenceOpen}
            onClose={closeRecurrence}
            anchor={recurrenceTrigger}
            matchWidth
            class="rounded border border-surface-popover-border bg-surface-popover shadow-lg"
          >
            {#snippet content()}
              <div
                id="recurrence-listbox"
                transition:fly={{ y: -4, duration: 100 }}
                role="listbox"
                tabindex="-1"
                aria-label="Recurrence options"
              >
                <div class="p-2 border-b border-surface-card-border">
                  <input
                    type="text"
                    placeholder="Custom (e.g. every 5 days)"
                    aria-label="Custom recurrence rule"
                    class="w-full px-2 py-1 text-[12px] font-label-sm bg-surface-card border border-surface-card-border rounded text-text-primary focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40"
                    bind:value={customRecurrence}
                    onkeydown={(e) => {
                      if (e.key === 'Enter') {
                        e.preventDefault()
                        e.stopPropagation()
                        if (customRecurrence.trim())
                          void commitRecurrence(customRecurrence.trim())
                      } else if (e.key === 'Escape') {
                        e.preventDefault()
                        e.stopPropagation()
                        closeRecurrence()
                      }
                    }}
                  />
                </div>
                {#each RECURRENCE_PRESETS as preset, i (preset.value)}
                  <button
                    type="button"
                    role="option"
                    aria-selected={recurrenceState === preset.value}
                    tabindex={recurrenceFocusIdx === i ? 0 : -1}
                    class="w-full text-left px-3 py-1.5 text-[12px] font-label-sm hover:bg-hover transition-colors {recurrenceState ===
                    preset.value
                      ? 'text-accent-primary-start font-label-sm-bold'
                      : 'text-text-primary'} {recurrenceFocusIdx === i
                      ? 'bg-hover'
                      : ''}"
                    onclick={() => void commitRecurrence(preset.value)}
                  >
                    {preset.value}
                    {#if preset.hint}
                      <span class="text-text-muted text-[10px] ml-1"
                        >({preset.hint})</span
                      >
                    {/if}
                  </button>
                {/each}
                {#if recurrenceState}
                  <div class="border-t border-surface-card-border">
                    <button
                      type="button"
                      role="option"
                      aria-selected={false}
                      tabindex={recurrenceFocusIdx === RECURRENCE_PRESETS.length
                        ? 0
                        : -1}
                      class="w-full text-left px-3 py-1.5 text-[12px] font-label-sm text-text-muted hover:bg-hover transition-colors {recurrenceFocusIdx ===
                      RECURRENCE_PRESETS.length
                        ? 'bg-hover'
                        : ''}"
                      onclick={() => void commitRecurrence('')}
                    >
                      Stop recurring
                    </button>
                  </div>
                {/if}
              </div>
            {/snippet}
          </Popover>
        {:else}
          <p class="text-[11px] font-label-sm text-text-muted italic">
            Set a due date first to configure recurrence.
          </p>
        {/if}
      </section>

      <!-- Read-only details -->
      <section>
        <h3
          class="font-label-sm-bold uppercase tracking-widest text-[10px] text-text-muted mb-3"
        >
          Details
        </h3>
        <dl class="flex flex-col gap-2.5 text-[12px] font-label-sm">
          <div class="flex items-center justify-between gap-2">
            <dt class="text-text-muted shrink-0">
              <label for="task-owner-input">Owner</label>
            </dt>
            <dd class="flex-1 max-w-[200px]">
              <input
                id="task-owner-input"
                type="text"
                class="w-full px-2 py-0.5 border rounded-sm text-text-primary border-surface-card-border bg-surface-card text-right focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40 {ownerPending
                  ? 'opacity-50'
                  : ''}"
                placeholder="Unassigned"
                bind:value={ownerDraft}
                readonly={ownerPending}
                aria-busy={ownerPending}
                onblur={commitOwner}
                onkeydown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    void commitOwner()
                  }
                }}
              />
            </dd>
          </div>
          <div class="flex items-center justify-between gap-2">
            <dt id="task-priority-label" class="text-text-muted shrink-0">
              Priority
            </dt>
            <dd class="flex-1 max-w-[220px]">
              <!-- svelte-ignore a11y_no_static_element_interactions
                   role="radiogroup" is a composite widget that handles
                   arrow-key navigation for its radio children per WAI-ARIA
                   APG (same pattern as the Status control above). -->
              <div
                class="flex items-center gap-0.5 bg-surface-panel border border-surface-panel-border rounded-lg p-0.5"
                role="radiogroup"
                aria-labelledby="task-priority-label"
                tabindex="-1"
                onkeydown={onPriorityKeydown}
              >
                {#each PRIORITIES as p, i (p)}
                  <button
                    data-priority={p}
                    type="button"
                    onclick={() => void commitPriority(p)}
                    role="radio"
                    aria-checked={priorityState === p}
                    tabindex={priorityCheckedIdx >= 0
                      ? priorityCheckedIdx === i
                        ? 0
                        : -1
                      : i === 0
                        ? 0
                        : -1}
                    disabled={priorityPending}
                    class="flex-1 px-2 py-0.5 rounded font-label-sm border-none cursor-pointer transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                    class:bg-hover={priorityState === p}
                    class:text-accent-primary-start={priorityState === p}
                    class:text-text-muted={priorityState !== p}
                  >
                    {PRIORITY_LABELS[p]}
                  </button>
                {/each}
              </div>
            </dd>
          </div>
          {#if task.created_at || task.completed_at}
            <div class="flex flex-start justify-between gap-2">
              <dt class="text-text-muted shrink-0">Created</dt>
              <dd class="text-text-primary text-right">
                {#if task.created_at}
                  {formatTimestamp(task.created_at)}
                {:else}
                  <span class="text-text-muted">—</span>
                {/if}
                {#if task.completed_at}
                  <span class="block text-text-muted">
                    Completed {formatTimestamp(task.completed_at)}
                  </span>
                {/if}
              </dd>
            </div>
          {/if}
          {#if nextOccurrence}
            <div class="flex items-center justify-between">
              <dt class="text-text-muted">Next occurrence</dt>
              <dd class="text-accent-secondary-start">{nextOccurrence}</dd>
            </div>
          {:else if recurrenceState && dueDateState}
            <div class="flex items-center justify-between">
              <dt class="text-text-muted">Next occurrence</dt>
              <dd class="text-text-muted italic text-[11px]">
                Computed on completion
              </dd>
            </div>
          {/if}
          <div class="flex items-center justify-between">
            <dt class="text-text-muted">Start date</dt>
            <dd class="text-text-primary">{task.start_date || '—'}</dd>
          </div>
          <div class="flex flex-start justify-between gap-2">
            <dt class="text-text-muted shrink-0 pt-0.5">Tags</dt>
            <dd class="flex-1 min-w-0">
              <span class="sr-only" aria-live="polite">{tagsAnnouncement}</span>
              <ul class="flex flex-wrap gap-1 justify-end items-center">
                {#each tagsState as tg (tg)}
                  <li
                    class="flex items-center gap-0.5 px-1.5 py-0.5 border rounded-sm text-[12px] text-accent-secondary-start border-accent-secondary-start/30 bg-accent-secondary-glow"
                  >
                    <span>{tg}</span>
                    <button
                      type="button"
                      class="text-text-muted hover:text-error transition-colors disabled:opacity-50"
                      aria-label="Remove tag {tg}"
                      disabled={tagsPending}
                      onclick={() => removeTag(tg)}
                    >
                      <span aria-hidden="true">×</span></button
                    >
                  </li>
                {/each}
                <li>
                  <label class="sr-only" for="task-tag-add">Add a tag</label>
                  <input
                    id="task-tag-add"
                    type="text"
                    class="flex-1 min-w-[100px] px-1.5 py-0.5 text-[12px] bg-transparent border border-surface-card-border rounded focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40 text-text-primary placeholder:text-text-muted disabled:opacity-50"
                    placeholder="Add…"
                    bind:value={tagDraft}
                    disabled={tagsPending}
                    onkeydown={(e) => {
                      if (e.key === 'Enter') {
                        e.preventDefault()
                        addTag()
                      }
                    }}
                  />
                </li>
              </ul>
            </dd>
          </div>
        </dl>
      </section>

      <!-- Counts -->
      <section class="flex items-center gap-4">
        <div class="flex items-center gap-1.5 text-text-muted">
          <span class="material-symbols-outlined text-[16px]">chat_bubble</span>
          <span class="text-[12px] font-label-sm">{task.comments_count}</span>
          <span class="text-[10px] font-label-sm text-text-muted">comments</span
          >
        </div>
        <div class="flex items-center gap-1.5 text-text-muted">
          <span class="material-symbols-outlined text-[16px]">link</span>
          <span class="text-[12px] font-label-sm">{task.links_count}</span>
          <span class="text-[10px] font-label-sm text-text-muted">links</span>
        </div>
      </section>

      <DependencyPicker
        cardId={task.id}
        blockedBy={blockedByList}
        {ctx}
        {onMetaChanged}
      />

      {#if onOpenSubEditor}
        <section>
          <button
            type="button"
            onclick={onOpenSubEditor}
            class="w-full flex items-center justify-center gap-2 px-3 py-2 rounded border border-surface-card-border bg-surface-card text-text-primary hover:bg-hover transition-all font-label-sm-bold"
          >
            <span class="material-symbols-outlined text-[16px]">edit_note</span>
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
            <span class="material-symbols-outlined text-[16px]"
              >open_in_new</span
            >
            Open source page
          </button>
        </section>
      {/if}

      <!-- Source breadcrumb (source-aware) -->
      <section class="pt-2 border-t border-surface-card-border">
        <p class="text-[10px] font-label-sm text-text-muted break-all">
          {#if isStandalone}
            Standalone task
          {:else}
            {task.notebook} › {task.section} › {task.page}
          {/if}
        </p>
      </section>
    </div>
  </div>

  {#if pendingBlockedDone}
    <BlockedDoneDialog
      cardText={task.clean_content}
      blockers={pendingBlockedDone.blockers}
      onConfirm={confirmBlockedDone}
      onCancel={cancelBlockedDone}
    />
  {/if}
{/if}
