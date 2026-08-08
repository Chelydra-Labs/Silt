// Reactive controller for Settings → AI → Local MCP. Owns IPC and state so
// AIProviderTab stays layout-only (same pattern as aiProviderController).
import { SvelteDate } from 'svelte/reactivity'
import {
  ClearMCPAudit,
  GetCloseToTray,
  GetLocalMCPConfig,
  GetLocalMCPInstallHint,
  GetLocalMCPStatus,
  GetLocalMCPToken,
  GetMCPAudit,
  SetCloseToTray,
  SetLocalMCPConfig
} from '../../../../bindings/silt/app.js' // settings/ai → frontend root

export type LocalMCPStatus = {
  state?: string
  message?: string
  endpoint?: string
  write_enabled?: boolean
}

export type LocalMCPInstallNote = { title: string; body: string }

export type MCPAuditEntry = {
  ts?: string
  client?: string
  tool?: string
  vault?: string
  outcome?: string
  error?: string
  args?: Record<string, unknown>
}

export type MCPAuditLoadState = 'idle' | 'loading' | 'ready' | 'error'

const MCP_OUTCOMES = [
  'ok',
  'error',
  'denied',
  'rejected',
  'rejected_schema'
] as const

export function createLocalMcpController() {
  let enabled = $state(false)
  let write = $state(false)
  let http = $state(true)
  let port = $state(17887)
  let status = $state<LocalMCPStatus | null>(null)
  let saving = $state(false)
  let error = $state('')
  let hint = $state('')
  let tokenVisible = $state(false)
  let token = $state('')
  let tokenCopied = $state(false)
  let trayPrompt = $state(false)
  let tokenClearTimer: ReturnType<typeof setTimeout> | null = null
  let tokenCopiedTimer: ReturnType<typeof setTimeout> | null = null
  // True after a successful Copy token until we best-effort clear the clipboard.
  let clipboardHoldsToken = false

  // MCP activity viewer (#886) — lazy-load on <details> expand.
  let auditOpen = $state(false)
  let audit = $state<MCPAuditEntry[]>([])
  let auditState = $state<MCPAuditLoadState>('idle')
  let auditError = $state<string | null>(null)
  let outcomeFilter = $state('all')
  let toolQuery = $state('')

  function clearTokenFromMemory() {
    token = ''
    tokenVisible = false
    tokenCopied = false
    if (tokenClearTimer) {
      clearTimeout(tokenClearTimer)
      tokenClearTimer = null
    }
    if (tokenCopiedTimer) {
      clearTimeout(tokenCopiedTimer)
      tokenCopiedTimer = null
    }
  }

  function clearClipboardIfNeeded() {
    if (!clipboardHoldsToken) return
    clipboardHoldsToken = false
    // Best-effort: clipboard managers / history may still retain a copy.
    void navigator.clipboard?.writeText?.('').catch(() => {})
  }

  function scheduleTokenClear() {
    if (tokenClearTimer) clearTimeout(tokenClearTimer)
    tokenClearTimer = setTimeout(() => {
      clearTokenFromMemory()
      clearClipboardIfNeeded()
    }, 30_000)
  }

  async function refresh() {
    try {
      const [cfg, st, h] = await Promise.all([
        GetLocalMCPConfig(),
        GetLocalMCPStatus(),
        GetLocalMCPInstallHint()
      ])
      enabled = !!(cfg as { enabled?: boolean })?.enabled
      write = !!(cfg as { write_enabled?: boolean })?.write_enabled
      http = (cfg as { http_enabled?: boolean })?.http_enabled !== false
      const p = (cfg as { http_port?: number })?.http_port
      if (typeof p === 'number' && p > 0) port = p
      status = st
      hint = typeof h === 'string' ? h : ''
    } catch (e) {
      console.error('Local MCP status failed', e)
    }
  }

  async function save(next: {
    enabled?: boolean
    write?: boolean
    http?: boolean
    port?: number
  }) {
    if (saving) return
    saving = true
    error = ''
    const nextEnabled = next.enabled ?? enabled
    const nextWrite = next.write ?? write
    const nextHttp = next.http ?? http
    const nextPort = next.port ?? port
    try {
      if (nextEnabled && !enabled) {
        try {
          const tray = await GetCloseToTray()
          if (!tray) trayPrompt = true
        } catch {
          /* ignore */
        }
      }
      await SetLocalMCPConfig(nextEnabled, nextHttp, nextWrite, nextPort)
      enabled = nextEnabled
      write = nextWrite
      http = nextHttp
      port = nextPort
      await refresh()
    } catch (e) {
      error = 'Could not save local MCP settings.'
      console.error(e)
      await refresh()
    } finally {
      saving = false
    }
  }

  async function acceptTray() {
    try {
      await SetCloseToTray(true)
    } catch (e) {
      console.error(e)
    }
    trayPrompt = false
  }

  function dismissTrayPrompt() {
    trayPrompt = false
  }

  async function revealToken() {
    try {
      token = (await GetLocalMCPToken()) || ''
      tokenVisible = true
      scheduleTokenClear()
    } catch (e) {
      console.error(e)
    }
  }

  async function copyToken() {
    try {
      if (!token) token = (await GetLocalMCPToken()) || ''
      if (!token) return
      await navigator.clipboard.writeText(token)
      clipboardHoldsToken = true
      tokenVisible = true
      tokenCopied = true
      if (tokenCopiedTimer) clearTimeout(tokenCopiedTimer)
      tokenCopiedTimer = setTimeout(() => {
        tokenCopied = false
        tokenCopiedTimer = null
      }, 1500)
      scheduleTokenClear()
    } catch (e) {
      console.error(e)
    }
  }

  async function copyHint() {
    try {
      await navigator.clipboard.writeText(
        hint || (await GetLocalMCPInstallHint())
      )
    } catch (e) {
      console.error(e)
    }
  }

  function installNotes(): LocalMCPInstallNote[] {
    const ep = status?.endpoint || `http://127.0.0.1:${port}`
    // Command name only — users may need the full path to the Silt binary
    // when it is not on PATH (common on Windows installers).
    const bin = 'silt'
    return [
      {
        title: 'Any MCP client',
        body: `Generic MCP — same for every client.\n\n1. Enable Local MCP (vault open).\n2. Stdio (preferred):\n   command: ${bin}\n   args: ["mcp"]\n   (Use the full path to the Silt binary if \`${bin}\` is not on PATH.)\n3. HTTP clients: ${ep}\n   Authorization: Bearer <token from Show/Copy token>\n4. Optional skill: integrations/silt-agent/SKILL.md\n\nAfter toggling write tools, restart the client's MCP / stdio session so it reloads the tool list.\n\nSee docs/LOCAL_MCP.md.`
      },
      {
        title: 'OpenCode sample',
        body:
          hint ||
          JSON.stringify(
            {
              mcp: {
                silt: {
                  type: 'local',
                  command: [bin, 'mcp'],
                  enabled: true
                }
              }
            },
            null,
            2
          )
      }
    ]
  }

  function toPlainAudit(raw: unknown): MCPAuditEntry[] {
    if (!Array.isArray(raw)) return []
    return raw.map((row) => {
      const r = row as Record<string, unknown>
      return {
        ts: typeof r.ts === 'string' ? r.ts : undefined,
        client: typeof r.client === 'string' ? r.client : undefined,
        tool: typeof r.tool === 'string' ? r.tool : undefined,
        vault: typeof r.vault === 'string' ? r.vault : undefined,
        outcome: typeof r.outcome === 'string' ? r.outcome : undefined,
        error: typeof r.error === 'string' ? r.error : undefined,
        args:
          r.args && typeof r.args === 'object' && !Array.isArray(r.args)
            ? (r.args as Record<string, unknown>)
            : undefined
      }
    })
  }

  async function loadAudit() {
    auditState = 'loading'
    auditError = null
    try {
      audit = toPlainAudit(await GetMCPAudit())
      auditState = 'ready'
    } catch (e) {
      audit = []
      auditState = 'error'
      auditError = e instanceof Error ? e.message : String(e)
    }
  }

  async function clearAudit() {
    try {
      await ClearMCPAudit()
      audit = []
      auditError = null
      auditState = 'ready'
    } catch (e) {
      auditError = e instanceof Error ? e.message : String(e)
      auditState = 'error'
    }
  }

  function formatAuditTime(ts: string | undefined): string {
    if (!ts) return '—'
    const d = new SvelteDate(ts)
    if (Number.isNaN(d.getTime())) return ts
    return d.toLocaleString()
  }

  function formatArgs(args: Record<string, unknown> | undefined): string {
    if (!args || Object.keys(args).length === 0) return '—'
    try {
      return JSON.stringify(args)
    } catch {
      return '—'
    }
  }

  function filteredAudit(): MCPAuditEntry[] {
    const q = toolQuery.trim().toLowerCase()
    return audit.filter((e) => {
      if (outcomeFilter !== 'all' && (e.outcome ?? '') !== outcomeFilter) {
        return false
      }
      if (q && !(e.tool ?? '').toLowerCase().includes(q)) {
        return false
      }
      return true
    })
  }

  function auditSummary(): string {
    if (auditState === 'loading') return 'Loading…'
    if (auditState === 'error') return 'Failed to load'
    if (audit.length === 0) return 'No activity yet'
    const filtered = filteredAudit()
    if (filtered.length === audit.length) {
      return `${audit.length} call${audit.length === 1 ? '' : 's'}`
    }
    return `${filtered.length} of ${audit.length} calls`
  }

  function destroy() {
    // Closing Settings must not leave the bearer on the clipboard after Copy.
    clearTokenFromMemory()
    clearClipboardIfNeeded()
  }

  return {
    get enabled() {
      return enabled
    },
    get write() {
      return write
    },
    get http() {
      return http
    },
    get port() {
      return port
    },
    get status() {
      return status
    },
    get saving() {
      return saving
    },
    get error() {
      return error
    },
    get hint() {
      return hint
    },
    get tokenVisible() {
      return tokenVisible
    },
    get token() {
      return token
    },
    get tokenCopied() {
      return tokenCopied
    },
    get trayPrompt() {
      return trayPrompt
    },
    get auditOpen() {
      return auditOpen
    },
    set auditOpen(v: boolean) {
      auditOpen = v
    },
    get audit() {
      return audit
    },
    get auditState() {
      return auditState
    },
    get auditError() {
      return auditError
    },
    get outcomeFilter() {
      return outcomeFilter
    },
    set outcomeFilter(v: string) {
      outcomeFilter = v
    },
    get toolQuery() {
      return toolQuery
    },
    set toolQuery(v: string) {
      toolQuery = v
    },
    get outcomes() {
      return MCP_OUTCOMES
    },
    get filteredAudit() {
      return filteredAudit()
    },
    get auditSummary() {
      return auditSummary()
    },
    refresh,
    save,
    acceptTray,
    dismissTrayPrompt,
    revealToken,
    copyToken,
    copyHint,
    installNotes,
    loadAudit,
    clearAudit,
    formatAuditTime,
    formatArgs,
    destroy
  }
}

export type LocalMcpController = ReturnType<typeof createLocalMcpController>
