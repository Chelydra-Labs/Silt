/**
 * Editable Source mode IPC (#660).
 * Thin typed wrappers over generated Wails bindings.
 */
import {
  SavePageMarkdown,
  FetchPageMarkdown,
  FetchPageMarkdownProgrammatic
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

/** On-disk page body for NON-editor readers (history compare): the
 *  editor-load variant re-baselines the external-edit fingerprint, which a
 *  read-only consumer must not do (#1005). */
export async function fetchPageMarkdownProgrammatic(
  notebook: string,
  section: string,
  page: string
): Promise<string> {
  const result = await FetchPageMarkdownProgrammatic(notebook, section, page)
  if (typeof result !== 'string') {
    throw new Error('Could not read the current page.')
  }
  return result
}
