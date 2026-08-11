---
id: byo-model
title: Bring your own model
---

## Silt does not ship a model

Silt makes no cloud AI calls of its own. You point it at a local server
(Ollama, LM Studio, etc.) or an OpenAI-compatible API you have a key for.

## Where to configure

**Settings → AI** (smart_toy icon). Configure **Chat** and **Embedding**
providers separately. You can mix local chat with a cloud embedding API.

## Local (Ollama)

1. Install Ollama and pull a chat model and an embedding model.
2. In Settings → AI, set Chat and Embedding to **Local**, base URL
   `http://localhost:11434`, and the model names you pulled.
3. Use **Test connection** on each card.

No API key is required for typical local servers.

## Cloud (OpenAI-compatible)

Switch a provider card to **OpenAI-compatible**, set the base URL, model, and
API key. Keys stay on your machine in Silt settings — plugins never see them
directly.

## What needs which model

- **Chat** — agent, Q&A answers, writing assists, summaries, extract.
- **Embedding** — semantic search, related notes, link suggestions (when
  Semantic search is on).
