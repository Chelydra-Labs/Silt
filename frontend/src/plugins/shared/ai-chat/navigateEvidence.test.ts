import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render } from '@testing-library/svelte'
import {
  dispatchNavigateEvidence,
  evidenceNavigateDetail
} from './navigateEvidence'
import { evidenceEntry, textEntry, type AIChatEntry } from './types'
import type { EvidenceTarget } from './types'
import type { SystemConfig } from '../../../settings/store.svelte'
import type { SettingsDialogsController } from '../../../shell/useSettingsDialogs.svelte'
import type { TabManagerController } from '../../../lib/tabs/useTabManager.svelte'

const browserMocks = vi.hoisted(() => ({
  OpenURL: vi.fn()
}))

const startupMocks = vi.hoisted(() => ({
  eventsOn: vi.fn(),
  MarkFrontendReady: vi.fn().mockResolvedValue(undefined),
  GetStartupEvents: vi.fn().mockResolvedValue([]),
  ResolveQuarantinedLinks: vi.fn().mockResolvedValue([])
}))

vi.mock('@wailsio/runtime', () => ({
  Browser: {
    OpenURL: browserMocks.OpenURL
  },
  Events: { On: startupMocks.eventsOn },
  Call: { ByID: vi.fn(), ByName: vi.fn() },
  CancellablePromise: class {},
  Create: {
    Nullable: (fn: unknown) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    MarkFrontendReady: startupMocks.MarkFrontendReady,
    GetStartupEvents: startupMocks.GetStartupEvents,
    ResolveQuarantinedLinks: startupMocks.ResolveQuarantinedLinks
  })
)

vi.mock('../../../components/editor/EditorUtilityBar.svelte', () => ({
  OPEN_TASKS_FOR_PAGE_EVENT: 'silt:open-tasks-for-page'
}))

import ChatShell from './ChatShell.svelte'
import {
  createStartupEvents,
  type StartupEventsDeps
} from '../../../shell/useStartupEvents.svelte'

function makeStartupDeps(): {
  deps: StartupEventsDeps
  handleSearchJump: ReturnType<typeof vi.fn>
} {
  const nav = { notebook: 'Work', section: 'Inbox', page: 'Alpha' }
  const handleSearchJump = vi.fn()
  const settingsDialogs = {
    openSettingsMismatch: vi.fn(),
    openGrantsMigration: vi.fn(),
    setQuarantinedLinks: vi.fn()
  } as unknown as SettingsDialogsController
  const tabManager = {
    initBaseline: vi.fn(),
    handleConfigChangedTabRehydrate: vi.fn(),
    resetTabs: vi.fn(),
    invalidateRecentPages: vi.fn(),
    pageRenamed: vi.fn(),
    openPage: vi.fn()
  } as unknown as TabManagerController
  const deps: StartupEventsDeps = {
    getActiveNotebook: () => nav.notebook,
    getActiveSection: () => nav.section,
    getActivePage: () => nav.page,
    setActiveNotebook: (nb: string) => {
      nav.notebook = nb
    },
    setActiveSection: (sec: string) => {
      nav.section = sec
    },
    setActivePage: vi.fn((pg: string) => {
      nav.page = pg
    }),
    setActiveView: vi.fn(),
    getSettings: () =>
      ({
        plugins: { disabled: [] },
        ui: { open_tabs: [] }
      }) as unknown as SystemConfig,
    setSettingsSection: vi.fn(),
    setShowSearch: vi.fn(),
    setShowQuickAdd: vi.fn(),
    getShowTemplatePicker: () => false,
    setShowTemplatePicker: vi.fn(),
    setTemplatePickerMode: vi.fn(),
    setSelectedTag: vi.fn(),
    getSidebarCollapsed: () => false,
    setSidebarCollapsed: vi.fn(),
    setSearchTargetHeading: vi.fn(),
    setSearchTargetKey: vi.fn(),
    getNavigationCatalog: () => [],
    settingsDialogs,
    tabManager,
    openSettings: vi.fn(),
    openTasksView: vi.fn(),
    handleSwitchVault: vi.fn().mockResolvedValue(undefined),
    handleMenuSave: vi.fn().mockResolvedValue(undefined),
    handleSearchJump
  }
  return { deps, handleSearchJump }
}

describe('navigateEvidence (#875)', () => {
  let startupController: ReturnType<typeof createStartupEvents> | undefined

  beforeEach(() => {
    vi.clearAllMocks()
    startupMocks.GetStartupEvents.mockResolvedValue([])
    startupMocks.eventsOn.mockImplementation(() => vi.fn())
  })

  afterEach(() => {
    startupController?.dispose()
    startupController = undefined
  })

  it('builds a full navigate-to-block detail from the evidence target', () => {
    const target: EvidenceTarget = {
      blockId: 'block-1',
      notebook: 'Work',
      section: 'Notes',
      page: 'Plan'
    }
    expect(evidenceNavigateDetail(target)).toEqual({
      notebook: 'Work',
      section: 'Notes',
      page: 'Plan',
      blockId: 'block-1'
    })
  })

  it('does not navigate for product-help evidence targets (#928)', () => {
    const handler = vi.fn()
    window.addEventListener('navigate-to-block', handler)
    dispatchNavigateEvidence({
      blockId: 'help:getting-started#enable-ai',
      sourceKind: 'product_help'
    })
    dispatchNavigateEvidence({
      blockId: 'help:backup',
      sourceKind: 'vault' // still blocked by help: prefix
    })
    expect(handler).not.toHaveBeenCalled()
    window.removeEventListener('navigate-to-block', handler)
  })

  it('forwards optional locator fields when present (not only blockId)', () => {
    // Regression: the drawer used to drop notebook/section/page at dispatch.
    const target: EvidenceTarget = {
      blockId: 'only-id-was-sent',
      notebook: 'Vault NB',
      section: 'Sec',
      page: 'Page'
    }
    const detail = evidenceNavigateDetail(target)
    expect(detail).toHaveProperty('notebook', 'Vault NB')
    expect(detail).toHaveProperty('section', 'Sec')
    expect(detail).toHaveProperty('page', 'Page')
    expect(detail).toHaveProperty('blockId', 'only-id-was-sent')
    expect(Object.keys(detail).sort()).toEqual(
      ['blockId', 'notebook', 'page', 'section'].sort()
    )
  })

  it('dispatches navigate-to-block with the full locator', () => {
    const handler = vi.fn()
    window.addEventListener('navigate-to-block', handler)

    const target: EvidenceTarget = {
      blockId: 'block-42',
      notebook: 'Work',
      section: 'Inbox',
      page: 'Daily'
    }
    dispatchNavigateEvidence(target)

    expect(handler).toHaveBeenCalledTimes(1)
    const event = handler.mock.calls[0][0] as CustomEvent
    expect(event.detail).toEqual({
      notebook: 'Work',
      section: 'Inbox',
      page: 'Daily',
      blockId: 'block-42'
    })

    window.removeEventListener('navigate-to-block', handler)
  })

  it('citation click → onNavigateEvidence → dispatch delivers full locator (drawer path)', async () => {
    // Mirrors AIChatDrawer: ChatShell calls onNavigateEvidence(target), which
    // the drawer wires to dispatchNavigateEvidence.
    const target: EvidenceTarget = {
      blockId: 'block-1',
      notebook: 'Work',
      section: 'Notes',
      page: 'Plan'
    }
    const transcript: AIChatEntry[] = [
      textEntry({ id: 'u', role: 'user', content: 'Where is launch?' }),
      evidenceEntry({
        id: 'e',
        role: 'assistant',
        citationIndex: 1,
        title: 'Launch plan',
        excerpt: 'Ship in August',
        target
      })
    ]
    const handler = vi.fn()
    window.addEventListener('navigate-to-block', handler)

    const { getByRole } = render(ChatShell, {
      props: {
        title: 'Silt AI',
        transcript,
        busy: false,
        lastOutcome: null,
        providerReady: true,
        onSend: vi.fn(),
        onStop: vi.fn(),
        onAcceptProposal: vi.fn(),
        onDiscardProposal: vi.fn(),
        onConfirmStaging: vi.fn(),
        onRejectStaging: vi.fn(),
        onOpenSettings: vi.fn(),
        onNavigateEvidence: dispatchNavigateEvidence,
        onClear: vi.fn()
      }
    })

    const btn = getByRole('button', { name: 'Open source 1: Launch plan' })
    await fireEvent.click(btn)

    expect(handler).toHaveBeenCalledTimes(1)
    expect((handler.mock.calls[0][0] as CustomEvent).detail).toEqual({
      notebook: 'Work',
      section: 'Notes',
      page: 'Plan',
      blockId: 'block-1'
    })

    window.removeEventListener('navigate-to-block', handler)
  })

  it('citation click → dispatch → startup handler → handleSearchJump with full locator', async () => {
    // End-to-end bus path for #875: ChatShell (drawer wiring) through the
    // global navigate-to-block listener into handleSearchJump.
    const { deps, handleSearchJump } = makeStartupDeps()
    startupController = createStartupEvents(deps)
    startupController.attach()

    const target: EvidenceTarget = {
      blockId: 'block-1',
      notebook: 'Work',
      section: 'Notes',
      page: 'Plan'
    }
    const transcript: AIChatEntry[] = [
      textEntry({ id: 'u', role: 'user', content: 'Where is launch?' }),
      evidenceEntry({
        id: 'e',
        role: 'assistant',
        citationIndex: 1,
        title: 'Launch plan',
        excerpt: 'Ship in August',
        target
      })
    ]

    const { getByRole } = render(ChatShell, {
      props: {
        title: 'Silt AI',
        transcript,
        busy: false,
        lastOutcome: null,
        providerReady: true,
        onSend: vi.fn(),
        onStop: vi.fn(),
        onAcceptProposal: vi.fn(),
        onDiscardProposal: vi.fn(),
        onConfirmStaging: vi.fn(),
        onRejectStaging: vi.fn(),
        onOpenSettings: vi.fn(),
        onNavigateEvidence: dispatchNavigateEvidence,
        onClear: vi.fn()
      }
    })

    await fireEvent.click(
      getByRole('button', { name: 'Open source 1: Launch plan' })
    )

    expect(handleSearchJump).toHaveBeenCalledTimes(1)
    expect(handleSearchJump).toHaveBeenCalledWith(
      {
        source: undefined,
        notebook: 'Work',
        section: 'Notes',
        page: 'Plan'
      },
      undefined,
      'block-1'
    )
    expect(deps.openTasksView).not.toHaveBeenCalled()
  })
})
