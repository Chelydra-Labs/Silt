import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'
import {
  registerEditor,
  _resetEditorRegistryForTests
} from '../../lib/editor/editorRegistry.svelte'

const appMocks = vi.hoisted(() =>
  createAppIpcMocks({
    ListPageVersions: vi.fn(),
    GetPageVersion: vi.fn(),
    RestorePageVersion: vi.fn()
  })
)

vi.mock('$silt-app', () => appMocks)

const settingsMock = vi.hoisted(() => ({
  config: {
    editor: { auto_versioning_enabled: true }
  }
}))

vi.mock('../../settings/store.svelte', () => ({
  settings: settingsMock
}))

import PageHistoryModal from './PageHistoryModal.svelte'

const VERSIONS = [
  {
    id: 'v-new',
    timestamp: '2026-08-16T18:00:00Z',
    source: 'editor',
    bytes: 120
  },
  {
    id: 'v-old',
    timestamp: '2026-08-15T09:30:00Z',
    source: 'source',
    bytes: 96
  }
]

function renderModal(onClose = vi.fn()) {
  return render(PageHistoryModal, {
    props: {
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      onClose
    }
  })
}

beforeEach(() => {
  settingsMock.config.editor.auto_versioning_enabled = true
  appMocks.ListPageVersions.mockReset().mockResolvedValue(VERSIONS)
  appMocks.GetPageVersion.mockReset().mockImplementation(
    async (_nb: string, _sec: string, _pg: string, id: string) =>
      id === 'v-old' ? '# older body' : '# newest body'
  )
  appMocks.RestorePageVersion.mockReset().mockResolvedValue(undefined)
  _resetEditorRegistryForTests()
})

afterEach(() => {
  cleanup()
  _resetEditorRegistryForTests()
})

