/**
 * Re-mint warning toast builder (#443).
 *
 * Extracted from App.svelte's event handler so the payload-shaping contract —
 * the message copy, singular/plural, and the "Show file" navigation CTA — is
 * unit-testable without mounting the App component (the event subscription
 * itself is App.svelte wiring; this module is the testable core). App.svelte
 * injects its `openPage` closure as the CTA's action.
 */
import type { PushOptions } from '../notifications/store.svelte'

/** Payload of the backend `index:re-mint-warning` event (ReMintWarning in Go). */
export interface ReMintWarning {
  notebook: string
  section: string
  page: string
  minted_count: number
  prior_count: number
}

/**
 * The shape of App.svelte's `openPage` entry point, narrowed to what the CTA
 * uses. Kept structural (not imported from tabs.ts) so this module stays
 * decoupled from the tab state machine.
 */
export type OpenPageForReMint = (
  ref: { notebook: string; section: string; page: string },
  mode: 'preview' | 'pin'
) => void

/**
 * Build the sticky info toast for a mass-re-mint warning. Leads with the
 * user-visible impact (broken note-to-note links), names the page, and offers
 * a "Show file" action that opens the affected page so the user can inspect or
 * fix it. The copy avoids internal jargon (no `((uuid))` / "block id") —
 * "identity markers" + "links between notes" is the user-facing framing.
 */
export function reMintToast(
  w: ReMintWarning,
  openPage: OpenPageForReMint
): PushOptions {
  return {
    kind: 'info',
    message: `Silt recreated the identity markers for ${w.minted_count} block${w.minted_count === 1 ? '' : 's'} in “${w.page}”. Another app likely removed them, which can break links between notes. Restore the file from a backup, or re-create any broken links.`,
    autoDismissMs: 0,
    action: {
      label: 'Show file',
      run: () =>
        openPage(
          { notebook: w.notebook, section: w.section, page: w.page },
          'preview'
        )
    }
  }
}
