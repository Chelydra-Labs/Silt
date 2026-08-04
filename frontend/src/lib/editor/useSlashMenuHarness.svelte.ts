// Test-only harness that mounts `createSlashMenu` inside a `$effect.root`
// scope so the menu's `$state` cells + palette-measuring `$effect` have a
// reactive owner. Mirrors the `$effect.root` pattern from
// useSpellcheckMenuHarness.svelte.ts.
import type { Editor } from 'svelte-tiptap'
import { createSlashMenu, type SlashMenuOptions } from './useSlashMenu.svelte'

export interface SlashMenuHarness {
  controller: ReturnType<typeof createSlashMenu>
  destroy: () => void
}

export function createSlashMenuHarness(
  getEditor: () => Editor | null | undefined,
  opts: Partial<Omit<SlashMenuOptions, 'getEditor'>> = {}
): SlashMenuHarness {
  let controller!: ReturnType<typeof createSlashMenu>
  const noop = (): void => {}
  const cleanup = $effect.root(() => {
    controller = createSlashMenu({
      getEditor,
      onOpenMathPopover: opts.onOpenMathPopover ?? noop,
      onOpenTableSizePicker: opts.onOpenTableSizePicker ?? noop,
      onOpenColorPicker: opts.onOpenColorPicker ?? noop,
      onShowEmbedPicker: opts.onShowEmbedPicker ?? noop,
      onShowTemplatePicker: opts.onShowTemplatePicker ?? noop
    })
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
