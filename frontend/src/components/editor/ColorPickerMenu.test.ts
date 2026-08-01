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

  it('renders a hover tooltip (title) on the trigger for both mark types', () => {
    // Every other toolbar button exposes a title tooltip; the color triggers
    // must too (#856-adjacent a11y/UX gap). The title mirrors the aria-label.
    // Scope to each render's container so two renders in one test don't both
    // match a global button query.
    const cases = [
      { markType: 'textColor', label: 'Text color' },
      { markType: 'highlight', label: 'Background color' }
    ] as const
    for (const { markType, label } of cases) {
      const editor = makeMockEditor()
      const { container } = render(ColorPickerMenu, {
        props: { editor: editor as never, markType, isDark: false }
      })
      const trigger = container.querySelector('button')
      expect(trigger).not.toBeNull()
      expect(trigger?.getAttribute('title')).toBe(label)
      expect(trigger?.getAttribute('aria-label')).toBe(label)
    }
  })
})
