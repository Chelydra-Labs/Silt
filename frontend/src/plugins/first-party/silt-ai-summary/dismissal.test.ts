import { describe, expect, it } from 'vitest'
import { isDismissed, unDismiss } from './dismissal'

// Pure helper tests for the #455 dismissal-keying logic. The legacy
// bare-pageId form (pre-#455) and the new `${pageId}:${contentHash}` form
// must coexist: legacy entries stay dismissed (we can't rebind them to a
// content version we never recorded), and new entries re-show on edit.

describe('isDismissed', () => {
  it('matches a legacy bare-pageId entry regardless of hash', () => {
    // A v1 dismissal has no content binding — treat as always-dismissed so
    // an upgrade doesn't suddenly re-show every previously-dismissed banner.
    expect(isDismissed(['NB/S/P'], 'NB/S/P', 'anyhash')).toBe(true)
    expect(isDismissed(['NB/S/P'], 'NB/S/P', undefined)).toBe(true)
  })

  it('matches the exact (pageId, contentHash) entry', () => {
    expect(isDismissed(['NB/S/P:abc123'], 'NB/S/P', 'abc123')).toBe(true)
  })

  it('does NOT match a keyed entry when the hash differs (edit re-shows)', () => {
    // The crux of #455: dismissing content v1 must NOT suppress content v2.
    expect(isDismissed(['NB/S/P:aaa'], 'NB/S/P', 'bbb')).toBe(false)
  })

  it('does NOT match a different page with the same hash', () => {
    expect(isDismissed(['NB/S/P:aaa'], 'OTHER/S/P', 'aaa')).toBe(false)
  })

  it('returns false when hash is unknown and only keyed entries exist', () => {
    // Hash undefined = content read failed OR error state with no result.
    // Don't guess — surface the banner (the user can re-dismiss if needed).
    expect(isDismissed(['NB/S/P:aaa'], 'NB/S/P', undefined)).toBe(false)
  })

  it('returns false when nothing matches', () => {
    expect(isDismissed(['X/Y/Z:h', 'X/Y/Z'], 'NB/S/P', 'aaa')).toBe(false)
    expect(isDismissed([], 'NB/S/P', 'aaa')).toBe(false)
  })

  it('treats a pageId that happens to contain ":" as still distinct', () => {
    // pageId is `${notebook}/${section}/${page}` — slashes, not colons — so
    // a page named "a:b" still keys correctly. The legacy check uses strict
    // equality and the keyed check uses `${pageId}:` prefix, so a pageId
    // containing ':' itself can't collide with another page's keyed entry
    // unless the pageIds themselves are equal.
    const pageWithColon = 'NB/S/a:b'
    expect(isDismissed([`${pageWithColon}:hash`], pageWithColon, 'hash')).toBe(
      true
    )
    expect(isDismissed([`${pageWithColon}:hash`], pageWithColon, 'other')).toBe(
      false
    )
  })
})

describe('unDismiss', () => {
  it('removes a legacy bare-pageId entry', () => {
    expect(unDismiss(['NB/S/P'], 'NB/S/P')).toEqual([])
  })

  it('removes a keyed entry regardless of hash', () => {
    // The chip click clears dismissal without knowing which hash was active
    // at dismiss time — an edit may have produced a newer hash than the one
    // originally persisted.
    expect(unDismiss(['NB/S/P:aaa'], 'NB/S/P')).toEqual([])
    expect(unDismiss(['NB/S/P:aaa', 'NB/S/P:bbb'], 'NB/S/P')).toEqual([])
  })

  it('removes both legacy and keyed forms together', () => {
    expect(
      unDismiss(['NB/S/P', 'NB/S/P:aaa', 'OTHER/S/P:xxx'], 'NB/S/P')
    ).toEqual(['OTHER/S/P:xxx'])
  })

  it('leaves entries for other pages untouched', () => {
    expect(
      unDismiss(['NB/S/P:aaa', 'NB/S/P2:aaa', 'NB/S/P'], 'NB/S/P')
    ).toEqual(['NB/S/P2:aaa'])
  })

  it('is a no-op when nothing matches', () => {
    expect(unDismiss(['OTHER/S/P'], 'NB/S/P')).toEqual(['OTHER/S/P'])
    expect(unDismiss([], 'NB/S/P')).toEqual([])
  })
})
