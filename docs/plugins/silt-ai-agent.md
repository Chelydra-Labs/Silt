# AI Agent (`silt-ai-agent`)

A first-party, **headless capability provider** that uses tools to search,
read, create, and organize notes across your vault. The agent runs a
**multi-step loop**: it decides which tools to call, reads their results, and
continues until it has enough to answer or act. Its tool calls, evidence, and
answers render in the single unified **Silt AI** drawer alongside capabilities
from Q&A and Writing Assistant. There is no standalone AgentHub surface;
destructive operations require explicit confirmation in the shared drawer.

**Off by default.** Enable under **Settings → AI** (master **Enable AI**
switch). The agent adds no settings tab of its own — it inherits the
configured chat + embedding models.

> Not the same as **AI Assistant** (`silt-ai-qa`; vault Q&A / search with
> citations) or **Writing Assistant** (`silt-ai-assistant`; curated writing
> actions whose every result is an accept/discard proposal). The agent is
> the multi-step **actor**: it chains tools together to complete a goal. The
> AI Summary plugin annotates a single note; the agent orchestrates across
> many.

## What it does

1. **Receives a goal** from the chat input (e.g. "find my overdue tasks in
   the Work notebook and draft a follow-up note").
2. **Plans + calls tools** — up to 8 iterations per turn. Each iteration
   sends the conversation + the tool catalog to the chat model; if the model
   requests tools, they dispatch in parallel and their results feed the next
   iteration.
3. **Answers** in plain prose when no further tool use is needed, streamed
   live into the chat.

The agent **never writes unsolicited**. Read-only tools run inline; any
destructive operation is staged behind a confirmation gate (see **Safety
model** below).

### Tool catalog

Thirteen tools, registered in three tiers. Most are read-only and run inline.
Three tiers of write safety apply:

- **Read-only** — query the vault and return text; no mutation.
- **Direct write** — append or rewrite markdown. These are *not* staged: each is
  a single, reversible edit (markdown is the source of truth and each change is
  one undo step). `extract_and_save` only ever writes to a brand-new page, so
  source blocks are never touched.
- **Staged (destructive)** — `rename_tag` rewrites the hashtag token across
  every matching block in one shot, so it routes through the confirmation gate
  in **Safety model** below before any block is touched.

| Tier | Tool | Safety | Parameters |
|---|---|---|---|
| **P0** | `search_notes` | read-only | `query` (req), `top_k?` (1–50, default 10), `filters?` `{notebook?, section?, type?}` |
| | `read_blocks` | read-only | `block_ids` (req, max 20), `include_context?` (default true — parent + siblings) |
| | `get_backlinks` | read-only | `target` (req, UUID or page path), `include_embeds?` (default true), `max_results?` (1–100, default 20) |
| | `query_tasks` | read-only | `status?`, `owner?`, `priority_min?` (1–3), `due_before?`, `due_after?`, `tags?`, `notebook?`, `is_blocked?`, `limit?` (1–50, default 20) |
| | `create_note` | direct write | `page` (req), `content` (req), `notebook?` (default = active), `section?`, `tags?` |
| **P1** | `get_related_notes` | read-only | `block_id` (req), `top_k?` (1–50, default 10), `min_score?` (0–1, default 0.5) |
| | `update_block` | direct write | `block_id` (req), `content` (req), `tags?` (TASK → `setTaskTags`; else folded as `#hashtags`) |
| | `list_tags` | read-only | _(none)_ — tag paths with usage counts, top 200 |
| | `find_untagged` | read-only | `scope?` (notebook), `limit?` (1–100, default 20) — TASK blocks with no tags |
| | `rename_tag` | **staged** | `old_tag` (req), `new_tag` (req) — bulk `#hashtag` rewrite |
| **P2** | `get_vault_statistics` | read-only | `scope?` (notebook) — block/task counts, orphans, stale tasks, top tags, recent edits |
| | `suggest_link_targets` | read-only | `block_id` (req), `max_suggestions?` (1–20, default 5) — excludes already-linked targets |
| | `extract_and_save` | direct write | `source_block_ids` (req, max 20), `mode` (req: `summary`\|`flashcards`\|`qa_pairs`\|`action_items`), `target` (req: `{notebook, page, section?}`) |

Required parameters are marked `(req)`; others are optional. Every tool result
fed to the model is capped at 10 KB (larger bodies carry a visible `[… truncated]`
marker). SQL is parameterized throughout — the agent has no raw-SQL tool.

## Setup

1. **Settings → AI**
   - Turn on **Enable AI**.
   - Configure a **chat** model (local Ollama or OpenAI-compatible).
   - For semantic tools (`search_notes`, `get_related_notes`,
     `suggest_link_targets`), enable **Semantic search** and configure an
     **embedding** model.
   - See [BRING_YOUR_OWN_MODEL.md](../BRING_YOUR_OWN_MODEL.md).
2. Open **Silt AI** from the title bar and state a goal.

## Write policy & untrusted content

- **Direct writes** (`create_note`, `update_block`, `extract_and_save`) apply
  immediately as single reversible markdown edits.
- **Staged / destructive** ops (`rename_tag`) pause for explicit confirmation
  in the drawer before any vault mutation.
- Tool results that contain vault text are wrapped in
  `<vault_data tool="…">…</vault_data>` so the model treats them as data, not
  instructions. The system prompt also forbids acting on embedded commands
  found inside tool output.

Tool-calling works best on models that advertise tool/function support.
Small local models may misroute calls; the structural arg-validation in the
tool registry keeps a malformed call from reaching tool code (it surfaces
as an error the model can recover from on the next iteration).

## The agent loop

- **User-invoked.** Nothing runs until you send a message — no background
  polling, no auto-run on note open.
- **Bounded.** A turn stops after **8 iterations** whether or not the model
  emitted a final answer (reported as "hit iteration cap" so you can
  rephrase).
- **Transparent.** Every tool call and result renders as a card between
  your prompt and the answer; the chat shows what the agent did and why.
- **Cancellable.** **Escape** (or the stop button) aborts the in-flight run
  between iterations; partial tool side-effects already applied remain
  (they are normal edits, each a single undo step).

## Safety model

Destructive operations — today `rename_tag`, and future delete / merge /
bulk-rewrite tools — cannot be undone from the model's side, so the agent
**stages** them rather than executing directly:

1. The tool returns a **preview** ("Rename tag #work → #project across 17
   blocks") plus a **single-use token**.
2. The loop pauses; the chat surfaces a confirm dialog. The model does not
   see the staged payload — it only learns the outcome after you resolve.
3. **Confirm** redeems the token and runs the tool's commit half against
   the **stored** parameters (the model cannot mutate the op between
   staging and confirmation). **Reject** marks the token consumed and feeds
   "rejected by user" back to the model so it can re-plan.

Tokens are 128-bit crypto-random, expire after **5 minutes**, and are
parameter-bound in SQL (never interpolated) so a malformed token cannot
inject. The staging table lives in this plugin's per-plugin SQLite store
and is swept on every vault open.

This is what re-opens the "autonomous agent / tool-use loop" the Writing
Assistant spike explicitly excluded: user-invoked entry, transparent tool
calls, and staged confirmation for anything destructive together preserve
the "no unsolicited writes" invariant while letting the agent act. See
[silt-ai-assistant-spike.md](./silt-ai-assistant-spike.md).

## Privacy

- Note content reached by the agent is sent to the **configured chat
  endpoint** on every iteration, and to the **configured embedding
  endpoint** when a semantic tool runs.
- **Local** endpoints (Ollama on this machine) keep data on-device.
  **Cloud** endpoints process content under that provider's policy.
- The staging-token table and the embedding cache live only under
  `<vault>/.system/plugins/silt-ai-agent/data/plugin.db` and are **deleted
  on uninstall**.
- The agent has **no raw-SQL tool**. It reads via typed SDK helpers
  (`sqliteQuery` is SELECT/WITH-only; `queryByTag` / `queryByDateRange` /
  `getBacklinks`) and writes via the SDK mutators — every privileged path
  is capability-gated and session-verified, the same boundary every other
  plugin uses.

## Troubleshooting

| Symptom | Fix |
|---|---|
| Agent calls the wrong tool / loops on the same call | Use a larger or tool-advertised model; rephrase the goal; the 8-iteration cap stops runaway loops |
| Semantic tools return empty | Set the embedding model in AI Provider; check that other notes exist |
| "Chat model not configured" | Settings → AI Provider → set chat model |
| Tool result truncated in chat | Tool bodies cap at 10 KB for the model; the agent re-queries with a narrower call when it needs more |
| Staged op shows "expired" | Tokens live 5 minutes — re-run the request and confirm promptly |
| Agent hit the iteration cap | Rephrase toward a narrower goal, or split into two turns |

## Related

- Plugin author notes: [PLUGIN_DEVELOPMENT.md](../PLUGIN_DEVELOPMENT.md) (`ctx.ai.complete` with tools, `plugin-db`)
- Vault Q&A / search: [silt-ai-qa.md](./silt-ai-qa.md)
- Writing proposals: [silt-ai-assistant.md](./silt-ai-assistant.md)
- Spike decision note: [silt-ai-assistant-spike.md](./silt-ai-assistant-spike.md) (this plugin re-opens the agent-loop exclusion under safety gates)
- Sprint issues: #596–#608
