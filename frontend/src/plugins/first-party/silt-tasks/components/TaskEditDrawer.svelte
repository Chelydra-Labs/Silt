<script lang="ts">
  import { fly } from 'svelte/transition'
  import { tick, untrack } from 'svelte'
  import type { PluginContext, TaskStatus } from '../../../sdk'
  import { plusDaysISO } from '../../../sdk'
  import { trailingDebounce } from '../debounce'
  import { friendlyCaughtError } from '../errors'
  import ErrorBanner from './ErrorBanner.svelte'

  // Coerce a caught value to a friendly meta-error string. Maps the backend
  // focus-lock sentinel (#444) to actionable copy; passes everything else
  // through so unknown failures stay diagnosable.
  function errMsg(e: unknown): string {
    return friendlyCaughtError(e)
  }
  import type { TaskDetail } from '../types'
  import { PRIORITY_LABELS, laneLabel, priorityClass } from '../types'
  import DependencyPicker from './DependencyPicker.svelte'
  import BlockedDoneDialog from './BlockedDoneDialog.svelte'
  import CommentThread from './CommentThread.svelte'
  import Popover from '../../../../components/Popover.svelte'
  import { STANDALONE_TASKS_NOTEBOOK } from '../../../../lib/standaloneTasksNav'

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
  // Gate focus-capture on the closed→open transition only, so a task prop
  // reassignment (e.g. an onMetaChanged reload) doesn't yank focus back to
  // the panel out of whatever field the user is mid-edit on.
  let drawerOpen = false
  let lastTaskId = ''
  $effect(() => {
    const tid = task?.id ?? ''
    if (task && !drawerOpen) {
      drawerOpen = true
      previouslyFocused = document.activeElement as HTMLElement
      // tabindex=-1 lets the panel receive focus without joining the tab order.
      tick().then(() => panelRef?.focus())
    } else if (task && drawerOpen && tid !== lastTaskId) {
      // Switching A→B with the drawer open: capture B's trigger so close
      // returns to the most recently clicked card, not A's (which may be
      // scrolled out of view). Guard against capturing the panel itself.
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
  // statusCommitted is the anchor for revert: the last status successfully
  // persisted (or the task's authoritative value on load). #442: arrow-key
  // navigation now flips statusState (local) instantly and debounces the
  // commit; on commit failure statusState reverts to statusCommitted rather
  // than the immediately-prior (possibly also-uncommitted) selection.
  let statusCommitted = $state<TaskStatus>('TODO')
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
  // priorityCommitted mirrors statusCommitted for the priority radiogroup (#442).
  let priorityCommitted = $state(2)
  let tagsState = $state<string[]>([])
  let tagsPending = $state(false)
  let tagDraft = $state('')
  // Live-region text announcing tag adds/removes (a11y).
  let tagsAnnouncement = $state('')
  let titleState = $state('')
  let titleDraft = $state('')
  let titlePending = $state(false)
  // Estimate draft is the raw [estimate::] string (e.g. "2h"); empty clears.
  let estimateDraft = $state('')
  let estimatePending = $state(false)
  // True after a failed estimate save until the next successful edit/clear.
  let estimateInvalid = $state(false)

  /** Format minutes for the estimate input; hide missing/zero. */
  function formatEstimateDraft(mins: number | null | undefined): string {
    if (mins == null || mins <= 0) return ''
    if (mins % 480 === 0) return `${mins / 480}d`
    if (mins % 60 === 0) return `${mins / 60}h`
    if (mins % 30 === 0 && mins > 60) return `${mins / 60}h`
    return `${mins}m`
  }

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
    void task?.estimate_minutes
    // Guard each mirror with its pending flag (via untrack so the flag
    // doesn't become a dependency) — a reload during an in-flight edit
    // mustn't clobber the optimistic value. Without untrack, the pending
    // flag flipping to false after an error would re-fire this effect and
    // clear metaError before the user sees it.
    if (untrack(() => !pinPending)) pinState = task?.pinned ? true : false
    if (untrack(() => !progressPending)) progressState = task?.progress ?? 0
    if (untrack(() => !recurrencePending))
      recurrenceState = task?.recurrence ?? ''
    if (untrack(() => !dueDatePending)) dueDateState = task?.due_date ?? ''
    if (untrack(() => !statusPending)) {
      statusState = task?.status ?? 'TODO'
      statusCommitted = task?.status ?? 'TODO'
    }
    if (untrack(() => !ownerPending)) {
      ownerState = task?.owner ?? ''
      ownerDraft = task?.owner ?? ''
    }
    if (untrack(() => !priorityPending)) {
      priorityState = task?.priority ?? 2
      priorityCommitted = task?.priority ?? 2
    }
    if (untrack(() => !tagsPending)) {
      tagsState = task?.tags ? task.tags.split('|').filter(Boolean) : []
    }
    if (untrack(() => !titlePending)) {
      titleState = task?.clean_content ?? ''
      titleDraft = task?.clean_content ?? ''
    }
    if (untrack(() => !estimatePending)) {
      estimateDraft = formatEstimateDraft(task?.estimate_minutes)
      estimateInvalid = false
    }
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
      metaError = errMsg(e)
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
        metaError = errMsg(err)
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
      metaError = errMsg(e)
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
      metaError = errMsg(e)
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
    // #442: update the local selection INSTANTLY on every arrow/Home/End and
    // reschedule a trailing-debounced commit. The previous code swallowed any
    // arrow pressed during an in-flight write (if (statusPending) return), so
    // rapid nav (Normal → Low → wrap to Critical) landed only the first press.
    const idx = nextRadiogroupIndex(
      e.key,
      STATUSES.indexOf(statusState),
      STATUSES.length
    )
    if (idx === null) return
    e.preventDefault()
    statusState = STATUSES[idx]
    statusDebouncer.trigger()
    // Move focus with the arrow immediately. The DONE-on-blocked guard now
    // fires after the debounce settle (inside applyStatus); if the user
    // cancels it, cancelBlockedDone reverts statusState and re-points focus.
    ;(e.currentTarget as HTMLElement)
      .querySelector<HTMLElement>(`[data-status="${STATUSES[idx]}"]`)
      ?.focus()
  }

  // applyStatus is the single entry point for a status selection, shared by the
  // debounced arrow path (flushStatusCommit) and the immediate click path
  // (setStatus). It runs the DONE-on-blocked guard then delegates the write to
  // commitStatusWrite. confirmBlockedDone calls commitStatusWrite directly
  // (the user already confirmed the guard — don't re-trigger it).
  async function applyStatus(s: TaskStatus) {
    if (!task || s === statusCommitted || statusPending) return
    // DONE-on-blocked guard (#302): pause and render the shared
    // BlockedDoneDialog before committing. statusState may already show DONE
    // optimistically (arrow path); cancelBlockedDone reverts it on cancel.
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
        metaError = errMsg(e)
        return
      }
    }
    await commitStatusWrite(s)
  }

  // commitStatusWrite performs the optimistic write + revert-on-error, with
  // statusCommitted as the revert anchor. No guard — callers handle that.
  async function commitStatusWrite(s: TaskStatus) {
    if (!task || s === statusCommitted || statusPending) return
    statusPending = true
    metaError = ''
    try {
      await ctx.updateBlockState(task.id, s)
      statusCommitted = s
      onMetaChanged?.()
    } catch (e) {
      statusState = statusCommitted // revert local selection to last committed
      metaError = errMsg(e)
    } finally {
      statusPending = false
      // #442 follow-up: if a newer arrow selection landed (statusState diverged
      // from statusCommitted) while this commit was in-flight on a slow IPC,
      // applyStatus's `statusPending` early-return dropped it. Re-arm the
      // debouncer so the latest selection eventually commits. Terminates: a
      // successful commit sets statusCommitted=s (no divergence); a failed one
      // reverts statusState=statusCommitted (no divergence) — so this fires at
      // most one catch-up cycle per dropped selection.
      if (statusState !== statusCommitted) statusDebouncer.trigger()
    }
  }

  // Click path: commit immediately (clicks are discrete, no rapid-fire).
  async function setStatus(s: TaskStatus) {
    statusState = s
    await applyStatus(s)
  }

  // Debounced arrow-key commit: applies the latest local selection.
  function flushStatusCommit() {
    void applyStatus(statusState)
  }
  const statusDebouncer = trailingDebounce(flushStatusCommit, 200)

  function confirmBlockedDone() {
    pendingBlockedDone = null
    void commitStatusWrite('DONE')
  }

  async function cancelBlockedDone() {
    pendingBlockedDone = null
    // Revert the optimistic DONE flip the arrow path made; statusCommitted
    // still holds the pre-DONE value (we never committed DONE).
    statusState = statusCommitted
    // The DONE radio (the arrow target) holds focus with tabindex=-1 while
    // the checked radio holds tabindex=0. Re-point focus to the still-checked
    // radio so roving tabindex stays consistent.
    await tick()
    panelRef
      ?.querySelector<HTMLElement>(`[data-status="${statusState}"]`)
      ?.focus()
  }

  // --- Estimate editor (#439) ---
  // Commit on blur/Enter. Empty string clears the estimate. The backend
  // validates m/h/d grammar; invalid input reverts the draft.
  async function commitEstimate() {
    if (!task || estimatePending) return
    const trimmed = estimateDraft.trim()
    const prev = formatEstimateDraft(task.estimate_minutes)
    if (trimmed === prev) {
      estimateInvalid = false
      return
    }
    const prevDraft = estimateDraft
    estimateDraft = trimmed
    estimatePending = true
    metaError = ''
    try {
      await ctx.setTaskEstimate(task.id, trimmed)
      estimateInvalid = false
      onMetaChanged?.()
    } catch (e) {
      estimateDraft = prevDraft
      estimateInvalid = true
      metaError = errMsg(e)
    } finally {
      estimatePending = false
    }
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
      metaError = errMsg(e)
    } finally {
      ownerPending = false
    }
  }

  // --- Priority editor (#412) ---
  // Segmented radiogroup; reuses the shared nextRadiogroupIndex helper.
  // #442: arrow nav updates priorityState instantly + debounces the commit;
  // clicks go through commitPriority immediately (discrete events).
  let priorityCheckedIdx = $derived(PRIORITIES.indexOf(priorityState))

  async function commitPriority(p: number) {
    if (!task || priorityPending || p === priorityCommitted) return
    priorityState = p
    priorityPending = true
    metaError = ''
    try {
      await ctx.setTaskPriority(task.id, p)
      priorityCommitted = p
      onMetaChanged?.()
    } catch (e) {
      priorityState = priorityCommitted // revert local selection to last committed
      metaError = errMsg(e)
    } finally {
      priorityPending = false
      // #442 follow-up: catch up a newer arrow selection that landed while this
      // commit was in-flight (see commitStatusWrite for the termination proof).
      if (priorityState !== priorityCommitted) priorityDebouncer.trigger()
    }
  }

  function onPriorityKeydown(e: KeyboardEvent) {
    // Instant local update on every arrow/Home/End; no in-flight swallow.
    const curIdx = priorityCheckedIdx >= 0 ? priorityCheckedIdx : 0
    const idx = nextRadiogroupIndex(e.key, curIdx, PRIORITIES.length)
    if (idx === null) return
    e.preventDefault()
    priorityState = PRIORITIES[idx]
    priorityDebouncer.trigger()
    ;(e.currentTarget as HTMLElement)
      .querySelector<HTMLElement>(`[data-priority="${PRIORITIES[idx]}"]`)
      ?.focus()
  }

  function flushPriorityCommit() {
    void commitPriority(priorityState)
  }
  const priorityDebouncer = trailingDebounce(flushPriorityCommit, 200)

  // #442: cancel any in-flight debounced commit when the drawer unmounts so a
  // pending write never fires against a stale/gone task.
  $effect(() => {
    return () => {
      statusDebouncer.cancel()
      priorityDebouncer.cancel()
    }
  })

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
      metaError = errMsg(e)
    } finally {
      tagsPending = false
    }
  }

  function addTag() {
    if (!task || tagsPending) return
    // Chips render without '#', so users naturally type '#work'. Strip a single
    // leading '#' so the local dedupe check (tagsState holds bare names) works
    // and we never send '#work' to a backend that re-prefixes '#'.
    const t = tagDraft.trim().replace(/^#/, '')
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
      metaError = errMsg(e)
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
    // Don't close on Escape while a Popover (recurrence/due-date), the
    // BlockedDoneDialog, or the comment reply composer is open — those
    // consume Escape first (reply also stopPropagation on its keydown).
    if (
      e.key === 'Escape' &&
      task &&
      !recurrenceOpen &&
      !dueDateOpen &&
      !pendingBlockedDone
    ) {
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
        {#if priorityState >= 1 && priorityState <= 3}
          <span
            class="self-start px-1.5 py-0.5 border rounded-sm font-label-sm text-type-3xs uppercase tracking-wide w-fit {priorityClass(
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
              class="material-symbols-outlined text-icon-md text-accent-secondary-start shrink-0 mt-1"
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
        <ErrorBanner
          message={`Couldn't save: ${metaError}`}
          dataTestId="task-meta-error"
        />
      {/if}

      <!-- Status radiogroup -->
      <section>
        <h3
          class="font-label-sm-bold uppercase tracking-widest text-type-2xs text-text-muted mb-2"
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
          disabled={dueDatePending}
          aria-haspopup="dialog"
          aria-expanded={dueDateOpen}
          class="w-full flex items-center justify-between px-3 py-2 rounded border border-surface-card-border bg-surface-card hover:bg-hover transition-colors disabled:opacity-50 text-type-sm font-label-sm text-text-primary"
        >
          <span class="flex items-center gap-2">
            <span
              class="material-symbols-outlined text-icon-md {dueDateState
                ? 'text-accent-secondary-start'
                : 'text-text-muted'}"
              aria-hidden="true">event</span
            >
            {dueDateState || 'Set due date…'}
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
                  class="w-full text-left px-3 py-1.5 text-type-sm font-label-sm hover:bg-hover transition-colors {dueDateState ===
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
              {#if dueDateState}
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
          disabled={pinPending}
          class="w-full flex items-center justify-between px-3 py-2 rounded border border-surface-card-border bg-surface-card hover:bg-hover transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          aria-pressed={pinState}
        >
          <span
            class="flex items-center gap-2 text-type-sm font-label-sm text-text-primary"
          >
            <span class="material-symbols-outlined text-icon-md">push_pin</span>
            {pinState ? 'Pinned' : 'Pin'}
          </span>
          {#if pinState}
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
            class="w-28 px-2 py-0.5 border rounded-sm text-text-primary border-surface-card-border bg-surface-card text-right focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40 {estimatePending
              ? 'opacity-50'
              : ''}"
            placeholder="—"
            bind:value={estimateDraft}
            readonly={estimatePending}
            aria-busy={estimatePending}
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
            class="w-full flex items-center justify-between px-3 py-2 rounded border border-surface-card-border bg-surface-card hover:bg-hover transition-colors disabled:opacity-50 text-type-sm font-label-sm text-text-primary"
          >
            <span class="flex items-center gap-2">
              <span
                class="material-symbols-outlined text-icon-md {recurrenceState
                  ? 'text-accent-secondary-start'
                  : 'text-text-muted'}"
                aria-hidden="true">event_repeat</span
              >
              {recurrenceState || 'Set recurrence…'}
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
                    aria-selected={recurrenceState === preset.value}
                    tabindex={recurrenceFocusIdx === i ? 0 : -1}
                    class="w-full text-left px-3 py-1.5 text-type-sm font-label-sm hover:bg-hover transition-colors {recurrenceState ===
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
                {#if recurrenceState}
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
            <dd class="flex-1 max-w-55">
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
                {#each tagsState as tg (tg)}
                  <li
                    class="flex items-center gap-0.5 px-1.5 py-0.5 border rounded-sm text-type-sm text-accent-secondary-start border-accent-secondary-start/30 bg-accent-secondary-glow"
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
                    class="flex-1 min-w-25 px-1.5 py-0.5 text-type-sm bg-transparent border border-surface-card-border rounded focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40 text-text-primary placeholder:text-text-muted disabled:opacity-50"
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
          <span class="material-symbols-outlined text-icon-md">chat_bubble</span
          >
          <span class="text-type-sm font-label-sm">{task.comments_count}</span>
          <span class="text-type-2xs font-label-sm text-text-muted"
            >comments</span
          >
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

      <!-- Source breadcrumb (source-aware) -->
      <section class="pt-2 border-t border-surface-card-border">
        <p class="text-type-2xs font-label-sm text-text-muted break-all">
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
