# Writing Assistant (`silt-ai-assistant`)

Curated AI **writing and structuring** actions for notes — draft, rewrite,
clarify, extract tasks, suggest tags, and suggest related notes. Every result
is a **proposal** you accept or discard. The AI never writes into your vault
unsolicited.

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
3. Open the panel from the title-bar **ink pen** icon, or run a slash command
   in the editor (`/Draft`, `/Rewrite succinct`, …).

## Accept / reject model

1. Run an action (sidebar **Run** or slash command).
2. Review the streamed or structured proposal.
3. **Accept** applies the change via the plugin SDK (markdown stays source of
   truth). **Discard** drops it — nothing is written.

## Drawer

The Writing Assistant opens in a right-side drawer. It is **mutually exclusive**
with the **AI Assistant** (Q&A) drawer: opening one closes the other so they
never squeeze the note pane. Toggle either from its title-bar icon.

**Escape** discards any in-flight proposal, then closes the Writing Assistant
drawer.

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
| No title-bar ink pen icon | Enable Writing Assistant in settings |
| “Chat model not configured” | Settings → AI Provider → set chat model |
| Related notes empty / blocked | Set embedding model; ensure other notes exist |
| Action missing from slash menu | Enable that action under Writing Assistant settings |
| Large note truncated | Raise “Max input characters” or select a smaller range |

## Related

- Spike decision note: [silt-ai-assistant-spike.md](./silt-ai-assistant-spike.md)
- Plugin author guide: [PLUGIN_DEVELOPMENT.md](../PLUGIN_DEVELOPMENT.md) (§ AI)
- Q&A / search: [silt-ai-qa.md](./silt-ai-qa.md)
- Issues: #229–#233
