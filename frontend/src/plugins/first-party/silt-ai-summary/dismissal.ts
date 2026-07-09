/** Dismissal helpers for silt-ai-summary (#455).
 *
 *  Dismissal is keyed by `${pageId}:${contentHash}`. Editing the note changes
 *  its content hash, so the next open re-shows the banner for the new content
 *  rather than silently suppressing it for the lifetime of the page. Legacy
 *  bare-`pageId` entries (pre-#455) carry no content binding — they're treated
 *  as always-dismissed since we can't tell which content the user dismissed;
 *  the status-bar re-open chip is the escape hatch.
 *
 *  Extracted into a pure module (alongside mountKind.ts) so the decision logic
 *  is unit-testable without driving the side-effectful surface swap. */

/** A page is dismissed when the exact (pageId, contentHash) entry is present
 *  (post-#455 keyed dismissal) OR a legacy bare-pageId entry exists (pre-#455
 *  format — always-dismissed since the v1 key bound no content). When `hash`
 *  is undefined (content read failed, or the page is in an error state with no
 *  result), only the legacy form can match. */
export function isDismissed(
  dismissedNotes: readonly string[],
  pageId: string,
  hash: string | undefined
): boolean {
  if (dismissedNotes.includes(pageId)) return true
  if (hash !== undefined && dismissedNotes.includes(`${pageId}:${hash}`))
    return true
  return false
}

/** Remove every dismissal entry for a page: the legacy bare-pageId form AND
 *  any `${pageId}:<hash>` form. Used by the re-open chip so a click clears
 *  dismissal regardless of which keyed form was persisted (an edit may have
 *  produced a newer hash than the one originally dismissed). */
export function unDismiss(
  dismissedNotes: readonly string[],
  pageId: string
): string[] {
  return dismissedNotes.filter(
    (e) => e !== pageId && !e.startsWith(`${pageId}:`)
  )
}
