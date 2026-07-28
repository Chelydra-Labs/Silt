// Test-only harness that mounts `createSpellcheckMenu` inside a `$effect.root`
// scope so the controller's `$state` cell + listener `$effect` have a reactive
// owner. Mirrors the `$effect.root` pattern from useSuggestsHarness.svelte.ts.
import type { Editor } from 'svelte-tiptap'
import {
  createSpellcheckMenu,
  type SpellcheckMenuController
} from './useSpellcheckMenu.svelte'

export interface SpellcheckMenuHarness {
  controller: SpellcheckMenuController
  destroy: () => void
}

export function createSpellcheckMenuHarness(
  getEditor: () => Editor | null
): SpellcheckMenuHarness {
  let controller!: SpellcheckMenuController
  const cleanup = $effect.root(() => {
    controller = createSpellcheckMenu({ getEditor })
  })
  return {
    get controller() {
      return controller
    },
    destroy() {
      controller.dispose()
      cleanup()
    }
  }
}
