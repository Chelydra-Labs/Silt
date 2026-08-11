---
id: getting-started
title: Getting started with Silt AI
---

## Open the Silt AI drawer

Click **Silt AI** in the title bar. One drawer hosts the agent, vault Q&A, and
writing assists. There is no separate Agent window.

## Enable AI

1. Open **Settings → AI**.
2. Turn on **Enable AI** (master switch).
3. Configure a **chat** model (local Ollama or OpenAI-compatible). See the
   Bring your own model help topic.
4. Optional: enable **Semantic search** and configure an **embedding** model
   for note search and related-note tools.

With AI off, the agent and other AI capabilities stay unavailable.

## Semantic search

**Semantic search** powers hybrid note search (`search_notes`), related notes,
and link suggestions. Without it, the agent can still read blocks, query tasks,
and use product help — but not vector search over your vault.

## Agent vault writes

Under **Settings → AI → Agent vault writes**:

- **Read only** — agent cannot change notes.
- **Confirm** (default) — writes need your approval in the chat.
- **Auto** — small single edits apply immediately; bulk rename and extract
  still always ask for Confirm.
