/**
 * Stable Vitest/Vite alias for Wails app bindings (`frontend/bindings/silt/app.js`).
 * Prefer this over relative `../../bindings/silt/app.js` paths so mock depth
 * does not depend on the test file location.
 */
export const SILT_APP_BINDINGS = '$silt-app'

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
  return { ...overrides }
}

declare global {
  // Injected by frontend/vitest.setup.ts for hoisted mock factories.
  var createAppIpcMocks: typeof import('./ipcMock').createAppIpcMocks
  var SILT_APP_BINDINGS: typeof import('./ipcMock').SILT_APP_BINDINGS
}

export {}
