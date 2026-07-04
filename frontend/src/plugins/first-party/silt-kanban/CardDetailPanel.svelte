<script lang="ts">
  import { fly } from 'svelte/transition'
  import { tick } from 'svelte'
  import type { PluginContext, TaskStatus } from '../../sdk'
  import type { KanbanCard } from './types'
  import { PRIORITY_LABELS, laneLabel } from './types'
  import DependencyPicker from './DependencyPicker.svelte'
  import Popover from '../../../components/Popover.svelte'

  interface Props {
    card: KanbanCard | null
    ctx: PluginContext
    onClose: () => void
    // Called after a successful updateTaskMeta so the parent board can
    // re-query and reflect the new pin/progress on the card. Without this,
    // the board's lanes hold stale data until the next unrelated reload.
    onMetaChanged?: () => void
  }

  let { card, ctx, onClose, onMetaChanged }: Props = $props()

  // Focus management (mirrors BlockedDoneDialog): move focus into the panel on
  // open and restore it to the trigger on close, so keyboard/AT users keep
  // context. The panel is always-mounted with card toggling null, so the
  // $effect keys on card presence and tracks the previously-focused element.
  let panelRef = $state<HTMLDivElement | null>(null)
  let previouslyFocused: HTMLElement | null = null
  $effect(() => {
    if (card) {
      previouslyFocused = document.activeElement as HTMLElement
      // Focus the panel container once it renders. tabindex=-1 lets it
      // receive focus without joining the tab order.
      tick().then(() => panelRef?.focus())
    } else if (previouslyFocused) {
      previouslyFocused.focus?.()
      previouslyFocused = null
    }
  })

  function statusChipClass(s: TaskStatus): string {
    if (s === 'TODO') return 'text-text-muted border-border-muted bg-surface'
    if (s === 'DOING')
      return 'text-accent-secondary-start border-accent-secondary-start/30 bg-accent-secondary-glow'
    return 'text-accent-primary-start border-accent-primary-start/30 bg-accent-primary-glow'
  }

  let tagList = $derived(card?.tags ? card.tags.split('|').filter(Boolean) : [])

  // The blocked_by edge list is pipe-delimited in the Kanban SQL projection
  // (#301); split it into the uuid array the DependencyPicker consumes.
  let blockedByList = $derived(
    card?.blocked_by ? card.blocked_by.split('|').filter(Boolean) : []
  )

  // Local optimistic mirrors for the two mutable metadata fields (pin +
  // progress). The panel is the only writer for these while open, so an
  // optimistic update + revert-on-failure matches the board's `commitMove`
  // contract: the UI reflects the change immediately, and if the markdown
  // write fails (focus lock held, disk error) we revert + surface the
  // reason in an aria-live region instead of silently drifting.
  let pinState = $state(false)
  let progressState = $state(0)
  let recurrenceState = $state('')
  let metaError = $state('')
  // Pending flags disable the control while an IPC write is in-flight.
  // This serializes user interactions so two rapid pin toggles (or slider
  // changes) can't race on the Go side — LockFileWrite serializes writes
  // per file but preserves Go's IPC arrival order, not JS dispatch order,
  // so concurrent in-flight calls can land out-of-order and leave the disk
  // (last writer) out of sync with the optimistic UI state.
  let pinPending = $state(false)
  let progressPending = $state(false)
  let recurrencePending = $state(false)
  let recurrenceOpen = $state(false)
  // Anchor for the recurrence <Popover>; the trigger button binds this so the
  // floating listbox can be positioned + portaled out of the scroll container.
  let recurrenceTrigger = $state<HTMLButtonElement | null>(null)
  let recurrenceFocusIdx = $state(-1)
  let customRecurrence = $state('')

  // Common recurrence presets with short subtitles to disambiguate
  // easily-misread pairs (every day vs every weekday).
  const RECURRENCE_PRESETS: { value: string; hint: string }[] = [
    { value: 'every day', hint: '7 days a week' },
    { value: 'every weekday', hint: 'Mon – Fri' },
    { value: 'every week', hint: '' },
    { value: 'every 2 weeks', hint: '' },
    { value: 'every month', hint: '' },
    { value: 'every 3 months', hint: 'Quarterly' },
    { value: 'every year', hint: '' }
  ]

  $effect(() => {
    // Read the individual fields so Svelte 5's fine-grained reactivity
    // tracks them as deps — if the parent ever mutates card.pinned or
    // card.progress on the same object identity, the effect re-runs.
    void card?.pinned
    void card?.progress
    void card?.recurrence
    pinState = card?.pinned ?? false
    progressState = card?.progress ?? 0
    recurrenceState = card?.recurrence ?? ''
    metaError = ''
  })

  async function togglePin() {
    if (!card || pinPending) return
    const prev = pinState
    pinState = !pinState
    pinPending = true
    metaError = ''
    try {
      await ctx.updateTaskMeta(card.id, { pinned: pinState })
      onMetaChanged?.()
    } catch (e) {
      pinState = prev
      metaError = e instanceof Error ? e.message : String(e)
    } finally {
      pinPending = false
    }
  }

  // Monotonic token so a failed earlier slider write can't revert over a
  // successful later one. With the slider disabled during writes, the two
  // can't overlap, but the guard is retained as defense-in-depth.
  let progressSeq = 0
  function onProgressChange(e: Event) {
    if (!card || progressPending) return
    const v = Number((e.target as HTMLInputElement).value)
    const prev = progressState
    const my = ++progressSeq
    progressState = v
    progressPending = true
    metaError = ''
    void (async () => {
      try {
        await ctx.updateTaskMeta(card.id, { progress: v })
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

  function openInEditor() {
    if (!card) return
    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: {
          notebook: card.notebook,
          section: card.section,
          page: card.page,
          date: card.file_date,
          blockId: card.id
        }
      })
    )
  }

  async function commitRecurrence(value: string) {
    if (!card || recurrencePending) return
    const prev = recurrenceState
    recurrenceState = value
    closeRecurrence()
    recurrencePending = true
    metaError = ''
    try {
      await ctx.setTaskRecurrence(card.id, value)
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

  // Keyboard handler on the trigger button: ↓/↑ navigate the option list,
  // Enter commits the focused option, Escape closes the dropdown (not the
  // panel). stopPropagation prevents the window-level Escape handler from
  // also firing and closing the entire panel.
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

  // Computed next-occurrence preview for the metadata section: shows the
  // user what date the next instance will land on if they complete the task
  // today. Client-side only — the authoritative computation is server-side
  // at completion time. When the task is overdue, we omit the preview
  // rather than show a misleading date (the skip-missed logic depends on
  // the server's clock, not the client's, so a client-side guess for
  // non-day rules would be wrong).
  let nextOccurrence = $derived.by(() => {
    if (!card?.recurrence || !card?.due_date) return ''
    const due = new Date(card.due_date + 'T00:00:00')
    if (isNaN(due.getTime())) return ''
    const today = new Date()
    today.setHours(0, 0, 0, 0)
    // Overdue: the server-side skip-missed resolver will advance, but the
    // exact landing date depends on the server's clock and the rule unit.
    // Showing a wrong guess is worse than showing nothing.
    if (due <= today) return ''
    // Not overdue: one interval forward from the due date is deterministic.
    const rule = card.recurrence.toLowerCase()
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
    // Don't close the panel on Escape if the recurrence dropdown is open —
    // the dropdown's own Escape handler closes the dropdown first.
    if (e.key === 'Escape' && card && !recurrenceOpen) {
      e.preventDefault()
      onClose()
    }
  }

  // Esc-to-close listener is bound only while the panel is open (card
  // is non-null). When closed, no global keydown listener intercepts
  // Esc presses that other handlers (Settings, command palette) may need.
  $effect(() => {
    if (!card) return
    window.addEventListener('keydown', onWindowKeydown)
    return () => window.removeEventListener('keydown', onWindowKeydown)
  })
</script>

{#if card}
  <!-- Click-away scrim: a click on the area beside the panel closes it. -->
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div
    class="fixed inset-0 z-30 bg-black/30"
    aria-hidden="true"
    onclick={onClose}
  ></div>
  <div
    bind:this={panelRef}
    transition:fly={{ x: 320, duration: 200 }}
    class="fixed right-0 top-14 h-[calc(100vh-56px)] w-96 bg-panel border-l border-border-muted z-40 overflow-y-auto custom-scrollbar focus:outline-none"
    role="dialog"
    aria-modal="true"
    aria-labelledby="card-detail-title"
    tabindex="-1"
  >
    <!-- Header -->
    <div
      class="flex items-start justify-between gap-2 px-5 py-4 border-b border-border-muted sticky top-0 bg-panel"
    >
      <div class="flex flex-col gap-1.5 min-w-0">
        {#if card.priority && card.priority <= 3}
          <span
            class="self-start px-1.5 py-0.5 border rounded-sm font-label-sm text-[9px] uppercase tracking-wide w-fit {statusChipClass(
              card.status
            )}"
          >
            {PRIORITY_LABELS[card.priority] ?? 'Normal'}
          </span>
        {/if}
        <h2
          id="card-detail-title"
          class="font-headline-md text-headline-md text-text-primary break-words"
        >
          {#if card.recurrence}
            <span
              class="material-symbols-outlined text-[16px] text-accent-secondary-start align-middle mr-1"
              aria-hidden="true"
              title="Recurring: {card.recurrence}">event_repeat</span
            >
          {/if}
          {card.clean_content}
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
      <!-- Metadata -->
      <section>
        <h3
          class="font-label-sm-bold uppercase tracking-widest text-[10px] text-text-muted mb-3"
        >
          Metadata
        </h3>
        <dl class="flex flex-col gap-2.5 text-[12px] font-label-sm">
          <div class="flex items-center justify-between">
            <dt class="text-text-muted">Status</dt>
            <dd
              class="flex items-center gap-1 px-2 py-0.5 border rounded-sm {statusChipClass(
                card.status
              )}"
            >
              {#if card.status === 'DOING'}
                <span class="material-symbols-outlined text-[14px]"
                  >radio_button_checked</span
                >
              {/if}
              {laneLabel(card.status)}
            </dd>
          </div>
          <div class="flex items-center justify-between">
            <dt class="text-text-muted">Owner</dt>
            <dd
              class="px-2 py-0.5 border rounded-sm text-text-primary border-border-muted bg-surface"
            >
              {card.owner || '—'}
            </dd>
          </div>
          <div class="flex items-center justify-between">
            <dt class="text-text-muted">Priority</dt>
            <dd class="text-text-primary">
              {card.priority
                ? (PRIORITY_LABELS[card.priority] ?? 'Normal')
                : '—'}
            </dd>
          </div>
          <div class="flex items-center justify-between">
            <dt class="text-text-muted">Due date</dt>
            <dd class="text-text-primary">{card.due_date || '—'}</dd>
          </div>
          <div class="flex items-center justify-between">
            <dt class="text-text-muted">Recurrence</dt>
            <dd class="text-text-primary">{recurrenceState || '—'}</dd>
          </div>
          {#if nextOccurrence}
            <div class="flex items-center justify-between">
              <dt class="text-text-muted">Next occurrence</dt>
              <dd class="text-accent-secondary-start">{nextOccurrence}</dd>
            </div>
          {:else if card.recurrence && card.due_date}
            <!-- Overdue recurring task: the next date depends on the server's
            skip-missed resolver at completion time. -->
            <div class="flex items-center justify-between">
              <dt class="text-text-muted">Next occurrence</dt>
              <dd class="text-text-muted italic text-[11px]">
                Computed on completion
              </dd>
            </div>
          {/if}
          <div class="flex items-center justify-between">
            <dt class="text-text-muted">Start date</dt>
            <dd class="text-text-primary">{card.start_date || '—'}</dd>
          </div>
          {#if tagList.length > 0}
            <div class="flex items-start justify-between gap-2">
              <dt class="text-text-muted shrink-0 pt-0.5">Tags</dt>
              <dd class="flex flex-wrap gap-1 justify-end">
                {#each tagList as tg (tg)}
                  <span
                    class="px-1.5 py-0.5 border rounded-sm text-[10px] text-accent-secondary-start border-accent-secondary-start/30 bg-accent-secondary-glow"
                    >{tg}</span
                  >
                {/each}
              </dd>
            </div>
          {/if}
        </dl>
      </section>

      <!-- Pin toggle -->
      <section>
        <button
          type="button"
          onclick={togglePin}
          disabled={pinPending}
          class="w-full flex items-center justify-between px-3 py-2 rounded border border-border-muted bg-surface hover:bg-hover transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
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
          class="mt-2 h-1 bg-surface border border-border-muted rounded overflow-hidden"
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
        {#if card.due_date}
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
            class="w-full flex items-center justify-between px-3 py-2 rounded border border-border-muted bg-surface hover:bg-hover transition-colors disabled:opacity-50 text-[12px] font-label-sm text-text-primary"
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
            class="rounded border border-border-muted bg-panel shadow-lg"
          >
            {#snippet content()}
              <div
                id="recurrence-listbox"
                transition:fly={{ y: -4, duration: 100 }}
                role="listbox"
                tabindex="-1"
                aria-label="Recurrence options"
              >
                <!-- Custom free-text input for rules beyond the presets -->
                <div class="p-2 border-b border-border-muted">
                  <input
                    type="text"
                    placeholder="Custom (e.g. every 5 days)"
                    aria-label="Custom recurrence rule"
                    class="w-full px-2 py-1 text-[12px] font-label-sm bg-surface border border-border-muted rounded text-text-primary focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40"
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
                  <div class="border-t border-border-muted">
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
            <button
              type="button"
              class="text-accent-primary-start underline ml-1 not-italic"
              onclick={openInEditor}>Open editor</button
            >
          </p>
        {/if}
      </section>

      <!-- Counts -->
      <section class="flex items-center gap-4">
        <div class="flex items-center gap-1.5 text-text-muted">
          <span class="material-symbols-outlined text-[16px]">chat_bubble</span>
          <span class="text-[12px] font-label-sm">{card.comments_count}</span>
          <span class="text-[10px] font-label-sm text-text-muted">comments</span
          >
        </div>
        <div class="flex items-center gap-1.5 text-text-muted">
          <span class="material-symbols-outlined text-[16px]">link</span>
          <span class="text-[12px] font-label-sm">{card.links_count}</span>
          <span class="text-[10px] font-label-sm text-text-muted">links</span>
        </div>
      </section>

      {#if card}
        <DependencyPicker
          cardId={card.id}
          blockedBy={blockedByList}
          {ctx}
          {onMetaChanged}
        />
      {/if}

      <!-- Open in editor -->
      <section>
        <button
          type="button"
          onclick={openInEditor}
          class="w-full flex items-center justify-center gap-2 px-3 py-2 rounded border border-accent-primary-start/30 bg-accent-primary-glow text-accent-primary-start hover:brightness-110 transition-all font-label-sm-bold"
        >
          <span class="material-symbols-outlined text-[16px]">open_in_new</span>
          Open in editor
        </button>
      </section>

      <!-- Source context breadcrumb -->
      <section class="pt-2 border-t border-border-muted">
        <p class="text-[10px] font-label-sm text-text-muted break-all">
          {card.notebook} › {card.section} › {card.page}
        </p>
      </section>
    </div>
  </div>
{/if}
