/**
 * Editable Source mode write path (#660).
 * Calls App.SavePageMarkdown by name so we don't need a full bindings regen
 * mid-branch; the Go method is registered on App like every other IPC.
 */
import { Call } from '@wailsio/runtime'
import type { ParsedBlock } from './types'

export async function savePageMarkdown(
  notebook: string,
  section: string,
  page: string,
  markdown: string
): Promise<ParsedBlock[]> {
  // package.struct.method — Wails v3 ByName format.
  const result = await Call.ByName(
    'main.App.SavePageMarkdown',
    notebook,
    section,
    page,
    markdown
  )
  return (result ?? []) as ParsedBlock[]
}
