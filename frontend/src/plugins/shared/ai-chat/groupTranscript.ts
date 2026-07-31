import type {
  AIChatEntry,
  EvidenceEntry,
  ToolCallEntry,
  ToolResultEntry
} from './types'

export type ToolActivityItem = ToolCallEntry | ToolResultEntry

export type TranscriptSegment =
  | { kind: 'entry'; entry: AIChatEntry }
  | {
      kind: 'tool-activity'
      id: string
      items: ToolActivityItem[]
      callCount: number
      resultCount: number
    }

function isToolActivity(entry: AIChatEntry): entry is ToolActivityItem {
  return entry.kind === 'tool-call' || entry.kind === 'tool-result'
}

/** Evidence may sit between a tool call and its result (search_notes path). */
function isToolRunTransparent(entry: AIChatEntry): entry is EvidenceEntry {
  return entry.kind === 'evidence'
}

/**
 * While a run is busy, hide tool-call/result entries that belong to the
 * current turn only (after the last user message). Prior completed tool
 * activity stays visible in multi-turn chats (#845).
 */
export function filterTranscriptForBusyDisplay(
  transcript: AIChatEntry[],
  busy: boolean
): AIChatEntry[] {
  if (!busy) return transcript
  let lastUserIdx = -1
  for (let i = transcript.length - 1; i >= 0; i--) {
    if (transcript[i].kind === 'text' && transcript[i].role === 'user') {
      lastUserIdx = i
      break
    }
  }
  return transcript.filter((e, i) => {
    if (e.kind !== 'tool-call' && e.kind !== 'tool-result') return true
    if (lastUserIdx < 0) return false
    return i <= lastUserIdx
  })
}

/**
 * Collapse tool-call / tool-result entries into activity groups for display.
 * Evidence between a call and result (production search path) does not split
 * the group; evidence is emitted as its own segments after the group so source
 * cards stay visible. Controller transcript shape is unchanged (#845).
 */
export function groupTranscript(
  transcript: AIChatEntry[]
): TranscriptSegment[] {
  const segments: TranscriptSegment[] = []
  let i = 0
  while (i < transcript.length) {
    const entry = transcript[i]
    if (!isToolActivity(entry) && !isToolRunTransparent(entry)) {
      segments.push({ kind: 'entry', entry })
      i++
      continue
    }
    if (isToolRunTransparent(entry)) {
      // Evidence outside a tool run (or before any tool) stays a normal card.
      segments.push({ kind: 'entry', entry })
      i++
      continue
    }

    // Collect tools through a run that may interleave evidence.
    const items: ToolActivityItem[] = []
    const evidenceInRun: EvidenceEntry[] = []
    const startId = entry.id
    let j = i
    while (j < transcript.length) {
      const e = transcript[j]
      if (isToolActivity(e)) {
        items.push(e)
        j++
        continue
      }
      if (isToolRunTransparent(e)) {
        evidenceInRun.push(e)
        j++
        continue
      }
      break
    }

    let callCount = 0
    let resultCount = 0
    for (const item of items) {
      if (item.kind === 'tool-call') callCount++
      else resultCount++
    }
    if (items.length > 0) {
      segments.push({
        kind: 'tool-activity',
        id: `tool-activity-${startId}`,
        items,
        callCount,
        resultCount
      })
    }
    for (const ev of evidenceInRun) {
      segments.push({ kind: 'entry', entry: ev })
    }
    i = j
  }
  return segments
}

export function toolActivitySummaryLabel(
  callCount: number,
  resultCount: number
): string {
  const parts: string[] = []
  if (callCount > 0) {
    parts.push(`${callCount} tool call${callCount === 1 ? '' : 's'}`)
  }
  if (resultCount > 0) {
    parts.push(`${resultCount} result${resultCount === 1 ? '' : 's'}`)
  }
  if (parts.length === 0) return 'Tool activity'
  return `Tool activity · ${parts.join(', ')}`
}
