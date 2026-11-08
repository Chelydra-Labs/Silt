/**
 * Stable Vitest/Vite alias for Wails app bindings (`frontend/bindings/silt/app.js`).
 * Prefer this over relative `../../bindings/silt/app.js` paths so mock depth
 * does not depend on the test file location.
 */
export const SILT_APP_BINDINGS = '$silt-app'

/**
 * Default stub for the task-undo binding (#1011). Spread it into a
 * `createAppIpcMocks({ PluginUndoLastTaskMutation: ... })` override when a test
 * needs a resolved empty-journal response, or replace it with a `vi.fn` to
 * assert the call.
 */
export function undoLastTaskMutationMock() {
  return Promise.resolve({
    undone: false,
    skipped: false,
    blockIds: [] as string[],
    skippedBlockIds: [] as string[],
    notebook: '',
    section: '',
    page: '',
    removedSpawnedId: '',
    changedFields: [] as string[],
    remaining: 0
  })
}

/**
 * Default stub for the read-only task-undo state binding (#1011). An empty
 * journal keeps components that probe the undo surface stable in unrelated
 * tests; override it with a `vi.fn` to assert the call or supply state.
 */
export function getTaskUndoStateMock() {
  return Promise.resolve({
    canUndo: false,
    remaining: 0,
    changedFields: [] as string[],
    blockIds: [] as string[],
    notebook: '',
    section: '',
    page: '',
    source: ''
  })
}

/**
 * Build a bindings mock map for `vi.mock('$silt-app', ...)`.
 *
 * **Critical:** `vi.mock` / `vi.hoisted` run before ESM imports initialize.
 * Do **not** `import { createAppIpcMocks }` and call it inside `vi.hoisted` —
 * that hits a TDZ on the import binding. The factory is registered on
 * `globalThis` from `vitest.setup.ts`, so call the bare name:
 *
 * ```ts
 * import { vi } from 'vitest'
 *
 * const appMocks = vi.hoisted(() =>
 *   createAppIpcMocks({
 *     GetX: vi.fn().mockResolvedValue(/* ... *\/),
 *   })
 * )
 * vi.mock('$silt-app', () => appMocks)
 * // imports AFTER mocks
 * import { GetX } from '$silt-app'
 * ```
 *
 * See also `GeneralTab.test.ts` for a fuller example.
 */
export function createAppIpcMocks<T extends Record<string, unknown>>(
  overrides: T = {} as T
): T {
  return {
    // Task undo (#1011): generated stubs keep components that probe the undo
    // surface from crashing in unrelated tests; override them where the call
    // itself matters.
    PluginUndoLastTaskMutation: undoLastTaskMutationMock,
    PluginGetTaskUndoState: getTaskUndoStateMock,
    ...overrides
  } as unknown as T
}

declare global {
  // Injected by frontend/vitest.setup.ts for hoisted mock factories.
  var createAppIpcMocks: typeof import('./ipcMock').createAppIpcMocks
  var SILT_APP_BINDINGS: typeof import('./ipcMock').SILT_APP_BINDINGS
}

export {}
