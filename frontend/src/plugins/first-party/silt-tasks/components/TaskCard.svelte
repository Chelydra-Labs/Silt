<script lang="ts">
  // Presentational board task card. Pure render from props; the board owns
  // all DnD/selection state and passes pre-bound callbacks keyed to this
  // card + its column.
  import {
    PRIORITY_LABELS,
    laneLabel,
    priorityClass,
    type TaskDetail
  } from '../types'
  import { dueDateClass, dueDateTextClass } from '../dueDate'
  import type { GroupBy } from '../state.svelte'

  interface Props {
    card: TaskDetail
    index: number
    colLabel: string
    dndEnabled: boolean
    groupBy: GroupBy
    today: string
    dragging: boolean
    dragOver: boolean
    onDragStart: (e: DragEvent) => void
    onDragEnd: () => void
    onCardDragOver: (e: DragEvent) => void
    onCardDragLeave: () => void
    onCardDrop: (e: DragEvent) => void
    onKeydown: (e: KeyboardEvent) => void
    onSelect: () => void
    selected?: boolean
  }

  let {
    card,
    index,
    colLabel,
    dndEnabled,
    groupBy,
    today,
    dragging,
    dragOver,
    onDragStart,
    onDragEnd,
    onCardDragOver,
    onCardDragLeave,
    onCardDrop,
    onKeydown,
    onSelect,
    selected = false
  }: Props = $props()

  let cardTags = $derived(
    (card.tags ?? '')
      .split('|')
      .map((tag) => tag.trim())
      .filter(Boolean)
  )
</script>

<div
  data-card={card.id}
  data-index={index}
  data-status={card.status}
  data-task-hit
  role="button"
  tabindex="0"
  aria-grabbed={dragging ? 'true' : 'false'}
  aria-current={selected ? 'true' : undefined}
  aria-label={`${card.clean_content}, ${colLabel}${card.owner ? `, owner ${card.owner}` : ''}${card.due_date ? `, due ${card.due_date}` : ''}${card.pinned ? ', pinned' : ''}${card.recurrence ? `, recurring ${card.recurrence}` : ''}${card.is_blocked ? ', blocked by unfinished prerequisite' : ''}${card.subtask_total > 0 ? `, ${card.subtask_done} of ${card.subtask_total} subtasks done` : ''}.${dndEnabled ? ' Arrow keys change ' + groupBy + '.' : ''}`}
  draggable={dndEnabled ? 'true' : 'false'}
  class="task-card group relative overflow-hidden rounded-lg border border-surface-card-border bg-surface-card p-3 pl-3.5 shadow-sm transition-all duration-200 hover:-translate-y-px hover:border-border-active hover:bg-hover hover:shadow-md focus:outline-none focus-visible:ring-2 focus-visible:ring-border-focus {dndEnabled
    ? 'cursor-grab active:cursor-grabbing'
    : ''} {dragging ? 'opacity-40 scale-95' : ''} {dragOver
    ? 'task-card-drop-target'
    : ''} {selected ? 'tasks-selected' : ''}"
  ondragstart={(e) => onDragStart(e)}
  ondragend={onDragEnd}
  ondragover={(e) => onCardDragOver(e)}
  ondragleave={onCardDragLeave}
  ondrop={(e) => onCardDrop(e)}
  onkeydown={(e) => onKeydown(e)}
  onclick={onSelect}
