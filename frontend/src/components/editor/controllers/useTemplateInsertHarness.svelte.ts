// Test-only harness that mounts `createTemplateInsert` inside a `$effect.root`
// scope so the controller's `$state` cells have a reactive owner. Mirrors the
// `$effect.root` pattern from useSuggestsHarness.svelte.ts.
import type { Editor } from 'svelte-tiptap'
import {
  createTemplateInsert,
  type TemplateInsertController
} from './useTemplateInsert.svelte'

export interface TemplateInsertHarness {
  controller: TemplateInsertController
  destroy: () => void
}

export function createTemplateInsertHarness(
  getEditor: () => Editor | null
): TemplateInsertHarness {
  let controller!: TemplateInsertController
  const cleanup = $effect.root(() => {
    controller = createTemplateInsert({ getEditor })
  })
  return {
    get controller() {
      return controller
    },
    destroy() {
      cleanup()
    }
  }
}
