# Spike: silt-ai-assistant action catalog (#229)

**Sprint 23** scoping decision for the Writing Assistant plugin.  
Target models: local small models (Qwen3-class MoE, Gemma-class) via OpenAI-compatible endpoints, plus one cloud OpenAI-compatible provider.

## Product naming

| Layer | Value | Why |
|---|---|---|
| Plugin id | `silt-ai-assistant` | Matches milestone issues |
| Display name | **Writing Assistant** | Avoids collision with `silt-ai-qa` product name “AI Assistant” |
| Settings tab | Settings → AI → Capabilities (Writing Assistant; was standalone `plugin:silt-ai-assistant`) | deep-link aliases → `ai` |

## Evaluation summary

Candidates scored on usefulness (note workflow), reliability (parse success), and small-model suitability (single-shot, strict output).

| Action | Usefulness | Reliability | Small-model fit | Ship? |
|---|---|---|---|---|
| Draft / Expand | High | High | High | **Yes** |
| Rewrite succinct | High | High | High | **Yes** |
| Improve clarity | High | High | High | **Yes** |
| Extract action items | High | Medium–High | High (JSON list) | **Yes** |
| Suggest tags (existing vocab) | High | Medium | Medium (filter client-side) | **Yes** |
| Suggest related notes | High | Medium | High (embed rank, not LLM) | **Yes** |
| Generate note title | Medium | High | High | **No (v1)** — low incremental value vs draft/expand; revisit later |

## Actions to ship (Sprint 23)

### 1. `draft-expand` — Draft / Expand
- **Input:** short description (sidebar) or selection
- **Output:** markdown outline or draft only (no preamble)
- **Apply kind:** `insert-below` or `replace-selection`
- **Prompt contract:** “Output markdown only. No title line unless asked. No commentary.”

### 2. `rewrite-succinct` — Rewrite succinct
- **Input:** selection or current block
- **Output:** condensed bullets / note form, meaning preserved
- **Apply kind:** `replace-selection`
- **Prompt contract:** preserve task checkboxes and `((uuid))` if present in input; never invent id comments

### 3. `improve-clarity` — Improve clarity
- **Input:** selection or block (bounded by `max_input_chars`)
- **Output:** clearer prose, same meaning and structure level
- **Apply kind:** `replace-selection`
- **Oversized:** warn + truncate mid-note (head+tail) like AI Summary

### 4. `extract-tasks` — Extract action items
- **Input:** note/selection
- **Output:** JSON `{ "tasks": string[] }` → proposed GFM `- [ ]` lines
- **Apply kind:** `insert-tasks`
- **Dedupe:** normalize titles vs existing page tasks before propose

### 5. `suggest-tags` — Suggest tags
- **Input:** note excerpt + existing vocabulary from `tags` table
- **Output:** JSON `{ "tags": string[] }` then filter
- **Filters:** `existing_vocab_only`, `max_tag_suggestions`, prefer hierarchical path completion
- **Apply kind:** `apply-tags` / insert hashtags

### 6. `suggest-related` — Suggest related notes
- **Input:** selection/note text
- **Method:** `ctx.ai.embed` on query + candidate blocks (FTS/recent), cosine rank — **no** second full-vault index; **no** read of `silt-ai-qa` plugin DB
- **Output:** ranked `{ blockId, snippet, score }[]`
- **Apply kind:** `insert-links` as `((uuid))` list

## Excluded (with rationale)

| Idea | Rationale |
|---|---|
| Open-ended autonomous agent / tool-use loop | Unreliable on small models; violates “no unsolicited writes” _(re-opened by Sprint 41’s `silt-ai-agent` — [silt-ai-agent.md](./silt-ai-agent.md) — under safety gates: user-invoked entry, transparent tool calls, staged confirmation for destructive ops)_ |
| Image generation | Out of scope for note writing; not high-value for Silt’s markdown core |
| Auto-generate content on note open | Unsolicited AI slop; user must invoke |
| Free-form chat that edits the vault | Overlaps Q&A drawer; edit path must stay proposal-based |
| Multi-file refactor / bulk rewrite | High risk, weak small-model reliability |
| Generate note title (v1) | Marginal value vs draft; can add later without framework change |

## Prompt / parse approach

1. **Strict contracts** — system prompts demand markdown-only or JSON-only; no fences required but strip fences if present.
2. **Strip block identity** from model input (`<!-- id: … -->`); re-apply only via SDK mutators on accept.
3. **One retry** on parse failure for JSON actions.
4. **Streaming** for long markdown actions when provider supports it; buffer then parse for JSON actions.
5. **Prompt overrides** — advanced setting map `prompt_overrides[actionId]` replaces the default system prompt when non-empty.

## Context budgets

| Setting | Default | Notes |
|---|---|---|
| `max_input_chars` | 12000 | Align with AI Summary; warn when truncated |
| Related candidates | 40 blocks | Embed in batches of 16 |
| Tag vocab sample | 200 paths | Prefer paths matching note keywords |

## SDK feasibility (confirmed)

| Surface | Available |
|---|---|
| `ctx.ai.complete` / stream | Yes (Sprint 20/22) |
| `ctx.ai.embed` | Yes |
| `registerSlashCommand` | Yes |
| First-party Svelte panel / drawer | Yes (pattern: `silt-ai-qa`) |
| `content-mutate` mutators | Yes |
| `sqliteQuery` on `tags` / `blocks` | Yes |
| Host proposed-edit API | **No** — plugin-local proposal model |

## Non-goals reaffirmed

No Playwright. Off by default. AI never writes without Accept.
