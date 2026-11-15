/**
 * The comment thread still displays every NOTE descendant owned by the task
 * (type-only), while the sub-notes editor excludes comment blocks — a NOTE
 * carrying the explicit [comment::] marker or comment attribution. Unattributed,
 * unmarked prose stays editable. The count/thread rule change to this predicate
 * lands in a later PR; keep the asymmetry deliberate until then.
 */
export interface NoteLike {
  type?: string
  author?: string
  timestamp?: string
  /** Explicit [comment::] marker on the NOTE block. */
  is_comment?: boolean
}

/** Just enough hierarchy to walk ownership: a block id and its parent link. */
export interface HierarchyNoteLike extends NoteLike {
  id?: string
  parent_id?: string
}

/** Display-side type test: is this block a NOTE at all? */
export function isNoteBlock(b: NoteLike): boolean {
  return (b.type ?? '').toUpperCase() === 'NOTE'
}

/**
 * Edit-side predicate: a NOTE carrying the explicit [comment::] marker or
 * comment attribution. Type alone is not enough — the editor serializes every
 * prose paragraph as an unmarked, unattributed NOTE. Mirrors the Go
 * parser.IsCommentBlock predicate.
 */
export function isCommentBlock(b: NoteLike): boolean {
  return (
    isNoteBlock(b) &&
    (Boolean(b.is_comment) || Boolean(b.author?.trim() || b.timestamp?.trim()))
  )
}

// 64 is the cap comments_count walks too (backend/db/indexing.go), so a
// deeper chain can never render a note the task's badge does not count.
const OWNERSHIP_WALK_LIMIT = 64

/**
 * Ownership: a NOTE belongs to `taskId` when walking its parent chain reaches
 * `taskId` without passing through a nearer TASK. This is the same rule
 * comment preservation (isTaskComment) and comments_count derive, so the
 * thread's header count can never disagree with the task's badge.
 */
export function isNoteOwnedByTask(
  blocks: readonly HierarchyNoteLike[],
  note: HierarchyNoteLike,
  taskId: string
): boolean {
  if (!isNoteBlock(note)) return false
  const byId = new Map<string, HierarchyNoteLike>()
  for (const b of blocks) {
    if (b.id) byId.set(b.id, b)
  }
  let pid = note.parent_id ?? ''
  // Bounded so a cyclic or pathologically deep graph terminates like the count.
  for (let steps = 0; pid && steps < OWNERSHIP_WALK_LIMIT; steps++) {
    if (pid === taskId) return true
    const parent = byId.get(pid)
    if (!parent || parent.parent_id === pid) return false
    if ((parent.type ?? '').toUpperCase() === 'TASK') return false
    pid = parent.parent_id ?? ''
  }
  return false
}

export interface CommentPartition<T> {
  /** Blocks that belong to the editor surface (prose, tasks, regions, …). */
  content: T[]
  /** Comment blocks (marked or attributed), in their original order. */
  comments: T[]
}

/** Split a fetched sub-tree into editor content and comment blocks. */
export function partitionComments<T extends NoteLike>(
  blocks: T[]
): CommentPartition<T> {
  const content: T[] = []
  const comments: T[] = []
  for (const b of blocks) {
    if (isCommentBlock(b)) comments.push(b)
    else content.push(b)
  }
  return { content, comments }
}
