<script lang="ts">
  // Presentational board task card. Pure render from props; the board owns
  // all DnD/selection state and passes pre-bound callbacks keyed to this
  // card + its column.
  import { PRIORITY_LABELS, priorityClass, type TaskDetail } from '../types'
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
    onSelect
  }: Props = $props()
</script>

<div
  data-card={card.id}
  data-index={index}
  role="button"
  tabindex="0"
  aria-grabbed={dragging ? 'true' : 'false'}
  aria-label={`${card.clean_content}, ${colLabel}${card.owner ? `, owner ${card.owner}` : ''}${card.due_date ? `, due ${card.due_date}` : ''}${card.pinned ? ', pinned' : ''}${card.recurrence ? `, recurring ${card.recurrence}` : ''}${card.is_blocked ? ', blocked by unfinished prerequisite' : ''}${card.subtask_total > 0 ? `, ${card.subtask_done} of ${card.subtask_total} subtasks done` : ''}.${dndEnabled ? ' Arrow keys change ' + groupBy + '.' : ''}`}
  draggable={dndEnabled ? 'true' : 'false'}
  class="group relative bg-surface-card border border-surface-card-border rounded-lg p-3 transition-all duration-200 hover:bg-hover hover:-translate-y-px hover:shadow-lg focus:outline-none focus:ring-2 focus:ring-accent-primary-start/40 {card.status ===
  'DOING'
    ? 'border-l-2 border-l-accent-secondary-start'
    : ''} {dndEnabled ? 'cursor-grab' : ''} {dragging
    ? 'opacity-40 rotate-2'
    : ''} {dragOver ? 'ring-2 ring-accent-primary-start/60' : ''}"
  ondragstart={(e) => onDragStart(e)}
  ondragend={onDragEnd}
  ondragover={(e) => onCardDragOver(e)}
  ondragleave={onCardDragLeave}
  ondrop={(e) => onCardDrop(e)}
  onkeydown={(e) => onKeydown(e)}
  onclick={onSelect}
>
  {#if card.pinned}
    <span
      class="material-symbols-outlined absolute top-2 right-2 text-icon-sm text-accent-primary-start"
      aria-label="pinned">push_pin</span
    >
  {/if}
  <div class="flex justify-between items-start mb-2 gap-2">
    {#if card.priority && card.priority <= 3}
      <span
        class="px-1.5 py-0.5 border rounded-sm font-label-sm text-type-3xs uppercase tracking-wide {priorityClass(
          card.priority
        )}"
      >
        {PRIORITY_LABELS[card.priority] ?? 'Normal'}
      </span>
    {/if}
    {#if card.status === 'DONE'}
      <span
        class="material-symbols-outlined text-accent-primary-start text-icon-md {card.pinned
          ? ''
          : 'ml-auto'}">check_circle</span
      >
    {/if}
  </div>
  <p
    class="text-type-md font-body-md text-text-primary mb-2 {card.status ===
    'DONE'
      ? 'line-through opacity-60'
      : ''}"
  >
    {card.clean_content}
  </p>
  {#if card.progress > 0}
    <div class="h-0.5 bg-surface-panel rounded overflow-hidden mb-2">
      <div
        class="h-full bg-accent-secondary-start transition-all"
        style="width: {card.progress}%"
      ></div>
    </div>
  {/if}
  <div class="flex justify-between items-center gap-2">
    <div class="flex items-center gap-1.5">
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
          class="text-type-3xs text-accent-secondary-start bg-accent-secondary-glow border border-accent-secondary-start/30 rounded-sm px-1.5 py-0.5 font-label-sm"
        >
          [{card.owner}]
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
