import { fireEvent, render, waitFor } from '@testing-library/svelte'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SpellcheckMenu from './SpellcheckMenu.svelte'

const mocks = vi.hoisted(() => ({
  suggest: vi.fn<(word: string) => string[]>(),
  ignoreWordSession: vi.fn(),
  requestSpellcheckRecheck: vi.fn(),
  add: vi.fn()
}))

vi.mock('../../lib/editor/spellcheck/dictionary', () => ({
  suggest: mocks.suggest,
  ignoreWordSession: mocks.ignoreWordSession
}))

vi.mock('../../lib/editor/spellcheck/SpellcheckExtension', () => ({
  requestSpellcheckRecheck: mocks.requestSpellcheckRecheck
}))

vi.mock('../../lib/editor/spellcheck/customDictionary.svelte', () => ({
  customDictionary: { add: mocks.add }
}))

function setup() {
  const chain = {
    focus: vi.fn(),
    deleteRange: vi.fn(),
    insertContentAt: vi.fn(),
    run: vi.fn()
  }
  chain.focus.mockReturnValue(chain)
  chain.deleteRange.mockReturnValue(chain)
  chain.insertContentAt.mockReturnValue(chain)

  const editor = {
    chain: vi.fn(() => chain),
    commands: { focus: vi.fn() }
  }
  const onClose = vi.fn()
  const view = render(SpellcheckMenu, {
    props: {
      editor: editor as never,
      word: 'mispelt',
      range: { from: 2, to: 9 },
      anchor: { x: 20, y: 30 },
      onClose
    }
  })

  return { ...view, editor, onClose }
}

describe('SpellcheckMenu keyboard focus', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.suggest.mockReturnValue([])
    mocks.add.mockResolvedValue(undefined)
  })

  it('focuses Add when there are no suggestions and wraps across enabled actions', async () => {
    const { getByRole } = setup()
    const noSuggestions = getByRole('menuitem', { name: 'No suggestions' })
    const add = getByRole('menuitem', { name: 'Add to dictionary' })
    const ignore = getByRole('menuitem', { name: 'Ignore' })

    await waitFor(() => expect(add).toHaveFocus())
    await fireEvent.keyDown(window, { key: 'ArrowUp' })
    expect(ignore).toHaveFocus()
    await fireEvent.keyDown(window, { key: 'ArrowDown' })
    expect(add).toHaveFocus()
    expect(noSuggestions).toBeDisabled()
  })

  it('focuses the first suggestion and wraps suggestion navigation', async () => {
    mocks.suggest.mockReturnValue(['misspelt', 'misspelled'])
    const { getByRole } = setup()
    const first = getByRole('menuitem', { name: 'Replace with misspelt' })
    const ignore = getByRole('menuitem', { name: 'Ignore' })

    await waitFor(() => expect(first).toHaveFocus())
    await fireEvent.keyDown(window, { key: 'ArrowUp' })
    expect(ignore).toHaveFocus()
    await fireEvent.keyDown(window, { key: 'ArrowDown' })
    expect(first).toHaveFocus()
  })

  it.each([
    ['Escape', async () => fireEvent.keyDown(window, { key: 'Escape' })],
    [
      'apply',
      async (getByRole: ReturnType<typeof setup>['getByRole']) => {
        await fireEvent.click(
          getByRole('menuitem', { name: 'Replace with misspelt' })
        )
      }
    ],
    [
      'add',
      async (getByRole: ReturnType<typeof setup>['getByRole']) => {
        await fireEvent.click(
          getByRole('menuitem', { name: 'Add to dictionary' })
        )
      }
    ],
    [
      'ignore',
      async (getByRole: ReturnType<typeof setup>['getByRole']) => {
        await fireEvent.click(getByRole('menuitem', { name: 'Ignore' }))
      }
    ]
  ])('restores editor focus after %s', async (_action, act) => {
    mocks.suggest.mockReturnValue(['misspelt'])
    const view = setup()

    await act(view.getByRole)

    await waitFor(() => {
      expect(view.onClose).toHaveBeenCalledOnce()
      expect(view.editor.commands.focus).toHaveBeenCalledOnce()
    })
  })

  it('closes when the backdrop is clicked', async () => {
    const { getByLabelText, onClose, editor } = setup()
    await fireEvent.click(getByLabelText('Close spelling suggestions'))
    expect(onClose).toHaveBeenCalledOnce()
    expect(editor.commands.focus).toHaveBeenCalledOnce()
  })

  it('closes on backdrop right-click and stops propagation to the editor host', async () => {
    const { getByLabelText, onClose, editor } = setup()
    const backdrop = getByLabelText('Close spelling suggestions')
    const event = new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true
    })
    const stopSpy = vi.spyOn(event, 'stopPropagation')
    backdrop.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
    expect(stopSpy).toHaveBeenCalledOnce()
    expect(onClose).toHaveBeenCalledOnce()
    expect(editor.commands.focus).toHaveBeenCalledOnce()
  })

  it('closes on window resize', async () => {
    const { onClose, editor } = setup()
    window.dispatchEvent(new Event('resize'))
    expect(onClose).toHaveBeenCalledOnce()
    expect(editor.commands.focus).toHaveBeenCalledOnce()
  })

  it('closes on document scroll (capture)', async () => {
    const { onClose, editor } = setup()
    document.dispatchEvent(new Event('scroll', { bubbles: true }))
    expect(onClose).toHaveBeenCalledOnce()
    expect(editor.commands.focus).toHaveBeenCalledOnce()
  })

  it('portals the menu layer to document.body (escapes note zoom)', async () => {
    const { container, getByRole } = setup()
    const menu = getByRole('menu', { name: 'Spelling suggestions' })
    // Layer must live under body, not under the render container (which sits
    // inside .note-page-zoom in production and would scale fixed coords).
    expect(document.body.contains(menu)).toBe(true)
    expect(container.contains(menu)).toBe(false)
    expect(menu.style.left).toBe('20px')
    expect(menu.style.top).toBe('30px')
  })
})
