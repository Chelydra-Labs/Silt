---
name: silt
description: Work with a local Silt vault via MCP — search first, cite sources, confirm before edits. No credentials in prompts.
---

# Silt vault agent skill

You are helping the user with notes in their **local Silt vault** through the
Silt MCP server (`silt` / `silt mcp`). Markdown on disk is the source of truth;
SQLite is a cache.

## Principles

1. **Search first.** Prefer `search_blocks` / `search_notes` before reading whole pages.
2. **Cite sources.** When answering from vault content, cite `notebook` / `section` / `page` (and block id when relevant).
3. **Confirm before writes.** Never call write tools (`create_page`, `append_to_page`, `insert_under_heading`, `create_task`, `update_blocks`, `set_page_property`, `set_page_type`) unless the user explicitly asked to change the vault. Summarize the planned edit and wait for confirmation.
4. **No credentials.** Do not ask for or store API keys, MCP bearer tokens, or OS keyring secrets in chat. Tokens live in Silt Settings / the OS keyring only.
5. **Bounded tools.** There is no delete, move, or bulk-wipe tool. Do not invent destructive operations.
6. **Surgical writes first.** Prefer `append_to_page` / `insert_under_heading` / `create_task` over full-page `update_blocks`. When replacing a page, keep existing block `id` values whenever possible so links and history stay stable.

## Typical workflow

1. `list_notebooks` if you need structure.
2. `search_blocks` with a focused query (+ optional notebook/tag filters).
3. `read_page` / `read_blocks`, or `get_block` / `get_backlinks` / `get_page_metadata` when you need a single block, inbound links, or typed properties.
4. Answer with citations.
5. Only after confirmation: prefer `append_to_page`, `insert_under_heading`, or `create_task`; use `create_page` + `update_blocks` only when a full-page rewrite is required.
6. If `insert_under_heading` returns ambiguous/not-found `candidates`, retry with a full `A::B` path from that list.

## Errors

If a tool returns `isError`, read the message and fix the request (missing vault, write grant off, bad path). Do not retry blindly.
