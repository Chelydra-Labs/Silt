// Open/close state for Writing Assistant right drawer.
//
// Opening is coordinated with the AI Assistant (Q&A) drawer so the two never
// squeeze the note pane at once (#542); see `lib/drawers.svelte.ts`. This
// module owns the open flag and the close/reset primitives; the exclusive
// open/toggle entry points live in the coordinator.

export const writingAssistantDrawer = $state({
  open: false
})

export function closeWritingAssistantDrawer(): void {
  writingAssistantDrawer.open = false
}

export function resetWritingAssistantDrawer(): void {
  writingAssistantDrawer.open = false
}
