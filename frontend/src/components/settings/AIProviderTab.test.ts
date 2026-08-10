// Component-level coverage for the AI Provider settings tab.
//
// Covers: initial render of both provider cards from
// GetAIProviderConfig, provider-type switching → UpdateAIProviderConfig,
// API key save (explicit-button model + post-save field clear) and
// clear, the live-Region contract for the connection probe (success →
// role=status, failure → role=alert), the keyring toggle, the
// keyring-available and keyring-fallback UI states, and the lazy-loaded
// audit log (expand → GetAIAudit, Clear → ClearAIAudit).
//
// The wails IPC bindings are mocked wholesale via vi.hoisted state so
// the component never hits real IPC under test (per AGENTS.md — no real
// IPC in tests; mock via vi.mock + vi.hoisted).

import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor,
  within
} from '@testing-library/svelte'

// Hoisted mock state + IPC function mocks. vi.hoisted keeps these refs
// available inside the vi.mock factories (which are themselves hoisted
// above the imports). Each test mutates `configState` (or another field)
// BEFORE calling render() so the component reads the desired shape on
// its onMount reload.
const mocks = vi.hoisted(() => {
  // Default config: chat is openai-compatible with a key (so the
  // setup-nudge doesn't show and the openai-compatible radio is the
  // checked one); embedding is local-default with no key. use_keyring
  // is on and the keyring is available.
  const defaultConfig = {
    chat: {
      provider_type: 'openai-compatible',
      base_url: 'https://api.openai.com/v1',
      model: 'gpt-4o',
      has_key: true,
      temperature: 0.7,
      max_tokens: 2048,
      reasoning_effort: 'medium',
      timeout_ms: 30000
    },
    embedding: {
      provider_type: 'local',
      base_url: 'http://localhost:11434',
      model: 'nomic-embed-text',
      has_key: false,
      // temperature/max_tokens are undefined — embeddings don't use them.
      timeout_ms: 60000,
      dimensions: 768
    },
    use_keyring: true,
    keyring_available: true,
    keyring_unusable_for: [] as string[],
    features: {
      enabled: false,
      rag_enabled: false,
      summaries_enabled: false,
      agent_writes: 'confirm'
    }
  }
  return {
    configState: structuredClone(defaultConfig),
    resetConfig() {
      this.configState = structuredClone(defaultConfig)
    },
    auditState: [
      {
        plugin: 'summarizer',
        kind: 'chat',
        host: 'api.openai.com',
        model: 'gpt-4o',
        status: 'ok',
        at: '2026-01-01T00:00:00Z',
        prompt_tokens: 120,
        completion_tokens: 80,
        total_tokens: 200
      },
      {
        plugin: 'search-index',
        kind: 'embed',
        host: 'localhost:11434',
        model: 'nomic-embed-text',
        status: 'error',
        at: '2026-01-02T00:00:00Z'
      }
    ],
    GetAIProviderConfig: vi.fn(),
    UpdateAIProviderConfig: vi.fn(),
    UpdateAIFeatures: vi.fn(),
    SetAIAPIKey: vi.fn(),
    CopyAIAPIKey: vi.fn(),
    ClearAIAPIKey: vi.fn(),
    SetUseKeyring: vi.fn(),
    TestAIConnection: vi.fn(),
    ListModels: vi.fn(),
    GetAIAudit: vi.fn(),
    ClearAIAudit: vi.fn(),
    // Used by stale-index detection via settings/store.updatePluginSetting.
    UpdatePluginSetting: vi.fn(),
    // Local MCP (#687)
    GetCloseToTray: vi.fn().mockResolvedValue(false),
    SetCloseToTray: vi.fn().mockResolvedValue(undefined),
    GetLocalMCPConfig: vi.fn().mockResolvedValue({
      enabled: false,
      http_enabled: true,
      http_port: 17887,
      write_enabled: false
    }),
    GetLocalMCPStatus: vi.fn().mockResolvedValue({
      state: 'disabled',
      message: '',
      endpoint: '',
      write_enabled: false
    }),
    GetLocalMCPInstallHint: vi.fn().mockResolvedValue('# sample'),
    GetLocalMCPToken: vi.fn().mockResolvedValue(''),
    SetLocalMCPConfig: vi.fn().mockResolvedValue(undefined),
    // MCP activity viewer (#886)
    mcpAuditState: [] as Array<Record<string, unknown>>,
    GetMCPAudit: vi.fn(),
    ClearMCPAudit: vi.fn()
  }
})

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    GetAIProviderConfig: mocks.GetAIProviderConfig,
    UpdateAIProviderConfig: mocks.UpdateAIProviderConfig,
    UpdateAIFeatures: mocks.UpdateAIFeatures,
    SetAIAPIKey: mocks.SetAIAPIKey,
    CopyAIAPIKey: mocks.CopyAIAPIKey,
    ClearAIAPIKey: mocks.ClearAIAPIKey,
    SetUseKeyring: mocks.SetUseKeyring,
    TestAIConnection: mocks.TestAIConnection,
    ListModels: mocks.ListModels,
    GetAIAudit: mocks.GetAIAudit,
    ClearAIAudit: mocks.ClearAIAudit,
    UpdatePluginSetting: mocks.UpdatePluginSetting,
    GetCloseToTray: mocks.GetCloseToTray,
    SetCloseToTray: mocks.SetCloseToTray,
    GetLocalMCPConfig: mocks.GetLocalMCPConfig,
    GetLocalMCPStatus: mocks.GetLocalMCPStatus,
    GetLocalMCPInstallHint: mocks.GetLocalMCPInstallHint,
    GetLocalMCPToken: mocks.GetLocalMCPToken,
    SetLocalMCPConfig: mocks.SetLocalMCPConfig,
    GetMCPAudit: mocks.GetMCPAudit,
    ClearMCPAudit: mocks.ClearMCPAudit
  })
)

vi.mock('../../settings/store.svelte', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('../../settings/store.svelte')>()
  return {
    ...actual,
    loadConfig: vi.fn().mockResolvedValue(true),
    // Preserve the binding path so stale-index tests still see UpdatePluginSetting.
    updatePluginSetting: (...args: unknown[]) =>
      mocks.UpdatePluginSetting(...args)
  }
})

vi.mock('../../plugins/loader', () => ({
  loadPlugins: vi.fn().mockResolvedValue({ plugins: new Map(), errors: [] }),
  getSessionToken: vi.fn(() => undefined)
}))

vi.mock('../../plugins/context', () => ({
  makePluginContext: vi.fn((id: string) => ({ pluginID: id }))
}))

// Fine-tuning embeds are heavy; use lightweight stubs that still mount so
// progressive-disclosure tests can assert presence (#639).
vi.mock('../../plugins/first-party/silt-ai-qa/QASettings.svelte', async () => {
  const mod = await import('./__stubs__/QASettings.stub.svelte')
  return { default: mod.default }
})
vi.mock(
  '../../plugins/first-party/silt-ai-assistant/AssistantSettings.svelte',
  async () => {
    const mod = await import('./__stubs__/AssistantSettings.stub.svelte')
    return { default: mod.default }
  }
)
vi.mock(
  '../../plugins/first-party/silt-ai-summary/AISummarySettings.svelte',
  async () => {
    const mod = await import('./__stubs__/AISummarySettings.stub.svelte')
    return { default: mod.default }
  }
)

import AIProviderTab from './AIProviderTab.svelte'

