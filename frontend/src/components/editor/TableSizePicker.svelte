<script lang="ts">
  // Floating size picker for the "custom table" command (#172). Replaces the
  // native window.prompt with an in-app popover consistent with the link-input
  // and color-picker popovers. Rows/columns are clamped to a sane range so a
  // typo can't create a grid that freezes the editor.

  interface Props {
    left: number
    top: number
    onConfirm: (rows: number, cols: number) => void
    onCancel: () => void
  }

  let { left, top, onConfirm, onCancel }: Props = $props()

  const MIN = 1
  const MAX = 20
  let rows = $state(3)
  let cols = $state(3)

  function clamp(n: number): number {
    if (!Number.isFinite(n)) return MIN
    return Math.min(Math.max(Math.trunc(n), MIN), MAX)
  }

  function confirm(): void {
    onConfirm(clamp(rows), clamp(cols))
  }

  function onKeydown(e: KeyboardEvent): void {
    if (e.key === 'Enter') {
      e.preventDefault()
      confirm()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      onCancel()
    }
  }
</script>

<div
  class="table-size-popover"
  style="left:{left}px; top:{top}px"
  role="dialog"
  aria-label="Custom table dimensions"
  tabindex="-1"
  onkeydown={onKeydown}
>
  <label class="ts-field">
    <span class="ts-label">Rows</span>
    <input
      class="ts-input"
      type="number"
      inputmode="numeric"
      min={MIN}
      max={MAX}
      bind:value={rows}
      aria-label="Rows"
    />
  </label>
  <span class="ts-times" aria-hidden="true">×</span>
  <label class="ts-field">
    <span class="ts-label">Columns</span>
    <input
      class="ts-input"
      type="number"
      inputmode="numeric"
      min={MIN}
      max={MAX}
      bind:value={cols}
      aria-label="Columns"
    />
  </label>
  <button type="button" class="ts-insert" onclick={confirm}> Insert </button>
</div>

<style>
  .table-size-popover {
    position: fixed;
    z-index: 100;
    margin-top: 4px;
    padding: 8px;
    border-radius: 8px;
    background: var(--color-surface, #1e1e22);
    border: 1px solid var(--color-border-muted, #33333a);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .ts-field {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .ts-label {
    font-size: 10px;
    color: var(--color-text-muted, #8b95a3);
  }
  .ts-input {
    width: 56px;
    padding: 4px 6px;
    border: 1px solid var(--color-border-muted, #3a3f4b);
    border-radius: 6px;
    background: var(--color-surface, #1a1d24);
    color: var(--color-text-primary, #e6e6e6);
    font-size: 0.8rem;
    outline: none;
  }
  .ts-input:focus {
    border-color: var(--color-accent-primary-glow, #6fa3ff);
  }
  .ts-times {
    color: var(--color-text-muted, #8b95a3);
    margin-top: 14px;
  }
  .ts-insert {
    margin-top: 14px;
    padding: 4px 10px;
    border: none;
    border-radius: 6px;
    background: var(--color-accent-primary-start, #2dd4bf);
    color: #001813;
    font-size: 0.8rem;
    font-weight: 600;
    cursor: pointer;
  }
  .ts-insert:hover {
    filter: brightness(1.08);
  }
</style>
