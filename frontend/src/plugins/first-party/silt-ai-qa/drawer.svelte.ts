// Open/close state for the AI Assistant right drawer.
// Module-level $state so title-bar toggle, host, and lifecycle share it.

export const aiSearchDrawer = $state({
  open: false
})

export function openAISearchDrawer(): void {
  aiSearchDrawer.open = true
}

export function closeAISearchDrawer(): void {
  aiSearchDrawer.open = false
}

export function toggleAISearchDrawer(): void {
  aiSearchDrawer.open = !aiSearchDrawer.open
}

export function resetAISearchDrawer(): void {
  aiSearchDrawer.open = false
}
