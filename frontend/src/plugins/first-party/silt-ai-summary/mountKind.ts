/** The kind of surface to mount for a page. Pure — extracted from the plugin
 *  entry so the mount DECISION (the crux of the on-demand UX) is unit-testable
 *  without loading the side-effectful entry module (which registers surfaces).
 *
 *  - The banner is shown when the page is NOT dismissed AND the user hasn't
 *    chosen on-demand mode. On-demand suppresses the auto banner so the user
 *    drives every generation; they reach the banner via the re-open chip.
 *  - The re-open chip is shown when the page IS dismissed OR on-demand is on.
 *    Clicking it clears dismissal + shows the banner for the session. */
export function decideMountKind(opts: {
  dismissed: boolean
  onDemandOnly: boolean
}): 'banner' | 'reopen' {
  return !opts.dismissed && !opts.onDemandOnly ? 'banner' : 'reopen'
}
