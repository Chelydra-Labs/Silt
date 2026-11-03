import { describe, it, expect, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import ColorPickerMenu from './ColorPickerMenu.svelte'

function makeMockEditor(opts: { editable?: boolean } = {}) {
  const dispatch = vi.fn()
  return {
    // Editable by default; the #1005 conflict-hold case flips it to false.
    isEditable: opts.editable !== false,
    on: vi.fn(),
    off: vi.fn(),
    chain: () => ({
      focus: () => ({
        setMark: () => ({ run: dispatch }),
        unsetMark: () => ({ run: dispatch }),
        run: dispatch
      })
    }),
    _dispatch: dispatch
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

// #1005: a conflict-held editor is setEditable(false) — color marks must not
// dispatch into the buffer the armed reload would destroy.
describe('ColorPickerMenu conflict hold (#1005)', () => {
  it('disables the trigger and refuses mark dispatch on a non-editable editor', () => {
    const editor = makeMockEditor({ editable: false })
    const { container } = render(ColorPickerMenu, {
      props: { editor: editor as never, markType: 'textColor', isDark: false }
    })
    const trigger = container.querySelector('button') as HTMLButtonElement
    expect(trigger.disabled).toBe(true)
    // Even if a dispatch is forced past the disabled affordance, the
    // apply-level refusal holds.
    trigger.click()
    expect(editor._dispatch).not.toHaveBeenCalled()
  })
})
