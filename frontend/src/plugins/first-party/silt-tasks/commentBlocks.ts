/**
 * The comment thread displays every NOTE descendant owned by the task (legacy
 * token-less NOTEs included, shown as Unknown/Undated), while the sub-notes
 * editor excludes only provably-attributed comments so unattributed prose stays
 * editable. Keep both predicates here rather than "aligning" them — the
 * asymmetry is deliberate.
 */
export interface NoteLike {
  type?: string
  author?: string
  timestamp?: string
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
 * Edit-side predicate: a NOTE carrying comment attribution. Type alone is not
 * enough — the editor serializes every prose paragraph as an unattributed NOTE.
 */
export function isCommentBlock(b: NoteLike): boolean {
  return isNoteBlock(b) && Boolean(b.author?.trim() || b.timestamp?.trim())
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
  /** Attributed NOTE comments, in their original order. */
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
