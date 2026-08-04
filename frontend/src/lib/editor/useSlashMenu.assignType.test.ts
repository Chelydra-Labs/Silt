// Contract test for the /type slash command → assign-page-type window-event
// bridge. The slash menu (editor) dispatches a CustomEvent that App routes to
// the page-type controller; renaming the event string on one side breaks
// silently. Both sides import ASSIGN_PAGE_TYPE_EVENT from shell/pageTypeEvents
// so the literal lives in one place; this test exercises the real dispatch path
// (handleSlashSelect('type')) and pins that the emitted event type matches the
// shared constant. App's listener uses the same import (verified by tsc/check).
import { describe, expect, it, vi } from 'vitest'
import type { Editor } from 'svelte-tiptap'
import { createSlashMenuHarness } from './useSlashMenuHarness.svelte'
import { ASSIGN_PAGE_TYPE_EVENT } from '../../shell/pageTypeEvents'

// Minimal editor stub: handleSlashSelect('type') only touches isDestroyed,
// state.selection.$from (start/parentOffset), and commands.deleteRange before
// dispatching the event — no live TipTap editor is needed to reach that branch.
function makeStubEditor(): unknown {
  return {
    isDestroyed: false,
    state: {
      selection: {
        $from: { start: () => 1, parentOffset: 0 }
      }
    },
    commands: { deleteRange: vi.fn() }
  }
}

describe('/type slash command → assign-page-type bridge', () => {
  it('dispatches the event with the shared contract name', () => {
    const harness = createSlashMenuHarness(
      () => makeStubEditor() as unknown as Editor
    )
    const seen: string[] = []
    const listener = (e: Event): void => {
      seen.push(e.type)
    }
    window.addEventListener(ASSIGN_PAGE_TYPE_EVENT, listener)

    try {
      harness.controller.handleSlashSelect('type')
      expect(seen).toEqual([ASSIGN_PAGE_TYPE_EVENT])
    } finally {
      window.removeEventListener(ASSIGN_PAGE_TYPE_EVENT, listener)
      harness.destroy()
    }
  })
})
