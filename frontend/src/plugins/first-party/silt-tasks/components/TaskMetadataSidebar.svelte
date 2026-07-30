<script lang="ts">
  import { SvelteDate } from 'svelte/reactivity'
  import { fly } from 'svelte/transition'
  import { tick, untrack } from 'svelte'
  import type { PluginContext, TaskStatus } from '../../../sdk'
  import { plusDaysISO } from '../../../sdk'
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
  }

  // busy is a write-only $bindable: the sidebar pushes its interaction state UP
  // to the host's Esc guard (TaskEditDrawer + TaskSubEditorModal bind it); the
  // sidebar never reads its own value, so the default is never consumed here.
  // Do NOT mirror it onto aria-busy — "a popover is open" is not a loading
  // state and would mislead assistive tech.
  // eslint-disable-next-line no-useless-assignment
  let { task, ctx, onMetaChanged, busy = $bindable() }: Props = $props()

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
  let tagDraft = $state('')
  let tagsAnnouncement = $state('')
  let titleDraft = $state('')
  let estimateInvalid = $state(false)

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
  let nextOccurrence = $derived.by(() => {
    const recurrence = recurrenceField.value
    const dueDate = dueDateField.value
    if (!recurrence || !dueDate) return ''
    const due = new SvelteDate(dueDate + 'T00:00:00')
    if (isNaN(due.getTime())) return ''
    const today = new SvelteDate()
    today.setHours(0, 0, 0, 0)
    if (due <= today) return ''
    const rule = recurrence.toLowerCase()
    let step: Date
    if (rule.includes('day') && !rule.includes('weekday')) {
      const n = parseInt(rule.match(/(\d+)\s*day/)?.[1] ?? '1')
      step = new SvelteDate(due.getTime() + n * 86400000)
    } else if (rule.includes('weekday')) {
      step = new SvelteDate(due.getTime() + 86400000)
      while (step.getDay() === 0 || step.getDay() === 6)
        step.setDate(step.getDate() + 1)
    } else if (rule.includes('week')) {
      const n = parseInt(rule.match(/(\d+)\s*week/)?.[1] ?? '1')
      step = new SvelteDate(due.getTime() + n * 7 * 86400000)
    } else if (rule.includes('month')) {
      const n = parseInt(rule.match(/(\d+)\s*month/)?.[1] ?? '1')
      step = new SvelteDate(
        due.getFullYear(),
        due.getMonth() + n,
        due.getDate()
      )
    } else if (rule.includes('year')) {
      const n = parseInt(rule.match(/(\d+)\s*year/)?.[1] ?? '1')
      step = new SvelteDate(
        due.getFullYear() + n,
        due.getMonth(),
        due.getDate()
      )
    } else {
      return ''
    }
    return step.toISOString().slice(0, 10)
  })
</script>

