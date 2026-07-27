// Test-only harness that mounts `useSuggests` inside a `$effect.root` scope so
// the tag controller's `$effect` (config-hot-reload re-rank) has a reactive
// scope to register against. Plain `.test.ts` files can't use runes, and
// calling `useSuggests` directly at test top-level throws because `$effect`
// outside a component/ root has no owner scope. Mirrors the `$effect.root`
// pattern used by `settings/editor-tokens.svelte.ts`.
import type { Editor } from 'svelte-tiptap'
import { useSuggests, type SuggestsController } from './useSuggests.svelte'

export interface SuggestsHarness {
  controller: SuggestsController
  destroy: () => void
}

export function createSuggestsHarness(
  getEditor: () => Editor | null
): SuggestsHarness {
  let controller!: SuggestsController
  const cleanup = $effect.root(() => {
    controller = useSuggests({ getEditor })
  })
  return {
    get controller() {
      return controller
    },
    destroy() {
      // Cancel in-flight debounce / IPC requests before tearing down the
      // effect scope so no deferred callback fires against a destroyed root.
      controller.mention.destroy()
      controller.blockRef.destroy()
      controller.pageLink.destroy()
      cleanup()
    }
  }
}
