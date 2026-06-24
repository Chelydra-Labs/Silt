# Improvements

- Quote blocks: prefix a note with `>` to render it with a left border and indent; nested quotes via `>>`; toggle with `/quote` or `Ctrl/Cmd+Shift+9`.
- Callouts: Obsidian-style `> [!type]` with seven variants (note, info, tip, warning, danger, success, quote); `/callout` slash command; editable title and body with variant accent colors.
- Fenced code blocks: GFM ` ```lang` syntax with multi-line editing, language badge, and a copy button; `/code` slash command or `Ctrl+Alt+C`.
- Foldable details: `<details><summary>` HTML round-trips as a collapsible section with a disclosure toggle; `/details` slash command.
- GFM pipe tables: consecutive `| col | col |` lines with a separator row are detected as an editable table with cell navigation, column resizing, and row/column operations; `/table` slash command.

## Known limitations

- **Tables:** The custom Cyber-Ink grid NodeView, contextual toolbar, and format-bar button are forthcoming. Tables currently render with the default editor grid.
- **Code blocks:** Syntax highlighting (Shiki) is forthcoming. Code blocks render as plain monospace text with a language badge and copy button.
- **Callouts and details:** Block-level nesting (e.g. a code block inside a callout, a table inside details) is not yet supported. Both containers accept inline content only.
- **Multi-line details:** The Go parser treats each `<details>` line as a separate NOTE block; the frontend converter merges them on load (bounded at 50 blocks).
