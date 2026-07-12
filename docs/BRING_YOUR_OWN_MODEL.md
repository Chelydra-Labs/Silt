# Bring Your Own Model

Silt's plugin system can call AI models for chat completions and embeddings —
but Silt itself ships **no model and makes no cloud calls of its own**. You
point it at a model server you run (local) or an API you have a key for
(cloud), and plugins that declare the `ai` capability route through that
endpoint. This guide covers the common setups.

> **Where to configure:** Settings → **AI Provider** (the tab with the
> `smart_toy` icon).

---

## What gets configured

Two independent providers, each configurable separately:

| Provider | Powers | Default |
| :--- | :--- | :--- |
| **Chat** | `ctx.ai.complete()` — plugin features that generate text (summaries, rewrites, Q&A) | Local (`http://localhost:11434`) |
| **Embedding** | `ctx.ai.embed()` — plugin features that compute vector representations (semantic search, dedup, RAG). Used by **AI Search** (`silt-ai-qa`; see `docs/plugins/silt-ai-qa.md`) | Local (`http://localhost:11434`) |

You can mix freely: a local chat model with a cloud embedding API, or both
cloud, or both local. Plugins specify *which* of the two they're calling; they
never see the endpoint URL, model name, or API key.

---

## Local setup (Ollama)

