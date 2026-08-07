<script lang="ts">
  import { SvelteDate } from 'svelte/reactivity'
  import { fly } from 'svelte/transition'
  import { tick, untrack } from 'svelte'
  import type { PluginContext, TaskStatus } from '../../../sdk'
  import { trailingDebounce } from '../debounce'
  import { friendlyCaughtError } from '../errors'
  import { optimisticField } from '../optimisticField.svelte'
  import { useBlockedDoneGuard } from '../shared.svelte'
  import ErrorBanner from './ErrorBanner.svelte'

  import type { TaskDetail } from '../types'
  import { PRIORITY_LABELS, formatEstimateMinutes, laneLabel } from '../types'
  import DependencyPicker from './DependencyPicker.svelte'
  import BlockedDoneGuard from './BlockedDoneGuard.svelte'
  import CommentThread from './CommentThread.svelte'
  import Popover from '../../../../components/Popover.svelte'
  import { buildDueDatePresets } from './dueDatePresets'
  import { nextRecurrenceDate } from './recurrencePreview'
  import { getTaskWeekStart } from '../../../../lib/taskWeekStart.svelte'
  import { motionDuration } from '../motion'

  /**
   * The shared task metadata surface — extracted from TaskEditDrawer so the
   * same controls (title, status, due date, pin, progress, estimate,
   * recurrence, owner, priority, timestamps, tags, dependencies, comments)
   * render identically inside the TaskEditDrawer inspector AND the
   * TaskSubEditorModal right sidebar (#780).
   *
   * Owns every optimistic-commit field + the DONE-on-blocked guard. The host
   * (drawer / modal) owns only the chrome and calls `onMetaChanged` after each
   * successful write so its list/board view can re-query.
   */
  interface Props {
    task: TaskDetail
    ctx: PluginContext
    /** Called after a successful metadata write so the host can re-query. */
    onMetaChanged?: () => void
    /**
     * Mirrors whether any popover/dialog inside the sidebar is open, so the
     * host's window-level Esc handler can avoid closing while the sidebar is
     * mid-interaction (the popovers/dialog stopPropagation on their own Esc,
     * but the BlockedDoneDialog's <svelte:window> listener does not, so the
     * host needs this signal as a safety net).
     */
    busy?: boolean
    /** Pins the primary controls to the top of the host's scroll container. */
    stickyPrimary?: boolean
    /** When supplied, close becomes part of the shared primary action header. */
    onClose?: () => void
    /** Gives the host dialog a stable accessible name without duplicating UI. */
    headingId?: string
  }

  // busy is a write-only $bindable: the sidebar pushes its interaction state UP
  // to the host's Esc guard (TaskEditDrawer + TaskSubEditorModal bind it); the
  // sidebar never reads its own value, so the default is never consumed here.
  // Do NOT mirror it onto aria-busy — "a popover is open" is not a loading
  // state and would mislead assistive tech.
  let {
    task,
    ctx,
    onMetaChanged,
    // eslint-disable-next-line no-useless-assignment
    busy = $bindable(),
    stickyPrimary = false,
    onClose,
    headingId
  }: Props = $props()

  let rootRef = $state<HTMLDivElement | null>(null)

  // Friendly local rendering of the [created::]/[completed::] ISO timestamps
  // (#417). Falls back to the raw string when the value isn't a parseable date.
  const timestampFormatter = new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: 'numeric',
    minute: '2-digit'
  })
  function formatTimestamp(iso: string): string {
    if (!iso) return ''
    const parsed = new SvelteDate(iso)
    return isNaN(parsed.getTime()) ? iso : timestampFormatter.format(parsed)
  }

  let blockedByList = $derived(
    task?.blocked_by ? task.blocked_by.split('|').filter(Boolean) : []
  )

  // metaError is the shared failure banner for every metadata editor.
  let metaError = $state('')
  let progressState = $state(0)
  let progressPending = $state(false)

  const notifyMeta = () => onMetaChanged?.()
  const setMetaError = (m: string) => {
    metaError = m
  }

  // Optimistic-commit fields. Each owns its { value, pending } and the
  // snapshot → optimistic set → write → revert-on-error skeleton.
  const pinField = optimisticField<boolean>({
    initial: false,
    write: (v) => ctx.updateTaskMeta(task!.id, { pinned: v }),
    onChanged: notifyMeta,
    onError: setMetaError
  })
  const recurrenceField = optimisticField<string>({
    initial: '',
    write: (v) => ctx.setTaskRecurrence(task!.id, v),
    onChanged: notifyMeta,
    onError: setMetaError
  })
  const dueDateField = optimisticField<string>({
    initial: '',
    write: (v) => ctx.setTaskDueDate(task!.id, v),
    onChanged: notifyMeta,
    onError: setMetaError
  })
  const startDateField = optimisticField<string>({
    initial: '',
    write: (v) => ctx.setTaskStartDate(task!.id, v),
    onChanged: notifyMeta,
    onError: setMetaError
  })
  const statusField = optimisticField<TaskStatus>({
    initial: 'TODO',
    write: (s) => ctx.updateBlockState(task!.id, s),
    onChanged: notifyMeta,
    onError: setMetaError
  })
  const estimateField = optimisticField<string>({
    initial: '',
    write: (v) => ctx.setTaskEstimate(task!.id, v),
    onChanged: notifyMeta,
    onError: setMetaError
  })
  const ownerField = optimisticField<string>({
    initial: '',
    write: (v) => ctx.setTaskOwner(task!.id, v),
    onChanged: notifyMeta,
    onError: setMetaError
  })
  const priorityField = optimisticField<number>({
    initial: 2,
    write: (p) => ctx.setTaskPriority(task!.id, p),
    onChanged: notifyMeta,
    onError: setMetaError
  })
  const tagsField = optimisticField<string[]>({
    initial: [],
    write: (tags) => ctx.setTaskTags(task!.id, tags),
    onChanged: notifyMeta,
    onError: setMetaError
  })
  const titleField = optimisticField<string>({
    initial: '',
    write: (v) => ctx.setTaskTitle(task!.id, v),
    onChanged: notifyMeta,
    onError: setMetaError
  })

  // Anchor for revert: the last status/priority successfully persisted.
  let statusCommitted = $state<TaskStatus>('TODO')
  let priorityCommitted = $state(2)

  let recurrenceOpen = $state(false)
  let dueDateOpen = $state(false)
  let recurrenceTrigger = $state<HTMLButtonElement | null>(null)
  let dueDateTrigger = $state<HTMLButtonElement | null>(null)
  let recurrenceFocusIdx = $state(-1)
  let customRecurrence = $state('')

  let ownerDraft = $state('')
  let startDateDraft = $state('')
  let tagDraft = $state('')
  let tagsAnnouncement = $state('')
  let titleDraft = $state('')
  let estimateInvalid = $state(false)
  let weekStart = $derived(getTaskWeekStart())
  let dueDatePresets = $derived(buildDueDatePresets(ctx.today, weekStart))
  let disclosureTaskId = $state('')
  let planningOpen = $state(false)
  let activityOpen = $state(false)

  // ctx is a stable plugin-context singleton — see BoardView for rationale.
  // svelte-ignore state_referenced_locally
  const blockedGuard = useBlockedDoneGuard<undefined>(ctx, (e) => {
    metaError = friendlyCaughtError(e)
  })

  const RECURRENCE_PRESETS: { value: string; hint: string }[] = [
    { value: 'every day', hint: '7 days a week' },
    { value: 'every weekday', hint: 'Mon – Fri' },
    { value: 'every week', hint: '' },
    { value: 'every 2 weeks', hint: '' },
    { value: 'every month', hint: '' },
    { value: 'every 3 months', hint: 'Quarterly' },
    { value: 'every year', hint: '' }
  ]

  const STATUSES: TaskStatus[] = ['TODO', 'DOING', 'DONE']
  const PRIORITIES: number[] = [1, 2, 3]

  // Mirror the busy state up to the host so its window Esc handler can avoid
  // closing while a popover or the BlockedDoneDialog is open.
  $effect(() => {
    busy = recurrenceOpen || dueDateOpen || !!blockedGuard.pending
  })

  $effect(() => {
    void task?.pinned
    void task?.progress
    void task?.recurrence
    void task?.due_date
    void task?.start_date
    void task?.status
    void task?.owner
    void task?.priority
    void task?.tags
    void task?.clean_content
    void task?.estimate_minutes
    if (untrack(() => !pinField.pending))
      pinField.reset(task?.pinned ? true : false)
    if (untrack(() => !progressPending)) progressState = task?.progress ?? 0
    if (untrack(() => !recurrenceField.pending))
      recurrenceField.reset(task?.recurrence ?? '')
    if (untrack(() => !dueDateField.pending))
      dueDateField.reset(task?.due_date ?? '')
    if (untrack(() => !startDateField.pending)) {
      startDateField.reset(task?.start_date ?? '')
      startDateDraft = task?.start_date ?? ''
    }
    if (untrack(() => !statusField.pending)) {
      statusField.reset(task?.status ?? 'TODO')
      statusCommitted = task?.status ?? 'TODO'
    }
    if (untrack(() => !ownerField.pending)) {
      ownerField.reset(task?.owner ?? '')
      ownerDraft = task?.owner ?? ''
    }
    if (untrack(() => !priorityField.pending)) {
      priorityField.reset(task?.priority ?? 2)
      priorityCommitted = task?.priority ?? 2
    }
    if (untrack(() => !tagsField.pending)) {
      tagsField.reset(task?.tags ? task.tags.split('|').filter(Boolean) : [])
    }
    if (untrack(() => !titleField.pending)) {
      titleField.reset(task?.clean_content ?? '')
      titleDraft = task?.clean_content ?? ''
    }
    if (untrack(() => !estimateField.pending)) {
      estimateField.reset(formatEstimateMinutes(task?.estimate_minutes))
      estimateInvalid = false
    }
    metaError = ''
  })

  // Seed disclosures once per task. User choices survive optimistic host
  // refreshes; switching tasks gets a fresh, content-aware default.
  $effect(() => {
    if (task.id === disclosureTaskId) return
    disclosureTaskId = task.id
    planningOpen =
      task.progress > 0 ||
      task.estimate_minutes !== null ||
      !!task.recurrence ||
      blockedByList.length > 0
    activityOpen = task.comments_count > 0 || !!task.completed_at
  })

  async function togglePin() {
    if (!task || pinField.pending) return
    await pinField.commit(!pinField.value)
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
        metaError = friendlyCaughtError(err)
      } finally {
        progressPending = false
      }
    })()
  }

  async function commitRecurrence(value: string) {
    if (!task || recurrenceField.pending) return
    closeRecurrence()
    await recurrenceField.commit(value)
  }

  function closeRecurrence() {
    recurrenceOpen = false
    recurrenceFocusIdx = -1
    customRecurrence = ''
    void tick().then(() => recurrenceTrigger?.focus())
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
    if (recurrenceField.value) optionValues.push('')
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

  async function commitDueDate(value: string) {
    if (!task || dueDateField.pending) return
    closeDueDate()
    await dueDateField.commit(value)
  }

  async function commitStartDate(value: string) {
    startDateDraft = value
    if (!task || startDateField.pending || value === startDateField.value)
      return
    const ok = await startDateField.commit(value)
    if (!ok) startDateDraft = startDateField.value
  }

  function closeDueDate() {
    dueDateOpen = false
    void tick().then(() => dueDateTrigger?.focus())
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

  // Shared radiogroup arrow-key index helper (WAI-ARIA APG).
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
    const idx = nextRadiogroupIndex(
      e.key,
      STATUSES.indexOf(statusField.value),
      STATUSES.length
    )
    if (idx === null) return
    e.preventDefault()
    statusField.value = STATUSES[idx]
    statusDebouncer.trigger()
    ;(e.currentTarget as HTMLElement)
      .querySelector<HTMLElement>(`[data-status="${STATUSES[idx]}"]`)
      ?.focus()
  }

  async function applyStatus(s: TaskStatus) {
    if (!task || s === statusCommitted || statusField.pending) return
    if (s === 'DONE' && task.is_blocked) {
      const result = await blockedGuard.check(task.id, true, undefined)
      if (result !== 'clear') return
    }
    await commitStatusWrite(s)
  }

  async function commitStatusWrite(s: TaskStatus) {
    if (!task || s === statusCommitted || statusField.pending) return
    const ok = await statusField.commit(s)
    if (ok) {
      statusCommitted = s
    } else {
      statusField.value = statusCommitted
    }
    if (statusField.value !== statusCommitted) statusDebouncer.trigger()
  }

  async function setStatus(s: TaskStatus) {
    statusField.value = s
    await applyStatus(s)
  }

  function flushStatusCommit() {
    void applyStatus(statusField.value)
  }
  const statusDebouncer = trailingDebounce(flushStatusCommit, 200)

  function confirmBlockedDone() {
    blockedGuard.dismiss()
    void commitStatusWrite('DONE')
  }

  async function cancelBlockedDone() {
    blockedGuard.dismiss()
    statusField.value = statusCommitted
    await tick()
    rootRef
      ?.querySelector<HTMLElement>(`[data-status="${statusField.value}"]`)
      ?.focus()
  }

  async function commitEstimate() {
    if (!task || estimateField.pending) return
    const trimmed = estimateField.value.trim()
    const prev = formatEstimateMinutes(task.estimate_minutes)
    if (trimmed === prev) {
      estimateInvalid = false
      return
    }
    const ok = await estimateField.commit(trimmed)
    estimateInvalid = !ok
  }

  async function commitOwner() {
    if (!task || ownerField.pending) return
    const trimmed = ownerDraft.trim()
    if (trimmed === ownerField.value) return
    const ok = await ownerField.commit(trimmed)
    if (!ok) {
      ownerDraft = ownerField.value
    }
  }

  let priorityCheckedIdx = $derived(PRIORITIES.indexOf(priorityField.value))

  async function commitPriority(p: number) {
    if (!task || priorityField.pending || p === priorityCommitted) return
    const ok = await priorityField.commit(p)
    if (ok) {
      priorityCommitted = p
    } else {
      priorityField.value = priorityCommitted
    }
    if (priorityField.value !== priorityCommitted) priorityDebouncer.trigger()
  }

  function onPriorityKeydown(e: KeyboardEvent) {
    const curIdx = priorityCheckedIdx >= 0 ? priorityCheckedIdx : 0
    const idx = nextRadiogroupIndex(e.key, curIdx, PRIORITIES.length)
    if (idx === null) return
    e.preventDefault()
    priorityField.value = PRIORITIES[idx]
    priorityDebouncer.trigger()
    ;(e.currentTarget as HTMLElement)
      .querySelector<HTMLElement>(`[data-priority="${PRIORITIES[idx]}"]`)
      ?.focus()
  }

  function flushPriorityCommit() {
    void commitPriority(priorityField.value)
  }
  const priorityDebouncer = trailingDebounce(flushPriorityCommit, 200)

  $effect(() => {
    return () => {
      statusDebouncer.cancel()
      priorityDebouncer.cancel()
    }
  })

  async function commitTags(newTags: string[], announcement: string) {
    if (!task || tagsField.pending) return
    tagDraft = ''
    tagsAnnouncement = announcement
    const ok = await tagsField.commit(newTags)
    if (!ok) {
      tagsAnnouncement = announcement
        .replace('Added tag', "Couldn't add")
        .replace('Removed tag', "Couldn't remove")
    }
  }

  function addTag() {
    if (!task || tagsField.pending) return
    const t = tagDraft.trim().replace(/^#/, '')
    if (!t || tagsField.value.includes(t)) {
      tagDraft = ''
      return
    }
    void commitTags([...tagsField.value, t], `Added tag ${t}`)
  }

  function removeTag(t: string) {
    if (!task || tagsField.pending || !tagsField.value.includes(t)) return
    void commitTags(
      tagsField.value.filter((x) => x !== t),
      `Removed tag ${t}`
    )
  }

  async function commitTitle() {
    if (!task || titleField.pending) return
    const trimmed = titleDraft.trim()
    if (!trimmed) {
      titleDraft = titleField.value
      return
    }
    if (trimmed === titleField.value) return
    const ok = await titleField.commit(trimmed)
    if (!ok) {
      titleDraft = titleField.value
    }
  }

  // Computed next-occurrence preview from the LOCAL optimistic mirrors.
  let nextOccurrence = $derived(
    nextRecurrenceDate(recurrenceField.value, dueDateField.value)
  )
</script>

<div bind:this={rootRef} class="task-metadata-sidebar">
  {#if headingId}
    <h2 id={headingId} class="sr-only">Edit task: {task.clean_content}</h2>
  {/if}

  <header
    data-testid="task-primary-header"
    class="space-y-3 border-b border-surface-card-border bg-surface-card px-5 py-4 {stickyPrimary
      ? 'sticky top-0 z-20 shadow-lg'
      : ''}"
  >
    <div class="flex items-start gap-2">
      {#if task.recurrence}
        <span
          class="material-symbols-outlined mt-1 shrink-0 text-icon-md text-accent-secondary-start"
          aria-hidden="true"
          title="Recurring: {task.recurrence}">event_repeat</span
        >
      {/if}
      <input
        type="text"
        class="min-w-0 flex-1 rounded border-none bg-transparent px-1 font-headline-md text-headline-md text-text-primary outline-none placeholder:text-text-muted hover:bg-hover focus:ring-1 focus:ring-accent-primary-start/40 {titleField.pending
          ? 'opacity-50'
          : ''}"
        bind:value={titleDraft}
        readonly={titleField.pending}
        aria-busy={titleField.pending}
        aria-label="Task title"
        onblur={commitTitle}
        onkeydown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            void commitTitle()
          }
        }}
      />
      {#if onClose}
        <button
          type="button"
          onclick={onClose}
          aria-label="Close detail panel"
          class="shrink-0 rounded p-1 text-text-muted transition-colors hover:bg-hover hover:text-text-primary focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent-primary-start"
        >
          <span class="material-symbols-outlined" aria-hidden="true">close</span
          >
        </button>
      {/if}
    </div>

    <section aria-labelledby="task-status-label">
      <h3
        id="task-status-label"
        class="mb-1 font-label-sm-bold text-type-2xs uppercase tracking-widest text-text-muted"
      >
        Status
      </h3>
      <div
        class="flex items-center gap-0.5 rounded-lg border border-surface-panel-border bg-surface-panel p-0.5"
        role="radiogroup"
        aria-label="Task status"
        tabindex="-1"
        onkeydown={onStatusKeydown}
      >
        {#each STATUSES as s (s)}
          <button
            data-status={s}
            type="button"
            onclick={() => void setStatus(s)}
            role="radio"
            aria-checked={statusField.value === s}
            tabindex={statusField.value === s ? 0 : -1}
            disabled={statusField.pending}
            class="flex-1 cursor-pointer rounded border-none px-2.5 py-1 font-label-sm transition-colors disabled:cursor-not-allowed disabled:opacity-50"
            class:bg-hover={statusField.value === s}
            class:text-accent-primary-start={statusField.value === s}
            class:text-text-muted={statusField.value !== s}
          >
            {laneLabel(s)}
          </button>
        {/each}
      </div>
    </section>

    <div class="grid grid-cols-2 gap-2">
      <section class="min-w-0">
        <h3
          class="mb-1 font-label-sm-bold text-type-2xs uppercase tracking-widest text-text-muted"
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
          disabled={dueDateField.pending}
          aria-haspopup="dialog"
          aria-expanded={dueDateOpen}
          class="flex w-full items-center justify-between gap-1 rounded border border-surface-card-border bg-surface-card px-2 py-1.5 font-label-sm text-type-xs text-text-primary transition-colors hover:bg-hover disabled:opacity-50"
        >
          <span class="flex min-w-0 items-center gap-1.5">
            <span
              class="material-symbols-outlined shrink-0 text-icon-sm {dueDateField.value
                ? 'text-accent-secondary-start'
                : 'text-text-muted'}"
              aria-hidden="true">event</span
            >
            <span class="truncate">{dueDateField.value || 'Set date…'}</span>
          </span>
          <span
            class="material-symbols-outlined shrink-0 text-icon-sm text-text-muted"
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
              transition:fly={{ y: -4, duration: motionDuration(100) }}
              role="dialog"
              aria-label="Due date options"
            >
              <div class="border-b border-surface-card-border p-2">
                <input
                  type="date"
                  aria-label="Custom due date"
                  class="w-full rounded border border-surface-card-border bg-surface-card px-2 py-1 font-label-sm text-type-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40"
                  value={dueDateField.value}
                  oninput={(e) => (dueDateField.value = e.currentTarget.value)}
                  onkeydown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault()
                      e.stopPropagation()
                      if (dueDateField.value)
                        void commitDueDate(dueDateField.value)
                    } else if (e.key === 'Escape') {
                      e.preventDefault()
                      e.stopPropagation()
                      closeDueDate()
                    }
                  }}
                />
              </div>
              {#each dueDatePresets as preset (preset.label)}
                <button
                  type="button"
                  class="w-full px-3 py-1.5 text-left font-label-sm text-type-sm transition-colors hover:bg-hover {dueDateField.value ===
                  preset.value
                    ? 'font-label-sm-bold text-accent-primary-start'
                    : 'text-text-primary'}"
                  onclick={() => void commitDueDate(preset.value)}
                >
                  {preset.label}
                  <span class="ml-1 text-type-2xs text-text-muted"
                    >{preset.value}</span
                  >
                </button>
              {/each}
              {#if dueDateField.value}
                <div class="border-t border-surface-card-border">
                  <button
                    type="button"
                    class="w-full px-3 py-1.5 text-left font-label-sm text-type-sm text-text-muted transition-colors hover:bg-hover"
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

      <section>
        <h3
          class="mb-1 font-label-sm-bold text-type-2xs uppercase tracking-widest text-text-muted"
        >
          Pin
        </h3>
        <button
          type="button"
          onclick={togglePin}
          disabled={pinField.pending}
          class="flex w-full items-center justify-between rounded border border-surface-card-border bg-surface-card px-2 py-1.5 font-label-sm text-type-xs text-text-primary transition-colors hover:bg-hover disabled:cursor-not-allowed disabled:opacity-50"
          aria-pressed={pinField.value}
        >
          <span class="flex items-center gap-1.5">
            <span
              class="material-symbols-outlined text-icon-sm"
              aria-hidden="true">push_pin</span
            >
            {pinField.value ? 'Pinned' : 'Pin task'}
          </span>
          {#if pinField.value}
            <span
              class="material-symbols-outlined text-icon-sm text-accent-primary-start"
              aria-hidden="true">check</span
            >
          {/if}
        </button>
      </section>
    </div>

    {#if metaError}
      <ErrorBanner
        message={`Couldn't save: ${metaError}`}
        dataTestId="task-meta-error"
      />
    {/if}
  </header>

  <section
    aria-labelledby="task-essentials-heading"
    class="space-y-4 px-5 py-5"
  >
    <div class="flex items-center gap-2">
      <span
        class="material-symbols-outlined text-icon-md text-accent-secondary-start"
        aria-hidden="true">person_edit</span
      >
      <h3
        id="task-essentials-heading"
        class="font-label-sm-bold text-type-xs uppercase tracking-widest text-text-primary"
      >
        Essentials
      </h3>
    </div>
    <dl class="flex flex-col gap-3 font-label-sm text-type-sm">
      <div class="flex items-center justify-between gap-3">
        <dt class="shrink-0 text-text-muted">
          <label for="task-owner-input">Owner</label>
        </dt>
        <dd class="min-w-0 flex-1">
          <input
            id="task-owner-input"
            type="text"
            class="w-full rounded-sm border border-surface-card-border bg-surface-card px-2 py-1 text-right text-text-primary focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40 {ownerField.pending
              ? 'opacity-50'
              : ''}"
            placeholder="Unassigned"
            bind:value={ownerDraft}
            readonly={ownerField.pending}
            aria-busy={ownerField.pending}
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
      <div class="flex items-center justify-between gap-3">
        <dt id="task-priority-label" class="shrink-0 text-text-muted">
          Priority
        </dt>
        <dd class="min-w-0 flex-1">
          <div
            class="flex items-center gap-0.5 rounded-lg border border-surface-panel-border bg-surface-panel p-0.5"
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
                aria-checked={priorityField.value === p}
                tabindex={priorityCheckedIdx >= 0
                  ? priorityCheckedIdx === i
                    ? 0
                    : -1
                  : i === 0
                    ? 0
                    : -1}
                disabled={priorityField.pending}
                class="flex-1 cursor-pointer rounded border-none px-2 py-0.5 font-label-sm transition-colors disabled:cursor-not-allowed disabled:opacity-50"
                class:bg-hover={priorityField.value === p}
                class:text-accent-primary-start={priorityField.value === p}
                class:text-text-muted={priorityField.value !== p}
              >
                {PRIORITY_LABELS[p]}
              </button>
            {/each}
          </div>
        </dd>
      </div>
      <div class="flex items-start justify-between gap-3">
        <dt class="shrink-0 pt-1 text-text-muted">
          <label for="task-start-date-input">Start day</label>
        </dt>
        <dd class="min-w-0 flex-1">
          <input
            id="task-start-date-input"
            type="date"
            bind:value={startDateDraft}
            disabled={startDateField.pending}
            aria-busy={startDateField.pending}
            onchange={(e) => void commitStartDate(e.currentTarget.value)}
            class="w-full rounded-sm border border-surface-card-border bg-surface-card px-2 py-1 text-text-primary focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40 disabled:opacity-50"
          />
        </dd>
      </div>
      <div class="flex items-start justify-between gap-3">
        <dt class="shrink-0 pt-1 text-text-muted">Tags</dt>
        <dd class="min-w-0 flex-1">
          <span class="sr-only" aria-live="polite">{tagsAnnouncement}</span>
          <ul class="flex flex-wrap items-center justify-end gap-1">
            {#each tagsField.value as tg (tg)}
              <li
                class="flex items-center gap-0.5 rounded-sm border border-accent-secondary-start/30 bg-accent-secondary-glow px-1.5 py-0.5 text-type-sm text-accent-secondary-start"
              >
                <span>{tg}</span>
                <button
                  type="button"
                  class="text-text-muted transition-colors hover:text-error disabled:opacity-50"
                  aria-label="Remove tag {tg}"
                  disabled={tagsField.pending}
                  onclick={() => removeTag(tg)}
                >
                  <span aria-hidden="true">×</span></button
                >
              </li>
            {/each}
            <li class="min-w-0 flex-1">
              <label class="sr-only" for="task-tag-add">Add a tag</label>
              <input
                id="task-tag-add"
                type="text"
                class="w-full rounded border border-surface-card-border bg-transparent px-1.5 py-0.5 text-right text-type-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40 disabled:opacity-50"
                placeholder="Add tag…"
                bind:value={tagDraft}
                disabled={tagsField.pending}
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

  <details
    class="group border-t border-surface-card-border"
    bind:open={planningOpen}
    data-testid="task-planning-disclosure"
  >
    <summary
      class="cursor-pointer px-5 py-4 text-text-primary transition-colors hover:bg-hover focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent-primary-start"
    >
      <span class="font-label-sm-bold text-type-sm">Planning & tracking</span>
    </summary>
    <div class="space-y-6 px-5 pb-5 pt-2">
      <section>
        <div class="mb-2 flex items-center justify-between">
          <h3
            class="font-label-sm-bold text-type-2xs uppercase tracking-widest text-text-muted"
          >
            Progress
          </h3>
          <span class="font-label-sm text-type-xs text-text-primary">
            {progressState}%{#if task.subtask_total > 0}
              <span
                class="ml-1 text-text-muted"
                data-testid="task-subtask-count"
                aria-label="{task.subtask_done} of {task.subtask_total} subtasks done"
                >[{task.subtask_done}/{task.subtask_total}]</span
              >
            {/if}
          </span>
        </div>
        <input
          type="range"
          min="0"
          max="100"
          value={progressState}
          oninput={(e) => {
            if (!progressPending) progressState = Number(e.currentTarget.value)
          }}
          onchange={onProgressChange}
          disabled={progressPending}
          aria-label="Task progress"
          class="w-full accent-accent-secondary-start disabled:opacity-50"
        />
        <div
          class="mt-2 h-1 overflow-hidden rounded border border-surface-card-border bg-surface-card"
        >
          <div
            class="h-full bg-accent-secondary-start transition-all"
            style="width: {progressState}%"
          ></div>
        </div>
      </section>

      <section>
        <div class="mb-1 flex items-center justify-between gap-2">
          <h3
            class="font-label-sm-bold text-type-2xs uppercase tracking-widest text-text-muted"
          >
            <label for="task-estimate-input">Estimate</label>
          </h3>
          <input
            id="task-estimate-input"
            type="text"
            data-testid="task-estimate-input"
            class="w-28 rounded-sm border border-surface-card-border bg-surface-card px-2 py-0.5 text-right text-text-primary focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40 {estimateField.pending
              ? 'opacity-50'
              : ''}"
            placeholder="—"
            value={estimateField.value}
            oninput={(e) => (estimateField.value = e.currentTarget.value)}
            readonly={estimateField.pending}
            aria-busy={estimateField.pending}
            aria-invalid={estimateInvalid}
            aria-describedby="task-estimate-hint"
            onblur={() => void commitEstimate()}
            onkeydown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                void commitEstimate()
              }
            }}
          />
        </div>
        <p
          id="task-estimate-hint"
          class="font-label-sm text-type-2xs text-text-muted"
        >
          Try 30m, 2h, 1d, or 2.5d
        </p>
      </section>

      <section>
        <h3
          class="mb-2 font-label-sm-bold text-type-2xs uppercase tracking-widest text-text-muted"
        >
          Recurrence
        </h3>
        {#if dueDateField.value}
          <button
            bind:this={recurrenceTrigger}
            type="button"
            onclick={() => {
              recurrenceOpen = !recurrenceOpen
              recurrenceFocusIdx = 0
            }}
            onkeydown={onRecurrenceKeydown}
            disabled={recurrenceField.pending}
            aria-haspopup="listbox"
            aria-expanded={recurrenceOpen}
            aria-controls="recurrence-listbox"
            class="flex w-full items-center justify-between rounded border border-surface-card-border bg-surface-card px-3 py-2 font-label-sm text-type-sm text-text-primary transition-colors hover:bg-hover disabled:opacity-50"
          >
            <span class="flex items-center gap-2">
              <span
                class="material-symbols-outlined text-icon-md {recurrenceField.value
                  ? 'text-accent-secondary-start'
                  : 'text-text-muted'}"
                aria-hidden="true">event_repeat</span
              >
              {recurrenceField.value || 'Set recurrence…'}
            </span>
            <span
              class="material-symbols-outlined text-icon-sm text-text-muted"
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
                transition:fly={{ y: -4, duration: motionDuration(100) }}
                role="listbox"
                tabindex="-1"
                aria-label="Recurrence options"
              >
                <div class="border-b border-surface-card-border p-2">
                  <input
                    type="text"
                    placeholder="Custom (e.g. every 5 days)"
                    aria-label="Custom recurrence rule"
                    class="w-full rounded border border-surface-card-border bg-surface-card px-2 py-1 font-label-sm text-type-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40"
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
                    aria-selected={recurrenceField.value === preset.value}
                    tabindex={recurrenceFocusIdx === i ? 0 : -1}
                    class="w-full px-3 py-1.5 text-left font-label-sm text-type-sm transition-colors hover:bg-hover {recurrenceField.value ===
                    preset.value
                      ? 'font-label-sm-bold text-accent-primary-start'
                      : 'text-text-primary'} {recurrenceFocusIdx === i
                      ? 'bg-hover'
                      : ''}"
                    onclick={() => void commitRecurrence(preset.value)}
                  >
                    {preset.value}
                    {#if preset.hint}
                      <span class="ml-1 text-type-2xs text-text-muted"
                        >({preset.hint})</span
                      >
                    {/if}
                  </button>
                {/each}
                {#if recurrenceField.value}
                  <div class="border-t border-surface-card-border">
                    <button
                      type="button"
                      role="option"
                      aria-selected={false}
                      tabindex={recurrenceFocusIdx === RECURRENCE_PRESETS.length
                        ? 0
                        : -1}
                      class="w-full px-3 py-1.5 text-left font-label-sm text-type-sm text-text-muted transition-colors hover:bg-hover {recurrenceFocusIdx ===
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
          <p class="font-label-sm text-type-xs italic text-text-muted">
            Set a due date first to configure recurrence.
          </p>
        {/if}
        {#if nextOccurrence}
          <p class="mt-2 font-label-sm text-type-xs text-text-muted">
            Next occurrence <span class="text-accent-secondary-start"
              >{nextOccurrence}</span
            >
          </p>
        {:else if recurrenceField.value && dueDateField.value}
          <p class="mt-2 font-label-sm text-type-xs italic text-text-muted">
            Next occurrence is computed on completion.
          </p>
        {/if}
      </section>

      <DependencyPicker
        cardId={task.id}
        blockedBy={blockedByList}
        {ctx}
        {onMetaChanged}
      />
    </div>
  </details>

  <details
    class="group border-y border-surface-card-border"
    bind:open={activityOpen}
    data-testid="task-activity-disclosure"
  >
    <summary
      class="cursor-pointer px-5 py-4 text-text-primary transition-colors hover:bg-hover focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent-primary-start"
    >
      <span class="font-label-sm-bold text-type-sm">Activity</span>
      <span class="ml-1 font-label-sm text-type-xs text-text-muted">
        {task.comments_count} comments · {task.links_count} links · timestamps
      </span>
    </summary>
    <div class="space-y-5 px-5 pb-5 pt-2">
      {#if task.created_at || task.completed_at}
        <dl class="flex flex-col gap-2 font-label-sm text-type-xs">
          {#if task.created_at}
            <div class="flex items-start justify-between gap-3">
              <dt class="text-text-muted">Created</dt>
              <dd class="text-right text-text-primary">
                <time datetime={task.created_at}
                  >{formatTimestamp(task.created_at)}</time
                >
              </dd>
            </div>
          {/if}
          {#if task.completed_at}
            <div class="flex items-start justify-between gap-3">
              <dt class="text-text-muted">Completed</dt>
              <dd class="text-right text-text-primary">
                <time datetime={task.completed_at}
                  >{formatTimestamp(task.completed_at)}</time
                >
              </dd>
            </div>
          {/if}
        </dl>
      {/if}

      <CommentThread
        taskId={task.id}
        notebook={task.notebook}
        section={task.section}
        page={task.page}
        fileDate={task.file_date ?? ''}
        {ctx}
        onCommentsChanged={onMetaChanged}
      />
    </div>
  </details>
</div>

<BlockedDoneGuard
  pending={blockedGuard.pending}
  cardText={task.clean_content}
  onConfirm={confirmBlockedDone}
  onCancel={cancelBlockedDone}
/>

<style>
  /* Establish this component as a container so field pairs can collapse to a
     single column on narrow hosts (the 320px sub-editor modal aside) while
     staying side-by-side in the 480–540px task drawer. Native container-query
     pattern mirroring FormatToolbar.svelte — no JS, no props. The actual
     `.field-pair` collapse rule is added alongside its first use (header date
     row) to keep each commit free of unused-CSS warnings. */
  .task-metadata-sidebar {
    container-type: inline-size;
  }
</style>
