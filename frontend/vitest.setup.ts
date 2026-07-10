import { vi } from 'vitest'
import '@testing-library/jest-dom/vitest'

// jsdom omits or stubs layout-dependent DOM APIs that TipTap v3's Placeholder
// viewport tracker touches during editor construction. Force-override them
// (not conditionally) because some jsdom versions define elementFromPoint as
// a non-functional stub that still throws.
if (typeof document !== 'undefined') {
  document.elementFromPoint = vi.fn(() => document.body)
}

// Svelte 5 transitions (transition:fly/fade) call element.animate(), which
// jsdom does not implement. Polyfill globally so any component using a
// transition renders in tests without per-file stubs.
if (typeof Element !== 'undefined' && !Element.prototype.animate) {
  Element.prototype.animate = function (
    this: Element,
    _keyframes: Keyframe[] | PropertyIndexedKeyframes | null,
    options?: number | KeyframeAnimationOptions
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
