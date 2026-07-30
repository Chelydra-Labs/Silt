// SiltHardBreak — TipTap HardBreak that works inside isolating prose blocks.
//
// Stock HardBreak.setHardBreak returns false when the parent is isolating
// (noteBlock / taskBlock / headerBlock all are). That makes Shift/Mod-Enter a
// no-op in the outliner. We keep the upstream insert + keepMarks path and only
// drop the isolating guard. Shift-Enter is owned by SiltBlockKeymaps (task
// modal vs soft break); this extension binds Mod-Enter only.

import HardBreak from '@tiptap/extension-hard-break'

export const SiltHardBreak = HardBreak.extend({
  addCommands() {
    return {
      setHardBreak:
        () =>
        ({ commands, chain, state, editor }) => {
          return commands.first([
            () => commands.exitCode(),
            () =>
              commands.command(() => {
                const { selection, storedMarks } = state
                const { keepMarks } = this.options
                const { splittableMarks } = editor.extensionManager
                const marks =
                  storedMarks ||
                  (selection.$to.parentOffset && selection.$from.marks())

                return chain()
                  .insertContent({ type: this.name })
                  .command(({ tr, dispatch }) => {
                    if (dispatch && marks && keepMarks) {
                      const filteredMarks = marks.filter((mark) =>
                        splittableMarks.includes(mark.type.name)
                      )
                      tr.ensureMarks(filteredMarks)
                    }
                    return true
                  })
                  .run()
              })
          ])
        }
    }
  },

  addKeyboardShortcuts() {
    // Shift-Enter is handled by SiltBlockKeymaps (task → modal, else soft break).
    return {
      'Mod-Enter': () => this.editor.commands.setHardBreak()
    }
  }
})
