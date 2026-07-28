import { beforeEach, describe, expect, it, vi } from 'vitest'
import { NavOrderManager, sortByName, type NavOrderState } from './navOrder'

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    GetNavOrder: vi.fn().mockResolvedValue({
      notebooks: ['Work', 'Personal'],
      sections: { Work: ['Journal', 'Projects'] },
      pages: { 'Work/Journal': ['2026-06-22', '2026-06-21'] }
    }),
    SetNavNotebookOrder: vi.fn().mockResolvedValue(undefined),
    SetNavSectionOrder: vi.fn().mockResolvedValue(undefined),
    SetNavPageOrder: vi.fn().mockResolvedValue(undefined),
    ClearNavNotebookOrder: vi.fn().mockResolvedValue(undefined),
    ClearNavSectionOrder: vi.fn().mockResolvedValue(undefined),
    ClearNavPageOrder: vi.fn().mockResolvedValue(undefined)
  })
)

describe('sortByName', () => {
  it('returns items in alphabetical order without a custom order', () => {
    const items = [{ name: 'C' }, { name: 'A' }, { name: 'B' }]
    expect(sortByName(items, []).map((item) => item.name)).toEqual([
      'A',
      'B',
      'C'
    ])
  })

  it('sorts configured items first and remaining items alphabetically', () => {
    const items = [{ name: 'C' }, { name: 'A' }, { name: 'B' }, { name: 'D' }]
    expect(sortByName(items, ['B', 'A']).map((item) => item.name)).toEqual([
      'B',
      'A',
      'C',
      'D'
    ])
    expect(items.map((item) => item.name)).toEqual(['C', 'A', 'B', 'D'])
  })
})

describe('NavOrderManager', () => {
  beforeEach(() => vi.clearAllMocks())

  it('loads all order scopes from IPC', async () => {
    let state: NavOrderState | null = null
    const manager = new NavOrderManager({
      onStateChange: (next) => (state = next)
    })

    await manager.load()

    expect(state!.notebooks).toEqual(['Work', 'Personal'])
    expect(state!.sections.Work).toEqual(['Journal', 'Projects'])
    expect(state!.pages['Work/Journal']).toEqual(['2026-06-22', '2026-06-21'])
  })

  it('persists notebook, nested section, and page scopes through narrow calls', async () => {
    const bindings = await import('$silt-app')
    const manager = new NavOrderManager({ onStateChange: () => {} })
    await manager.load()

    await manager.persistNotebookOrder(['Personal', 'Work'])
    await manager.persistSectionOrder('Work', 'Projects', ['Current', 'Active'])
    await manager.persistPageOrder('Work', 'Journal', [
      '2026-06-21',
      '2026-06-22'
    ])

    expect(bindings.SetNavNotebookOrder).toHaveBeenCalledWith([
      'Personal',
      'Work'
    ])
    expect(bindings.SetNavSectionOrder).toHaveBeenCalledWith(
      'Work',
      'Projects',
      ['Current', 'Active']
    )
    expect(bindings.SetNavPageOrder).toHaveBeenCalledWith('Work', 'Journal', [
      '2026-06-21',
      '2026-06-22'
    ])
  })

  it('keeps different order scopes independent at the IPC boundary', async () => {
    const bindings = await import('$silt-app')
    const manager = new NavOrderManager({ onStateChange: () => {} })
    await manager.load()

    await Promise.all([
      manager.persistSectionOrder('Work', '', ['Projects', 'Journal']),
      manager.persistSectionOrder('Personal', '', ['Home', 'Archive']),
      manager.persistPageOrder('Work', '', ['Inbox', 'README'])
    ])

    expect(bindings.SetNavSectionOrder).toHaveBeenNthCalledWith(1, 'Work', '', [
      'Projects',
      'Journal'
    ])
    expect(bindings.SetNavSectionOrder).toHaveBeenNthCalledWith(
      2,
      'Personal',
      '',
      ['Home', 'Archive']
    )
    expect(bindings.SetNavPageOrder).toHaveBeenCalledWith('Work', '', [
      'Inbox',
      'README'
    ])
    expect(bindings.SetNavNotebookOrder).not.toHaveBeenCalled()
    expect(manager.current.sections.Work).toEqual(['Projects', 'Journal'])
    expect(manager.current.sections.Personal).toEqual(['Home', 'Archive'])
    expect(manager.current.pages['Work/']).toEqual(['Inbox', 'README'])
  })

  it('uses narrow clear calls for empty explicit orders', async () => {
    const bindings = await import('$silt-app')
    const manager = new NavOrderManager({ onStateChange: () => {} })
    await manager.load()

    await manager.persistNotebookOrder([])
    await manager.persistSectionOrder('Work', '', [])
    await manager.persistPageOrder('Work', 'Journal', [])

    expect(bindings.ClearNavNotebookOrder).toHaveBeenCalledOnce()
    expect(bindings.ClearNavSectionOrder).toHaveBeenCalledWith('Work', '')
    expect(bindings.ClearNavPageOrder).toHaveBeenCalledWith('Work', 'Journal')
  })

  it('rolls back only the failed scope', async () => {
    const bindings = await import('$silt-app')
    vi.mocked(bindings.SetNavSectionOrder).mockRejectedValueOnce(
      new Error('save failed')
    )
    const manager = new NavOrderManager({ onStateChange: () => {} })
    await manager.load()
    await manager.persistPageOrder('Work', '', ['README', 'Inbox'])

    await manager.persistSectionOrder('Work', '', ['Projects', 'Journal'])

    expect(manager.current.sections.Work).toEqual(['Journal', 'Projects'])
    expect(manager.current.pages['Work/']).toEqual(['README', 'Inbox'])
  })

  it('leaves defaults in place when loading fails', async () => {
    const { GetNavOrder } = await import('$silt-app')
    vi.mocked(GetNavOrder).mockRejectedValueOnce(new Error('no vault'))
    const manager = new NavOrderManager({ onStateChange: () => {} })
    await manager.load()
    expect(manager.current).toEqual({ notebooks: [], sections: {}, pages: {} })
  })
})
