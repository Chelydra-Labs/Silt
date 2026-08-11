---
id: slash-commands
title: Slash commands in the editor
---

## Open the slash menu

In a note page, type **`/`** at the start of a line or after a space. A
palette lists built-in commands (and any plugin commands). Filter by typing
part of the **label** (for example `table`, `heading`, or `callout`), then
press Enter or click an item.

You can also open **Keyboard shortcuts** from the slash menu (**Keyboard
shortcuts**) or with **Shift+?** to see hotkeys.

## Insert a table with slash

| Command label | What it does |
| --- | --- |
| **Table** | Inserts a **3×3** GFM table at the cursor |
| **Custom table…** | Opens a size picker, then inserts a table with the rows and columns you choose |

Type `/table` (or just `/` and filter for “Table”) and pick one of those
entries. Tables are plain markdown pipes on disk; you can also paste a GFM
table.

## Other common slash commands

| Label | Purpose |
| --- | --- |
| **Heading 1** … **Heading 6** | Turn the current block into a heading |
| **Task** / **Plain note** | Convert the block to a task or plain note |
| **Bold**, **Italic**, **Underline**, **Strikethrough**, **Inline code**, **Highlight** | Toggle marks on the selection |
| **Quote** | Toggle a blockquote |
| **Callout** / **Callout: Note/Info/Tip/Warning/Danger/Success** | Insert a callout |
| **Code block** | Insert a fenced code block |
| **Mermaid diagram** | Insert a Mermaid code block |
| **Math equation** | Insert a display math block |
| **Foldable section** | Insert a collapsible details block |
| **Text color** / **Background color** | Open a color picker |
| **Today** / **Calendar** | Insert today’s date or pick a date |
| **Embed Block** | Embed another block |
| **Template** | Insert a page template at the cursor |
| **Set page type** | Assign or change this page’s type |
| **Keyboard shortcuts** | Open the shortcuts cheatsheet |

Plugin features (for example Writing Assistant actions when AI is on) may add
more slash entries.

## Slash vs format toolbar

Many inserts are also on the floating **format toolbar** (when the caret is
in the editor). Use **Insert → Table** for a quick 3×3 table without the
slash menu. Slash is best when you remember a name; the toolbar is best for
discovery.
