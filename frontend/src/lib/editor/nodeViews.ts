import { SvelteNodeViewRenderer } from 'svelte-tiptap'
import { TaskBlock, NoteBlock, HeaderBlock } from './schema'
import TaskBlockView from '../../components/editor/TaskBlockView.svelte'
import NoteBlockView from '../../components/editor/NoteBlockView.svelte'
import HeaderBlockView from '../../components/editor/HeaderBlockView.svelte'

// Production extensions: the base schema nodes extended with Svelte NodeView
// rendering. TipTapEditor uses these; the converter tests use the base
// SiltBlockExtensions (without NodeViews) to avoid needing a Svelte rendering
// context in jsdom.
export const SiltBlockExtensionsWithNodeViews = [
  TaskBlock.extend({
    addNodeView() {
      return SvelteNodeViewRenderer(TaskBlockView)
    }
  }),
  NoteBlock.extend({
    addNodeView() {
      return SvelteNodeViewRenderer(NoteBlockView)
    }
  }),
  HeaderBlock.extend({
    addNodeView() {
      return SvelteNodeViewRenderer(HeaderBlockView)
    }
  })
]