<div bind:this={rootRef} class="space-y-6">
  {#if metaError}
    <ErrorBanner
      message={`Couldn't save: ${metaError}`}
      dataTestId="task-meta-error"
    />
  {/if}

  <!-- Title editor -->
  <section>
    <input
      type="text"
      class="w-full bg-transparent border-none outline-none focus:ring-1 focus:ring-accent-primary-start/40 hover:bg-hover rounded -mx-1 px-1 placeholder:text-text-muted font-headline-md text-headline-md text-text-primary break-words {titleField.pending
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
  </section>

  <!-- Status radiogroup -->
  <section>
    <h3
      class="font-label-sm-bold uppercase tracking-widest text-type-2xs text-text-muted mb-2"
    >
      Status
    </h3>
    <div
      class="flex items-center gap-0.5 bg-surface-panel border border-surface-panel-border rounded-lg p-0.5"
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
          class="flex-1 px-2.5 py-1 rounded font-label-sm border-none cursor-pointer transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          class:bg-hover={statusField.value === s}
          class:text-accent-primary-start={statusField.value === s}
          class:text-text-muted={statusField.value !== s}
        >
          {laneLabel(s)}
        </button>
      {/each}
    </div>
  </section>

  <!-- Due-date editor -->
  <section>
    <h3
      class="font-label-sm-bold uppercase tracking-widest text-type-2xs text-text-muted mb-2"
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
      class="w-full flex items-center justify-between px-3 py-2 rounded border border-surface-card-border bg-surface-card hover:bg-hover transition-colors disabled:opacity-50 text-type-sm font-label-sm text-text-primary"
    >
      <span class="flex items-center gap-2">
        <span
          class="material-symbols-outlined text-icon-md {dueDateField.value
            ? 'text-accent-secondary-start'
            : 'text-text-muted'}"
          aria-hidden="true">event</span
        >
        {dueDateField.value || 'Set due date…'}
      </span>
      <span
        class="material-symbols-outlined text-icon-sm text-text-muted"
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
              class="w-full px-2 py-1 text-type-sm font-label-sm bg-surface-card border border-surface-card-border rounded text-text-primary focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40"
              value={dueDateField.value}
              oninput={(e) => (dueDateField.value = e.currentTarget.value)}
              onkeydown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  e.stopPropagation()
                  if (dueDateField.value) void commitDueDate(dueDateField.value)
                } else if (e.key === 'Escape') {
                  e.preventDefault()
                  e.stopPropagation()
                  closeDueDate()
                }
              }}
            />
          </div>
          {#each [{ label: 'Today', value: ctx.today }, { label: 'Tomorrow', value: plusDaysISO(ctx.today, 1) }, { label: 'Next week', value: plusDaysISO(ctx.today, 7) }] as preset (preset.label)}
            <button
              type="button"
              class="w-full text-left px-3 py-1.5 text-type-sm font-label-sm hover:bg-hover transition-colors {dueDateField.value ===
              preset.value
                ? 'text-accent-primary-start font-label-sm-bold'
                : 'text-text-primary'}"
              onclick={() => void commitDueDate(preset.value)}
            >
              {preset.label}
              <span class="text-text-muted text-type-2xs ml-1"
                >{preset.value}</span
              >
            </button>
          {/each}
          {#if dueDateField.value}
            <div class="border-t border-surface-card-border">
              <button
                type="button"
                class="w-full text-left px-3 py-1.5 text-type-sm font-label-sm text-text-muted hover:bg-hover transition-colors"
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
      disabled={pinField.pending}
      class="w-full flex items-center justify-between px-3 py-2 rounded border border-surface-card-border bg-surface-card hover:bg-hover transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
      aria-pressed={pinField.value}
    >
      <span
        class="flex items-center gap-2 text-type-sm font-label-sm text-text-primary"
      >
        <span class="material-symbols-outlined text-icon-md">push_pin</span>
        {pinField.value ? 'Pinned' : 'Pin'}
      </span>
      {#if pinField.value}
        <span
          class="material-symbols-outlined text-icon-md text-accent-primary-start"
          >check</span
        >
      {/if}
    </button>
  </section>

  <!-- Progress slider -->
  <section>
    <div class="flex items-center justify-between mb-2">
      <h3
        class="font-label-sm-bold uppercase tracking-widest text-type-2xs text-text-muted"
      >
        Progress
      </h3>
      <span class="text-type-xs font-label-sm text-text-primary">
        {progressState}%{#if task.subtask_total > 0}
          <span
            class="text-text-muted ml-1"
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
      class="mt-2 h-1 bg-surface-card border border-surface-card-border rounded overflow-hidden"
    >
      <div
        class="h-full bg-accent-secondary-start transition-all"
        style="width: {progressState}%"
      ></div>
    </div>
  </section>

  <!-- Estimate (#439) -->
  <section>
    <div class="flex items-center justify-between gap-2 mb-1">
      <h3
        class="font-label-sm-bold uppercase tracking-widest text-type-2xs text-text-muted"
      >
        <label for="task-estimate-input">Estimate</label>
      </h3>
      <input
        id="task-estimate-input"
        type="text"
        data-testid="task-estimate-input"
        class="w-28 px-2 py-0.5 border rounded-sm text-text-primary border-surface-card-border bg-surface-card text-right focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40 {estimateField.pending
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
      class="text-type-2xs text-text-muted font-label-sm"
    >
      30m, 2h, 1d, 2.5d
    </p>
  </section>

  <!-- Recurrence editor -->
  <section>
    <h3
      class="font-label-sm-bold uppercase tracking-widest text-type-2xs text-text-muted mb-2"
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
        class="w-full flex items-center justify-between px-3 py-2 rounded border border-surface-card-border bg-surface-card hover:bg-hover transition-colors disabled:opacity-50 text-type-sm font-label-sm text-text-primary"
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
                class="w-full px-2 py-1 text-type-sm font-label-sm bg-surface-card border border-surface-card-border rounded text-text-primary focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40"
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
                class="w-full text-left px-3 py-1.5 text-type-sm font-label-sm hover:bg-hover transition-colors {recurrenceField.value ===
                preset.value
                  ? 'text-accent-primary-start font-label-sm-bold'
                  : 'text-text-primary'} {recurrenceFocusIdx === i
                  ? 'bg-hover'
                  : ''}"
                onclick={() => void commitRecurrence(preset.value)}
              >
                {preset.value}
                {#if preset.hint}
                  <span class="text-text-muted text-type-2xs ml-1"
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
                  class="w-full text-left px-3 py-1.5 text-type-sm font-label-sm text-text-muted hover:bg-hover transition-colors {recurrenceFocusIdx ===
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
      <p class="text-type-xs font-label-sm text-text-muted italic">
        Set a due date first to configure recurrence.
      </p>
    {/if}
  </section>

  <!-- Read-only details -->
  <section>
    <h3
      class="font-label-sm-bold uppercase tracking-widest text-type-2xs text-text-muted mb-3"
    >
      Details
    </h3>
    <dl class="flex flex-col gap-2.5 text-type-sm font-label-sm">
      <div class="flex items-center justify-between gap-2">
        <dt class="text-text-muted shrink-0">
          <label for="task-owner-input">Owner</label>
        </dt>
        <dd class="flex-1 max-w-50">
          <input
            id="task-owner-input"
            type="text"
            class="w-full px-2 py-0.5 border rounded-sm text-text-primary border-surface-card-border bg-surface-card text-right focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40 {ownerField.pending
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
      <div class="flex items-center justify-between gap-2">
        <dt id="task-priority-label" class="text-text-muted shrink-0">
          Priority
        </dt>
        <dd class="flex-1 max-w-55">
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
                aria-checked={priorityField.value === p}
                tabindex={priorityCheckedIdx >= 0
                  ? priorityCheckedIdx === i
                    ? 0
                    : -1
                  : i === 0
                    ? 0
                    : -1}
                disabled={priorityField.pending}
                class="flex-1 px-2 py-0.5 rounded font-label-sm border-none cursor-pointer transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
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
      {:else if recurrenceField.value && dueDateField.value}
        <div class="flex items-center justify-between">
          <dt class="text-text-muted">Next occurrence</dt>
          <dd class="text-text-muted italic text-type-xs">
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
            {#each tagsField.value as tg (tg)}
              <li
                class="flex items-center gap-0.5 px-1.5 py-0.5 border rounded-sm text-type-sm text-accent-secondary-start border-accent-secondary-start/30 bg-accent-secondary-glow"
              >
                <span>{tg}</span>
                <button
                  type="button"
                  class="text-text-muted hover:text-error transition-colors disabled:opacity-50"
                  aria-label="Remove tag {tg}"
                  disabled={tagsField.pending}
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
                class="flex-1 min-w-25 px-1.5 py-0.5 text-type-sm bg-transparent border border-surface-card-border rounded focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40 text-text-primary placeholder:text-text-muted disabled:opacity-50"
                placeholder="Add…"
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

  <!-- Counts -->
  <section class="flex items-center gap-4">
    <div class="flex items-center gap-1.5 text-text-muted">
      <span class="material-symbols-outlined text-icon-md">chat_bubble</span>
      <span class="text-type-sm font-label-sm">{task.comments_count}</span>
      <span class="text-type-2xs font-label-sm text-text-muted">comments</span>
    </div>
    <div class="flex items-center gap-1.5 text-text-muted">
      <span class="material-symbols-outlined text-icon-md">link</span>
      <span class="text-type-sm font-label-sm">{task.links_count}</span>
      <span class="text-type-2xs font-label-sm text-text-muted">links</span>
    </div>
  </section>

  <CommentThread
    taskId={task.id}
    notebook={task.notebook}
    section={task.section}
    page={task.page}
    fileDate={task.file_date ?? ''}
    {ctx}
    onCommentsChanged={onMetaChanged}
  />

  <DependencyPicker
    cardId={task.id}
    blockedBy={blockedByList}
    {ctx}
    {onMetaChanged}
  />
</div>

<BlockedDoneGuard
  pending={blockedGuard.pending}
  cardText={task.clean_content}
  onConfirm={confirmBlockedDone}
  onCancel={cancelBlockedDone}
/>