describe('AIProviderTab', () => {
  beforeEach(() => {
    mocks.resetConfig()
    mocks.GetAIProviderConfig.mockReset()
    mocks.UpdateAIProviderConfig.mockReset()
    mocks.UpdateAIFeatures.mockReset()
    mocks.SetAIAPIKey.mockReset()
    mocks.CopyAIAPIKey.mockReset()
    mocks.ClearAIAPIKey.mockReset()
    mocks.SetUseKeyring.mockReset()
    mocks.TestAIConnection.mockReset()
    mocks.ListModels.mockReset()
    mocks.GetAIAudit.mockReset()
    mocks.ClearAIAudit.mockReset()
    mocks.UpdatePluginSetting.mockReset()
    mocks.GetMCPAudit.mockReset()
    mocks.ClearMCPAudit.mockReset()
    mocks.mcpAuditState = [
      {
        ts: '2026-01-01T12:00:00Z',
        tool: 'search_blocks',
        outcome: 'ok',
        vault: 'abc',
        args: { query_len: 3 }
      },
      {
        ts: '2026-01-01T12:01:00Z',
        tool: 'create_page',
        outcome: 'denied',
        vault: 'abc',
        error: 'write tools disabled'
      },
      {
        ts: '2026-01-01T12:02:00Z',
        tool: 'search_blocks',
        outcome: 'rejected_schema',
        vault: 'abc',
        error: 'missing required field'
      }
    ]
    mocks.GetMCPAudit.mockImplementation(async () =>
      structuredClone(mocks.mcpAuditState)
    )
    mocks.ClearMCPAudit.mockImplementation(async () => {
      mocks.mcpAuditState = []
    })
    // Default happy-path resolutions; individual tests override.
    mocks.GetAIProviderConfig.mockResolvedValue(
      structuredClone(mocks.configState)
    )
    mocks.UpdateAIProviderConfig.mockResolvedValue(undefined)
    mocks.UpdateAIFeatures.mockResolvedValue(undefined)
    mocks.SetAIAPIKey.mockResolvedValue(undefined)
    mocks.CopyAIAPIKey.mockResolvedValue(undefined)
    mocks.ClearAIAPIKey.mockResolvedValue(undefined)
    mocks.SetUseKeyring.mockResolvedValue(undefined)
    mocks.TestAIConnection.mockResolvedValue({
      ok: true,
      kind: 'chat',
      message: 'probe ok'
    })
    // ListModels returns empty by default (cold-start cache miss) → free-text
    // model input. Individual tests override to populate the dropdown.
    mocks.ListModels.mockResolvedValue([])
    mocks.GetAIAudit.mockResolvedValue(structuredClone(mocks.auditState))
    mocks.ClearAIAudit.mockResolvedValue(undefined)
    mocks.UpdatePluginSetting.mockResolvedValue(undefined)
  })

  afterEach(() => {
    cleanup()
  })

  // Helper: wait for the load effect to land so the form is interactive.
  async function ready() {
    await waitFor(() =>
      expect(
        screen.getByRole('radiogroup', { name: /chat provider type/i })
      ).toBeInTheDocument()
    )
  }

  describe('features card', () => {
    it('renders Enable AI and nested feature toggles', async () => {
      render(AIProviderTab)
      await ready()
      expect(
        screen.getByRole('checkbox', { name: /Enable AI/i })
      ).toBeInTheDocument()
      expect(
        screen.getByRole('checkbox', { name: /Semantic search/i })
      ).toBeInTheDocument()
      expect(
        screen.getByRole('checkbox', { name: /Note summaries/i })
      ).toBeInTheDocument()
      expect(
        screen.getByRole('checkbox', { name: /Semantic search/i })
      ).toBeDisabled()
    })

    it('renders Setup and Advanced segment nav (Capabilities only when AI is on)', async () => {
      render(AIProviderTab)
      await ready()
      const nav = screen.getByRole('navigation', {
        name: /AI settings sections/i
      })
      expect(nav).toBeInTheDocument()
      expect(screen.getByRole('tab', { name: /^Setup$/i })).toBeInTheDocument()
      expect(
        screen.getByRole('tab', { name: /^Advanced$/i })
      ).toBeInTheDocument()
      // Master AI off in default mock config → no Capabilities segment.
      expect(screen.queryByRole('tab', { name: /^Capabilities$/i })).toBeNull()
    })

    it('shows Capabilities segment when master AI is enabled', async () => {
      mocks.GetAIProviderConfig.mockResolvedValue({
        ...structuredClone(mocks.configState),
        features: {
          enabled: true,
          rag_enabled: false,
          summaries_enabled: false
        }
      })
      render(AIProviderTab)
      await ready()
      expect(
        screen.getByRole('tab', { name: /^Capabilities$/i })
      ).toBeInTheDocument()
    })

    it('master AI off: no Capabilities segment and no fine-tuning embeds', async () => {
      // Default mock: features.enabled=false.
      render(AIProviderTab)
      await ready()
      expect(screen.queryByRole('tab', { name: /^Capabilities$/i })).toBeNull()
      expect(document.getElementById('ai-writing-tuning')).toBeNull()
      expect(document.getElementById('ai-search-tuning')).toBeNull()
      expect(document.getElementById('ai-summary-tuning')).toBeNull()
      expect(screen.queryByTestId('assistant-settings-embed')).toBeNull()
      expect(screen.queryByTestId('qa-settings-embed')).toBeNull()
      expect(screen.queryByTestId('ai-summary-settings-embed')).toBeNull()
    })

    it('master AI on: Capabilities shows Writing Assistant embed', async () => {
      mocks.GetAIProviderConfig.mockResolvedValue({
        ...structuredClone(mocks.configState),
        features: {
          enabled: true,
          rag_enabled: false,
          summaries_enabled: false
        }
      })
      render(AIProviderTab)
      await ready()
      await fireEvent.click(
        screen.getByRole('tab', { name: /^Capabilities$/i })
      )
      expect(document.getElementById('ai-writing-tuning')).toBeInTheDocument()
      expect(screen.getByTestId('assistant-settings-embed')).toBeInTheDocument()
      expect(screen.getByText('Writing Assistant')).toBeInTheDocument()
      // Nested flags off → no Search/Summary embeds.
      expect(document.getElementById('ai-search-tuning')).toBeNull()
      expect(document.getElementById('ai-summary-tuning')).toBeNull()
      expect(screen.queryByTestId('qa-settings-embed')).toBeNull()
      expect(screen.queryByTestId('ai-summary-settings-embed')).toBeNull()
    })

    it('master + rag_enabled: Semantic search embed present', async () => {
      mocks.GetAIProviderConfig.mockResolvedValue({
        ...structuredClone(mocks.configState),
        features: {
          enabled: true,
          rag_enabled: true,
          summaries_enabled: false
        }
      })
      render(AIProviderTab)
      await ready()
      await fireEvent.click(
        screen.getByRole('tab', { name: /^Capabilities$/i })
      )
      expect(document.getElementById('ai-search-tuning')).toBeInTheDocument()
      expect(screen.getByTestId('qa-settings-embed')).toBeInTheDocument()
      expect(screen.getByTestId('assistant-settings-embed')).toBeInTheDocument()
      expect(screen.queryByTestId('ai-summary-settings-embed')).toBeNull()
    })

    it('master + summaries_enabled: Note summaries embed present', async () => {
      mocks.GetAIProviderConfig.mockResolvedValue({
        ...structuredClone(mocks.configState),
        features: {
          enabled: true,
          rag_enabled: false,
          summaries_enabled: true
        }
      })
      render(AIProviderTab)
      await ready()
      await fireEvent.click(
        screen.getByRole('tab', { name: /^Capabilities$/i })
      )
      expect(document.getElementById('ai-summary-tuning')).toBeInTheDocument()
      expect(
        screen.getByTestId('ai-summary-settings-embed')
      ).toBeInTheDocument()
      expect(screen.getByTestId('assistant-settings-embed')).toBeInTheDocument()
      expect(screen.queryByTestId('qa-settings-embed')).toBeNull()
    })

    it('switches views when Advanced is selected (hides Features, shows Advanced)', async () => {
      render(AIProviderTab)
      await ready()
      expect(
        screen.getByRole('checkbox', { name: /Enable AI/i })
      ).toBeInTheDocument()
      await fireEvent.click(screen.getByRole('tab', { name: /^Advanced$/i }))
      expect(screen.queryByRole('checkbox', { name: /Enable AI/i })).toBeNull()
      expect(screen.getByText(/Advanced Options/i)).toBeInTheDocument()
      await fireEvent.click(screen.getByRole('tab', { name: /^Setup$/i }))
      expect(
        screen.getByRole('checkbox', { name: /Enable AI/i })
      ).toBeInTheDocument()
    })

    it('Arrow/Home/End move the active segment (roving tabindex)', async () => {
      render(AIProviderTab)
      await ready()
      const nav = screen.getByRole('navigation', {
        name: /AI settings sections/i
      })
      const tablist = within(nav).getByRole('tablist')
      const setup = screen.getByRole('tab', { name: /^Setup$/i })
      const advanced = screen.getByRole('tab', { name: /^Advanced$/i })
      expect(setup.getAttribute('aria-selected')).toBe('true')
      expect(setup.getAttribute('tabindex')).toBe('0')
      setup.focus()
      await fireEvent.keyDown(tablist, { key: 'ArrowRight' })
      expect(advanced.getAttribute('aria-selected')).toBe('true')
      expect(advanced.getAttribute('tabindex')).toBe('0')
      expect(setup.getAttribute('tabindex')).toBe('-1')
      expect(screen.getByText(/Advanced Options/i)).toBeInTheDocument()
      await fireEvent.keyDown(tablist, { key: 'Home' })
      expect(setup.getAttribute('aria-selected')).toBe('true')
      expect(
        screen.getByRole('checkbox', { name: /Enable AI/i })
      ).toBeInTheDocument()
      await fireEvent.keyDown(tablist, { key: 'End' })
      expect(advanced.getAttribute('aria-selected')).toBe('true')
    })

    it('shows embedding empty-state CTA when RAG is on without an embedding model', async () => {
      mocks.GetAIProviderConfig.mockResolvedValue({
        ...structuredClone(mocks.configState),
        features: {
          enabled: true,
          rag_enabled: true,
          summaries_enabled: false
        },
        embedding: {
          provider_type: 'openai-compatible',
          base_url: '',
          model: '',
          has_key: false,
          timeout_ms: 60000
        }
      })
      render(AIProviderTab)
      await ready()
      expect(
        screen.getByText(/Semantic search needs an embedding model/i)
      ).toBeInTheDocument()
      expect(
        screen.getByRole('button', { name: /Set up embedding/i })
      ).toBeInTheDocument()
    })

    it('calls UpdateAIFeatures when Enable AI is toggled', async () => {
      render(AIProviderTab)
      await ready()
      const master = screen.getByRole('checkbox', { name: /Enable AI/i })
      await fireEvent.click(master)
      await waitFor(() => {
        expect(mocks.UpdateAIFeatures).toHaveBeenCalledWith({ enabled: true })
      })
    })

    it('enables nested Semantic search after master is on', async () => {
      mocks.GetAIProviderConfig.mockResolvedValue({
        ...structuredClone(mocks.configState),
        features: {
          enabled: true,
          rag_enabled: false,
          summaries_enabled: false,
          agent_writes: 'confirm'
        }
      })
      render(AIProviderTab)
      await ready()
      const rag = screen.getByRole('checkbox', { name: /Semantic search/i })
      expect(rag).not.toBeDisabled()
      await fireEvent.click(rag)
      await waitFor(() => {
        expect(mocks.UpdateAIFeatures).toHaveBeenCalledWith({
          rag_enabled: true
        })
      })
    })

    it('renders Agent vault writes select disabled when AI is off', async () => {
      render(AIProviderTab)
      await ready()
      const select = screen.getByLabelText(/Agent vault writes/i)
      expect(select).toBeDisabled()
      expect(select).toHaveValue('confirm')
    })

    it('calls UpdateAIFeatures when Agent vault writes changes', async () => {
      mocks.GetAIProviderConfig.mockResolvedValue({
        ...structuredClone(mocks.configState),
        features: {
          enabled: true,
          rag_enabled: false,
          summaries_enabled: false,
          agent_writes: 'confirm'
        }
      })
      render(AIProviderTab)
      await ready()
      const select = screen.getByLabelText(/Agent vault writes/i)
      expect(select).not.toBeDisabled()
      await fireEvent.change(select, { target: { value: 'read_only' } })
      await waitFor(() => {
        expect(mocks.UpdateAIFeatures).toHaveBeenCalledWith({
          agent_writes: 'read_only'
        })
      })
    })
  })

  describe('initial render', () => {
    it('renders both provider cards with the config-sourced field values', async () => {
      render(AIProviderTab)
      await ready()

      // Two radiogroups (chat + embedding provider-type selectors).
      const groups = screen.getAllByRole('radiogroup', {
        name: /provider type/i
      })
      expect(groups).toHaveLength(2)

      // Base URL inputs carry the config values.
      const chatBaseUrl = document.getElementById(
        'ai-chat-base-url'
      ) as HTMLInputElement
      const embedBaseUrl = document.getElementById(
        'ai-embedding-base-url'
      ) as HTMLInputElement
      expect(chatBaseUrl.value).toBe('https://api.openai.com/v1')
      expect(embedBaseUrl.value).toBe('http://localhost:11434')

      // Models propagate too.
      const chatModel = document.getElementById(
        'ai-chat-model'
      ) as HTMLInputElement
      const embedModel = document.getElementById(
        'ai-embedding-model'
      ) as HTMLInputElement
      expect(chatModel.value).toBe('gpt-4o')
      expect(embedModel.value).toBe('nomic-embed-text')
    })

    it('marks the active provider type radio as checked for each card', async () => {
      render(AIProviderTab)
      await ready()

      // chat is openai-compatible → that radio is checked.
      const chatLocal = screen.getAllByRole('radio', {
        name: /Local \(Ollama\)/i
      })
      const chatOpenAI = screen.getAllByRole('radio', {
        name: /OpenAI-compatible/i
      })
      // First instance is the chat card; second is the embedding card.
      expect(chatOpenAI[0]).toHaveAttribute('aria-checked', 'true')
      expect(chatLocal[0]).toHaveAttribute('aria-checked', 'false')
      // Embedding is local → its local radio is checked.
      expect(chatLocal[1]).toHaveAttribute('aria-checked', 'true')
      expect(chatOpenAI[1]).toHaveAttribute('aria-checked', 'false')
    })

    it('hides the setup nudge when a local chat model is set (Ollama runs keyless)', async () => {
      // #450: the nudge now follows the unified aiProviderNeedsSetup predicate.
      // A LOCAL provider with a model chosen is ready to serve completions —
      // Ollama/llama.cpp need no key — so the nudge must not show even with
      // has_key=false. (The legacy "both providers untouched" heuristic would
      // have shown it here, which is exactly the incoherence #450 fixes.)
      mocks.configState = {
        chat: {
          provider_type: 'local',
          base_url: 'http://localhost:11434',
          model: 'llama3.1',
          has_key: false,
          temperature: 0.7,
          max_tokens: 2048,
          reasoning_effort: 'medium',
          timeout_ms: 30000
        },
        embedding: {
          provider_type: 'local',
          base_url: 'http://localhost:11434',
          model: 'nomic-embed-text',
          has_key: false,
          timeout_ms: 60000,
          dimensions: 768
        },
        use_keyring: true,
        keyring_available: true,
        keyring_unusable_for: [],
        features: {
          enabled: false,
          rag_enabled: false,
          summaries_enabled: false
        }
      }
      mocks.GetAIProviderConfig.mockResolvedValue(
        structuredClone(mocks.configState)
      )
      render(AIProviderTab)
      await ready()

      expect(screen.queryByText(/Set up an AI provider/i)).toBeNull()
    })

    it('shows the setup nudge when no chat model is configured', async () => {
      // The unified predicate's primary trigger: no chat model ⇒ not ready,
      // regardless of provider type or key. This is the case the Plugins-tab
      // badge and this nudge must agree on (click-the-badge-lands-on-a-nudge).
      mocks.configState = {
        chat: {
          provider_type: 'openai-compatible',
          base_url: 'https://openrouter.ai/api/v1',
          model: '',
          has_key: true,
          temperature: 0.7,
          max_tokens: 2048,
          reasoning_effort: 'medium',
          timeout_ms: 30000
        },
        embedding: {
          provider_type: 'local',
          base_url: 'http://localhost:11434',
          model: 'nomic-embed-text',
          has_key: false,
          timeout_ms: 60000,
          dimensions: 768
        },
        use_keyring: true,
        keyring_available: true,
        keyring_unusable_for: [],
        features: {
          enabled: false,
          rag_enabled: false,
          summaries_enabled: false
        }
      }
      mocks.GetAIProviderConfig.mockResolvedValue(
        structuredClone(mocks.configState)
      )
      render(AIProviderTab)
      await ready()

      expect(screen.getByText(/Set up an AI provider/i)).toBeInTheDocument()
    })

    it('shows the setup nudge for an openai-compatible model with no key', async () => {
      // The key-aware branch: a cloud provider needs a key. The local-only
      // Plugins-tab badge cannot see this (keys are scrubbed from SystemConfig),
      // but this tab can, so it nudges.
      mocks.configState = {
        chat: {
          provider_type: 'openai-compatible',
          base_url: 'https://openrouter.ai/api/v1',
          model: 'gpt-4o',
          has_key: false,
          temperature: 0.7,
          max_tokens: 2048,
          reasoning_effort: 'medium',
          timeout_ms: 30000
        },
        embedding: {
          provider_type: 'local',
          base_url: 'http://localhost:11434',
          model: 'nomic-embed-text',
          has_key: false,
          timeout_ms: 60000,
          dimensions: 768
        },
        use_keyring: true,
        keyring_available: true,
        keyring_unusable_for: [],
        features: {
          enabled: false,
          rag_enabled: false,
          summaries_enabled: false
        }
      }
      mocks.GetAIProviderConfig.mockResolvedValue(
        structuredClone(mocks.configState)
      )
      render(AIProviderTab)
      await ready()

      expect(screen.getByText(/Set up an AI provider/i)).toBeInTheDocument()
    })

    it('hides the setup nudge once a provider has a key', async () => {
      // The default mock config has chat.has_key=true, so no nudge.
      render(AIProviderTab)
      await ready()
      expect(screen.queryByText(/Set up an AI provider/i)).toBeNull()
    })
  })

  describe('provider type switching', () => {
    it('switches embedding from local to openai-compatible and persists the patch', async () => {
      render(AIProviderTab)
      await ready()

      // The embedding card's OpenAI-compatible radio is the second match.
      const openAIRadios = screen.getAllByRole('radio', {
        name: /OpenAI-compatible/i
      })
      await fireEvent.click(openAIRadios[1])

      expect(mocks.UpdateAIProviderConfig).toHaveBeenCalledWith(
        'embedding',
        expect.objectContaining({
          provider_type: 'openai-compatible',
          base_url: 'https://api.openai.com/v1'
        })
      )
    })

    it('snaps base_url to the local default when switching to local', async () => {
      render(AIProviderTab)
      await ready()

      // Switch chat (currently openai-compatible) to local.
      const localRadios = screen.getAllByRole('radio', {
        name: /Local \(Ollama\)/i
      })
      await fireEvent.click(localRadios[0])

      expect(mocks.UpdateAIProviderConfig).toHaveBeenCalledWith(
        'chat',
        expect.objectContaining({
          provider_type: 'local',
          base_url: 'http://localhost:11434'
        })
      )
    })

    it('shows the local privacy reassurance for a local provider', async () => {
      render(AIProviderTab)
      await ready()

      // The default config has embedding = local; the reassurance must be present.
      expect(screen.getByText(/doesn't leave this device/i)).toBeInTheDocument()
    })

    it('shows the cloud privacy warning for an openai-compatible provider', async () => {
      // Chat defaults to openai-compatible in the mock config.
      render(AIProviderTab)
      await ready()

      expect(screen.getByText(/leaves your machine/i)).toBeInTheDocument()
    })
  })

  describe('native provider options', () => {
    it('renders all four provider type radios', async () => {
      render(AIProviderTab)
      await ready()

      expect(
        screen.getAllByRole('radio', { name: /Local \(Ollama\)/i })
      ).toHaveLength(2)
      expect(
        screen.getAllByRole('radio', { name: /OpenAI-compatible/i })
      ).toHaveLength(2)
      expect(screen.getAllByRole('radio', { name: /Google AI/i })).toHaveLength(
        2
      )
      expect(screen.getAllByRole('radio', { name: /Anthropic/i })).toHaveLength(
        2
      )
    })

    it('switches chat to Google AI and snaps the base URL', async () => {
      render(AIProviderTab)
      await ready()

      const googleRadios = screen.getAllByRole('radio', { name: /Google AI/i })
      await fireEvent.click(googleRadios[0]) // chat card

      expect(mocks.UpdateAIProviderConfig).toHaveBeenCalledWith(
        'chat',
        expect.objectContaining({
          provider_type: 'google',
          base_url: 'https://generativelanguage.googleapis.com'
        })
      )
    })

    it('persists the provider config before refreshing models on switch', async () => {
      // Wails IPC does not guarantee ordering between independent fire-and-forget
      // calls. If the refresh fires before the save lands, ListModels snapshots
      // the OLD provider config and polls the wrong endpoint. This test verifies
      // the save completes before the force-poll fires.
      let releaseSave!: () => void
      mocks.UpdateAIProviderConfig.mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            releaseSave = () => resolve()
          })
      )
      render(AIProviderTab)
      await ready()

      const googleRadios = screen.getAllByRole('radio', { name: /Google AI/i })
      await fireEvent.click(googleRadios[0])

      // Save is called immediately...
      await waitFor(() =>
        expect(mocks.UpdateAIProviderConfig).toHaveBeenCalledWith(
          'chat',
          expect.anything()
        )
      )
      // ...but ListModels(force=true) must NOT have fired yet (save is pending).
      expect(mocks.ListModels).not.toHaveBeenCalledWith('chat', true)

      // Release the save — the force-poll should fire now.
      releaseSave()
      await waitFor(() =>
        expect(mocks.ListModels).toHaveBeenCalledWith('chat', true)
      )
    })

    it('does not force-refresh models when persisting the new provider fails', async () => {
      // A failed save (IPC rejection, or invalid advanced settings) leaves the
      // backend on the OLD provider config; a forced ListModels would poll the
      // wrong endpoint and show stale models under the new type label. The
      // refresh must be gated on a successful persist and the failure surfaced.
      mocks.UpdateAIProviderConfig.mockRejectedValueOnce(
        new Error('save failed')
      )
      render(AIProviderTab)
      await ready()

      const googleRadios = screen.getAllByRole('radio', { name: /Google AI/i })
      await fireEvent.click(googleRadios[0])

      // The provider type flips locally and the save is attempted...
      await waitFor(() =>
        expect(mocks.UpdateAIProviderConfig).toHaveBeenCalledWith(
          'chat',
          expect.objectContaining({ provider_type: 'google' })
        )
      )
      // ...but the forced model refresh must NOT fire against stale backend config.
      expect(mocks.ListModels).not.toHaveBeenCalledWith('chat', true)
      // The save failure surfaces on the model error channel.
      await waitFor(() =>
        expect(
          screen.getByText(/failed to save provider settings/i)
        ).toBeInTheDocument()
      )
    })

    it('switches chat to Anthropic and snaps the base URL', async () => {
      render(AIProviderTab)
      await ready()

      const anthropicRadios = screen.getAllByRole('radio', {
        name: /Anthropic/i
      })
      await fireEvent.click(anthropicRadios[0]) // chat card

      expect(mocks.UpdateAIProviderConfig).toHaveBeenCalledWith(
        'chat',
        expect.objectContaining({
          provider_type: 'anthropic',
          base_url: 'https://api.anthropic.com'
        })
      )
    })

    it('snaps a custom OpenAI-compat base URL to the Google default on switch (regression)', async () => {
      // Footgun: Google's OpenAI-compatible shim URL (…/v1beta/openai/) pasted
      // under openai-compatible must NOT survive a switch to native Google — the
      // native paths would double up (/v1beta/openai//v1beta/models) and 404.
      mocks.configState.chat.provider_type = 'openai-compatible'
      mocks.configState.chat.base_url =
        'https://generativelanguage.googleapis.com/v1beta/openai/'
      mocks.GetAIProviderConfig.mockResolvedValue(
        structuredClone(mocks.configState)
      )
      render(AIProviderTab)
      await ready()

      const googleRadios = screen.getAllByRole('radio', { name: /Google AI/i })
      await fireEvent.click(googleRadios[0])

      expect(mocks.UpdateAIProviderConfig).toHaveBeenCalledWith(
        'chat',
        expect.objectContaining({
          provider_type: 'google',
          base_url: 'https://generativelanguage.googleapis.com'
        })
      )
    })

    it('snaps an arbitrary custom base URL to the Anthropic default on switch', async () => {
      mocks.configState.chat.provider_type = 'openai-compatible'
      mocks.configState.chat.base_url =
        'https://my-custom-gateway.example.com/v1'
      mocks.GetAIProviderConfig.mockResolvedValue(
        structuredClone(mocks.configState)
      )
      render(AIProviderTab)
      await ready()

      const anthropicRadios = screen.getAllByRole('radio', {
        name: /Anthropic/i
      })
      await fireEvent.click(anthropicRadios[0])

      expect(mocks.UpdateAIProviderConfig).toHaveBeenCalledWith(
        'chat',
        expect.objectContaining({
          provider_type: 'anthropic',
          base_url: 'https://api.anthropic.com'
        })
      )
    })

    it('preserves a custom base URL when switching between local and openai-compatible', async () => {
      // Non-native targets accept arbitrary endpoints, so a custom gateway URL
      // must survive a local↔openai-compatible switch (only native targets
      // force-snap). Guards that the native-snap fix is surgical.
      mocks.configState.chat.provider_type = 'openai-compatible'
      mocks.configState.chat.base_url =
        'https://my-custom-gateway.example.com/v1'
      mocks.GetAIProviderConfig.mockResolvedValue(
        structuredClone(mocks.configState)
      )
      render(AIProviderTab)
      await ready()

      const localRadios = screen.getAllByRole('radio', {
        name: /Local \(Ollama\)/i
      })
      await fireEvent.click(localRadios[0])

      expect(mocks.UpdateAIProviderConfig).toHaveBeenCalledWith(
        'chat',
        expect.objectContaining({
          provider_type: 'local',
          base_url: 'https://my-custom-gateway.example.com/v1'
        })
      )
    })

    it('shows the cloud privacy warning for Google AI', async () => {
      mocks.configState.chat.provider_type = 'google'
      mocks.configState.chat.base_url =
        'https://generativelanguage.googleapis.com'
      mocks.GetAIProviderConfig.mockResolvedValue(
        structuredClone(mocks.configState)
      )
      render(AIProviderTab)
      await ready()

      expect(screen.getByText(/leaves your machine/i)).toBeInTheDocument()
    })

    it('shows the unsupported message for Anthropic embeddings', async () => {
      mocks.configState.embedding.provider_type = 'anthropic'
      mocks.configState.embedding.base_url = 'https://api.anthropic.com'
      mocks.GetAIProviderConfig.mockResolvedValue(
        structuredClone(mocks.configState)
      )
      render(AIProviderTab)
      await ready()

      expect(
        screen.getByText(/Anthropic doesn't offer embeddings/i)
      ).toBeInTheDocument()
    })
  })

  describe('model discovery dropdown', () => {
    it('shows the free-text input on cold start (no cached models)', async () => {
      // Default mock: ListModels returns [] (cold start) → free-text input.
      render(AIProviderTab)
      await ready()

      const chatModel = document.getElementById('ai-chat-model')
      expect(chatModel).toBeTruthy()
      expect(chatModel?.tagName).toBe('INPUT')
    })

    it('renders a populated dropdown after a successful Refresh poll', async () => {
      mocks.ListModels.mockResolvedValue([
        { id: 'gpt-4o', display_name: 'GPT-4o' },
        { id: 'gpt-4o-mini', display_name: 'GPT-4o Mini' }
      ])
      render(AIProviderTab)
      await ready()

      // Click the Refresh-models button in the chat card.
      const refreshBtns = screen.getAllByRole('button', {
        name: /Refresh models/i
      })
      await fireEvent.click(refreshBtns[0]) // chat card

      // After the poll resolves, the dropdown should show the models.
      await waitFor(() => {
        const chatModel = document.getElementById('ai-chat-model')
        expect(chatModel?.tagName).toBe('SELECT')
      })
      // The select should have options for both models.
      const select = document.getElementById(
        'ai-chat-model'
      ) as HTMLSelectElement
      expect(select.options.length).toBeGreaterThanOrEqual(2)
    })

    it('falls back to free-text when the poll returns empty', async () => {
      // force=true returns empty → stays in manual mode.
      mocks.ListModels.mockResolvedValue([])
      render(AIProviderTab)
      await ready()

      const refreshBtns = screen.getAllByRole('button', {
        name: /Refresh models/i
      })
      await fireEvent.click(refreshBtns[0])

      // Should still be a free-text input (no models to show).
      await waitFor(() => {
        expect(mocks.ListModels).toHaveBeenCalledWith('chat', true)
      })
      const chatModel = document.getElementById('ai-chat-model')
      expect(chatModel?.tagName).toBe('INPUT')
    })

    it('shows an error message when the poll fails', async () => {
      mocks.ListModels.mockRejectedValue(new Error('connection refused'))
      render(AIProviderTab)
      await ready()

      const refreshBtns = screen.getAllByRole('button', {
        name: /Refresh models/i
      })
      await fireEvent.click(refreshBtns[0])

      await waitFor(() => {
        expect(screen.getByText(/connection refused/i)).toBeInTheDocument()
      })
    })
  })

  describe('Advanced field validation', () => {
    it('blocks persist when temperature is out of range', async () => {
      mocks.configState.chat.temperature = 5 // invalid: max is 2
      // beforeEach captures a clone of configState before this mutation, so
      // re-seed the mock with the mutated state.
      mocks.GetAIProviderConfig.mockResolvedValue(
        structuredClone(mocks.configState)
      )
      render(AIProviderTab)
      await ready()

      // Blurring the chat base URL triggers persistProvider, which should
      // be gated by the validation check and NOT call UpdateAIProviderConfig.
      const chatBaseUrl = document.getElementById(
        'ai-chat-base-url'
      ) as HTMLInputElement
      await fireEvent.blur(chatBaseUrl)
      expect(mocks.UpdateAIProviderConfig).not.toHaveBeenCalled()
    })
  })

  describe('API key management', () => {
    it('Save calls SetAIAPIKey and clears the input field', async () => {
      render(AIProviderTab)
      await ready()

      const keyInput = document.getElementById(
        'ai-chat-key'
      ) as HTMLInputElement
      await fireEvent.input(keyInput, { target: { value: 'sk-test-123' } })
      expect(keyInput.value).toBe('sk-test-123')

      // Scope to the chat section so we resolve the chat Save button
      // (the embedding card has its own Save button, still disabled).
      const chatSection = screen
        .getByRole('radiogroup', { name: /chat provider type/i })
        .closest('section')!
      const saveBtn = within(chatSection).getByRole('button', {
        name: /Save key/i
      })
      await fireEvent.click(saveBtn)

      await waitFor(() =>
        expect(mocks.SetAIAPIKey).toHaveBeenCalledWith('chat', 'sk-test-123')
      )
      // The secret never lingers in the DOM after Save lands.
      await waitFor(() => expect(keyInput.value).toBe(''))
    })

    it('Save button stays disabled when the key input is empty', async () => {
      render(AIProviderTab)
      await ready()

      // Both Save buttons exist; both are disabled while their inputs
      // are empty.
      const saveBtns = screen.getAllByRole('button', { name: /Save key/i })
      expect(saveBtns).toHaveLength(2)
      for (const btn of saveBtns) expect(btn).toBeDisabled()
    })

    it('Clear calls ClearAIAPIKey for that provider', async () => {
      render(AIProviderTab)
      await ready()

      // chat has_key=true, so its Clear button is visible.
      const clearBtn = screen.getByRole('button', { name: /Clear key/i })
      await fireEvent.click(clearBtn)

      await waitFor(() =>
        expect(mocks.ClearAIAPIKey).toHaveBeenCalledWith('chat')
      )
    })

    it('Enter in the key input saves (explicit-commit affordance from the field itself)', async () => {
      render(AIProviderTab)
      await ready()

      const keyInput = document.getElementById(
        'ai-chat-key'
      ) as HTMLInputElement
      await fireEvent.input(keyInput, { target: { value: 'sk-enter' } })
      await fireEvent.keyDown(keyInput, { key: 'Enter' })

      await waitFor(() =>
        expect(mocks.SetAIAPIKey).toHaveBeenCalledWith('chat', 'sk-enter')
      )
    })
  })

  describe('sync providers', () => {
    // The default mock config has mismatched providers (chat=openai-compatible
    // with a key, embedding=local without), so syncProviders loads as false and
    // the toggle is the way to drive the sync-on path.

    it('toggling sync on shares the chat key with embedding server-side', async () => {
      render(AIProviderTab)
      await ready()

      const toggle = document.getElementById(
        'sync-providers-toggle'
      ) as HTMLInputElement
      await fireEvent.click(toggle)

      // Embedding adopts chat's provider type (openai-compatible supports
      // embeddings) and is persisted...
      await waitFor(() =>
        expect(mocks.UpdateAIProviderConfig).toHaveBeenCalledWith(
          'embedding',
          expect.objectContaining({ provider_type: 'openai-compatible' })
        )
      )
      // ...and the chat key is copied into the embedding slot without the
      // secret ever crossing to the renderer (CopyAIAPIKey, not SetAIAPIKey
      // with a value).
      await waitFor(() =>
        expect(mocks.CopyAIAPIKey).toHaveBeenCalledWith('chat', 'embedding')
      )
    })

    it('falls the embedding provider back to local for an Anthropic chat and skips the key copy', async () => {
      mocks.configState.chat.provider_type = 'anthropic'
      mocks.configState.chat.base_url = 'https://api.anthropic.com'
      mocks.configState.chat.has_key = true
      mocks.GetAIProviderConfig.mockResolvedValue(
        structuredClone(mocks.configState)
      )
      render(AIProviderTab)
      await ready()

      const toggle = document.getElementById(
        'sync-providers-toggle'
      ) as HTMLInputElement
      await fireEvent.click(toggle)

      // Anthropic has no embeddings endpoint — embedding must persist as local.
      await waitFor(() =>
        expect(mocks.UpdateAIProviderConfig).toHaveBeenCalledWith(
          'embedding',
          expect.objectContaining({
            provider_type: 'local',
            base_url: 'http://localhost:11434'
          })
        )
      )
      // Local (Ollama) is keyless, so no key is copied.
      expect(mocks.CopyAIAPIKey).not.toHaveBeenCalled()
    })

    it('rolls back the toggle and surfaces an error when the embedding persist fails', async () => {
      const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
      mocks.UpdateAIProviderConfig.mockRejectedValueOnce(
        new Error('persist failed')
      )
      try {
        render(AIProviderTab)
        await ready()

        const toggle = document.getElementById(
          'sync-providers-toggle'
        ) as HTMLInputElement
        await fireEvent.click(toggle)

        // The failure must surface (every other path does) and the optimistic
        // toggle roll back so the UI doesn't claim sync is on while the backend
        // embedding config is stale.
        await waitFor(() =>
          expect(
            screen.getByText(/failed to save provider settings/i)
          ).toBeInTheDocument()
        )
        expect(toggle.checked).toBe(false)
      } finally {
        consoleSpy.mockRestore()
      }
    })
  })

  describe('connection probe live regions', () => {
    it('success lands in a polite (role=status) region', async () => {
      render(AIProviderTab)
      await ready()

      // Embedding card's Test button (chat's would also work — pick
      // the one paired with the embedding provider-type label group).
      const testButtons = screen.getAllByRole('button', {
        name: /Test connection/i
      })
      await fireEvent.click(testButtons[1])

      const status = await screen.findByRole('status')
      expect(status).toBeInTheDocument()
      expect(status.textContent).toMatch(/Connected/i)
      expect(status.textContent).toContain('probe ok')
      // Failure region must NOT render alongside the success region.
      expect(screen.queryByRole('alert')).toBeNull()
    })

    it('failure lands in role=alert with the failure message', async () => {
      mocks.TestAIConnection.mockResolvedValueOnce({
        ok: false,
        kind: 'chat',
        message: '401 unauthorized'
      })
      render(AIProviderTab)
      await ready()

      const testButtons = screen.getAllByRole('button', {
        name: /Test connection/i
      })
      await fireEvent.click(testButtons[0]) // chat

      const alert = await screen.findByRole('alert')
      expect(alert.textContent).toMatch(/Connection failed/i)
      expect(alert.textContent).toContain('401 unauthorized')
    })

    it('disables the test button while the probe is in flight', async () => {
      let release!: () => void
      mocks.TestAIConnection.mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            release = () => resolve({ ok: true })
          })
      )
      render(AIProviderTab)
      await ready()

      const testButtons = screen.getAllByRole('button', {
        name: /Test connection/i
      })
      await fireEvent.click(testButtons[0])
      await tick()

      // While pending, both the "Testing…" labeled button is disabled.
      const testingBtn = screen.getByRole('button', { name: /Testing/i })
      expect(testingBtn).toBeDisabled()

      // Resolve and confirm re-enabled.
      release()
      await waitFor(() =>
        expect(
          screen.getByRole('button', { name: /Test connection/i })
        ).not.toBeDisabled()
      )
    })

    it('flushes un-blurred edits via UpdateAIProviderConfig before probing', async () => {
      // Without the pre-probe persist, a user who typed a new base_url/model
      // and clicked Test without blurring would probe stale backend state.
      render(AIProviderTab)
      await ready()

      const testButtons = screen.getAllByRole('button', {
        name: /Test connection/i
      })
      await fireEvent.click(testButtons[0]) // chat

      await waitFor(() =>
        expect(mocks.TestAIConnection).toHaveBeenCalledWith('chat')
      )
      // The probe must be preceded by a persist of the same provider so
      // the backend tests the values on screen, not a stale snapshot.
      expect(mocks.UpdateAIProviderConfig).toHaveBeenCalledWith(
        'chat',
        expect.anything()
      )
      expect(mocks.UpdateAIProviderConfig).toHaveBeenCalledBefore(
        mocks.TestAIConnection
      )
    })

    it('does not probe stale backend state when advanced validation blocks the pre-probe save', async () => {
      mocks.configState.chat.max_tokens = 0
      mocks.GetAIProviderConfig.mockResolvedValue(
        structuredClone(mocks.configState)
      )
      render(AIProviderTab)
      await ready()

      const testButtons = screen.getAllByRole('button', {
        name: /Test connection/i
      })
      await fireEvent.click(testButtons[0]) // chat

      await waitFor(() => expect(mocks.TestAIConnection).not.toHaveBeenCalled())
      expect(
        await screen.findByText(/Fix invalid advanced settings before testing/i)
      ).toBeInTheDocument()
    })

    it('does not probe stale backend state when the pre-probe save fails', async () => {
      const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
      mocks.UpdateAIProviderConfig.mockRejectedValueOnce(
        new Error('bad base URL')
      )
      try {
        render(AIProviderTab)
        await ready()

        const testButtons = screen.getAllByRole('button', {
          name: /Test connection/i
        })
        await fireEvent.click(testButtons[0]) // chat

        await waitFor(() =>
          expect(mocks.UpdateAIProviderConfig).toHaveBeenCalledWith(
            'chat',
            expect.anything()
          )
        )
        expect(mocks.TestAIConnection).not.toHaveBeenCalled()
        const alert = await screen.findByRole('alert')
        expect(alert.textContent).toMatch(/Failed to save provider settings/i)
        expect(alert.textContent).toContain('bad base URL')
      } finally {
        consoleSpy.mockRestore()
      }
    })
  })

  describe('keyring section', () => {
    async function goAdvanced() {
      await fireEvent.click(screen.getByRole('tab', { name: /^Advanced$/i }))
    }

    it('toggling calls SetUseKeyring with the new value', async () => {
      render(AIProviderTab)
      await ready()
      await goAdvanced()

      const toggle = document.getElementById(
        'ai-keyring-toggle'
      ) as HTMLInputElement
      expect(toggle.checked).toBe(true)

      await fireEvent.click(toggle)

      await waitFor(() =>
        expect(mocks.SetUseKeyring).toHaveBeenCalledWith(false)
      )
    })

    it('renders the unavailability warning when keyring_available=false', async () => {
      mocks.configState.keyring_available = false
      mocks.GetAIProviderConfig.mockResolvedValue(
        structuredClone(mocks.configState)
      )
      render(AIProviderTab)
      await ready()
      await goAdvanced()

      const warning = screen.getByText(/No OS keyring/i)
      expect(warning).toBeInTheDocument()
      expect(warning.closest('[role="alert"]')).not.toBeNull()
      expect(warning.textContent).toContain('config.yaml')
    })

    it('renders the per-card fallback note when keyring_unusable_for lists the provider', async () => {
      mocks.configState.keyring_unusable_for = ['chat']
      mocks.GetAIProviderConfig.mockResolvedValue(
        structuredClone(mocks.configState)
      )
      render(AIProviderTab)
      await ready()

      // The fallback note lives inside the chat card (which has_key=true).
      const notes = screen.getAllByText(/keyring was unreachable/i)
      expect(notes).toHaveLength(1)
    })

    it('does not render the per-card note for a provider not in keyring_unusable_for', async () => {
      render(AIProviderTab)
      await ready()
      expect(screen.queryAllByText(/keyring was unreachable/i)).toHaveLength(0)
    })
  })

  describe('audit log', () => {
    // Click target: the <summary> element. jsdom renders <details>
    // child content unconditionally (no UA-style hiding when closed),
    // so the empty-state text "No activity recorded yet." lives in the
    // DOM from initial render — use the summary's exact text rather
    // than a partial regex to disambiguate.
    function summaryEl(): HTMLElement {
      return screen.getByText('Plugin AI calls', { exact: true })
    }

    async function goAdvanced() {
      await fireEvent.click(screen.getByRole('tab', { name: /^Advanced$/i }))
    }

    it('expanding the <details> loads the audit log via GetAIAudit', async () => {
      render(AIProviderTab)
      await ready()
      await goAdvanced()

      await fireEvent.click(summaryEl())

      await waitFor(() => expect(mocks.GetAIAudit).toHaveBeenCalled())
      // Rows render: the table caption + two distinct plugins.
      expect(await screen.findByText('summarizer')).toBeInTheDocument()
      expect(screen.getByText('search-index')).toBeInTheDocument()
    })

    it('Clear log calls ClearAIAudit and empties the table', async () => {
      render(AIProviderTab)
      await ready()
      await goAdvanced()

      await fireEvent.click(summaryEl())
      await screen.findByText('summarizer')

      await fireEvent.click(screen.getByRole('button', { name: /Clear log/i }))

      await waitFor(() => expect(mocks.ClearAIAudit).toHaveBeenCalled())
      // After clear, the empty-state message renders and the rows are gone.
      await waitFor(() =>
        expect(
          screen.getByText(/No activity recorded yet/i)
        ).toBeInTheDocument()
      )
      expect(screen.queryByText('summarizer')).toBeNull()
    })

    it('shows the audit error banner when GetAIAudit rejects', async () => {
      mocks.GetAIAudit.mockRejectedValue(new Error('db locked'))
      render(AIProviderTab)
      await ready()
      await goAdvanced()

      await fireEvent.click(summaryEl())

      const message = await screen.findByText(/Failed to load audit log/i)
      expect(message.textContent).toContain('db locked')
      expect(message.closest('[role="alert"]')).not.toBeNull()
    })

    it('Retry re-fetches the audit log after a transient failure', async () => {
      // Once the load effect lands in 'error', re-opening <details> cannot
      // recover (the effect only fires from 'idle'). The Retry button
      // re-invokes loadAudit directly, clearing the error and re-running.
      mocks.GetAIAudit.mockRejectedValueOnce(
        new Error('db locked')
      ).mockResolvedValue(structuredClone(mocks.auditState))
      render(AIProviderTab)
      await ready()
      await goAdvanced()

      await fireEvent.click(summaryEl())

      const message = await screen.findByText(/Failed to load audit log/i)
      const alert = message.closest('[role="alert"]') as HTMLElement
      await fireEvent.click(
        within(alert).getByRole('button', { name: /Retry/i })
      )

      // Retry resolves → rows render and the error banner is gone.
      expect(await screen.findByText('summarizer')).toBeInTheDocument()
      expect(screen.queryByText(/Failed to load audit log/i)).toBeNull()
    })

    it('renders a localized timestamp instead of the raw ISO string', async () => {
      // Raw RFC3339 is hard to scan and not locale-aware. The cell should
      // show a localized short date/time; the full ISO stays on the title
      // attribute for hover precision. Locale output varies in CI, so assert
      // the negative (no ISO markers) plus non-empty rendered text.
      mocks.GetAIAudit.mockResolvedValue([
        {
          plugin: 'summarizer',
          kind: 'chat',
          host: 'api.openai.com',
          model: 'gpt-4o',
          status: 'ok',
          at: '2026-07-06T14:45:00.000Z',
          prompt_tokens: 120,
          completion_tokens: 80,
          total_tokens: 200
        }
      ])
      render(AIProviderTab)
      await ready()
      await goAdvanced()

      await fireEvent.click(summaryEl())
      await screen.findByText('summarizer')

      // The "When" cell is the first <td> in the (single) data row.
      const whenCell = screen.getAllByRole('cell')[0]
      expect(whenCell).toHaveAttribute('title', '2026-07-06T14:45:00.000Z')
      expect(whenCell.textContent).not.toContain('T')
      expect(whenCell.textContent).not.toContain('Z')
      expect(whenCell.textContent?.trim().length).toBeGreaterThan(0)
    })
  })

  describe('embedding model / index density', () => {
    it('marks the search index stale when the embedding model changes', async () => {
      // Callers mutate config before persist; stale detection must compare
      // against last-persisted values, not the already-mutated config.
      render(AIProviderTab)
      await ready()

      const embedModel = document.getElementById(
        'ai-embedding-model'
      ) as HTMLInputElement
      await fireEvent.input(embedModel, {
        target: { value: 'text-embedding-3-small' }
      })
      await fireEvent.blur(embedModel)

      await waitFor(() =>
        expect(mocks.UpdateAIProviderConfig).toHaveBeenCalledWith(
          'embedding',
          expect.objectContaining({ model: 'text-embedding-3-small' })
        )
      )
      await waitFor(() =>
        expect(mocks.UpdatePluginSetting).toHaveBeenCalledWith(
          'silt-ai-qa',
          'stale_reason',
          expect.stringMatching(/nomic-embed-text.*text-embedding-3-small/)
        )
      )
    })

    it('clears dimensions and marks stale when switching to a fixed-size model', async () => {
      // Default fixture has dimensions: 768. Fixed models (ada-002) hide the
      // density control; leftover overrides must not be sent to the API.
      render(AIProviderTab)
      await ready()

      const embedModel = document.getElementById(
        'ai-embedding-model'
      ) as HTMLInputElement
      await fireEvent.input(embedModel, {
        target: { value: 'text-embedding-ada-002' }
      })
      await fireEvent.blur(embedModel)

      await waitFor(() =>
        expect(mocks.UpdateAIProviderConfig).toHaveBeenCalledWith(
          'embedding',
          expect.objectContaining({
            model: 'text-embedding-ada-002',
            dimensions: undefined
          })
        )
      )
      await waitFor(() =>
        expect(mocks.UpdatePluginSetting).toHaveBeenCalledWith(
          'silt-ai-qa',
          'stale_reason',
          expect.stringMatching(/nomic-embed-text.*text-embedding-ada-002/)
        )
      )
    })
  })

  describe('initial load failure', () => {
    it('shows the load-error banner with a Retry button', async () => {
      mocks.GetAIProviderConfig.mockRejectedValue(new Error('boom'))
      render(AIProviderTab)

      const message = await screen.findByText(
        /Failed to load AI configuration/i
      )
      expect(message.textContent).toContain('boom')
      // The banner carries the alert role for assertive AT announcement.
      expect(message.closest('[role="alert"]')).not.toBeNull()
      expect(screen.getByRole('button', { name: /Retry/i })).toBeInTheDocument()

      // Retry re-attempts the load.
      mocks.GetAIProviderConfig.mockResolvedValue(
        structuredClone(mocks.configState)
      )
      await fireEvent.click(screen.getByRole('button', { name: /Retry/i }))
      await ready()
    })
  })

  describe('Local MCP settings', () => {
    it('loads MCP status on mount and enables via SetLocalMCPConfig', async () => {
      render(AIProviderTab)
      await ready()
      await waitFor(() => expect(mocks.GetLocalMCPConfig).toHaveBeenCalled())
      const enable = screen.getByLabelText(/Enable local AI integration/i)
      await fireEvent.click(enable)
      await waitFor(() =>
        expect(mocks.SetLocalMCPConfig).toHaveBeenCalledWith(
          true,
          true,
          false,
          17887
        )
      )
    })

    it('prompts for close-to-tray when enabling and tray is off', async () => {
      mocks.GetCloseToTray.mockResolvedValue(false)
      render(AIProviderTab)
      await ready()
      await fireEvent.click(
        screen.getByLabelText(/Enable local AI integration/i)
      )
      await waitFor(() =>
        expect(
          screen.getByRole('button', { name: /Enable close to tray/i })
        ).toBeInTheDocument()
      )
      await fireEvent.click(
        screen.getByRole('button', { name: /Enable close to tray/i })
      )
      await waitFor(() =>
        expect(mocks.SetCloseToTray).toHaveBeenCalledWith(true)
      )
    })

    it('reveals token and auto-clears after 30s', async () => {
      vi.useFakeTimers()
      mocks.GetLocalMCPToken.mockResolvedValue('secret-token')
      mocks.GetLocalMCPConfig.mockResolvedValue({
        enabled: true,
        http_enabled: true,
        http_port: 17887,
        write_enabled: false
      })
      render(AIProviderTab)
      await ready()
      await fireEvent.click(
        screen.getByRole('button', { name: /Show auth token/i })
      )
      await waitFor(() =>
        expect(screen.getByText(/secret-token/)).toBeInTheDocument()
      )
      await vi.advanceTimersByTimeAsync(30_000)
      await waitFor(() => expect(screen.queryByText(/secret-token/)).toBeNull())
      vi.useRealTimers()
    })

    it('surfaces save errors in an alert', async () => {
      mocks.SetLocalMCPConfig.mockRejectedValueOnce(new Error('nope'))
      render(AIProviderTab)
      await ready()
      await fireEvent.click(
        screen.getByLabelText(/Enable local AI integration/i)
      )
      await waitFor(() =>
        expect(screen.getByRole('alert')).toHaveTextContent(
          /Could not save local MCP settings/i
        )
      )
    })

    it('renders running status endpoint and message', async () => {
      mocks.GetLocalMCPConfig.mockResolvedValue({
        enabled: true,
        http_enabled: true,
        http_port: 17887,
        write_enabled: true
      })
      mocks.GetLocalMCPStatus.mockResolvedValue({
        state: 'running',
        message: 'MCP listening on http://127.0.0.1:17887',
        endpoint: 'http://127.0.0.1:17887',
        write_enabled: true
      })
      render(AIProviderTab)
      await ready()
      const status = await screen.findByText(/MCP availability/i)
      expect(status.textContent).toMatch(/running/i)
      expect(status.textContent).toMatch(/127\.0\.0\.1:17887/)
      expect(status.textContent).toMatch(/MCP listening/i)
    })

    it('Refresh status re-fetches GetLocalMCPStatus', async () => {
      render(AIProviderTab)
      await ready()
      const before = mocks.GetLocalMCPStatus.mock.calls.length
      await fireEvent.click(
        screen.getByRole('button', { name: /Refresh status/i })
      )
      await waitFor(() =>
        expect(mocks.GetLocalMCPStatus.mock.calls.length).toBeGreaterThan(
          before
        )
      )
    })

    it('Copy token writes the bearer to the clipboard and shows Copied', async () => {
      mocks.GetLocalMCPToken.mockResolvedValue('clip-token-xyz')
      const writeText = vi.fn().mockResolvedValue(undefined)
      vi.stubGlobal('navigator', {
        ...navigator,
        clipboard: { writeText }
      })
      try {
        render(AIProviderTab)
        await ready()
        await fireEvent.click(
          screen.getByRole('button', { name: /Copy token/i })
        )
        await waitFor(() =>
          expect(writeText).toHaveBeenCalledWith('clip-token-xyz')
        )
        await waitFor(() =>
          expect(
            screen.getByRole('button', { name: /^Copied$/i })
          ).toBeInTheDocument()
        )
      } finally {
        vi.unstubAllGlobals()
      }
    })

    it('unmount after Copy token clears the clipboard', async () => {
      mocks.GetLocalMCPToken.mockResolvedValue('clip-token-unmount')
      const writeText = vi.fn().mockResolvedValue(undefined)
      vi.stubGlobal('navigator', {
        ...navigator,
        clipboard: { writeText }
      })
      try {
        const { unmount } = render(AIProviderTab)
        await ready()
        await fireEvent.click(
          screen.getByRole('button', { name: /Copy token/i })
        )
        await waitFor(() =>
          expect(writeText).toHaveBeenCalledWith('clip-token-unmount')
        )
        unmount()
        await waitFor(() => expect(writeText).toHaveBeenCalledWith(''))
      } finally {
        vi.unstubAllGlobals()
      }
    })

    function mcpActivitySummary() {
      return screen.getByText('MCP activity', { exact: true })
    }

    it('expanding MCP activity loads the audit log via GetMCPAudit', async () => {
      render(AIProviderTab)
      await ready()
      await fireEvent.click(mcpActivitySummary())
      await waitFor(() => expect(mocks.GetMCPAudit).toHaveBeenCalled())
      expect(await screen.findByText('create_page')).toBeInTheDocument()
      expect(screen.getAllByText('search_blocks').length).toBeGreaterThan(0)
      expect(
        screen.getByRole('table', { name: /Recent local MCP tool calls/i })
      ).toHaveTextContent('denied')
    })

    it('filters MCP activity by outcome', async () => {
      render(AIProviderTab)
      await ready()
      await fireEvent.click(mcpActivitySummary())
      await screen.findByText('create_page')
      const outcome = screen.getByLabelText(
        /Filter MCP activity by outcome/i
      ) as HTMLSelectElement
      await fireEvent.change(outcome, { target: { value: 'denied' } })
      const table = screen.getByRole('table', {
        name: /Recent local MCP tool calls/i
      })
      expect(table).toHaveTextContent('create_page')
      expect(table).not.toHaveTextContent('rejected_schema')
      expect(table).not.toHaveTextContent('search_blocks')
    })

    it('filters MCP activity by tool name substring', async () => {
      render(AIProviderTab)
      await ready()
      await fireEvent.click(mcpActivitySummary())
      await screen.findByText('create_page')
      const tool = screen.getByLabelText(
        /Filter MCP activity by tool name/i
      ) as HTMLInputElement
      await fireEvent.input(tool, { target: { value: 'create' } })
      const table = screen.getByRole('table', {
        name: /Recent local MCP tool calls/i
      })
      expect(table).toHaveTextContent('create_page')
      expect(table).not.toHaveTextContent('search_blocks')
    })

    it('shows filter-empty message and still offers Clear log', async () => {
      render(AIProviderTab)
      await ready()
      await fireEvent.click(mcpActivitySummary())
      await screen.findByText('create_page')
      const tool = screen.getByLabelText(
        /Filter MCP activity by tool name/i
      ) as HTMLInputElement
      await fireEvent.input(tool, { target: { value: 'no-such-tool-xyz' } })
      expect(
        await screen.findByText(/No calls match the current filters/i)
      ).toBeInTheDocument()
      // Clear stays available whenever loaded audit has rows.
      expect(
        screen.getByRole('button', { name: /Clear log/i })
      ).toBeInTheDocument()
    })

    it('Clear log calls ClearMCPAudit and empties the MCP table', async () => {
      render(AIProviderTab)
      await ready()
      await fireEvent.click(mcpActivitySummary())
      await screen.findByText('create_page')
      await fireEvent.click(screen.getByRole('button', { name: /Clear log/i }))
      await waitFor(() => expect(mocks.ClearMCPAudit).toHaveBeenCalled())
      await waitFor(() =>
        expect(
          screen.getByText(/No activity recorded yet/i)
        ).toBeInTheDocument()
      )
      expect(screen.queryByText('create_page')).toBeNull()
    })

    it('shows MCP audit error banner when GetMCPAudit rejects', async () => {
      mocks.GetMCPAudit.mockRejectedValue(new Error('disk locked'))
      render(AIProviderTab)
      await ready()
      await fireEvent.click(mcpActivitySummary())
      const message = await screen.findByText(/Failed to load audit log/i)
      expect(message.textContent).toContain('disk locked')
      mocks.GetMCPAudit.mockImplementation(async () =>
        structuredClone(mocks.mcpAuditState)
      )
      await fireEvent.click(screen.getByRole('button', { name: /Retry/i }))
      await waitFor(() =>
        expect(screen.getByText('create_page')).toBeInTheDocument()
      )
    })

    it('keeps rows and shows clear error when ClearMCPAudit rejects', async () => {
      mocks.ClearMCPAudit.mockRejectedValueOnce(new Error('permission denied'))
      render(AIProviderTab)
      await ready()
      await fireEvent.click(mcpActivitySummary())
      await screen.findByText('create_page')
      await fireEvent.click(screen.getByRole('button', { name: /Clear log/i }))
      const message = await screen.findByText(/Failed to clear audit log/i)
      expect(message.textContent).toContain('permission denied')
      // Rows remain visible after a failed clear.
      expect(screen.getByText('create_page')).toBeInTheDocument()
    })
  })
})
