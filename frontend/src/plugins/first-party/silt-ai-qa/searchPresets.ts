// Preset definitions for Search Balance + Context Breadth (#614).

export const SEARCH_BALANCE_PRESETS = [
  {
    value: 0.3,
    label: 'Exact words',
    description:
      'Finds notes with your exact search terms. Best for names, code, and specific phrases.'
  },
  {
    value: 0.6,
    label: 'Balanced',
    description:
      'Searches by both exact words and meaning. Works well for most questions.'
  },
  {
    value: 0.85,
    label: 'By meaning',
    description:
      'Finds conceptually related notes, even without matching words. Best for "what did I think about…" questions.'
  }
] as const

export interface ContextBreadthPreset {
  value: string
  label: string
  description: string
  top_k: number
  max_context_chars: number
}

export const CONTEXT_BREADTH_PRESETS: ContextBreadthPreset[] = [
  {
    value: 'focused',
    label: 'Focused',
    description: 'A few key notes. Fastest answers.',
    top_k: 5,
    max_context_chars: 8000
  },
  {
    value: 'standard',
    label: 'Standard',
    description: 'A moderate number of notes for a well-rounded answer.',
    top_k: 10,
    max_context_chars: 24000
  },
  {
    value: 'thorough',
    label: 'Thorough',
    description:
      'Many notes for comprehensive synthesis. Slower but more complete.',
    top_k: 20,
    max_context_chars: 60000
  },
  {
    value: 'maximum',
    label: 'Maximum',
    description:
      'As many relevant notes as possible. For complex, multi-topic questions.',
    top_k: 30,
    max_context_chars: 120000
  }
]

/** Resolve the Context Breadth preset key for a (top_k, max_context_chars) pair. */
export function matchContextBreadth(topK: number, maxChars: number): string {
  const hit = CONTEXT_BREADTH_PRESETS.find(
    (p) => p.top_k === topK && p.max_context_chars === maxChars
  )
  return hit?.value ?? '__custom__'
}

export function contextBreadthFromKey(
  key: string
): ContextBreadthPreset | null {
  return CONTEXT_BREADTH_PRESETS.find((p) => p.value === key) ?? null
}
