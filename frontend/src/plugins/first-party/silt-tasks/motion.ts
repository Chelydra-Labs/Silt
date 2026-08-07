const REDUCED_MOTION_QUERY = '(prefers-reduced-motion: reduce)'

/** Resolve a Svelte transition duration without animating for opted-out users. */
export function motionDuration(duration: number): number {
  if (
    typeof window === 'undefined' ||
    typeof window.matchMedia !== 'function'
  ) {
    return duration
  }
  return window.matchMedia(REDUCED_MOTION_QUERY).matches ? 0 : duration
}
