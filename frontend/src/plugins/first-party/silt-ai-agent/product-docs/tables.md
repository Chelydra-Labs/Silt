---
id: tables
title: Tables in notes
---

## Create a table

You can insert a table in any of these ways:

1. **Slash command (recommended)**  
   - Type **`/`** in the editor.  
   - Choose **Table** for a default **3×3** grid.  
   - Or choose **Custom table…** to pick rows and columns, then confirm.

2. **Format toolbar**  
   Open the **Insert** menu on the formatting toolbar and choose **Table**
   (inserts 3×3).

3. **Markdown**  
   Type or paste a GFM pipe table, for example:

   ```markdown
   | Name | Status |
   | --- | --- |
   | Alpha | Done |
   ```

Silt stores tables as normal GFM markdown so they stay portable with other
editors.

## Edit rows and columns

With the caret **inside** a table, a floating **table actions** bar appears:

- **Insert row above** / **Insert row below**
- **Delete row**
- **Insert column left** / **Insert column right**
- **Delete column**

Default keyboard shortcuts (customizable under Settings → Hotkeys):

| Action | Default |
| --- | --- |
| Insert row above | Ctrl+Shift+Up |
| Insert row below | Ctrl+Shift+Down |
| Insert column left | Ctrl+Shift+Left |
| Insert column right | Ctrl+Shift+Right |

There is no cell merge (GFM tables are a simple grid).

## Tips

- Tab between cells in the editor like a spreadsheet-style grid.
- Prefer slash **Custom table…** when you know the size up front.
- For “how do I make a table with a slash command?”: type `/`, pick **Table**
  or **Custom table…**.
