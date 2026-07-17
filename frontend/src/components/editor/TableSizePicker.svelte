<script lang="ts">
  // Floating size picker for the "custom table" command (#172 / #583 / #586).
  // Replaces the native window.prompt with an in-app popover. This component
  // owns the correctness layer (viewport-safe placement, dialog focus trap +
  // restore, global Escape + click-outside dismissal, visible bounds with
  // non-silent clamping, theme-safe accent) and the UX layer (an 8×8 grid
  // preview that updates the dimensions live). Numeric controls still allow
  // the full 1–20 range; the grid is a fast selector capped at 8×8.

  import { flipOrClamp } from '../../lib/editor/popoverPositioning'

  interface Props {
    anchor: { top: number; bottom: number; left: number }
    onConfirm: (rows: number, cols: number) => void
    onCancel: () => void
  }

  let { anchor, onConfirm, onCancel }: Props = $props()

  const MIN = 1
  const MAX = 20
  // The directly selectable grid is 8×8; numeric controls cover the rest of
  // the 1–20 range (#586 OQ1).
  const GRID = 8

  let rows = $state(3)
  let cols = $state(3)
  // Grid-highlight extent (1..GRID), DERIVED from rows/cols so the filled cells
  // track numeric entry — not just grid clicks/arrows. The grid caps at 8×8
  // while the numeric controls reach 20, so a value above 8 lights the full
  // extent (the strongest available signal). Grid interaction writes rows/cols
  // via syncFromGrid; gridR/gridC are a read-only view of those, eliminating a
  // prior two-source-of-truth desync where typing >8 left the highlight stale.
  let gridR = $derived(clampGrid(rows))
  let gridC = $derived(clampGrid(cols))
  // Transient "Adjusted to N" notice when an input was silently corrected.
  let adjusted = $state('')

  let popoverEl = $state<HTMLDivElement | null>(null)
  let gridEl = $state<HTMLDivElement | null>(null)
  let pos = $state({ left: 0, top: 0 })
  let previouslyFocused: HTMLElement | null = null

  function clampInt(n: number): number {
    if (!Number.isFinite(n)) return MIN
    return Math.min(Math.max(Math.trunc(n), MIN), MAX)
  }
  function clampGrid(n: number): number {
    return Math.min(Math.max(Math.trunc(n), 1), GRID)
  }

  // Position via the shared flip/clamp helper using the picker's measured
  // rendered size, so placement reflects the real element and flips above the
  // cursor when there is no room below (#583 AC1). Recomputes when the anchor
  // or the measured element changes.
  $effect(() => {
    const _anchor = anchor
    const el = popoverEl
    const width = el?.offsetWidth || 264
    const height = el?.offsetHeight || 196
    pos = flipOrClamp(
      { top: _anchor.top, bottom: _anchor.bottom, left: _anchor.left },
      { width, height },
      { width: window.innerWidth, height: window.innerHeight }
    )
  })

  function syncFromGrid(r: number, c: number): void {
    // Grid values are 1..GRID; write them to the source of truth and let
    // gridR/gridC rederive. clampInt (not clampGrid) so a grid pick is stored
    // at its real value within the 1–20 numeric range.
    rows = clampInt(r)
    cols = clampInt(c)
    adjusted = ''
  }

  // Non-silent clamp: when a numeric input loses focus out of range, correct
  // it AND tell the user, so a typo never silently produces a different table
  // (#583 AC4 / OQ1).
  function normalize(field: 'rows' | 'cols', raw: number): void {
    const clamped = clampInt(raw)
    if (raw !== clamped || !Number.isFinite(raw)) {
      if (field === 'rows') rows = clamped
      else cols = clamped
      adjusted = `Adjusted ${field} to ${clamped}`
      window.setTimeout(() => {
        adjusted = ''
      }, 1500)
    }
  }

  function isFilled(r: number, c: number): boolean {
    return r <= gridR && c <= gridC
  }

  function onGridKeyDown(e: KeyboardEvent): void {
    let handled = true
    let r = gridR
    let c = gridC
    switch (e.key) {
      case 'ArrowRight':
        c = clampGrid(gridC + 1)
        break
      case 'ArrowLeft':
        c = clampGrid(gridC - 1)
        break
      case 'ArrowDown':
        r = clampGrid(gridR + 1)
        break
      case 'ArrowUp':
        r = clampGrid(gridR - 1)
        break
      default:
        handled = false
    }
    if (handled) {
      e.preventDefault()
      e.stopPropagation()
      syncFromGrid(r, c)
    }
  }

  function confirm(): void {
    onConfirm(clampInt(rows), clampInt(cols))
  }

  function close(fn: () => void): void {
    fn()
  }

  // Global Escape (works regardless of where focus sits) + click-outside
  // dismissal (#583 AC3). Capture phase so Escape is caught before any input
  // handler can swallow it.
  function onDocKeyDown(e: KeyboardEvent): void {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      close(onCancel)
    }
  }
  function onDocClick(e: MouseEvent): void {
    const t = e.target as HTMLElement | null
    if (popoverEl && t && !popoverEl.contains(t)) {
      close(onCancel)
    }
  }

  // Focus trap: Tab/Shift-Tab cycles within the dialog only (#583 AC2).
  function onDialogKeyDown(e: KeyboardEvent): void {
    if (e.key === 'Enter' && (e.target as HTMLElement)?.tagName !== 'BUTTON') {
      // Enter on a numeric input inserts (parity with the prior prompt UX).
      e.preventDefault()
      confirm()
      return
    }
    if (e.key !== 'Tab' || !popoverEl) return
    const focusable = Array.from(
      popoverEl.querySelectorAll<HTMLElement>(
        'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
      )
    ).filter((el) => !el.hasAttribute('disabled'))
    if (focusable.length === 0) return
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault()
      last.focus()
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault()
      first.focus()
    }
  }

  $effect(() => {
    // Capture the element that held focus before the picker opened so it can be
    // restored on close (#583 AC2).
    previouslyFocused = document.activeElement as HTMLElement | null
    document.addEventListener('keydown', onDocKeyDown, true)
    document.addEventListener('click', onDocClick)
    // Move focus into the dialog on open.
    queueMicrotask(() => gridEl?.focus())
    return () => {
      document.removeEventListener('keydown', onDocKeyDown, true)
      document.removeEventListener('click', onDocClick)
      previouslyFocused?.focus?.()
    }
  })