[Ollama](https://ollama.com) is the easiest local option. Install it, then
pull the models you want:

```bash
# A general-purpose chat model (confirm exact tags with `ollama list`):
ollama pull qwen3:30b-a3b

# An embedding model:
ollama pull nomic-embed-text
```

In Silt's AI Provider tab, set:

- **Chat** → Provider: *Local* · Base URL: `http://localhost:11434` · Model: `qwen3:30b-a3b`
- **Embedding** → Provider: *Local* · Base URL: `http://localhost:11434` · Model: `nomic-embed-text`

> **Other model options** (confirm the exact tag with `ollama show` or the
> provider's catalogue): for chat, `qwen3:30b-a3b` is a good starting point;
> for embeddings, `nomic-embed-text`, `bge-m3`, `snowflake-arctic-embed2`,
> `embeddinggemma`, and `qwen3-embedding` are all viable — they differ in
> dimensionality and language coverage. Set the **Dimensions** field in the
> Embedding card's Advanced section to match the model's output width.

No API key is needed for local servers. Hit **Test connection** on each card
to confirm Silt can reach Ollama.

> **Ollama's OpenAI-compatible endpoints.** Ollama exposes
> `/v1/chat/completions` and `/v1/embeddings`, which is the shape Silt sends.
> The base URL is the server root (`http://localhost:11434`), not the full
> endpoint path — Silt appends the route.

### Other local servers

Any server that speaks the OpenAI completions API works with the *Local*
provider type:

- **LM Studio** — start its local server, note the port (default
  `http://localhost:1234/v1`).
- **llama.cpp server** — point at its bind address.

Set the Base URL to the server root. If the server expects a dummy auth
header, leave the key field empty — the *Local* type sends no `Authorization`
header unless a key is set.

---

## Cloud setup (OpenAI-compatible)

Switch a provider card to **OpenAI-compatible** for any hosted endpoint that
accepts the OpenAI chat/embeddings request shape.

| Provider | Base URL | Notes |
| :--- | :--- | :--- |
| OpenAI | `https://api.openai.com/v1` | API key from platform.openai.com |
| OpenRouter | `https://openrouter.ai/api/v1` | One key, access many models |
| Together / Groq / Fireworks | their `/v1` endpoint | Each has its own key |

Enter the **API key** in the card's key field and click **Save key**. The key
is stored in your OS keyring (see [Key security](#key-security) below), not in
the vault's config file.

### Why "OpenAI-compatible" and not separate provider types?

Most cloud LLM providers (OpenRouter, Together, Groq, Azure OpenAI, LM Studio's
remote mode) now expose the same request/response shape as OpenAI's API. One
provider type with a configurable Base URL covers all of them without per-vendor
code. If a provider diverges from the OpenAI shape, it won't work yet — file an
issue and we'll evaluate a dedicated adapter.

---

## Key security

By default, Silt stores API keys in the **operating-system credential store**,
not in the vault's `config.yaml`:

| OS | Store |
| :--- | :--- |
| Windows | Credential Manager |
| macOS | Keychain |
| Linux | Secret Service (GNOME Keyring / KDE Wallet) via D-Bus |

This means a vault that syncs or gets backed up **does not carry your API
keys** to other machines or cloud storage. The keys are scoped to the vault
path on this machine.

**The tradeoff:** if you move a vault to a new machine (or a fresh OS install),
you'll need to re-enter each key once. This is intentional — your cloud API
key should not silently travel with a synced notes folder.

### When the keyring is unavailable

On some systems no OS keyring is reachable — common cases:

- **Headless Linux** without D-Bus or a Secret Service provider running.
- **WSL2** without a keyring agent bridged from Windows.
- A locked GNOME session at the moment of access.

When this happens, Silt falls back to storing keys in plaintext `config.yaml`
and surfaces a warning in the AI Provider tab ("The keyring was unreachable;
this key was saved to config.yaml instead"). You can also disable keyring
storage entirely from the tab if you prefer plaintext (e.g. for an automated
environment with no keyring daemon).

---

## Embedding dimensions

Embedding models produce vectors of a fixed size (the "dimensions"). Silt
reads the dimension from the model's first response, so you usually don't need
to set it. If you're pre-creating a `vec0` index in a plugin and need to match
the model's output, set the **dimensions** field in the Embedding card's
Advanced section (e.g. `nomic-embed-text` → 768, OpenAI `text-embedding-3-small`
→ 1532).

---

## Connection testing

Each provider card has a **Test connection** button. It sends a minimal probe
(chat: a 1-token completion; embedding: a single short embed) and reports
success or the error message from the server. Use it after changing any field
to catch typos in the base URL, an unreachable server, an invalid key, or a
wrong model name.

## Reasoning effort (chat only)

The chat provider card's **Advanced** section has a **Reasoning effort**
dropdown that controls how much internal reasoning a model does before
answering. This maps to the OpenAI-compatible `reasoning_effort` request field,
supported by OpenAI (o-series, GPT-5+), Ollama (Qwen3, DeepSeek-R1, GLM),
OpenRouter, vLLM, and other compatible servers:

| Value | Behavior |
| :--- | :--- |
| Default | Omit the field — use the model's built-in default |
| None | Thinking OFF — fastest, no reasoning tokens |
| Minimal | Lowest reasoning (GPT-5 era) |
| Low | Minimal reasoning for straightforward tasks |
| Medium | Balanced (default for most models) |
| High | Maximum reasoning for complex problems |
| xHigh | Extra high — best results, slowest (GPT-5.1+, GLM-5.2+) |
| Max | Absolute maximum (GLM-5.2, GPT-5.6, Ollama) |

**Default** (field omitted) is right for most setups. Use **None** with a
reasoning-capable model when you want a fast, direct answer without a thinking
phase. Not all providers recognize every value — if a provider rejects one,
Test connection will surface the error.

---

## Troubleshooting

**"Connection refused" on Local** — Ollama (or your local server) isn't
running, or it's on a different port. Check `ollama serve` is active and the
Base URL matches.

**"401 Unauthorized" on cloud** — the API key is wrong, expired, or lacks
credits. Re-enter it via the card's key field.

**"model not found"** — the model name doesn't match what the server has
loaded. For Ollama, `ollama list` shows installed models; for cloud, check the
provider's model catalogue for the exact ID (e.g. `gpt-4o`, not `GPT-4o`).

**Keyring warning won't go away** — on Linux, ensure a Secret Service provider
is running (`gnome-keyring-daemon` or `kwalletd`). On WSL2, there is no native
Secret Service; either disable keyring storage in the tab or bridge a Windows
credential manager.

**Plugins can't use AI even though config looks right** — the plugin must
declare the `ai` capability in its manifest, and you must grant it on first
use (Settings → Plugins). See `docs/PLUGIN_DEVELOPMENT.md` §8.14.
