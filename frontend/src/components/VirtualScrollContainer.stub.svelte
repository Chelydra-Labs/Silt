<script lang="ts">
  // App-shell harness stub for VirtualScrollContainer (#1005 view-toggle
  // gate): renders the props the App-level conflict-busy wiring reads as
  // observable DOM, and exposes the two callbacks as buttons a test can
  // drive — so the shell's guards can be exercised without a full editor.
  // The busy report mirrors the real component's lifecycle contract: state
  // changes report synchronously, and the $effect's TEARDOWN releases
  // (onConflictBusyChange(false)) — the unmount path that keeps the shell's
  // busy map wedge-free when a non-gated teardown (vault move, resetTabs)
  // destroys a resolving surface.
  let {
    viewMode = '',
    onConflictBusyChange,
    onToggleViewMode,
    ..._rest
  }: {
    viewMode?: string
    onConflictBusyChange?: (busy: boolean) => void
    onToggleViewMode?: () => void
    [key: string]: unknown
  } = $props()

  let conflictBusy = $state(false)
  $effect(() => {
    if (!conflictBusy) return
    return () => onConflictBusyChange?.(false)
  })
</script>

<div data-testid="vsc-stub" data-viewmode={viewMode}>
  <button
    type="button"
    data-testid="vsc-report-busy"
    onclick={() => {
      conflictBusy = true
      onConflictBusyChange?.(true)
    }}>busy</button
  >
  <button
    type="button"
    data-testid="vsc-report-idle"
    onclick={() => {
      conflictBusy = false
      onConflictBusyChange?.(false)
    }}>idle</button
  >
  <button
    type="button"
    data-testid="vsc-toggle"
    onclick={() => onToggleViewMode?.()}>toggle</button
  >
</div>
