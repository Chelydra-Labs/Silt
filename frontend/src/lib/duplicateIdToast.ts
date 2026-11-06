/**
 * Duplicate block-id warning toast builder.
 *
 * Mirrors reMintToast.ts: the payload-shaping contract (message copy and the
 * "Show file" navigation CTA) lives here so it is unit-testable without
 * mounting the App component. The event handler in useStartupEvents injects
 * the openPage closure as the CTA's action.
 *
 * Copy follows the re-mint framing: no internal jargon ("identity markers",
 * not "block id" / "((uuid))") and it leads with the user-visible impact —
 * links resolving to the wrong block.
 */
import type { PushOptions } from '../notifications/store.svelte'

/** Payload of the backend `index:duplicate-id-warning` event (DuplicateIDWarning in Go). */
export interface DuplicateIDPage {
  source: string
  notebook: string
  section: string
  page: string
}

export interface DuplicateIDWarning {
  block_id: string
  pages: DuplicateIDPage[]
  within_page: boolean
}

/**
 * The shape of the openPage entry point, narrowed to what the CTA uses.
 * Kept structural (same as reMintToast) so this module stays decoupled from
 * the tab state machine.
 */
export type OpenPageForDuplicateID = (
  ref: { notebook: string; section: string; page: string },
  mode: 'preview' | 'pin'
) => void

function pageLabel(p: DuplicateIDPage): string {
  // The section is part of the page's path in the sidebar; keep the label
  // short and let "Show file" disambiguate same-named pages.
  return p.section ? `${p.section}/${p.page}` : p.page
}

function markerTail(blockId: string): string {
  // The last 8 id chars: long enough to find the `<!-- id: ... -->` comment
  // via text search even when the copies' prose has diverged, short enough
  // not to read as a hex blob. The ellipsis marks it as a fragment.
  return `…${blockId.slice(-8)}`
}

/**
 * Build the sticky error toast for a duplicate block-id warning. Sticky
 * (never auto-dismisses): the collision stays until the user fixes one of
 * the files, and repeated re-index detections must not stack — the
 * notification store dedups on kind+message while visible.
 */
export function duplicateIdToast(
  w: DuplicateIDWarning,
  openPage: OpenPageForDuplicateID
): PushOptions {
  const first = w.pages[0]
  const second = w.pages[1] ?? first
  const more =
    w.pages.length > 2
      ? ` and ${w.pages.length - 2} more page${w.pages.length === 3 ? '' : 's'}`
      : ''
  // Two or more carrier pages always use the cross-page copy — even when one
  // page also holds the id twice, the cross-file half is the bigger problem.
  // The marker fragment also keeps distinct collisions on the same page pair
  // from collapsing into one indistinguishable toast (store dedups on
  // kind+message).
  const tail = markerTail(w.block_id)
  const message =
    w.pages.length === 1
      ? `The page “${pageLabel(first)}” contains the same identity marker twice, so links to it may open the wrong block. Delete one copy or give one a fresh marker (the marker ends ${tail}).`
      : `The pages “${pageLabel(first)}” and “${pageLabel(second)}”${more} carry the same identity marker, so links to it may open the wrong block. Delete one copy or give one a fresh marker (the marker ends ${tail}).`
  return {
    kind: 'error',
    message,
    autoDismissMs: 0,
    action: {
      label: 'Show file',
      run: () =>
        openPage(
          {
            notebook: first.notebook,
            section: first.section,
            page: first.page
          },
          'preview'
        )
    }
  }
}
