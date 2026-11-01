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

/** Human summary of what each resolution does to disk. */
export const CONFLICT_RESOLUTIONS: {
  key: ExternalConflictResolution
  label: string
  hint: string
}[] = [
  {
    key: 'keep_mine',
    label: 'Keep mine',
    hint: 'Save this version to the page; keep the on-disk version as a conflict copy'
  },
  {
    key: 'keep_theirs',
    label: 'Keep theirs',
    hint: 'Keep the on-disk version; save this version as a conflict copy'
  },
  {
    key: 'keep_both',
    label: 'Keep both',
    hint: 'Keep this version on the page and the on-disk version as a conflict copy'
  }
]

export function resolveExternalConflict(
  notebook: string,
  section: string,
  page: string,
  resolution: ExternalConflictResolution
): Promise<void> {
  return ResolveExternalConflict(notebook, section, page, resolution)
}
