// Body-only page history compare. Line diff via jsdiff; word-level
// highlighting only inside replaced lines. Equal runs longer than
// CONTEXT_LINES collapse behind an expand token (data only).

import { diffLines, diffWords } from 'diff'

export const CONTEXT_LINES = 3

export type DiffKind = 'equal' | 'add' | 'remove' | 'replace'

export interface WordPart {
  text: string
  kind: 'equal' | 'add' | 'remove'
}

export interface DiffHunk {
  kind: DiffKind
  previous: string
  current: string
  collapsed?: boolean
  hiddenLines?: number
  previousWords?: WordPart[]
  currentWords?: WordPart[]
}

export interface PageDiff {
  hunks: DiffHunk[]
  addedLines: number
  removedLines: number
}

function lineCount(text: string): number {
  if (text === '') return 0
  const parts = text.split('\n')
  // A trailing newline is a line terminator, not an extra blank line.
  return parts[parts.length - 1] === '' ? parts.length - 1 : parts.length
}

function wordParts(
  previous: string,
  current: string
): { previousWords: WordPart[]; currentWords: WordPart[] } {
  const changes = diffWords(previous, current)
  const previousWords: WordPart[] = []
  const currentWords: WordPart[] = []
  for (const c of changes) {
    if (c.added) {
      currentWords.push({ text: c.value, kind: 'add' })
    } else if (c.removed) {
      previousWords.push({ text: c.value, kind: 'remove' })
    } else {
      previousWords.push({ text: c.value, kind: 'equal' })
      currentWords.push({ text: c.value, kind: 'equal' })
    }
  }
  return { previousWords, currentWords }
}

function maybeCollapseEqual(text: string): DiffHunk {
  const lines = lineCount(text)
  if (lines <= CONTEXT_LINES) {
    return { kind: 'equal', previous: text, current: text }
  }
  return {
    kind: 'equal',
    previous: text,
    current: text,
    collapsed: true,
    hiddenLines: lines
  }
}

/** Compare two page bodies (no frontmatter). */
export function diffPageBodies(previous: string, current: string): PageDiff {
  const changes = diffLines(previous, current, { stripTrailingCr: true })
  const hunks: DiffHunk[] = []
  let addedLines = 0
  let removedLines = 0
  let pendingRemove: string | null = null

  const flushRemove = () => {
    if (pendingRemove == null) return
    removedLines += lineCount(pendingRemove)
    hunks.push({ kind: 'remove', previous: pendingRemove, current: '' })
    pendingRemove = null
  }

  for (const c of changes) {
    if (c.removed) {
      flushRemove()
      pendingRemove = c.value
      continue
    }
    if (c.added) {
      const added = c.value
      addedLines += lineCount(added)
      if (pendingRemove != null) {
        removedLines += lineCount(pendingRemove)
        const words = wordParts(pendingRemove, added)
        hunks.push({
          kind: 'replace',
          previous: pendingRemove,
          current: added,
          ...words
        })
        pendingRemove = null
      } else {
        hunks.push({ kind: 'add', previous: '', current: added })
      }
      continue
    }
    flushRemove()
    hunks.push(maybeCollapseEqual(c.value))
  }
  flushRemove()

  return { hunks, addedLines, removedLines }
}
