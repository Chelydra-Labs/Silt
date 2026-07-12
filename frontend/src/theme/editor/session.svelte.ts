// Immersive theme-editor session flag. When open, App hides the Settings
// sidebar nav so the editor is not nested under a third left rail
// (activity bar → settings sections → editor groups). AppearanceTab owns
// set/clear; App + SettingsPanel read it.

export const themeEditorSession = $state({
  open: false
})

export function setThemeEditorOpen(open: boolean): void {
  themeEditorSession.open = open
}
