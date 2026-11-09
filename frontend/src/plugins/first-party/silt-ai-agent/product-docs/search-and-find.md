---
id: search-and-find
title: Search vault, switch page, and AI search
---

## Search vault (keyword)

Open **Search vault** (default **Ctrl+Shift+F**). The modal placeholder is
along the lines of “Search notebooks, sections, or task content…”.

- Filters can limit to the main vault or **+ Linked** notebooks, and by
  notebook, tag, or block type (tasks, notes, headings).
- **Task filters** narrow to task attributes: status, owner (exact name),
  priority floor, an inclusive due-date range, and overdue-only. The priority
  floor follows the app’s 1 = High scale: **P1+** means High only,
  **P2+** means High or Normal, **P3+** means any recorded priority.
- Task filters need a query — they narrow a search rather than listing tasks
  on their own. To browse tasks, use the **Tasks** hub.
- A type, tag, or task filter hides **page-title** matches, because a page row
  carries no block-type/tag/task attributes. Task filters go further and
  restrict results to task rows, so notes and headings are hidden too.
  Notebook and Vault / + Linked scope do **not** hide page titles — they narrow
  page rows the same way they narrow block rows.
- Text matching covers block content **and** the notebook/section names, so a
  term that appears only in a notebook or section name matches every block in
  that notebook or section.
- Results jump to a page or block. Standalone tasks open the **Tasks** hub.
- Works **without** AI — this is classic full-text / keyword search.

## Switch page (quick open)

**Switch page** (default **Ctrl+P**) jumps by page title — navigation, not
full-text content search.

## Find in page

**Find in page** (default **Ctrl+F**) searches only the current open document.
The **⇄** control in the find bar reveals **Replace** / **Replace All**; **Ctrl+H**
opens them directly.

## Silt AI search (semantic / Q&A / agent)

Open **Silt AI** from the title bar (one drawer for Agent, vault Q&A, and
Writing).

| Need | Use |
| --- | --- |
| Answers only from **your notes** with citations | Vault **Q&A** (needs **Enable AI** + usually **Semantic search** + embedding model) |
| Multi-step tools, create/organize, product how-tos | **Agent** (**Enable AI**; writes follow **Agent vault writes**) |
| Rewrite / expand proposals | **Writing Assistant** |

**Semantic search** (Settings → AI) turns on embedding-backed retrieval for Q&A
and agent note-search tools. It is separate from **Search vault**.

## Tips

- “Find my note about X” → try **Search vault** first; use Q&A when you want a
  synthesized answer with citations.  
- “How do I use Silt?” → Agent + product help (`search_product_docs`).  
- Configure models under **Settings → AI** (see Bring your own model help).
