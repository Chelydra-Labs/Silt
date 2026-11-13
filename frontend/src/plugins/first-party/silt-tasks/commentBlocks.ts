/**
 * The comment thread displays every NOTE descendant (legacy token-less NOTEs
 * included, shown as Unknown/Undated), while the sub-notes editor excludes only
 * provably-attributed comments so unattributed prose stays editable. Keep both
 * predicates here rather than "aligning" them — the asymmetry is deliberate.
 */
export interface NoteLike {
  type?: string
  author?: string
  timestamp?: string
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
