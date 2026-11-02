// #1005 node-view conflict gates: the three attribute-writing NodeViews that
// shipped without a dedicated suite (MathNodeView's popover commit closure,
// CalloutBlockView's variant menu, CodeBlockView's language select) must
// refuse to dispatch while the editor is conflict-held (setEditable(false)) —
// updateAttributes dispatches regardless of isEditable, and the visible
// mutation would be destroyed by the armed reload. Ports the TaskBlockView
// conflict-hold pattern via the shared node-view harness.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mountNodeViewEditor } from '../../lib/editor/nodeview-test-harness'

const mocks = vi.hoisted(() => ({
  eventsOn: vi.fn(() => () => {}),
  highlightCode: vi.fn().mockResolvedValue(null),
  renderMermaid: vi.fn().mockResolvedValue({ svg: '', error: null }),
  renderKatex: vi.fn().mockResolvedValue({ html: '', error: null })
}))

vi.mock('$silt-app', () => createAppIpcMocks({}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: mocks.eventsOn
  },
  Call: { ByID: vi.fn(), ByName: vi.fn() },
  CancellablePromise: class {
    then() {
      return this
    }
    catch() {
      return this
    }
    finally() {
      return this
    }
  },
  Create: {
    Nullable: <T>(fn: T) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

// Shiki/KaTeX/Mermaid are heavyweight dynamic imports that only feed visual
// layers; stub them so the gating assertions stay fast and hermetic.
vi.mock('../../lib/editor/useShiki', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('../../lib/editor/useShiki')>()
  return { ...actual, highlightCode: mocks.highlightCode }
})
vi.mock('../../lib/editor/useMermaid', () => ({
  renderMermaid: mocks.renderMermaid
}))
vi.mock('../../lib/editor/useKatex', () => ({
  renderKatex: mocks.renderKatex
}))

// The harness accepts ParsedBlock[]; math/callout nodes have no ParsedBlock
// representation, so seed via setContent JSON (same node names/attrs the
// schema declares).
async function mountWithDoc(json: unknown) {
  const { editor, container, cleanup } = await mountNodeViewEditor([])
  editor.commands.setContent(json as never)
  await new Promise((r) => setTimeout(r, 0))
  return { editor, container, cleanup }
}

const DOC = {
  type: 'doc',
  content: [
    {
      type: 'paragraph',
      content: [
        { type: 'text', text: 'sum ' },
        { type: 'inlineMathNode', attrs: { latex: 'a+b' } }
      ]
    },
    {
      type: 'calloutBlock',
      attrs: { variant: 'note' },
      content: [
        {
          type: 'paragraph',
          content: [{ type: 'text', text: 'callout body' }]
        }
      ]
    },
    {
      type: 'codeBlock',
      attrs: { language: 'plaintext' },
      content: [{ type: 'text', text: 'const x = 1' }]
    }
  ]
}

const docJson = (editor: { getJSON: () => unknown }) =>
  JSON.stringify(editor.getJSON())

describe('node-view conflict gates (#1005)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('MathNodeView refuses the popover commit on a non-editable editor', async () => {
    const { editor, container, cleanup } = await mountWithDoc(DOC)
    const mathBtn = container.querySelector(
      'button.silt-math'
    ) as HTMLElement | null
    expect(mathBtn).toBeTruthy()

    const events: Array<(next: string) => void> = []
    const onEditMath = (e: Event): void => {
      events.push((e as CustomEvent).detail.onCommit)
    }
    window.addEventListener('silt:edit-math', onEditMath)
    try {
      mathBtn!.click()
      expect(events).toHaveLength(1)
      editor.setEditable(false)
      // The popover can outlive the hold that armed it (#1005): the commit
      // closure itself must refuse.
      events[0]!('c^2')
      await new Promise((r) => setTimeout(r, 0))
      expect(docJson(editor)).toContain('a+b')
      expect(docJson(editor)).not.toContain('c^2')

      // Positive control: the same closure dispatches on an editable editor.
      editor.setEditable(true)
      events[0]!('c^2')
      await new Promise((r) => setTimeout(r, 0))
      expect(docJson(editor)).toContain('c^2')
    } finally {
      window.removeEventListener('silt:edit-math', onEditMath)
      cleanup()
    }
  })

  it('CalloutBlockView refuses the variant change on a non-editable editor', async () => {
    const { editor, container, cleanup } = await mountWithDoc(DOC)
    const trigger = container.querySelector(
      'button.silt-callout-variant-btn'
    ) as HTMLElement | null
    expect(trigger).toBeTruthy()

    editor.setEditable(false)
    trigger!.click()
    await new Promise((r) => setTimeout(r, 0))
    // The menu is reachable; picking a DIFFERENT variant must be refused.
    const heldTip = Array.from(
      container.querySelectorAll('[role="menuitemradio"]')
    ).find((el) =>
      (el as HTMLElement).textContent?.toLowerCase().includes('tip')
    )
    expect(heldTip).toBeTruthy()
    ;(heldTip as HTMLElement).click()
    await new Promise((r) => setTimeout(r, 0))
    expect(docJson(editor)).toContain('"variant":"note"')
    expect(docJson(editor)).not.toContain('"variant":"tip"')

    // Positive control: on an editable editor the picker changes the variant.
    editor.setEditable(true)
    trigger!.click()
    await new Promise((r) => setTimeout(r, 0))
    const tip = Array.from(
      container.querySelectorAll('[role="menuitemradio"]')
    ).find((el) =>
      (el as HTMLElement).textContent?.toLowerCase().includes('tip')
    )
    expect(tip).toBeTruthy()
    ;(tip as HTMLElement).click()
    await new Promise((r) => setTimeout(r, 0))
    expect(docJson(editor)).toContain('"variant":"tip"')
    cleanup()
  })

  it('CodeBlockView refuses the language change on a non-editable editor', async () => {
    const { editor, container, cleanup } = await mountWithDoc(DOC)
    const select = container.querySelector(
      'select.silt-code-lang'
    ) as HTMLSelectElement | null
    expect(select).toBeTruthy()

    editor.setEditable(false)
    select!.value = 'go'
    select!.dispatchEvent(new Event('change', { bubbles: true }))
    await new Promise((r) => setTimeout(r, 0))
    expect(docJson(editor)).toContain('"language":"plaintext"')
    expect(docJson(editor)).not.toContain('"language":"go"')

    // Positive control.
    editor.setEditable(true)
    select!.value = 'go'
    select!.dispatchEvent(new Event('change', { bubbles: true }))
    await new Promise((r) => setTimeout(r, 0))
    expect(docJson(editor)).toContain('"language":"go"')
    cleanup()
  })
})
