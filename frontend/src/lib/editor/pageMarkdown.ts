/**
 * Editable Source mode IPC (#660).
 * Thin typed wrappers over generated Wails bindings.
 */
import {
  SavePageMarkdown,
  FetchPageMarkdown
} from '../../../bindings/silt/app.js'
import type { ParsedBlock } from './types'

export async function savePageMarkdown(
  notebook: string,
  section: string,
  page: string,
  markdown: string
): Promise<ParsedBlock[]> {
  const result = await SavePageMarkdown(notebook, section, page, markdown)
  return result ?? []
}

/** On-disk page body (no YAML frontmatter). */
export async function fetchPageMarkdown(
  notebook: string,
  section: string,
  page: string
): Promise<string> {
  const result = await FetchPageMarkdown(notebook, section, page)
  if (typeof result !== 'string') {
    throw new Error('Could not read the current page.')
  }
  return result
}
