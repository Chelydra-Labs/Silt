// Inline grammar pipeline + block-level converters, split into per-stage
// modules so each can be reasoned about, tested, and evolved independently.
// This barrel re-exports the public API; external consumers import from
// `./converters` (this file) — no sub-module paths.
//
// Contract:
//   blocksToDoc(blocks) -> doc JSON      (used on load / setContent)
//   docToBlocks(doc)    -> blocks        (used on save, debounced)
//
// Round-trip identity holds for the semantic fields:
//   docToBlocks(blocksToDoc(blocks)) preserves id, type, depth, status, owner,
//   start_date, due_date, priority, clean_text. raw_text is a serialization
//   artifact (produced by Go's RenderFileContent from clean_text + attrs), so
//   it is NOT compared in the identity tests. parent_id and line_number are
//   derived (from depth and doc order respectively), not stored as node attrs.
//
// --- Inline grammar pipeline (#198) ---
// The inline pipeline is split into three discrete stages:
//
//   tokenize(text) -> Token[]    — recursive-descent grammar (tokenize.ts),
//                                  emits the typed Token[] representation.
//   validate(tokens) -> Token[]  — centralized security sanitization
//                                  (validate.ts). The ONLY place future mark
//                                  sanitizers are added.
//   serialize(content) -> string — mark-diff serializer (serialize.ts),
//                                  NodeJSON[]-based, byte-for-byte proven.
//
// Block-level ParsedBlock ↔ doc JSON conversion lives in blocks.ts.
//
// Implementation note: the typed Token model is the canonical in-memory
// *output* representation. The MARK_PATTERNS table + tryMatchMarkAt +
// parseInlineTokens stack in tokenize.ts is the canonical mark *grammar*.
// Future work that wants a per-mark migration should preserve the existing
// MARK_PATTERNS dispatch order (longer delimiters before shorter,
// code-first) — the existing test suite pins this contract byte-for-byte.
//
// The legacy NodeJSON[] surface (`{ type: 'text', text, marks }`) is preserved
// via a thin adapter so the ProseMirror integration is unchanged. New code
// that doesn't need NodeJSON should consume the typed Token API directly.

export { tokenizeInline } from './tokenize'
export type {
  Token,
  MarkRef,
  TextToken,
  MarkToken,
  EmbedToken,
  BlockReferenceToken
} from './tokenize'

export { serializeInlineContent } from './serialize'

export {
  blocksToDoc,
  docToBlocks,
  stripAlignmentMarker,
  emitAlignmentMarker,
  embedBlockMarker,
  parseEmbedBlockMarker
} from './blocks'
