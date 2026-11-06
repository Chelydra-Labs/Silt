// Editor-wide constants whose invariants span modules. Kept in a neutral
// module (not inside one of the consumers) so every importer references one
// value — a per-module copy would silently drift.

/**
 * The deepest outline depth the editor renders. Matches the deepest
 * `[data-depth='N']` rule in the editor CSS (frontend/src/index.css —
 * `.ProseMirror > div[data-depth='6']`); beyond it a block renders at
 * depth-0 width (no matching rule), so a deeper indent would be invisible
 * to the user while still changing the saved file.
 *
 * Both depth writers clamp against this one constant: the Tab indent keymap
 * (keymaps.ts) and the drag indent-on-drop resolver (dragIndentDrop.ts).
 */
export const MAX_DEPTH = 6
