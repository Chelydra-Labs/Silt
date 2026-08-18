// Body-only page history compare. Line diff via jsdiff; word-level
// highlighting only inside replaced lines. Equal runs keep CONTEXT_LINES
// at each edge and collapse only the middle behind an expand token.

import { diffLines, diffWords } from 'diff'

export const CONTEXT_LINES = 3
/** Skip word-diff when a replace hunk is larger than this. */
export const WORD_DIFF_MAX_CHARS = 4000
export const WORD_DIFF_MAX_LINES = 50
/** Skip rendering a full compare when both sides are this large. */
export const COMPARE_MAX_CHARS = 200_000

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
  tooLarge?: boolean
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

function splitLines(text: string): string[] {
  if (text === '') return []
  const parts = text.split('\n')
  if (parts[parts.length - 1] === '') parts.pop()
  return parts
}

function joinLines(lines: string[]): string {
  return lines.length === 0 ? '' : `${lines.join('\n')}\n`
}

function maybeCollapseEqual(text: string): DiffHunk[] {
  const lines = splitLines(text)
  if (lines.length <= CONTEXT_LINES * 2) {
    return [{ kind: 'equal', previous: text, current: text }]
  }
  const head = joinLines(lines.slice(0, CONTEXT_LINES))
  const tail = joinLines(lines.slice(-CONTEXT_LINES))
  const hidden = lines.length - CONTEXT_LINES * 2
  const mid = joinLines(lines.slice(CONTEXT_LINES, -CONTEXT_LINES))
  return [
    { kind: 'equal', previous: head, current: head },
    {
      kind: 'equal',
      previous: mid,
      current: mid,
      collapsed: true,
      hiddenLines: hidden
    },
    { kind: 'equal', previous: tail, current: tail }
  ]
}

function shouldWordDiff(previous: string, current: string): boolean {
  return (
    previous.length + current.length <= WORD_DIFF_MAX_CHARS &&
    lineCount(previous) + lineCount(current) <= WORD_DIFF_MAX_LINES
  )
}

/** Compare two page bodies (no frontmatter). */
export function diffPageBodies(previous: string, current: string): PageDiff {
  if (
    previous.length > COMPARE_MAX_CHARS ||
    current.length > COMPARE_MAX_CHARS
  ) {
    return { hunks: [], addedLines: 0, removedLines: 0, tooLarge: true }
  }
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
        const hunk: DiffHunk = {
          kind: 'replace',
          previous: pendingRemove,
          current: added
        }
        if (shouldWordDiff(pendingRemove, added)) {
          Object.assign(hunk, wordParts(pendingRemove, added))
        }
        hunks.push(hunk)
        pendingRemove = null
      } else {
        hunks.push({ kind: 'add', previous: '', current: added })
      }
      continue
    }
    flushRemove()
    hunks.push(...maybeCollapseEqual(c.value))
  }
  flushRemove()

  return { hunks, addedLines, removedLines }
}
