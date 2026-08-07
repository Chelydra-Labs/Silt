<script lang="ts">
  interface Props {
    width: number
    min?: number
    max?: number
    defaultWidth?: number
    onWidthChange: (px: number) => void
    onWidthCommit: (px: number) => void
    /** Fired when a pointer drag starts (so parents can disable width transitions). */
    onDragStart?: () => void
    /** Fired when a pointer drag ends (commit or cancel). */
    onDragEnd?: () => void
    /**
     * Which edge of the resizable pane the handle sits on.
     * `start` (default): left sidebar — drag right grows width.
     * `end`: right pane — drag left grows width.
     */
    edge?: 'start' | 'end'
    ariaLabel?: string
  }

  let {
    width,
    min = 200,
    max = 480,
    defaultWidth = 256,
    onWidthChange,
    onWidthCommit,
    onDragStart,
    onDragEnd,
    edge = 'start',
    ariaLabel = 'Resize sidebar (drag, double-click to reset, or use arrow keys)'
  }: Props = $props()

  let handleEl: HTMLButtonElement
  let dragging = $state(false)

  function clamp(px: number): number {
    return Math.max(min, Math.min(max, px))
  }

  /** Signed delta applied to width: end edge inverts so drag-toward-center shrinks. */
  function widthDelta(clientDelta: number): number {
    return edge === 'end' ? -clientDelta : clientDelta
  }

  function handlePointerDown(e: PointerEvent) {
    e.preventDefault()
    handleEl.setPointerCapture(e.pointerId)
    dragging = true
    onDragStart?.()
    const startX = e.clientX
    const startWidth = width

    function onMove(ev: PointerEvent) {
      const delta = widthDelta(ev.clientX - startX)
      onWidthChange(clamp(startWidth + delta))
    }
    function onUp(ev: PointerEvent) {
      handleEl.releasePointerCapture(ev.pointerId)
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onUp)
      const delta = widthDelta(ev.clientX - startX)
      const finalWidth = clamp(startWidth + delta)
      dragging = false
      onDragEnd?.()
      onWidthCommit(finalWidth)
    }
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
  }

  function handleDoubleClick() {
    onWidthCommit(defaultWidth)
  }

  function handleKeyDown(e: KeyboardEvent) {
    const step = e.shiftKey ? 32 : 8
    // End edge: ArrowLeft grows (handle moves left), ArrowRight shrinks.
    const leftStep = edge === 'end' ? step : -step
    const rightStep = edge === 'end' ? -step : step
    let newWidth: number
    switch (e.key) {
      case 'ArrowLeft':
        newWidth = clamp(width + leftStep)
        break
      case 'ArrowRight':
        newWidth = clamp(width + rightStep)
        break
      case 'Home':
        newWidth = min
        break
      case 'End':
        newWidth = max
        break
      case 'Enter':
        newWidth = defaultWidth
        break
      default:
        return
    }
    e.preventDefault()
    onWidthChange(newWidth)
    onWidthCommit(newWidth)
  }
</script>

<button
  type="button"
  bind:this={handleEl}
  aria-label={ariaLabel}
  title="Drag to resize · Double-click to reset · Arrow keys to nudge"
  tabindex="0"
  onpointerdown={handlePointerDown}
  ondblclick={handleDoubleClick}
  onkeydown={handleKeyDown}
  class="sidebar-resize-handle"
  class:dragging
  class:edge-end={edge === 'end'}
  style="touch-action: none; flex-shrink: 0;"
></button>

<style>
  .sidebar-resize-handle {
    width: 4px;
    height: 100%;
    cursor: col-resize;
    background-color: var(--color-surface-sidebar-border);
    border: none;
    padding: 0;
    margin: 0;
    border-radius: 0;
    transition: background-color 120ms ease-out;
    z-index: 45;
    position: relative;
    -webkit-appearance: none;
    appearance: none;
  }
  /* In-flow right-pane handle stays below app chrome (sidebar handle is z-45). */
  .sidebar-resize-handle.edge-end {
    z-index: 10;
  }
  .sidebar-resize-handle:hover,
  .sidebar-resize-handle.dragging {
    background-color: var(--color-accent-primary-start);
  }
  .sidebar-resize-handle:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: -1px;
  }
</style>
