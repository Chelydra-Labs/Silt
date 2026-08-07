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
  | {
      kind: 'evidence-group'
      id: string
      items: EvidenceEntry[]
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
 * the group; multi-source evidence is emitted as one collapsible evidence-group
 * after the tool activity so a wall of cards does not dominate the drawer
 * (#845, #915). Controller transcript shape is unchanged.
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
      // Consecutive evidence outside a tool run: group when 2+ so lone cards
      // stay simple and multi-source runs collapse (#915).
      const run: EvidenceEntry[] = []
      let j = i
      while (j < transcript.length && isToolRunTransparent(transcript[j])) {
        run.push(transcript[j] as EvidenceEntry)
        j++
      }
      pushEvidenceSegments(segments, run)
      i = j
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
    pushEvidenceSegments(segments, evidenceInRun)
    i = j
  }
  return segments
}

/** One card when a single source; collapsible group when two or more. */
function pushEvidenceSegments(
  segments: TranscriptSegment[],
  items: EvidenceEntry[]
): void {
  if (items.length === 0) return
  if (items.length === 1) {
    segments.push({ kind: 'entry', entry: items[0] })
    return
  }
  segments.push({
    kind: 'evidence-group',
    id: `evidence-group-${items[0].id}`,
    items
  })
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

export function evidenceGroupSummaryLabel(count: number): string {
  if (count === 1) return '1 source'
  return `${count} sources`
}
