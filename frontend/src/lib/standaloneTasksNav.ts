// Standalone-task navigation router (#374).
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
// The router sends every `.silt` navigation target to the Tasks view
// (#370) and forwards a `blockTarget` so the targeted row is scrolled +
// highlighted. The pure `routeJumpTarget` reducer is the single source
// of truth for that policy — App.svelte's three funnel points
// (`openPage`, `handleSearchJump`, `navigate-to-block` listener) all
// delegate here.

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

/**
 * Discriminated union for the routing decision. App.svelte pattern-
 * matches on `kind`; the tests drive it directly to assert the routing
 * policy without needing to render App.svelte.
 */
export type JumpTarget =
  | {
      kind: 'tasks-view'
      notebook: typeof STANDALONE_TASKS_NOTEBOOK
      blockTarget?: { blockId?: string; fileDate?: string }
    }
  | {
      kind: 'open-page'
      notebook: string
      section: string
      page: string
      blockTarget?: { blockId?: string; fileDate?: string }
    }

/**
 * Decide where a navigation jump should land.
 *
 * - A `.silt` notebook ref always lands in the Tasks view (no tab
 *   creation — the Tasks view is itself a single-mount surface; the
 *   `activeView = 'tasks'` setter fires the activity-bar entry).
 * - Any other ref falls through to the standard open-page funnel.
 *
 * Optional blockTarget is forwarded unchanged to whichever surface
 * receives the navigation; the Tasks view consumes it as the
 * `focusBlockId` prop.
 */
export function routeJumpTarget(input: {
  notebook: string
  section: string
  page: string
  blockTarget?: { blockId?: string; fileDate?: string }
}): JumpTarget {
  if (isStandaloneTaskRef(input.notebook)) {
    return {
      kind: 'tasks-view',
      notebook: STANDALONE_TASKS_NOTEBOOK,
      blockTarget: input.blockTarget
    }
  }
  return {
    kind: 'open-page',
    notebook: input.notebook,
    section: input.section,
    page: input.page,
    blockTarget: input.blockTarget
  }
}