describe('PageHistoryModal', () => {
  it('renders the version list and previews without restoring', async () => {
    renderModal()
    await waitFor(() => {
      expect(screen.getByRole('option', { name: /Editor/i })).toBeTruthy()
    })
    expect(screen.getByRole('option', { name: /Source/i })).toBeTruthy()
    await waitFor(() => {
      expect(screen.getByTestId('page-history-preview-body')).toHaveTextContent(
        '# newest body'
      )
    })
    expect(appMocks.GetPageVersion).toHaveBeenCalledWith(
      'Work',
      'Journal',
      'Daily',
      'v-new'
    )
    expect(appMocks.RestorePageVersion).not.toHaveBeenCalled()
  })

  it('loads a preview when another version is selected', async () => {
    renderModal()
    await waitFor(() => {
      expect(screen.getByRole('option', { name: /Source/i })).toBeTruthy()
    })
    await fireEvent.click(screen.getByRole('option', { name: /Source/i }))
    await waitFor(() => {
      expect(screen.getByTestId('page-history-preview-body')).toHaveTextContent(
        '# older body'
      )
    })
    expect(appMocks.GetPageVersion).toHaveBeenCalledWith(
      'Work',
      'Journal',
      'Daily',
      'v-old'
    )
    expect(appMocks.RestorePageVersion).not.toHaveBeenCalled()
  })

  it('confirming Restore calls RestorePageVersion', async () => {
    renderModal()
    await waitFor(() => {
      expect(screen.getByTestId('page-history-restore')).toBeTruthy()
    })
    await fireEvent.click(screen.getByTestId('page-history-restore'))
    expect(appMocks.RestorePageVersion).not.toHaveBeenCalled()
    const confirm = await screen.findByTestId('page-history-confirm-confirm')
    expect(document.activeElement).not.toBe(confirm)
    await fireEvent.click(confirm)
    await waitFor(() => {
      expect(appMocks.RestorePageVersion).toHaveBeenCalledWith(
        'Work',
        'Journal',
        'Daily',
        'v-new'
      )
    })
  })

  it('arms forceExternalReload before restore so a dirty buffer cannot overwrite', async () => {
    const order: string[] = []
    registerEditor({
      key: 'Work\x00Journal\x00Daily',
      isDirty: () => true,
      flush: async () => {
        order.push('flush')
        return true
      },
      forceExternalReload: () => {
        order.push('forceExternalReload')
      },
      clearExternalReload: () => {
        order.push('clearExternalReload')
      },
      setProposedEdit: () => false,
      clearProposedEdit: () => {},
      hasProposal: () => false,
      acceptProposedEdit: () => false,
      verifySelectionText: () => false
    })
    appMocks.RestorePageVersion.mockImplementation(async () => {
      order.push('RestorePageVersion')
    })
    renderModal()
    await waitFor(() => {
      expect(screen.getByTestId('page-history-restore')).toBeTruthy()
    })
    await fireEvent.click(screen.getByTestId('page-history-restore'))
    await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
    await waitFor(() => {
      expect(order).toEqual([
        'flush',
        'forceExternalReload',
        'RestorePageVersion'
      ])
    })
  })

  it('closes on Escape', async () => {
    const onClose = vi.fn()
    renderModal(onClose)
    await waitFor(() => {
      expect(screen.getByTestId('page-history-modal')).toBeTruthy()
    })
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('ignores a stale preview response after another version is selected', async () => {
    let releaseStale: ((body: string) => void) | undefined
    appMocks.GetPageVersion.mockImplementation(
      (_nb: string, _sec: string, _pg: string, id: string) => {
        if (id === 'v-new') {
          return new Promise<string>((resolve) => {
            releaseStale = resolve
          })
        }
        return Promise.resolve('# older body')
      }
    )
    renderModal()
    await waitFor(() => {
      expect(screen.getByRole('option', { name: /Source/i })).toBeTruthy()
    })
    await fireEvent.click(screen.getByRole('option', { name: /Source/i }))
    await waitFor(() => {
      expect(screen.getByTestId('page-history-preview-body')).toHaveTextContent(
        '# older body'
      )
    })
    releaseStale?.('# newest body')
    await waitFor(() => {
      expect(screen.getByTestId('page-history-preview-body')).toHaveTextContent(
        '# older body'
      )
    })
    await fireEvent.click(screen.getByTestId('page-history-restore'))
    await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
    await waitFor(() => {
      expect(appMocks.RestorePageVersion).toHaveBeenCalledWith(
        'Work',
        'Journal',
        'Daily',
        'v-old'
      )
    })
  })

  it('does not restore when the dirty buffer cannot be flushed', async () => {
    registerEditor({
      key: 'Work\x00Journal\x00Daily',
      isDirty: () => true,
      flush: async () => false,
      forceExternalReload: () => {},
      clearExternalReload: () => {},
      setProposedEdit: () => false,
      clearProposedEdit: () => {},
      hasProposal: () => false,
      acceptProposedEdit: () => false,
      verifySelectionText: () => false
    })
    renderModal()
    await waitFor(() => {
      expect(screen.getByTestId('page-history-restore')).toBeTruthy()
    })
    await fireEvent.click(screen.getByTestId('page-history-restore'))
    await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
    await waitFor(() => {
      expect(
        screen.getByText(/Couldn't save the current page before restoring/)
      ).toBeTruthy()
    })
    expect(appMocks.RestorePageVersion).not.toHaveBeenCalled()
  })

  it('disarms forceExternalReload when restore fails', async () => {
    const order: string[] = []
    registerEditor({
      key: 'Work\x00Journal\x00Daily',
      isDirty: () => false,
      flush: async () => true,
      forceExternalReload: () => {
        order.push('forceExternalReload')
      },
      clearExternalReload: () => {
        order.push('clearExternalReload')
      },
      setProposedEdit: () => false,
      clearProposedEdit: () => {},
      hasProposal: () => false,
      acceptProposedEdit: () => false,
      verifySelectionText: () => false
    })
    appMocks.RestorePageVersion.mockRejectedValue(new Error('disk full'))
    renderModal()
    await waitFor(() => {
      expect(screen.getByTestId('page-history-restore')).toBeTruthy()
    })
    await fireEvent.click(screen.getByTestId('page-history-restore'))
    await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
    await waitFor(() => {
      expect(order).toEqual(['forceExternalReload', 'clearExternalReload'])
    })
    expect(screen.getByText(/disk full/)).toBeTruthy()
  })

  it('lets keyboard users focus the preview pane', async () => {
    renderModal()
    await waitFor(() => {
      expect(screen.getByTestId('page-history-preview-body')).toBeTruthy()
    })
    expect(screen.getByTestId('page-history-preview-body')).toHaveAttribute(
      'tabindex',
      '0'
    )
  })

  it('keeps Restore disabled until the selected version has a preview', async () => {
    appMocks.GetPageVersion.mockRejectedValue(new Error('preview failed'))
    renderModal()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Retry' })).toBeTruthy()
    })
    expect(screen.getByTestId('page-history-restore')).toBeDisabled()
    appMocks.GetPageVersion.mockResolvedValue('# newest body')
    await fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    await waitFor(() => {
      expect(screen.getByTestId('page-history-preview-body')).toHaveTextContent(
        '# newest body'
      )
    })
    expect(screen.getByTestId('page-history-restore')).not.toBeDisabled()
  })

  it('states that snapshots are not a backup and shows 0 B', async () => {
    appMocks.ListPageVersions.mockResolvedValue([
      {
        id: 'v-empty',
        timestamp: '2026-08-16T18:00:00Z',
        source: 'editor',
        bytes: 0
      }
    ])
    appMocks.GetPageVersion.mockResolvedValue('')
    renderModal()
    await waitFor(() => {
      expect(screen.getByText('0 B')).toBeTruthy()
    })
    expect(
      screen.getByText(/are not encrypted, and are not a backup/)
    ).toBeTruthy()
  })

  it('shows an enable hint when history is off and empty', async () => {
    settingsMock.config.editor.auto_versioning_enabled = false
    appMocks.ListPageVersions.mockResolvedValue([])
    renderModal()
    await waitFor(() => {
      expect(screen.getByTestId('page-history-empty')).toBeTruthy()
    })
    expect(screen.getByText('No versions yet')).toBeTruthy()
    expect(
      screen.getByText(/Turn on Capture page history in Settings → Editor/)
    ).toBeTruthy()
  })
})
