import { vi, afterAll } from 'vitest'
import '@testing-library/jest-dom/vitest'
import {
  createAppIpcMocks,
  SILT_APP_BINDINGS
} from './src/lib/testing/ipcMock'

// Available inside vi.hoisted / vi.mock factories (those run before ESM imports).
// Tests should not import createAppIpcMocks for use inside hoisted callbacks.
;(globalThis as unknown as {
  createAppIpcMocks: typeof createAppIpcMocks
  SILT_APP_BINDINGS: typeof SILT_APP_BINDINGS
}).createAppIpcMocks = createAppIpcMocks
;(globalThis as unknown as { SILT_APP_BINDINGS: typeof SILT_APP_BINDINGS }).SILT_APP_BINDINGS =
  SILT_APP_BINDINGS


// jsdom omits or stubs layout-dependent DOM APIs that TipTap v3's Placeholder
// viewport tracker touches during editor construction. Force-override them
// (not conditionally) because some jsdom versions define elementFromPoint as
// a non-functional stub that still throws.
if (typeof document !== 'undefined') {
  document.elementFromPoint = vi.fn(() => document.body)
}

// jsdom does not implement window.matchMedia. Any component using a
// responsive breakpoint (e.g. TaskSubEditorModal's two-column → disclosure
// collapse) would otherwise throw in every test that renders it.
if (typeof window !== 'undefined' && !window.matchMedia) {
  window.matchMedia = (query: string): MediaQueryList =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => true
    }) as MediaQueryList
}

// Svelte 5 transitions (transition:fly/fade) call element.animate(), which
// jsdom does not implement. Polyfill globally so any component using a
// transition renders in tests without per-file stubs.
if (typeof Element !== 'undefined' && !Element.prototype.animate) {
  Element.prototype.animate = function (
    this: Element,
    _keyframes: Keyframe[] | PropertyIndexedKeyframes | null,
    _options?: number | KeyframeAnimationOptions
  ): Animation {
    return {
      cancel: () => {},
      finish: () => {},
      oncancel: null,
      onfinish: null,
      onremove: null,
      play: () => {},
      pause: () => {},
      reverse: () => {},
      playbackRate: 1,
      currentTime: 0,
      startTime: 0,
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => true
    } as unknown as Animation
  }
}

// The Wails v3 runtime's drag.js calls window.setInterval at module-load time
// (a 50ms poll for environment readiness, up to 100 ticks). On CI the interval
// outlives the jsdom environment teardown and its callback throws an uncaught
// exception. Track real (non-faked) intervals and clear them once after the
// entire file finishes so they can't fire post-teardown. afterAll (not
// afterEach) avoids interfering with per-test waitFor polling intervals.
const _realSetInterval = globalThis.setInterval.bind(globalThis)
const _pendingIntervals = new Set<ReturnType<typeof setInterval>>()
globalThis.setInterval = ((
  handler: TimerHandler,
  timeout?: number,
  ...args: unknown[]
) => {
  const id = _realSetInterval(handler, timeout, ...args)
  _pendingIntervals.add(id)
  return id
}) as typeof setInterval

afterAll(() => {
  for (const id of _pendingIntervals) {
    globalThis.clearInterval(id)
  }
  _pendingIntervals.clear()
})
