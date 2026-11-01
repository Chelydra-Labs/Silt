/**
 * External-edit conflict resolution (#1005).
 *
 * The backend refuses a whole-page save whose file changed on disk since the
 * page was loaded (IPC code `external_edit_conflict`), stashing the refused
 * payload. These helpers name the three resolutions and wrap the single IPC
 * call every surface (edit-mode banner, Source-mode header) shares.
 */

import { ResolveExternalConflict } from '../../../bindings/silt/app.js'

export type ExternalConflictResolution =
  'keep_mine' | 'keep_theirs' | 'keep_both'

/** Human summary of what each resolution does to disk. All three keep both
 * versions — they differ only in which one stays on THIS page. */
export const CONFLICT_RESOLUTIONS: {
  key: ExternalConflictResolution
  label: string
  hint: string
}[] = [
  {
    key: 'keep_mine',
    label: 'Keep my changes',
    hint: "Keeps what you've written here. The other version is saved as a separate page."
  },
  {
    key: 'keep_theirs',
    label: 'Keep the other version',
    hint: 'Reloads the version on disk. Your changes are saved as a separate page.'
  },
  {
    key: 'keep_both',
    label: 'Keep both as pages',
    hint: 'Keeps your changes here and the other version as its own page.'
  }
]

/** Resolve a refused save. Resolves to the page name of the conflict copy the
 * backend created ('' when unavailable) for the success announcement. */
export function resolveExternalConflict(
  notebook: string,
  section: string,
  page: string,
  resolution: ExternalConflictResolution
): Promise<string> {
  return ResolveExternalConflict(notebook, section, page, resolution)
}
