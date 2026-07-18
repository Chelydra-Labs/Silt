/**
 * Editable Source mode IPC (#660).
 * Uses Call.ByName until `wails3 generate bindings` is run after merge;
 * method names match package main App methods.
 */
import { Call } from '@wailsio/runtime'
import type { ParsedBlock } from './types'

const SAVE = 'main.App.SavePageMarkdown'
const FETCH = 'main.App.FetchPageMarkdown'

export async function savePageMarkdown(
  notebook: string,
  section: string,
  page: string,
  markdown: string
): Promise<ParsedBlock[]> {
  const result = await Call.ByName(SAVE, notebook, section, page, markdown)
  return (result ?? []) as ParsedBlock[]
}

/** On-disk page body (no YAML frontmatter). */
export async function fetchPageMarkdown(
  notebook: string,
  section: string,
  page: string
): Promise<string> {
  const result = await Call.ByName(FETCH, notebook, section, page)
  return typeof result === 'string' ? result : ''
}
