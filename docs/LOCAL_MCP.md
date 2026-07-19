# Local MCP + Agent Skill (#687)

Silt can expose the **active vault** to desktop AI agents through the
[Model Context Protocol](https://modelcontextprotocol.io/). The host runs
**inside the Silt process** (Go). Agents never talk to SQLite or the filesystem
directly — tools call the same content paths as the app UI.

**This is a generic MCP server.** Any MCP-capable client (OpenCode, Claude
Desktop, Codex, Cursor, etc.) uses the same transports and tools. There is no
vendor-specific packaging path (no MCPB / Desktop Extension requirement) and no
per-client protocol fork — point the client at `silt mcp` (stdio) or the
loopback HTTP endpoint shown in Settings.

**Default: off.** Enable under **Settings → AI → Local MCP**.

## Client setup

1. Enable Local MCP in Silt (vault open). Optionally enable write tools.
2. Configure the client’s MCP entry as a **local stdio** server:
   - command: `silt`
   - args: `["mcp"]`
   - Sample OpenCode config: `docs/opencode-mcp.sample.json` (Settings can copy a snippet).
3. For HTTP clients: use the endpoint from Settings plus the bearer token
   (Settings → Show / Copy token). Loopback only.
4. Optional: install the portable Skill at `integrations/silt-agent/SKILL.md`
   into the client’s skills folder for search-first / confirm-before-edit
   guidance. The Skill is workflow text only — it does not replace MCP auth.

## Security model

| Rule | Behavior |
|------|----------|
| Bind | `127.0.0.1` only (loopback). Never non-loopback. |
| Auth | Bearer token in OS keyring (`Silt` / `mcp-local-auth-token`). |
| HTTP | Origin allowlist (localhost / empty); **required** `Content-Type: application/json` on POST (empty rejected). |
| Stdio | `silt mcp` logs to **stderr only**; stdout is JSON-RPC only. |
| Discovery | `silt mcp` prefers the OS-keyring-pinned endpoint (`Silt` / `mcp-local-endpoint`), then `mcp-endpoint.json` only when it matches the pin (or the pin is unavailable), then port 17887. The file alone cannot redirect the bearer. The endpoint file stores `{endpoint,pid}` under a cross-process lock; a second Silt instance will not clear or overwrite a live peer’s discovery record (or its keyring pin). Ownership requires a live foreign PID **and** a successful `GET /health` (`silt-mcp`) so a crashed instance whose PID was reused by an unrelated process can be reclaimed automatically. Multi-instance desktop use is still unsupported as a product mode — the pin remains single-slot — but bind failure must not break the first instance’s `silt mcp` discovery. |
| Health | `GET /health` is **intentionally unauthenticated** on loopback so `silt mcp` can discover a running host without the bearer token. It returns only a short presence/version string — no vault paths, tools, or secrets. Any local process can probe it; treat multi-tenant shared machines accordingly. |
| Writes | Opt-in grant (`write_enabled`). No delete/move/bulk tools. |
| Audit | `<vault>/.system/logs/mcp-audit.jsonl` — client, tool, vault path hash, outcome, redacted args (no note bodies). |

## Lifecycle

1. Enable Local MCP while a vault is open → host starts (HTTP on port **17887** by default).
2. Vault close / switch → host stops and restarts for the new vault (or stays stopped).
3. **Close to tray** keeps the process (and MCP) alive.
4. **Quit** → `ServiceShutdown` stops MCP.

When enabling, Silt may prompt to turn on Close to tray (you can decline).

## Transports

### Stdio (recommended for clients)

```bash
silt mcp
```

Proxies stdio JSON-RPC to the running instance’s loopback HTTP endpoint using
the keyring token. Requires Silt open with Local MCP enabled. Non-default ports
are discovered via the keyring-pinned endpoint (and a matching endpoint file),
or pass `--url`.

### Streamable HTTP

- URL: `http://127.0.0.1:17887` (or the endpoint shown in Settings)
- Header: `Authorization: Bearer <token>` (Settings → Show auth token)

## Tools

| Tool | Mode | Notes |
|------|------|--------|
| `search_blocks` / `search_notes` | read | FTS, bounded page size |
| `read_page` / `read_blocks` | read | Structured blocks or markdown |
| `list_notebooks` | read | Navigation tree |
| `create_page` | write grant | Empty page |
| `update_blocks` | write grant | Identity-preserving page replace |

## Config (`config.yaml`)

```yaml
ai:
  local_mcp:
    enabled: false
    http_enabled: true
    http_port: 17887
    write_enabled: false
```

## Client install (any MCP client)

Same steps for every client:

1. Install Silt on `PATH` as `silt` (or use the full path to the binary).
2. Enable Local MCP in Settings; open a vault. Optional: allow write tools.
3. Register an MCP server:
   - **Stdio (preferred):** command `silt`, args `["mcp"]`
   - **HTTP:** endpoint from Settings + `Authorization: Bearer <token>`
4. Optional Skill: copy `integrations/silt-agent/SKILL.md` into the client’s
   skills folder if it supports portable skills.
5. Smoke: list tools → `search_blocks` with a known phrase → confirm write tools
   are denied until the write grant is on.
6. After toggling **Allow write tools** (or any host tool surface change),
   **restart the client’s MCP / stdio session** so it reloads `tools/list`.
   A long-lived `silt mcp` proxy registers tools at connect time.

### OpenCode sample

```text
integrations/silt-agent/SKILL.md  →  ~/.config/opencode/skills/silt/SKILL.md
```

```json
{
  "mcp": {
    "silt": {
      "type": "local",
      "command": ["silt", "mcp"],
      "enabled": true
    }
  }
}
```

See also `docs/opencode-mcp.sample.json`.

## Skill

Portable agent instructions: [`integrations/silt-agent/SKILL.md`](../integrations/silt-agent/SKILL.md).

- Search first, cite notebook/section/page, confirm before edits, no credentials in chat.
