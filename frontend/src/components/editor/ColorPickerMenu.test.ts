import { describe, it, expect } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import ColorPickerMenu from './ColorPickerMenu.svelte'

function makeMockEditor() {
  return {
    chain: () => ({
      focus: () => ({
        setMark: () => ({ run: () => {} }),
        unsetMark: () => ({ run: () => {} })
      })
    })
  }
}

describe('ColorPickerMenu', () => {
  it('opens the color palette on trigger click', async () => {
    const editor = makeMockEditor()
    const { getByRole, getAllByRole } = render(ColorPickerMenu, {
      props: { editor: editor as never, markType: 'textColor', isDark: false }
    })
    await fireEvent.click(getByRole('button'))
    expect(getAllByRole('menuitem').length).toBeGreaterThan(0)
  })

  it('closes the menu on click outside', async () => {
    const editor = makeMockEditor()
    const { getByRole, queryAllByRole } = render(ColorPickerMenu, {
      props: { editor: editor as never, markType: 'textColor', isDark: false }
    })
    await fireEvent.click(getByRole('button'))
    expect(queryAllByRole('menuitem').length).toBeGreaterThan(0)

    await fireEvent.click(document.body)

    expect(queryAllByRole('menuitem')).toHaveLength(0)
  })
})
