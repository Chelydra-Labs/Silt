// RAG prompt assembly + citation parsing (#225).

import type { Citation, QAMessage, RetrievedPassage } from './types'

export const SYSTEM_PROMPT = `You are a helpful assistant answering questions about the user's personal notes.
Use ONLY the provided excerpts from their notes. Cite sources with [n] markers that match the excerpt numbers.
If the notes do not contain enough information, say so clearly — do not invent facts or citations.
When synthesizing across multiple notes, cite each source you use.
Answer the question directly — do not show your analysis, reasoning steps, or chain-of-thought.`

/** Remove [n] citation markers from prior assistant text used as history. */
export function stripCitationMarkers(text: string): string {
  return text
    .replace(/\[\d+\]/g, '')
    .replace(/\s+([.,;:!?])/g, '$1')
    .replace(/[ \t]{2,}/g, ' ')
    .replace(/\n{3,}/g, '\n\n')
    .trim()
}

export function buildRAGMessages(
  question: string,
  passages: RetrievedPassage[],
  history: QAMessage[] = []
): { role: 'system' | 'user' | 'assistant'; content: string }[] {
  const passageBlock = passages
    .map(
      (p) =>
        `[${p.citeIndex}] (${p.notebook}/${p.section}/${p.page} · block ${p.blockId})\n${p.text}`
    )
    .join('\n\n')

  const messages: { role: 'system' | 'user' | 'assistant'; content: string }[] =
    [{ role: 'system', content: SYSTEM_PROMPT }]

  // Prior turns (trim to last 4 exchanges to bound tokens).
  // Strip [n] citation markers from history so they cannot collide with this
  // turn's renumbered passage indices (local models especially conflate them).
  const recent = history.slice(-8)
  for (const m of recent) {
    const content =
      m.role === 'assistant' ? stripCitationMarkers(m.content) : m.content
    messages.push({ role: m.role, content })
  }

  messages.push({
    role: 'user',
    content: `Notes:\n\n${passageBlock || '(none)'}\n\nQuestion: ${question}`
  })
  return messages
}

/**
 * Extract [n] citations from the model answer and map to passages.
 * Unknown indices are dropped (never fabricate).
 */
export function parseCitations(
  answer: string,
  passages: RetrievedPassage[]
): Citation[] {
  const byIndex = new Map(passages.map((p) => [p.citeIndex, p]))
  const seen = new Set<number>()
  const out: Citation[] = []
  const re = /\[(\d+)\]/g
  let m: RegExpExecArray | null
  while ((m = re.exec(answer)) !== null) {
    const idx = Number(m[1])
    if (seen.has(idx)) continue
    const p = byIndex.get(idx)
    if (!p) continue
    seen.add(idx)
    out.push({
      index: idx,
      blockId: p.blockId,
      notebook: p.notebook,
      section: p.section,
      page: p.page,
      lineNumber: p.lineNumber,
      snippet: p.text.slice(0, 200)
    })
  }
  return out
}

export const NO_RESULTS_MESSAGE =
  'I could not find relevant notes for that question. Try different keywords, update the search index, or broaden the notebook scope in Settings.'
