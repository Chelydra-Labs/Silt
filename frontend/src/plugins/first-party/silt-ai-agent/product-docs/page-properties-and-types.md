---
id: page-properties-and-types
title: Page properties and types
---

## Open page properties

Edit frontmatter-backed fields for the current page:

- Hotkey **Edit page properties** (default **Ctrl+;**)
- Sidebar page context menu → **Page properties**
- Breadcrumb type chip (when the page already has a type)

The properties panel docks at the bottom; the editor stays usable.

## Core fields

Every page can carry:

- **Type** — optional schema for structured notes  
- **Date**, **tags**, **aliases**, **created**  
- **Modified** — read-only (from the file)

Values live in the page’s YAML frontmatter (markdown remains the source of
truth).

## Assign a page type

1. Open properties, or type **`/`** → **Set page type**.  
2. On an untyped page, the breadcrumb can offer **Assign a page type**.  
3. Pick a type defined under the vault’s type schemas
   (`.system/types/*.yaml`).

Typed pages gain extra fields from that schema (text, dates, relations, and
so on). Switching between types may open a **Turn into** mapping dialog so
values map cleanly.

## Type dashboards

From a typed page’s breadcrumb, **View all [Type]** opens a table of pages of
that type (with its own saved views, separate from the Tasks hub).

## Tips

- Relation fields use typeahead to pick other pages.  
- Tags in properties are the same `#hashtag` system used in the editor.  
- Untyped pages stay simple in the breadcrumb until you assign a type.
