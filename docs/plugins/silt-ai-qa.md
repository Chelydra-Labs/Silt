# AI Assistant (`silt-ai-qa`)

Search your vault with hybrid keyword + semantic retrieval, and get answers
grounded in your own notes with clickable citations.

**Off by default.** Enable under **Settings → AI Assistant** (or Plugins).

## What it does

1. **Indexes** note blocks with an embedding model into a per-plugin SQLite
   store (`sqlite-vec`). Markdown remains the source of truth; the index is a
   re-derivable cache.
2. **Retrieves** relevant passages with **hybrid search**: keyword (FTS5) +
   semantic (vector), fused with weighted Reciprocal Rank Fusion.
3. **Answers** via your configured chat model, with inline `[n]` citations that
   navigate to the source block.

## Setup

1. **Settings → AI Provider**
   - Configure a **chat** model (local Ollama or OpenAI-compatible).
   - Configure an **embedding** model independently (e.g. `nomic-embed-text`).
   - See [BRING_YOUR_OWN_MODEL.md](../BRING_YOUR_OWN_MODEL.md).
2. **Settings → AI Assistant**
   - Enable the plugin.
   - Optionally limit **notebook scope**.
   - Click **Rebuild index** (first run). Progress shows in the panel.
3. Open the **AI Assistant** drawer from the status bar and ask a question.

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
| “Embedding model not configured” | Set embedding model on AI Provider page |
| Empty / weak answers | Rebuild index; raise hybrid weight toward semantic; check notebook scope |
| Dimension / model change | Rebuild index (vec0 dimensions are fixed per model); model changes auto-trigger rebuild on open |
| Streaming fails (native Google/Anthropic) | Use OpenAI-compatible or local chat for streaming; non-stream fallback still works |
| Index slow on large vaults | Narrow notebook scope; leave auto re-embed on for incremental updates |

## Related

- Plugin author notes: [PLUGIN_DEVELOPMENT.md](../PLUGIN_DEVELOPMENT.md) (sqlite-vec, `ctx.ai`)
- Sprint issues: #224–#228
