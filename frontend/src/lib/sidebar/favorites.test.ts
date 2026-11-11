import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchFavoritePages, persistFavorite } from './favorites'

const mocks = vi.hoisted(() => ({
  getNavigationPreferences: vi.fn(),
  setFavoritePage: vi.fn()
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    GetNavigationPreferences: mocks.getNavigationPreferences,
    SetFavoritePage: mocks.setFavoritePage
  })
)

describe('favorites service (#1120 shared write path)', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('persists through SetFavoritePage and broadcasts the prefs change', async () => {
    const broadcast = vi.fn()
    window.addEventListener('navigation-preferences-changed', broadcast)
    mocks.setFavoritePage.mockResolvedValue(undefined)

    await persistFavorite(
      { notebook: 'Work', section: 'Journal', page: 'Daily' },
      true
    )

    expect(mocks.setFavoritePage).toHaveBeenCalledWith(
      'Work',
      'Journal',
      'Daily',
      true
    )
    expect(broadcast).toHaveBeenCalledTimes(1)
    window.removeEventListener('navigation-preferences-changed', broadcast)
  })

  it('does not broadcast when the write fails', async () => {
    const broadcast = vi.fn()
    window.addEventListener('navigation-preferences-changed', broadcast)
    mocks.setFavoritePage.mockRejectedValue(new Error('vault not loaded'))

    await expect(
      persistFavorite({ notebook: 'Work', section: '', page: 'Inbox' }, false)
    ).rejects.toThrow('vault not loaded')
    expect(broadcast).not.toHaveBeenCalled()
    window.removeEventListener('navigation-preferences-changed', broadcast)
  })

  it('fetchFavoritePages returns the narrow favorites slice', async () => {
    const favorites = [{ notebook: 'Work', section: '', page: 'Inbox' }]
    mocks.getNavigationPreferences.mockResolvedValue({
      favorites,
      recent_pages: [{ notebook: 'Work', section: '', page: 'Other' }],
      expanded_sections: []
    })

    await expect(fetchFavoritePages()).resolves.toEqual(favorites)
  })

  it('fetchFavoritePages tolerates a null preferences document', async () => {
    mocks.getNavigationPreferences.mockResolvedValue(null)
    await expect(fetchFavoritePages()).resolves.toEqual([])
  })
})
