import type { AIChatController } from './ai-chat-controller.svelte'

/** Shared open state for the single host-level AI drawer. */
export const aiChatDrawer = $state({ open: false })

// The host controller is registered by AIChatDrawer so the close/reset paths
// can stop in-flight runs without owning the controller themselves. Null until
// the drawer mounts; methods are optional-chained so pre-mount calls no-op.
let controller: AIChatController | null = null

/** Register the live controller (called by AIChatDrawer on mount). */
export function registerAIChatController(c: AIChatController | null): void {
  controller = c
}

export function openAIChatDrawer(): void {
  aiChatDrawer.open = true
}

export function closeAIChatDrawer(): void {
  // Dismissing the drawer stops any in-flight agent run so it cannot keep
  // mutating the vault while the UI is gone. The transcript is retained for
  // reopen (the run's late callbacks are generation-fenced by the controller).
  controller?.stop()
  aiChatDrawer.open = false
}

export function toggleAIChatDrawer(): void {
  if (aiChatDrawer.open) closeAIChatDrawer()
  else aiChatDrawer.open = true
}

export function resetAIChatDrawer(): void {
  // Vault teardown: stop the run and clear transcript/state so nothing from
  // the prior vault lingers into the next open.
  controller?.stop()
  controller?.clear()
  aiChatDrawer.open = false
}
