# AI Assistant (`silt-ai-qa`)

Search your vault with hybrid keyword + semantic retrieval, and get answers
grounded in your own notes with clickable citations.

This plugin is now a **headless capability provider** for the unified **Silt
AI** drawer. Its retrieval index, citations, settings, and lifecycle remain
plugin-owned; answers and evidence render in the shared typed transcript.

**Off by default.** Enable under **Settings → AI → Features → Semantic search**
(master **Enable AI** must be on). There is no independent Plugins-tab toggle.

## What it does

1. **Indexes** note blocks with an embedding model into a per-plugin SQLite
   store (`sqlite-vec`). Markdown remains the source of truth; the index is a
   re-derivable cache. Index schema/helpers are shared with the AI agent’s own
   hybrid index (separate `plugin.db` per plugin — not a shared database).
2. **Retrieves** relevant passages with **hybrid search**: keyword (FTS5) +
   semantic (vector), fused with weighted Reciprocal Rank Fusion.
3. **Answers** via your configured chat model, with inline `[n]` citations that
   navigate to the source block.

## Setup

1. **Settings → AI**
   - Turn on **Enable AI**, then **Semantic search**.
   - Configure a **chat** model (local Ollama or OpenAI-compatible).
   - Configure an **embedding** model independently (e.g. `nomic-embed-text`).
   - See [BRING_YOUR_OWN_MODEL.md](../BRING_YOUR_OWN_MODEL.md).
2. **Settings → AI → Capabilities** (fine-tuning only when Semantic search is on;
   Semantic search section)
   - Optionally limit **notebook scope**.
   - Click **Rebuild index** (first run). Progress shows in the settings page.
3. Open **Silt AI** from the title bar and ask a question.

When only one of keyword or semantic search fails mid-query, results continue
from the healthy side and a degraded-search signal is recorded (audit + UI).

## Unified AI chat

Q&A answers and clickable citations render in the shared Silt AI drawer. The
standard Search surface remains separate, and **Escape** closes the AI drawer.

## Privacy

- Note content is sent to the **configured embedding endpoint** when indexing
  and to the **configured chat endpoint** when answering.
- **Local** endpoints (Ollama on this machine) keep data on-device.
- **Cloud** endpoints process content under that provider’s policy.
- Vectors live only under
  `<vault>/.system/plugins/silt-ai-qa/data/plugin.db` and are **deleted on
  uninstall**.

## Troubleshooting

| Symptom | Fix |
|---|---|
| “Embedding model not configured” | Set embedding model on Settings → AI |
| Empty / weak answers | Rebuild index; raise hybrid weight toward semantic; check notebook scope |
| Dimension / model change | Rebuild index (vec0 dimensions are fixed per model); model changes auto-trigger rebuild on open |
| Token-by-token streaming feels chunky (native Google/Anthropic) | Host buffers those providers into one delta; use OpenAI-compatible/local for true SSE |
| Index slow on large vaults | Narrow notebook scope; indexing is always-on and incremental (page edits, deletes, and external file changes debounce into the vector index — no auto-re-embed toggle) |

## Related

- Plugin author notes: [PLUGIN_DEVELOPMENT.md](../PLUGIN_DEVELOPMENT.md) (sqlite-vec, `ctx.ai`)
- Writing transforms (not Q&A): [silt-ai-assistant.md](./silt-ai-assistant.md)
- Sprint issues: #224–#228
