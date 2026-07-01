# Highlights

- **Find and replace text across your notes.** Press `Ctrl+F` to find within the current note — with live match highlighting, a "1 of N" counter, and case-sensitive / whole-word / regular-expression toggles. Press `Ctrl+H` to find and replace in the current note. Press `Ctrl+Shift+G` to replace a term across every note in the vault at once, with a per-match review before anything changes.

# Improvements

- **Richer vault-wide search.** The global search (`Ctrl+Shift+F`) now lets you filter by notebook and by block type (tasks, notes, headings, code), sort by relevance or recency, and scope to in-vault notes or include linked notebooks.
- **Inline spellcheck.** Misspelled words are underlined as you type. Right-click a word (or click the spellcheck button in the format toolbar) for suggested corrections, add a word to your personal dictionary, or ignore it for the session. Manage your custom word list in Settings → Editor.
- **Typewriter mode.** An opt-in writing mode (Settings → Editor, or `Ctrl+Shift+Y`) keeps the line you're typing vertically centered — pairs with Focus mode for distraction-free writing.
- **Paste without formatting.** `Ctrl+Shift+V` pastes the clipboard as plain text; `Ctrl+V` still pastes with formatting.
- **Keyboard shortcuts realigned to familiar conventions.** Global search is now `Ctrl+Shift+F`, the command palette is `Alt+Q`, strikethrough is `Alt+Shift+5`, and the view-layout cycle is `Ctrl+Alt+V` (the old `Alt+Tab` was the OS window-switcher and never reached the app on Windows/Linux).

# Fixes

- **Global replace data-loss fix.** Editing the Find box after previewing no longer lets Apply run against a stale preview — the stale-guard now tracks the find text. A mid-batch save failure in global replace now leaves Undo available for every page that already persisted, instead of leaving changed pages on disk with no in-app revert path.
- **Typewriter mode recenters after find/replace navigation.** The mouse-scroll suppression flag is now consumed after one update, so programmatic cursor jumps (FindBar, search results) correctly recenter the line instead of being silently skipped after a click.
- **Visible vault-init errors.** When the vault fails to initialize on launch (unreadable settings, database open failure, network-filesystem vault, watcher error), the error now surfaces as a toast instead of vanishing silently — which had left the app showing an empty frame with no indication of what went wrong. Non-fatal scan warnings (symlink skips, permission errors) are surfaced too.

# Notes

- Markdown files stay fully portable. Silt uses standard GitHub-Flavored Markdown (tables, task lists, code fences) with HTML `<sub>`/`<sup>` for subscript/superscript, so notes render correctly on GitHub, in Obsidian, and in VS Code. The subscript/superscript shortcuts are `Ctrl+,` and `Ctrl+.`.
