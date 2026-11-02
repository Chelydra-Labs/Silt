// #1005: a conflict-held editor is setEditable(false) — the slash menu can
// outlive the hold that armed it, and its selections dispatch commands that
// ProseMirror's editable gate never sees. handleSlashSelect must refuse the
// whole branch, including the trigger-text deletion.
import { describe, expect, it, vi } from 'vitest'
import type { Editor } from 'svelte-tiptap'
import { createSlashMenuHarness } from './useSlashMenuHarness.svelte'

function makeStubEditor(opts: { editable?: boolean } = {}): unknown {
  return {
    isDestroyed: false,
    isEditable: opts.editable !== false,
    state: {
      selection: {
        $from: { start: () => 1, parentOffset: 0 }
      }
    },
    commands: { deleteRange: vi.fn() }
  }
}

describe('slash menu conflict hold (#1005)', () => {
  it('refuses the selection (and trigger deletion) on a non-editable editor', () => {
    const editor = makeStubEditor({ editable: false }) as {
      commands: { deleteRange: ReturnType<typeof vi.fn> }
    }
    const harness = createSlashMenuHarness(() => editor as unknown as Editor)
    try {
      harness.controller.handleSlashSelect('type')
      expect(editor.commands.deleteRange).not.toHaveBeenCalled()
    } finally {
      harness.destroy()
    }
  })
})
