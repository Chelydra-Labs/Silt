# Editor & Sidebar Follow-Ups

- **Drag a block sideways to indent it.** Dragging a block's grip now changes its indent based on where you drop — drop further right to nest it deeper, drop in line to keep it at the same level (Notion-style). A drop-zone indicator shows where the block will land and at what depth. `Alt+Up`/`Alt+Down` remains the keyboard way to move blocks.
- **Type `$…$` to insert inline math.** A paired `$…$` at a word boundary now turns into a rendered equation as soon as you close it. (Math containing spaces, or block `$$…$$`, still goes through the `/math` command.)
- **A proper LaTeX editor.** Editing or inserting an equation opens an in-app popover with a multi-line input and a **live preview**, replacing the old single-line native prompt. Commit with `Ctrl/Cmd+Enter`, cancel with `Esc`.
- **Mentioning someone assigns the task.** Confirming an `@owner` mention inside a task line now sets that task's owner automatically (outside a task, the mention is just a reference).
- **`Ctrl+Shift+B` focuses the sidebar.** Jump keyboard focus straight into the active sidebar (the page tree, a smart-list, or a scope picker). `Ctrl+B` still toggles the sidebar's visibility.

# Improvements

- **The `@`-mention typeahead scales to large vaults.** The owner list is now filtered on the server as you type and cached briefly, so opening the typeahead no longer re-fetches every owner on each editor focus — and a vault with thousands of owners won't ship a huge list over IPC.
- **Bulleted equations and embeds keep their bullet.** A line like `- $$x$$` or `- {{embed:…}}` no longer drops its `- ` marker when saved and reopened.
- **Kanban's sidebar filters match the current board.** The owner/tag quick-toggles now list only the owners and tags present on the current board, so a toggle never filters to nothing. The sidebar also caps saved boards at 50 (with a clear limit message) and matches the page-tree spacing.

# Fixes

- **Calendar sidebar no longer errors on a fresh vault.** A vault with no active tasks now shows a friendly "No active tasks" hint instead of a row of empty badges.
- **Switching vaults resets Kanban and Calendar state.** The scope, filters, and focus date from the previous vault no longer carry over — and plugins no longer hit "missing session token" errors when the sidebar remounts mid-switch.