</script>

<svelte:window on:resize={() => close(onCancel)} />

<div
  bind:this={popoverEl}
  class="tsp"
  style="left:{pos.left}px; top:{pos.top}px"
  role="dialog"
  aria-modal="true"
  aria-label="Custom table dimensions"
  tabindex="-1"
  onkeydown={onDialogKeyDown}
>
  <div class="tsp-grid-wrap">
    <div
      bind:this={gridEl}
      class="tsp-grid"
      role="grid"
      aria-label="Table size grid, {gridR} rows by {gridC} columns"
      tabindex="0"
      onkeydown={onGridKeyDown}
    >
      {#each Array(GRID) as _r, ri}
        {#each Array(GRID) as _c, ci}
          {@const r = ri + 1}
          {@const c = ci + 1}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- Cells are pointer targets; keyboard selection is delegated to the
               parent grid's arrow-key handler (ARIA grid roving-tabindex). -->
          <div
            class="tsp-cell"
            class:tsp-cell-filled={isFilled(r, c)}
            role="gridcell"
            aria-selected={isFilled(r, c)}
            tabindex="-1"
            onpointerenter={() => syncFromGrid(r, c)}
            onclick={() => syncFromGrid(r, c)}
          ></div>
        {/each}
      {/each}
    </div>
    <p class="tsp-preview" aria-live="polite">
      {clampInt(rows)} rows × {clampInt(cols)} columns
    </p>
  </div>

  <div class="tsp-controls">
    <label class="tsp-field">
      <span class="tsp-label">Rows <span class="tsp-range">(1–20)</span></span>
      <input
        class="tsp-input"
        type="number"
        inputmode="numeric"
        min={MIN}
        max={MAX}
        bind:value={rows}
        aria-label="Rows"
        onblur={() => normalize('rows', rows)}
      />
    </label>
    <span class="tsp-times" aria-hidden="true">×</span>
    <label class="tsp-field">
      <span class="tsp-label"
        >Columns <span class="tsp-range">(1–20)</span></span
      >
      <input
        class="tsp-input"
        type="number"
        inputmode="numeric"
        min={MIN}
        max={MAX}
        bind:value={cols}
        aria-label="Columns"
        onblur={() => normalize('cols', cols)}
      />
    </label>
    <button type="button" class="tsp-insert" onclick={confirm}>Insert</button>
  </div>
  {#if adjusted}
    <p class="tsp-adjusted" role="status">{adjusted}</p>
  {/if}
</div>

<style>
  .tsp {
    position: fixed;
    z-index: 100;
    padding: 10px;
    border-radius: 10px;
    background: var(--color-surface-popover);
    border: 1px solid var(--color-surface-popover-border);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
    display: flex;
    flex-direction: column;
    gap: 10px;
    /* Avoid overflow/clip on narrow viewports; the grid reflows below. */
    max-width: calc(100vw - 16px);
  }

  .tsp-grid-wrap {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .tsp-grid {
    display: grid;
    grid-template-columns: repeat(8, 1fr);
    gap: 3px;
    /* Touch-friendly cell size (≥40px combined hit area across the row). */
    width: min(248px, 70vw);
    aspect-ratio: 1 / 1;
  }
  .tsp-grid:focus,
  .tsp-grid:focus-visible {
    outline: 2px solid var(--color-border-focus, currentColor);
    outline-offset: 2px;
    border-radius: 4px;
  }

  .tsp-cell {
    background: var(--color-surface-popover-border);
    border: 1px solid transparent;
    border-radius: 3px;
    min-width: 20px;
    min-height: 20px;
    cursor: pointer;
    transition: background 60ms linear;
  }
  .tsp-cell-filled {
    background: var(--color-accent-primary-start);
  }

  .tsp-preview {
    margin: 0;
    font-size: 11px;
    color: var(--color-text-muted);
    text-align: center;
  }

  .tsp-controls {
    display: flex;
    align-items: flex-end;
    gap: 6px;
    flex-wrap: wrap;
  }

  .tsp-field {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .tsp-label {
    font-size: 10px;
    color: var(--color-text-muted);
  }
  .tsp-range {
    opacity: 0.8;
  }
  .tsp-input {
    width: 56px;
    /* ≥40px tall touch target. */
    min-height: 40px;
    padding: 4px 6px;
    border: 1px solid var(--color-surface-popover-border);
    border-radius: 6px;
    background: var(--color-surface-popover);
    color: var(--color-text-primary);
    font-size: 0.85rem;
    outline: none;
  }
  .tsp-input:focus,
  .tsp-input:focus-visible {
    border-color: var(--color-border-focus, currentColor);
  }

  .tsp-times {
    color: var(--color-text-muted);
    padding-bottom: 10px;
  }

  /* Primary action — visually dominant, theme-safe text on accent. */
  .tsp-insert {
    min-height: 40px;
    margin-left: auto;
    padding: 6px 16px;
    border: none;
    border-radius: 6px;
    background: var(--color-accent-primary-start);
    color: var(--color-text-on-accent);
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
  }
  .tsp-insert:hover {
    filter: brightness(1.08);
  }
  .tsp-insert:focus-visible {
    outline: 2px solid var(--color-border-focus, currentColor);
    outline-offset: 2px;
  }

  .tsp-adjusted {
    margin: 0;
    font-size: 11px;
    color: var(--color-text-primary);
  }
</style>
