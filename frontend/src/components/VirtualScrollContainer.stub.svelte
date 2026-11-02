<script lang="ts">
  // App-shell harness stub for VirtualScrollContainer (#1005 view-toggle
  // gate): renders the props the App-level conflict-busy wiring reads as
  // observable DOM, and exposes the two callbacks as buttons a test can
  // drive — so the shell's guards can be exercised without a full editor.
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
</script>

<div data-testid="vsc-stub" data-viewmode={viewMode}>
  <button
    type="button"
    data-testid="vsc-report-busy"
    onclick={() => onConflictBusyChange?.(true)}>busy</button
  >
  <button
    type="button"
    data-testid="vsc-report-idle"
    onclick={() => onConflictBusyChange?.(false)}>idle</button
  >
  <button
    type="button"
    data-testid="vsc-toggle"
    onclick={() => onToggleViewMode?.()}>toggle</button
  >
</div>
