/** Shared open state for the single host-level AI drawer. */
export const aiChatDrawer = $state({ open: false })

export function openAIChatDrawer(): void {
  aiChatDrawer.open = true
}

export function closeAIChatDrawer(): void {
  aiChatDrawer.open = false
}

export function toggleAIChatDrawer(): void {
  aiChatDrawer.open = !aiChatDrawer.open
}

export function resetAIChatDrawer(): void {
  aiChatDrawer.open = false
}
