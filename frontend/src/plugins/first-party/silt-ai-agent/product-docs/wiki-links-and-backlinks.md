---
id: wiki-links-and-backlinks
title: Wiki links and backlinks
---

## Create a page link

In the editor, type **`[[`** to open page-link typeahead. Choose a page or keep
typing a path. Common forms:

- `[[Page name]]` — shortest unique title
- `[[Notebook/Section/Page]]` — path-qualified when names collide
- `[[Page#Heading]]` — link to a heading on a page
- `[[target|label]]` — show a custom label

## Block references

Type **`((`** to pick a block and insert a block reference (`((uuid))`). That
points at a specific block, not just a page.

## Open backlinks

See who links **to** the current page:

1. Left **activity bar** → **Backlinks**, or  
2. Breadcrumb control **Show backlinks**

The panel lists inbound wiki links, block refs, and embeds, grouped by source
page. Click a row to open the source (and jump toward the reference when
possible).

## Unlinked mentions

The backlinks surface can show **unlinked mentions** — plain text that matches
the page title but is not yet a `[[link]]`. Use **Link** on a mention to turn
the first residual hit into a real wiki link.

## Tips

- Empty state means nothing links here yet — add `[[this page]]` from other notes.
- Renaming or moving a page rewrites unique inbound `[[…]]` targets when Silt can
  do so safely.
- Links into standalone tasks open the **Tasks** hub instead of a hidden system page.
