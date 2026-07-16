# Writing Assistant (`silt-ai-assistant`)

Curated AI **writing and structuring** actions for notes — draft, rewrite,
clarify, extract tasks, suggest tags, and suggest related notes. Every result
is a **proposal** you accept or discard. The AI never writes into your vault
unsolicited.

This plugin is now a **headless capability provider** for the unified **Silt
AI** drawer. Its settings, slash commands, lifecycle, and proposal/apply
flow remain; writing proposals render in the shared typed transcript rather
than a standalone assistant surface.

**Off by default.** Enable under **Settings → Writing Assistant** (or Plugins).

> Not the same as **AI Assistant** (`silt-ai-qa`), which is vault Q&A / search.
> Writing Assistant transforms and proposes edits; Q&A answers questions with
> citations.

## What it does

| Action | How to use | Result |
|---|---|---|
| **Draft / Expand** | Sidebar description or selection | Markdown draft/outline |
| **Rewrite succinct** | Selection or note | Condensed note/bullets |
| **Improve clarity** | Selection or note | Clearer prose, same meaning |
| **Extract action items** | Note/selection | Proposed GFM `- [ ]` tasks |
| **Suggest tags** | Note | Tags from your vocabulary |
| **Suggest related notes** | Note/selection | Ranked `((uuid))` link proposals |

## Setup

1. **Settings → AI Provider**
   - Configure a **chat** model (local Ollama or OpenAI-compatible).
   - For related-note suggestions, also configure an **embedding** model.
   - See [BRING_YOUR_OWN_MODEL.md](../BRING_YOUR_OWN_MODEL.md).
2. **Settings → Writing Assistant**
   - Enable the plugin.
   - Toggle individual actions.
   - Optionally set tag constraints and advanced prompt overrides.
3. Open **Silt AI** from the title bar, or run a slash command in the editor
   (`/Draft`, `/Rewrite succinct`, …).

## Accept / reject model

1. Run an action (sidebar **Run** or slash command).
2. Review the streamed or structured proposal.
3. **Accept** applies the change (markdown stays source of truth). **Discard**
   drops it — nothing is written.

### In-editor proposed edits (selection range)

When a run targets an **editor selection** on the focused page, the Writing
Assistant can preview the replacement **in the editor** (struck original range +
ghost proposed text) before Accept. **Accept** (or Ctrl/Cmd+Enter) applies one
ProseMirror replace transaction — a single undo step — then the normal autosave
path writes to disk. **Reject** / **Escape** clears the preview only (no
`docChanged`, no disk write). If the page is not focused, or the proposal is not
a selection replace, the unified drawer preview + SDK apply path remains.

**Multi-block proposals.** When the selection spans multiple blocks and the AI
returns multi-paragraph markdown, Accept creates one note block per paragraph —
paragraph structure is preserved instead of being flattened to a single line.
Single-paragraph proposals, or proposals targeting a within-block selection,
still use the inline replace path (flattened). If the target context can't
accept block nodes (e.g. inside a table cell), the in-editor preview is not
shown and the unified drawer preview + SDK apply path handles the proposal instead — no
proposal is ever silently dropped.

## Unified AI chat

Writing proposals render in the shared right-side Silt AI drawer alongside
agent tool activity and Q&A evidence. There is one AI drawer, so the note pane
is never squeezed by competing AI surfaces.

**Escape** discards any in-flight proposal, then closes the Silt AI drawer.

## Slash commands

When the plugin is enabled, slash commands appear for each enabled action.
They operate on the current selection when present, otherwise the active note.

## Privacy

- Note content is sent to the **configured chat endpoint** when you run a
  writing/extract/tag action.
- Related-note ranking sends text to the **configured embedding endpoint**.
- **Local** endpoints keep data on-device; **cloud** endpoints process content
  under that provider’s policy.
- No autonomous background generation.

## Troubleshooting

| Symptom | Fix |
|---|---|
| No writing action in Silt AI | Enable Writing Assistant in settings |
| “Chat model not configured” | Settings → AI Provider → set chat model |
| Related notes empty / blocked | Set embedding model; ensure other notes exist |
| Action missing from slash menu | Enable that action under Writing Assistant settings |
| Large note truncated | Raise “Max input characters” or select a smaller range |

## Related

- Spike decision note: [silt-ai-assistant-spike.md](./silt-ai-assistant-spike.md)
- Plugin author guide: [PLUGIN_DEVELOPMENT.md](../PLUGIN_DEVELOPMENT.md) (§ AI)
- Q&A / search: [silt-ai-qa.md](./silt-ai-qa.md)
- Issues: #229–#233
