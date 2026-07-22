import { SvelteNodeViewRenderer } from 'svelte-tiptap'
import type { Node as ProseMirrorNode } from '@tiptap/pm/model'
import {
  TaskBlock,
  NoteBlock,
  HeaderBlock,
  EmbedNode,
  BlockReferenceNode,
  PageLinkNode,
  MentionNode,
  InlineMathNode,
  BlockMathNode,
  EmbedBlockNode,
  CalloutBlock,
  CodeBlock
} from './schema'
import TaskBlockView from '../../components/editor/TaskBlockView.svelte'
import NoteBlockView from '../../components/editor/NoteBlockView.svelte'
import HeaderBlockView from '../../components/editor/HeaderBlockView.svelte'
import EmbedNodeView from '../../components/editor/EmbedNodeView.svelte'
import BlockReferenceNodeView from '../../components/editor/BlockReferenceNodeView.svelte'
import PageLinkNodeView from '../../components/editor/PageLinkNodeView.svelte'
import MentionNodeView from '../../components/editor/MentionNodeView.svelte'
import MathNodeView from '../../components/editor/MathNodeView.svelte'
import EmbedBlockNodeView from '../../components/editor/EmbedBlockNodeView.svelte'
import CalloutBlockView from '../../components/editor/CalloutBlockView.svelte'
import CodeBlockView from '../../components/editor/CodeBlockView.svelte'

// data-type values from each node's renderHTML. TipTap's getRenderedAttributes
// only merges attribute-level renderHTML (data-depth, data-id, …) — not the
// node-level `data-type` constant — so we re-attach it on the outer NodeView
// root. CSS (index.css) and drag/outline code key off `.ProseMirror > div[data-type]`
// / `[data-depth]` on that outer root, not the inner [data-node-view-wrapper].
const NODE_DATA_TYPE: Record<string, string> = {
  noteBlock: 'note',
  taskBlock: 'task',
  headerBlock: 'header',
  calloutBlock: 'callout',
  codeBlock: 'code',
  embedNode: 'embed',
  embedBlockNode: 'embed-block',
  blockMathNode: 'math-block',
  blockReferenceNode: 'block-ref',
  pageLinkNode: 'page-link',
  mentionNode: 'mention',
  inlineMathNode: 'math-inline'
}

/**
 * Schema HTML attrs for the outer svelte-tiptap mount target (the element that
 * is a direct child of `.ProseMirror`). Without this, NodeViews leave
 * data-depth / data-type only on the inner wrapper and indent CSS never matches
 * (#339 regression: Tab still bumps attrs.depth but nesting is invisible).
 */
export function outerNodeViewAttrs({
  node,
  HTMLAttributes
}: {
  node: ProseMirrorNode
  HTMLAttributes: Record<string, string>
}): Record<string, string> {
  const dataType = NODE_DATA_TYPE[node.type.name]
  if (!dataType) return { ...HTMLAttributes }
  return { ...HTMLAttributes, 'data-type': dataType }
}

function withOuterAttrs<T>(component: T) {
  return SvelteNodeViewRenderer(component as never, {
    attrs: outerNodeViewAttrs
  })
}

// Production extensions: the base schema nodes extended with Svelte NodeView
// rendering. NoteBlock first — it's the default block type (see schema.ts).
// Every NodeView passes outerNodeViewAttrs so schema chrome (data-type,
// data-depth, data-id, …) lands on the PM root element CSS targets.
export const SiltBlockExtensionsWithNodeViews = [
  NoteBlock.extend({
    addNodeView() {
      return withOuterAttrs(NoteBlockView)
    }
  }),
  TaskBlock.extend({
    addNodeView() {
      return withOuterAttrs(TaskBlockView)
    }
  }),
  HeaderBlock.extend({
    addNodeView() {
      return withOuterAttrs(HeaderBlockView)
    }
  }),
  // Smart Graph NodeViews (#85). EmbedNode is a block-level atomic node that
  // renders {{embed:uuid}} as a live portal; BlockReferenceNode is an inline
  // atomic node that renders ((uuid)) as a clickable chip.
  EmbedNode.extend({
    addNodeView() {
      return withOuterAttrs(EmbedNodeView)
    }
  }),
  BlockReferenceNode.extend({
    addNodeView() {
      return withOuterAttrs(BlockReferenceNodeView)
    }
  }),
  // Wiki / page link chip (#545). Inline atomic node rendering [[target]]
  // as a clickable PageLinkChip that resolves via ResolvePageLink.
  PageLinkNode.extend({
    addNodeView() {
      return withOuterAttrs(PageLinkNodeView)
    }
  }),
  // @-mention chip (#184). Inline atomic node rendering @[name] as a
  // non-editable chip; the suggestion list comes from DistinctOwners.
  MentionNode.extend({
    addNodeView() {
      return withOuterAttrs(MentionNodeView)
    }
  }),
  // KaTeX math (#191). One NodeView serves inline ($...$) and block ($$...$$);
  // displayMode follows the node type.
  InlineMathNode.extend({
    addNodeView() {
      return withOuterAttrs(MathNodeView)
    }
  }),
  BlockMathNode.extend({
    addNodeView() {
      return withOuterAttrs(MathNodeView)
    }
  }),
  // Generic plugin-extensible embed block (#110). The default NodeView renders
  // a minimal card; plugins with custom embed types provide their own NodeView.
  EmbedBlockNode.extend({
    addNodeView() {
      return withOuterAttrs(EmbedBlockNodeView)
    }
  }),
  // Callout / admonition (#180). A `> [!variant]` block rendered as an
  // iconified, accent-bordered box with editable inline content.
  CalloutBlock.extend({
    addNodeView() {
      return withOuterAttrs(CalloutBlockView)
    }
  }),
  // Fenced code block (#189). A dual-layer NodeView (transparent editable text
  // over a Shiki-highlighted layer) provides syntax highlighting while keeping
  // the content natively editable.
  CodeBlock.extend({
    addNodeView() {
      return withOuterAttrs(CodeBlockView)
    }
  })
]
