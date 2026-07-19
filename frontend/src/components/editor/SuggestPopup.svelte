<script lang="ts">
  export interface SuggestPopupItem {
    id: string
    label: string
    hint?: string
  }

  interface Props {
    items: SuggestPopupItem[]
    selected: number
    coords: { left: number; top: number }
    emptyLabel: string
    onPick: (index: number) => void
    onHover: (index: number) => void
    ariaLabel?: string
    className?: string
  }

  let {
    items,
    selected,
    coords,
    emptyLabel,
    onPick,
    onHover,
    ariaLabel = 'Suggestions',
    className = ''
  }: Props = $props()

  const popupId = $props.id()
  const optionId = (index: number) => `${popupId}-option-${index}`
  const activeOption = $derived(
    selected >= 0 && selected < items.length ? optionId(selected) : undefined
  )
</script>

<div
  id={popupId}
  class="suggest-popup {className}"
  style="left:{coords.left}px; top:{coords.top}px"
  role="listbox"
  tabindex="-1"
  aria-label={ariaLabel}
  aria-activedescendant={activeOption}
>
  {#if items.length === 0}
    <div class="suggest-popup-empty" aria-live="polite">{emptyLabel}</div>
  {:else}
    {#each items as item, index (item.id)}
      <button
        type="button"
        id={optionId(index)}
        class="suggest-popup-item {className ? `${className}-item` : ''}"
        class:selected={index === selected}
        role="option"
        aria-selected={index === selected}
        onclick={() => onPick(index)}
        onmouseenter={() => onHover(index)}
      >
        {#if item.hint}
          <span class="suggest-popup-label">{item.label}</span>
          <span class="suggest-popup-hint">{item.hint}</span>
        {:else}
          {item.label}
        {/if}
      </button>
    {/each}
  {/if}
</div>

<style>
  .suggest-popup {
    position: fixed;
    z-index: 50;
    min-width: 200px;
    margin-top: 4px;
    padding: 4px;
    border: 1px solid var(--color-surface-popover-border);
    border-radius: 8px;
    background: var(--color-surface-popover);
    box-shadow: 0 8px 24px rgb(0 0 0 / 35%);
    display: flex;
    flex-direction: column;
  }

  .suggest-popup.meta-suggest {
    min-width: 240px;
  }

  .suggest-popup-item {
    display: flex;
    align-items: baseline;
    gap: 4px;
    padding: 6px 8px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--color-text-primary);
    text-align: left;
    cursor: pointer;
    font-family: inherit;
  }

  .suggest-popup-item.selected {
    background: var(--color-accent-primary-start);
    color: var(--color-text-on-accent);
  }

  .suggest-popup-item:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: -2px;
  }

  .meta-suggest .suggest-popup-item {
    gap: 10px;
  }

  .meta-suggest .suggest-popup-label {
    min-width: 64px;
    font-family: var(--font-mono, monospace);
    font-size: 0.85rem;
    font-weight: 600;
  }

  .suggest-popup-hint {
    font-size: 0.8rem;
    opacity: 0.8;
  }

  .suggest-popup-empty {
    padding: 8px;
    color: var(--color-text-muted);
    font-size: 0.8rem;
    text-align: center;
  }
</style>
