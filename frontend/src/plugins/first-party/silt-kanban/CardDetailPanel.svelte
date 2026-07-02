<script lang="ts">
  import { fly } from 'svelte/transition'
  import type { PluginContext, TaskStatus } from '../../sdk'
  import type { KanbanCard } from './types'
  import { PRIORITY_LABELS, laneLabel } from './types'

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

  function statusChipClass(s: TaskStatus): string {
    if (s === 'TODO') return 'text-text-muted border-border-muted bg-surface'
    if (s === 'DOING')
      return 'text-accent-secondary-start border-accent-secondary-start/30 bg-accent-secondary-glow'
    return 'text-accent-primary-start border-accent-primary-start/30 bg-accent-primary-glow'
  }

  let tagList = $derived(card?.tags ? card.tags.split('|').filter(Boolean) : [])

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
  let recurrenceFocusIdx = $state(-1)

  // Common recurrence presets offered in the autocomplete dropdown. The
  // backend validates grammar server-side; these are the curated set from
  // the UX research (Todoist/Things/Obsidian Tasks sweet spot).
  const RECURRENCE_PRESETS: string[] = [
    'every day',
    'every weekday',
    'every week',
    'every 2 weeks',
    'every month',
    'every 3 months',
    'every year'
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
    recurrenceOpen = false
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

  function onRecurrenceKeydown(e: KeyboardEvent) {
    if (!recurrenceOpen) return
    const options = [...RECURRENCE_PRESETS]
    if (recurrenceState) options.push('') // "Stop recurring"
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      recurrenceFocusIdx = Math.min(recurrenceFocusIdx + 1, options.length - 1)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      recurrenceFocusIdx = Math.max(recurrenceFocusIdx - 1, 0)
    } else if (e.key === 'Enter') {
      e.preventDefault()
      const selected = options[recurrenceFocusIdx]
      if (selected !== undefined) void commitRecurrence(selected)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      recurrenceOpen = false
    }
  }

  function onWindowKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && card) {
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
    transition:fly={{ x: 320, duration: 200 }}
    class="fixed right-0 top-14 h-[calc(100vh-56px)] w-96 bg-panel border-l border-border-muted z-40 overflow-y-auto custom-scrollbar"
    role="dialog"
    aria-modal="true"
    aria-labelledby="card-detail-title"
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
              aria-label="Recurring task">event_repeat</span
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
            <dd class="text-text-primary">{card.recurrence || '—'}</dd>
          </div>
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
          <div class="relative">
            <button
              type="button"
              onclick={() => {
                recurrenceOpen = !recurrenceOpen
                recurrenceFocusIdx = 0
              }}
              disabled={recurrencePending}
              aria-haspopup="listbox"
              aria-expanded={recurrenceOpen}
              class="w-full flex items-center justify-between px-3 py-2 rounded border border-border-muted bg-surface hover:bg-hover transition-colors disabled:opacity-50 text-[12px] font-label-sm text-text-primary"
            >
              <span class="flex items-center gap-2">
                <span
                  class="material-symbols-outlined text-[16px] {recurrenceState
                    ? 'text-accent-secondary-start'
                    : 'text-text-muted'}">event_repeat</span
                >
                {recurrenceState || 'Set recurrence…'}
              </span>
              <span
                class="material-symbols-outlined text-[14px] text-text-muted"
                >expand_more</span
              >
            </button>
            {#if recurrenceOpen}
              <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
              <div
                class="fixed inset-0 z-10"
                onclick={() => (recurrenceOpen = false)}
                aria-hidden="true"
              ></div>
              <div
                transition:fly={{ y: -4, duration: 100 }}
                class="absolute left-0 right-0 mt-1 z-20 rounded border border-border-muted bg-panel shadow-lg overflow-hidden"
                role="listbox"
                tabindex="-1"
                aria-label="Recurrence options"
                onkeydown={onRecurrenceKeydown}
              >
                {#each RECURRENCE_PRESETS as preset, i (preset)}
                  <button
                    type="button"
                    role="option"
                    aria-selected={recurrenceState === preset}
                    tabindex={recurrenceFocusIdx === i ? 0 : -1}
                    class="w-full text-left px-3 py-1.5 text-[12px] font-label-sm hover:bg-hover transition-colors {recurrenceState ===
                    preset
                      ? 'text-accent-primary-start font-label-sm-bold'
                      : 'text-text-primary'} {recurrenceFocusIdx === i
                      ? 'bg-hover'
                      : ''}"
                    onclick={() => void commitRecurrence(preset)}
                  >
                    {preset}
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
            {/if}
          </div>
        {:else}
          <p class="text-[11px] font-label-sm text-text-muted italic">
            Set a due date first to configure recurrence.
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
