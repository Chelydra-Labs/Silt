# Local MCP + Agent Skill (#687)

Silt can expose the **active vault** to desktop AI agents through the
[Model Context Protocol](https://modelcontextprotocol.io/). The host runs
**inside the Silt process** (Go). Agents never talk to SQLite or the filesystem
directly — tools call the same content paths as the app UI.

**Default: off.** Enable under **Settings → AI → Local MCP**.

## Security model

| Rule | Behavior |
|------|----------|
| Bind | `127.0.0.1` only (loopback). Never non-loopback. |
| Auth | Bearer token in OS keyring (`Silt` / `mcp-local-auth-token`). |
| HTTP | Origin allowlist (localhost / empty); `Content-Type: application/json` on POST. |
| Stdio | `silt mcp` logs to **stderr only**; stdout is JSON-RPC only. |
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
the keyring token. Requires Silt open with Local MCP enabled.

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

## Client install

### OpenCode

Copy the skill:

```text
integrations/silt-agent/SKILL.md  →  ~/.config/opencode/skills/silt/SKILL.md
```

Sample `opencode.json` snippet:

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

### Claude Desktop (MCPB)

Minimal binary manifest: `integrations/claude-desktop/manifest.json`.

1. Install Silt on `PATH` as `silt`.
2. Enable Local MCP in Settings; open a vault.
3. Register the MCP server command: `silt mcp`.
4. Install the skill from `integrations/silt-agent/SKILL.md` into your user Skills folder.

### ChatGPT Desktop / Codex

1. Enable Local MCP + (optional) write grant in Silt.
2. Add an MCP server entry with command `silt mcp` (stdio), or HTTP URL + bearer token.
3. Install `integrations/silt-agent/SKILL.md` beside other agent skills if the client supports portable skills.
4. Smoke: list tools → `search_blocks` with a known phrase → confirm write tools denied until grant is on.

## Skill

Portable agent instructions: [`integrations/silt-agent/SKILL.md`](../integrations/silt-agent/SKILL.md).

- Search first, cite notebook/section/page, confirm before edits, no credentials in chat.
