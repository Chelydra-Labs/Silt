// Standalone-task navigation guard (#374).
//
// Standalone tasks (#368) live in <vault>/.silt/tasks.md, indexed under
// the synthetic notebook `.silt`. The sidebar hides this notebook (it
// is dot-prefixed and ListNavigation skips dot-prefixed names), so the
// only way a user can land on a standalone-task block is via a
// search-jump, tag-explorer click, or backlink. Without intervention
// every such navigation opens a raw `.silt / tasks` editor tab — the
// synthetic notebook name leaks into the tab header and the entire
// standalone-task list collapses into one editor page.
//
// App.svelte enforces the policy inline at its three funnel points
// (`openPage`, `handleSearchJump`, the `navigate-to-block` listener):
// each calls `isStandaloneTaskRef(notebook)` and, if true, hands off
// to `openTasksView(blockId)` instead of opening a page tab. The
// fail-loud `$effect` in App.svelte also drops any stray `.silt` entry
// that ever slips into `openTabs` and warns in the console.

/**
 * Mirrors the backend constant `parser.StandaloneTasksNotebook` in
 * `backend/parser/scanner.go`. Single source of truth for the routing
 * guard and the Tasks view's own standalone-task detection.
 */
export const STANDALONE_TASKS_NOTEBOOK = '.silt'

/** True when the page locator refers to a standalone-task block. */
export function isStandaloneTaskRef(notebook: string): boolean {
  return notebook === STANDALONE_TASKS_NOTEBOOK
}