>
  <span class="task-card-status-rail" aria-hidden="true"></span>
  {#if card.pinned}
    <span
      class="material-symbols-outlined absolute top-2 right-2 text-icon-sm text-accent-primary-start"
      aria-label="pinned">push_pin</span
    >
  {/if}
  <div class="mb-2 flex min-h-5 items-center gap-1.5 pr-5">
    <span
      class="text-type-3xs font-label-sm uppercase tracking-widest {card.status ===
      'DOING'
        ? 'text-accent-secondary-start'
        : card.status === 'DONE'
          ? 'text-accent-primary-start'
          : 'text-text-muted'}"
    >
      {laneLabel(card.status)}
    </span>
    {#if card.priority && card.priority <= 3}
      <span
        class="ml-auto px-1.5 py-0.5 border rounded-sm font-label-sm text-type-3xs uppercase tracking-wide {priorityClass(
          card.priority
        )}"
      >
        {PRIORITY_LABELS[card.priority] ?? 'Normal'}
      </span>
    {/if}
    {#if card.status === 'DONE'}
      <span
        class="material-symbols-outlined text-accent-primary-start text-icon-md"
        aria-hidden="true">check_circle</span
      >
    {/if}
  </div>
  <p
    class="mb-2 text-type-md font-body-md font-medium leading-snug text-text-primary {card.status ===
    'DONE'
      ? 'line-through opacity-60'
      : ''}"
  >
    {card.clean_content}
  </p>
  {#if card.progress > 0}
    <div
      class="mb-2 h-1 overflow-hidden rounded-full bg-surface-panel"
      role="progressbar"
      aria-label="Task progress"
      aria-valuemin="0"
      aria-valuemax="100"
      aria-valuenow={card.progress}
    >
      <div
        class="h-full bg-accent-secondary-start transition-all"
        style="width: {card.progress}%"
      ></div>
    </div>
  {/if}
  {#if cardTags.length > 0}
    <div class="mb-2 flex min-w-0 flex-wrap gap-1" aria-label="Tags">
      {#each cardTags.slice(0, 2) as tag (tag)}
        <span
          class="max-w-30 truncate rounded-full border border-surface-card-border bg-surface-panel px-1.5 py-0.5 text-type-3xs font-label-sm text-text-muted"
          title={tag}>#{tag}</span
        >
      {/each}
      {#if cardTags.length > 2}
        <span class="text-type-3xs font-label-sm text-text-muted"
          >+{cardTags.length - 2}</span
        >
      {/if}
    </div>
  {/if}
  <div
    class="flex justify-between items-center gap-2 border-t border-surface-card-border/60 pt-2"
  >
    <div class="flex min-w-0 items-center gap-1.5">
      {#if card.subtask_total > 0}
        <span
          class="text-type-3xs text-text-muted font-label-sm"
          data-testid={`board-subtask-badge-${card.id}`}
          title={`${card.subtask_done} of ${card.subtask_total} subtasks done`}
          aria-label={`${card.subtask_done} of ${card.subtask_total} subtasks done`}
        >
          [{card.subtask_done}/{card.subtask_total}]
        </span>
      {/if}
      {#if card.owner}
        <span
          class="max-w-25 truncate text-type-3xs text-accent-secondary-start bg-accent-secondary-glow border border-accent-secondary-start/30 rounded-full px-1.5 py-0.5 font-label-sm"
          title={`Owner: ${card.owner}`}
        >
          {card.owner}
        </span>
      {/if}
    </div>
    <div class="flex items-center gap-1.5">
      {#if card.comments_count > 0}
        <span
          class="text-type-3xs text-text-muted font-label-sm flex items-center gap-0.5"
          title="{card.comments_count} comments"
        >
          <span class="material-symbols-outlined text-icon-xs">chat_bubble</span
          >
          {card.comments_count}
        </span>
      {/if}
      {#if card.links_count > 0}
        <span
          class="text-type-3xs text-text-muted font-label-sm flex items-center gap-0.5"
          title="{card.links_count} links"
        >
          <span class="material-symbols-outlined text-icon-xs">link</span>
          {card.links_count}
        </span>
      {/if}
      {#if card.due_date}
        <span
          class="text-type-3xs {card.status === 'DONE'
            ? 'text-text-muted'
            : dueDateTextClass(
                dueDateClass(card.due_date, today)
              )} font-label-sm flex items-center gap-0.5"
        >
          <span class="material-symbols-outlined text-icon-xs">schedule</span>
          {card.due_date}
        </span>
      {/if}
      {#if card.recurrence}
        <span
          class="text-accent-secondary-start flex items-center"
          title="Recurring: {card.recurrence}"
        >
          <span
            class="material-symbols-outlined text-icon-xs"
            aria-hidden="true">event_repeat</span
          >
        </span>
      {/if}
      {#if card.is_blocked}
        <span
          class="text-status-warn flex items-center"
          role="img"
          title="Blocked by unfinished prerequisite task(s)"
          aria-label="Blocked by unfinished prerequisite task(s)"
        >
          <span
            class="material-symbols-outlined text-icon-xs"
            aria-hidden="true">lock</span
          >
        </span>
      {/if}
    </div>
  </div>
</div>

<style>
  .task-card-status-rail {
    position: absolute;
    inset: 0 auto 0 0;
    width: 3px;
    background: var(--color-text-muted);
    opacity: 0.45;
  }

  .task-card[data-status='DOING'] .task-card-status-rail {
    background: var(--color-accent-secondary-start);
    opacity: 1;
  }

  .task-card[data-status='DONE'] .task-card-status-rail {
    background: var(--color-accent-primary-start);
    opacity: 1;
  }

  .task-card-drop-target {
    border-color: var(--color-accent-primary-start);
    background: var(--color-accent-primary-glow);
    box-shadow:
      0 0 0 2px var(--color-accent-primary-start),
      var(--shadow-md);
    transform: translateY(-2px);
  }

  .task-card.tasks-selected {
    border-color: var(--color-accent-primary-start);
    background: color-mix(
      in srgb,
      var(--color-accent-primary-glow) 100%,
      transparent
    );
    box-shadow: 0 0 0 1px var(--color-accent-primary-start) inset;
  }

  @media (prefers-reduced-motion: reduce) {
    .task-card {
      transition: none;
    }
  }
</style>
