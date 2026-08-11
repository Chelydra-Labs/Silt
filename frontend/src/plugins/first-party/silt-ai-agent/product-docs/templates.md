---
id: templates
title: Page templates
---

## What templates are

Templates let you insert a structured page (or a section into the current page)
from a built-in library or your own custom templates.

## Where templates live

Custom templates live in your vault under `.system/templates/` as markdown
files. Built-in templates ship with the app. You can add custom templates
without rebuilding Silt.

## Using a template

Open the template picker from the UI (new page / insert from template flows)
and choose a built-in or custom template. The inserted content becomes normal
editable markdown on the page.

## Authoring tips

- Keep templates focused on structure (headings, placeholders, task lists).
- Prefer clear section titles so the picker and search stay useful.
- Custom templates travel with the vault (export/import and sync include
  `.system/`).
