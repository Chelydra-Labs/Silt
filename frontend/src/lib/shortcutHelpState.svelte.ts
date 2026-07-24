// Shared state for the keyboard-shortcut reference overlay. Lifted from
// App.svelte's local $state so the /shortcuts slash command can open the same
// overlay the Shift+? (open_shortcuts_help) hotkey does — one source of truth
// for the open/close state.
export const shortcutHelp = $state({ open: false })

export function openShortcutHelp(): void {
  shortcutHelp.open = true
}

export function closeShortcutHelp(): void {
  shortcutHelp.open = false
}

export function toggleShortcutHelp(): void {
  shortcutHelp.open = !shortcutHelp.open
}
