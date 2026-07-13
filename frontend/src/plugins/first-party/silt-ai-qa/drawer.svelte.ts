// Open/close state for the AI Assistant right drawer.
// Module-level $state so title-bar toggle, host, and lifecycle share it.
//
// Opening is coordinated with the Writing Assistant drawer so the two never
// squeeze the note pane at once (#542); see `lib/drawers.svelte.ts`. This
// module owns the open flag and the close/reset primitives; the exclusive
// open/toggle entry points live in the coordinator.

export const aiSearchDrawer = $state({
  open: false
})

export function closeAISearchDrawer(): void {
  aiSearchDrawer.open = false
}

export function resetAISearchDrawer(): void {
  aiSearchDrawer.open = false
}
