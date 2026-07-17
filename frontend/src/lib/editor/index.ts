// Public surface of the Silt TipTap editor library. Consumers (TipTapEditor
// component, tests) import from this barrel so the internal file layout can
// evolve without churn at call sites.

export {
  TaskBlock,
  NoteBlock,
  HeaderBlock,
  CalloutBlock,
  CodeBlock,
  MentionNode,
  CALLOUT_VARIANTS,
  normalizeCalloutVariant,
  SiltBlockExtensions,
  SiltInlineMarkExtensions,
  SiltColorMarkExtensions,
  SiltDetailsExtensions,
  SiltTableExtensions
} from './schema'
export type { CalloutVariant } from './schema'
export { SiltBlockExtensionsWithNodeViews } from './nodeViews'
export { blocksToDoc, docToBlocks } from './converters'
export {
  SiltInlineDragHandle,
  resolveDraggedBlockPosition,
  buildBlockSlice,
  buildNodeDragSelection,
  computeDragImageOffset
} from './siltInlineDragHandle'
export { UniqueBlockIds, freshId } from './uniqueIdPlugin'
export {
  snapshotEditCaret,
  resolveCaretInDoc,
  applyEditCaret
} from './editCaretRestore'
export type { EditCaretSnapshot } from './editCaretRestore'
export {
  SiltBlockKeymaps,
  convertToBlock,
  setBlockAlign,
  toggleBlockQuote,
  insertCallout,
  insertCodeBlock,
  insertDetails,
  insertTable,
  insertBlockMath,
  toggleDetails,
  findActiveBlock,
  moveActiveBlock,
  BLOCK_TYPES
} from './keymaps'
export {
  TaskMetaSuggest,
  applyMetaSuggestion,
  filterMetaKeys,
  getSuggestContext,
  getMetaSuggestState,
  META_KEYS,
  buildMetaToken,
  OWNER_TOKEN_RE
} from './taskMetaSuggest'
export type {
  MetaKey,
  SuggestContext,
  InsertPlan,
  TaskMetaSuggestOptions
} from './taskMetaSuggest'
export {
  MentionSuggest,
  applyMentionSuggestion,
  filterOwners,
  getMentionContext,
  planOwnerWriteback
} from './mentionSuggest'
export type {
  MentionContext,
  MentionSuggestOptions,
  OwnerWriteback
} from './mentionSuggest'
export type {
  ParsedBlock,
  BlockType,
  TaskStatus,
  NodeJSON,
  DocJSON
} from './types'
export {
  extractHeadings,
  extractHeadingsFromEditor,
  jumpToHeading,
  activeHeadingId
} from './outline'
export type { OutlineHeading } from './outline'
