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
import { render, screen, cleanup, fireEvent, waitFor, within } from '@testing-library/svelte'

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
    keyring_unusable_for: [] as string[]
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
    SetAIAPIKey: vi.fn(),
    ClearAIAPIKey: vi.fn(),
    SetUseKeyring: vi.fn(),
    TestAIConnection: vi.fn(),
    GetAIAudit: vi.fn(),
    ClearAIAudit: vi.fn()
  }
})

vi.mock('../../../wailsjs/go/main/App.js', () => ({
  GetAIProviderConfig: mocks.GetAIProviderConfig,
  UpdateAIProviderConfig: mocks.UpdateAIProviderConfig,
  SetAIAPIKey: mocks.SetAIAPIKey,
  ClearAIAPIKey: mocks.ClearAIAPIKey,
  SetUseKeyring: mocks.SetUseKeyring,
  TestAIConnection: mocks.TestAIConnection,
  GetAIAudit: mocks.GetAIAudit,
  ClearAIAudit: mocks.ClearAIAudit
}))

import AIProviderTab from './AIProviderTab.svelte'

describe('AIProviderTab', () => {
  beforeEach(() => {
    mocks.resetConfig()
    mocks.GetAIProviderConfig.mockReset()
    mocks.UpdateAIProviderConfig.mockReset()
    mocks.SetAIAPIKey.mockReset()
    mocks.ClearAIAPIKey.mockReset()
    mocks.SetUseKeyring.mockReset()
    mocks.TestAIConnection.mockReset()
    mocks.GetAIAudit.mockReset()
    mocks.ClearAIAudit.mockReset()
    // Default happy-path resolutions; individual tests override.
    mocks.GetAIProviderConfig.mockResolvedValue(
      structuredClone(mocks.configState)
    )
    mocks.UpdateAIProviderConfig.mockResolvedValue(undefined)
    mocks.SetAIAPIKey.mockResolvedValue(undefined)
    mocks.ClearAIAPIKey.mockResolvedValue(undefined)
    mocks.SetUseKeyring.mockResolvedValue(undefined)
    mocks.TestAIConnection.mockResolvedValue({
      ok: true,
      kind: 'chat',
      message: 'probe ok'
    })
    mocks.GetAIAudit.mockResolvedValue(structuredClone(mocks.auditState))
    mocks.ClearAIAudit.mockResolvedValue(undefined)
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

    it('shows a setup nudge when both providers are on local defaults with no key', async () => {
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
        keyring_unusable_for: []
      }
      mocks.GetAIProviderConfig.mockResolvedValue(
        structuredClone(mocks.configState)
      )
      render(AIProviderTab)
      await ready()

      expect(
        screen.getByText(/Set up an AI provider/i)
      ).toBeInTheDocument()
    })

    it('hides the setup nudge once a provider has a key', async () => {
      // The default mock config has chat.has_key=true, so no nudge.
      render(AIProviderTab)
      await ready()
      expect(
        screen.queryByText(/Set up an AI provider/i)
      ).toBeNull()
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
      expect(
        screen.getByText(/doesn't leave this device/i)
      ).toBeInTheDocument()
    })

    it('shows the cloud privacy warning for an openai-compatible provider', async () => {
      // Chat defaults to openai-compatible in the mock config.
      render(AIProviderTab)
      await ready()

      expect(
        screen.getByText(/leaves your machine/i)
      ).toBeInTheDocument()
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
  })

  describe('keyring section', () => {
    it('toggling calls SetUseKeyring with the new value', async () => {
      render(AIProviderTab)
      await ready()

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
      expect(
        screen.queryAllByText(/keyring was unreachable/i)
      ).toHaveLength(0)
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

    it('expanding the <details> loads the audit log via GetAIAudit', async () => {
      render(AIProviderTab)
      await ready()

      await fireEvent.click(summaryEl())

      await waitFor(() => expect(mocks.GetAIAudit).toHaveBeenCalled())
      // Rows render: the table caption + two distinct plugins.
      expect(
        await screen.findByText('summarizer')
      ).toBeInTheDocument()
      expect(screen.getByText('search-index')).toBeInTheDocument()
    })

    it('Clear log calls ClearAIAudit and empties the table', async () => {
      render(AIProviderTab)
      await ready()

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

      await fireEvent.click(summaryEl())

      const message = await screen.findByText(/Failed to load audit log/i)
      expect(message.textContent).toContain('db locked')
      expect(message.closest('[role="alert"]')).not.toBeNull()
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
      expect(
        screen.getByRole('button', { name: /Retry/i })
      ).toBeInTheDocument()

      // Retry re-attempts the load.
      mocks.GetAIProviderConfig.mockResolvedValue(
        structuredClone(mocks.configState)
      )
      await fireEvent.click(screen.getByRole('button', { name: /Retry/i }))
      await ready()
    })
  })
})
