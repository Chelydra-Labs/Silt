// Characterization tests for TipTapEditor's template-insert cluster (#769):
// the showTemplatePicker / pendingTemplateBlocks state machine and the seven
// handlers it gates (handleTemplateInsert, insertTemplateBlocks,
// confirmTemplateAtCursor, confirmTemplateAppend, cancelTemplateInsert,
// clearTemplateInsertDialog) plus the slash-menu `onShowTemplatePicker` bridge.
//
// These tests are written BEFORE the controller extraction and must stay GREEN
// unchanged afterwards — they lock the externally observable behaviour (what
// renders, what the editor ends up holding) rather than the internal layout.
//
// The mount harness mirrors TipTapEditor.context-menu.test.ts (the sibling
// test): same $silt-app / @wailsio/runtime / settings / dictionary mocks and
// the same jsdom stubs, extended with the template bindings the TemplatePicker
// reaches for (ListTemplates / RenderTemplate / RenderTemplateBlocks).

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, fireEvent, waitFor } from '@testing-library/svelte'
import TipTapEditor from './TipTapEditor.svelte'
import { mkBlock } from '../lib/editor/nodeview-test-harness'
import type { ParsedBlock } from '../lib/editor'
import { templatesState, _resetForTests } from '../templates/store.svelte'

// jsdom stubs: see TipTapEditor.context-menu.test.ts for the rationale
// (Placeholder viewport tracker + coordsAtPos via Range.getClientRects).
if (typeof document !== 'undefined' && !document.elementFromPoint) {
  document.elementFromPoint = () => document.body
}
if (
  typeof window !== 'undefined' &&
  window.Range &&
  !Range.prototype.getClientRects
) {
  const zeroRect: DOMRect = {
    x: 0,
    y: 0,
    left: 0,
    top: 0,
    right: 0,
    bottom: 0,
    width: 0,
    height: 0,
    toJSON() {
      return this
    }
  }
  Range.prototype.getClientRects = (() => [
    zeroRect
  ]) as unknown as typeof Range.prototype.getClientRects
  Range.prototype.getBoundingClientRect = () => zeroRect
}

const mocks = vi.hoisted(() => ({
  saveFileBlocks: vi.fn().mockResolvedValue(undefined),
  acquireFocusLock: vi.fn().mockResolvedValue(undefined),
  refreshFocusLock: vi.fn().mockResolvedValue(undefined),
  releaseFocusLock: vi.fn().mockResolvedValue(undefined),
  openDevTools: vi.fn().mockResolvedValue(undefined),
  distinctOwners: vi.fn().mockResolvedValue([]),
  queryTagHierarchy: vi.fn().mockResolvedValue([]),
  recordTagUsage: vi.fn().mockResolvedValue(undefined),
  eventsOn: vi.fn(() => () => {}),
  listTemplates: vi.fn().mockResolvedValue({ templates: [] }),
  renderTemplate: vi.fn().mockResolvedValue('# preview'),
  renderTemplateBlocks: vi.fn().mockResolvedValue([]),
  createPageFromTemplate: vi.fn().mockResolvedValue(undefined)
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    SaveFileBlocks: mocks.saveFileBlocks,
    AcquireFocusLock: mocks.acquireFocusLock,
    RefreshFocusLock: mocks.refreshFocusLock,
    ReleaseFocusLock: mocks.releaseFocusLock,
    OpenDevTools: mocks.openDevTools,
    DistinctOwners: mocks.distinctOwners,
    QueryTagHierarchy: mocks.queryTagHierarchy,
    RecordTagUsage: mocks.recordTagUsage,
    ListTemplates: mocks.listTemplates,
    RenderTemplate: mocks.renderTemplate,
    RenderTemplateBlocks: mocks.renderTemplateBlocks,
    CreatePageFromTemplate: mocks.createPageFromTemplate
  })
)

vi.mock('@wailsio/runtime', () => ({
  Events: { On: mocks.eventsOn },
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

const settingsMock = vi.hoisted(() => ({
  config: null as null | { ui?: { open_devtools_on_startup?: boolean } }
}))

vi.mock('../settings/store.svelte', () => ({
  settings: settingsMock,
  saveConfig: vi.fn(),
  appendDismissedTip: vi.fn()
}))

vi.mock('../theme/store.svelte', () => ({
  themeState: { mode: 'dark' }
}))

vi.mock('../notifications/store.svelte', () => ({
  pushNotification: vi.fn()
}))

vi.mock('../plugins/events', () => ({
  dispatch: vi.fn()
}))

vi.mock('../lib/perf/frame-budget', () => ({
  measureFrameBudget: vi.fn((_label: string, fn: () => unknown) => fn())
}))

vi.mock('../lib/editor/spellcheck/dictionary', () => ({
  loadDictionary: vi.fn().mockResolvedValue({ loaded: true }),
  isDictionaryLoaded: vi.fn(() => true),
  resetDictionary: vi.fn(),
  setCustomWords: vi.fn(),
  setDomainWords: vi.fn(),
  loadDomainPacks: vi.fn().mockResolvedValue(undefined),
  checkWord: vi.fn(() => true),
  ignoreWordSession: vi.fn(),
  suggest: vi.fn(() => []),
  getDictionaryLoadError: vi.fn(() => null),
  parseWordListText: vi.fn(() => [])
}))

// Marker text the mocked RenderTemplateBlocks yields. Distinctive enough that
// indexOf comparisons reliably tell cursor-insert from append ordering.
const TEMPLATE_TEXT = 'TPLINSERTED'
const templateSummary = {
  id: 'tpl-daily',
  title: 'Daily',
  category: 'Journal',
  source: 'builtin' as const,
  icon: 'today',
  description: '',
  placeholders: [],
  plugin_id: ''
}

type EditorLike = {
  isDestroyed: boolean
  commands: {
    focus: (pos?: string | number) => void
    insertContent: (content: string) => void
    setContent: (content: unknown) => void
    setTextSelection: (pos: number) => void
  }
  chain: () => {
    focus: () => { setTextSelection: (pos: number) => { run: () => void } }
    run: () => void
  }
  state: {
    doc: {
      textContent: string
      content: { size: number }
      forEach: (
        fn: (node: { textContent: string }, offset: number) => void
      ) => void
    }
    selection: { $from: { pos: number }; head: number }
  }
}

function getEditor(container: HTMLElement): EditorLike {
  const pm = container.querySelector('.ProseMirror') as unknown as {
    editor: EditorLike
  }
  if (!pm?.editor) throw new Error('editor not mounted')
  return pm.editor
}

function docText(editor: EditorLike): string {
  return editor.state.doc.textContent
}

/** Place the caret inside the first empty top-level block, or fall back to the
 *  start of the document. Used so `/template` lands cleanly at the start of an
 *  empty block (slash detection requires textBefore to start with `/`). */
function focusFirstEmptyBlock(editor: EditorLike): void {
  let pos = 1
  let found = false
  editor.state.doc.forEach((child, offset) => {
    if (!found && child.textContent === '') {
      pos = offset + 1
      found = true
    }
  })
  editor.chain().focus().setTextSelection(pos).run()
}

/** Render TipTapEditor and wait for ProseMirror to mount. */
async function mountEditor(
  blocks: ParsedBlock[]
): Promise<{ container: HTMLElement; unmount: () => void }> {
  const result = render(TipTapEditor, {
    props: {
      notebook: 'NB',
      section: 'S',
      page: 'P',
      blocks,
      onUpdate: () => {}
    }
  })
  await waitFor(() => {
    expect(result.container.querySelector('.ProseMirror')).toBeTruthy()
  })
  return { container: result.container, unmount: result.unmount }
}

/** Drive the `/template` slash command to open the template picker. Types the
 *  slash trigger into the current block, waits for the palette, and clicks the
 *  Template option — which deletes the trigger and fires onShowTemplatePicker. */
async function openTemplatePicker(container: HTMLElement): Promise<void> {
  const editor = getEditor(container)
  editor.commands.focus()
  editor.commands.insertContent('/template')
  await tick()

  await waitFor(() => {
    const palette = container.querySelector('[data-slash-palette]')
    expect(palette).toBeTruthy()
  })

  const options = Array.from(
    container.querySelectorAll<HTMLButtonElement>(
      '[data-slash-palette] button[role="option"]'
    )
  )
  const templateOption = options.find((btn) =>
    btn.textContent?.includes('Template')
  )
  expect(templateOption).toBeTruthy()
  await fireEvent.click(templateOption as HTMLButtonElement)

  await waitFor(() => {
    expect(
      document.querySelector('[role="dialog"][aria-label="Template picker"]')
    ).toBeTruthy()
  })
}

/** Click Insert in the open template picker. The auto-selected first template
 *  has no required placeholders, so confirm is immediately available. */
async function confirmInsertInPicker(): Promise<void> {
  const dialog = document.querySelector(
    '[role="dialog"][aria-label="Template picker"]'
  ) as HTMLElement | null
  expect(dialog).toBeTruthy()
  const insertBtn = Array.from(
    dialog!.querySelectorAll<HTMLButtonElement>('button')
  ).find((btn) => btn.textContent?.trim() === 'Insert')
  expect(insertBtn).toBeTruthy()
  await fireEvent.click(insertBtn as HTMLButtonElement)
}

function choiceDialog(): HTMLElement | null {
  return document.querySelector('[data-testid="template-insert-choice"]')
}

function clickChoice(which: 'primary' | 'secondary' | 'cancel'): void {
  const btn = document.querySelector(
    `[data-testid="template-insert-choice-${which}"]`
  ) as HTMLButtonElement | null
  expect(btn).toBeTruthy()
  btn!.click()
}

describe('TipTapEditor template-insert cluster', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    _resetForTests()
    templatesState.items = [templateSummary]
    mocks.renderTemplateBlocks.mockResolvedValue([
      mkBlock('NOTE', { clean_text: TEMPLATE_TEXT })
    ])
  })

  it('inserts at cursor on an empty editor without showing the choice dialog', async () => {
    const { container, unmount } = await mountEditor([
      mkBlock('NOTE', { clean_text: '' })
    ])
    const editor = getEditor(container)
    // Type the slash trigger at the start of the (empty) doc.
    editor.commands.focus()

    await openTemplatePicker(container)
    expect(docText(editor)).toBe('')

    mocks.renderTemplateBlocks.mockClear()
    await confirmInsertInPicker()

    // handleTemplateInsert bailed into the cursor path: no ChoiceDialog, and
    // the rendered template text now lives in the doc.
    await waitFor(() => {
      expect(docText(editor)).toContain(TEMPLATE_TEXT)
    })
    expect(choiceDialog()).toBeNull()

    // The picker closed after the insert callback fired.
    await waitFor(() => {
      expect(
        document.querySelector('[role="dialog"][aria-label="Template picker"]')
      ).toBeNull()
    })
    expect(mocks.renderTemplateBlocks).toHaveBeenCalledTimes(1)

    unmount()
  })

  it('opens the append-vs-cursor choice dialog when the page is non-empty and the caret is mid-doc', async () => {
    // Three blocks: content, an empty block to type `/template` in, and a
    // trailing block so the caret is provably NOT at doc end.
    const { container, unmount } = await mountEditor([
      mkBlock('NOTE', { clean_text: 'aaa' }),
      mkBlock('NOTE', { clean_text: '' }),
      mkBlock('NOTE', { clean_text: 'bbb' })
    ])
    const editor = getEditor(container)
    focusFirstEmptyBlock(editor)

    await openTemplatePicker(container)
    await confirmInsertInPicker()

    // Non-empty + caret not at end → pendingTemplateBlocks is set, the
    // ChoiceDialog renders.
    await waitFor(() => {
      expect(choiceDialog()).toBeTruthy()
    })
    expect(docText(editor)).not.toContain(TEMPLATE_TEXT)

    unmount()
  })

  it('confirmTemplateAtCursor inserts the pending blocks at the caret', async () => {
    const { container, unmount } = await mountEditor([
      mkBlock('NOTE', { clean_text: 'aaa' }),
      mkBlock('NOTE', { clean_text: '' }),
      mkBlock('NOTE', { clean_text: 'bbb' })
    ])
    const editor = getEditor(container)
    focusFirstEmptyBlock(editor)

    await openTemplatePicker(container)
    await confirmInsertInPicker()
    await waitFor(() => expect(choiceDialog()).toBeTruthy())

    clickChoice('primary')

    // Template text lands between 'aaa' and 'bbb' (cursor was in the middle
    // block) → TPLINSERTED precedes 'bbb' in the flattened text.
    await waitFor(() => {
      expect(docText(editor)).toContain(TEMPLATE_TEXT)
    })
    const text = docText(editor)
    expect(text.indexOf(TEMPLATE_TEXT)).toBeLessThan(text.indexOf('bbb'))
    // Dialog dismissed once a choice is taken.
    await waitFor(() => expect(choiceDialog()).toBeNull())

    unmount()
  })

  it('confirmTemplateAppend inserts the pending blocks at the doc end', async () => {
    const { container, unmount } = await mountEditor([
      mkBlock('NOTE', { clean_text: 'aaa' }),
      mkBlock('NOTE', { clean_text: '' }),
      mkBlock('NOTE', { clean_text: 'bbb' })
    ])
    const editor = getEditor(container)
    focusFirstEmptyBlock(editor)

    await openTemplatePicker(container)
    await confirmInsertInPicker()
    await waitFor(() => expect(choiceDialog()).toBeTruthy())

    clickChoice('secondary')

    await waitFor(() => {
      expect(docText(editor)).toContain(TEMPLATE_TEXT)
    })
    // Append puts the template AFTER 'bbb'.
    const text = docText(editor)
    expect(text.indexOf('bbb')).toBeLessThan(text.indexOf(TEMPLATE_TEXT))
    await waitFor(() => expect(choiceDialog()).toBeNull())

    unmount()
  })

  it('cancelTemplateInsert clears the pending dialog without inserting', async () => {
    const { container, unmount } = await mountEditor([
      mkBlock('NOTE', { clean_text: 'aaa' }),
      mkBlock('NOTE', { clean_text: '' }),
      mkBlock('NOTE', { clean_text: 'bbb' })
    ])
    const editor = getEditor(container)
    focusFirstEmptyBlock(editor)

    await openTemplatePicker(container)
    await confirmInsertInPicker()
    await waitFor(() => expect(choiceDialog()).toBeTruthy())

    clickChoice('cancel')

    await waitFor(() => expect(choiceDialog()).toBeNull())
    expect(docText(editor)).not.toContain(TEMPLATE_TEXT)

    unmount()
  })

  it('is a no-op when the editor is destroyed (guard)', async () => {
    const { container, unmount } = await mountEditor([
      mkBlock('NOTE', { clean_text: '' })
    ])
    const editor = getEditor(container)
    editor.commands.focus()

    await openTemplatePicker(container)
    // Tear the editor down before confirming. handleTemplateInsert must bail
    // on isDestroyed instead of dispatching into a dead view.
    ;(editor as unknown as { destroy: () => void }).destroy()
    await tick()

    mocks.renderTemplateBlocks.mockClear()
    await confirmInsertInPicker()

    // The IPC still fires (the picker owns that), but nothing lands in the
    // editor and no choice dialog opens.
    expect(mocks.renderTemplateBlocks).toHaveBeenCalledTimes(1)
    expect(choiceDialog()).toBeNull()

    unmount()
  })

  it('the /template slash command opens the template picker (onShowTemplatePicker bridge)', async () => {
    const { container, unmount } = await mountEditor([
      mkBlock('NOTE', { clean_text: '' })
    ])
    const editor = getEditor(container)
    editor.commands.focus()
    editor.commands.insertContent('/template')
    await tick()

    await waitFor(() => {
      expect(container.querySelector('[data-slash-palette]')).toBeTruthy()
    })

    const templateOption = Array.from(
      container.querySelectorAll<HTMLButtonElement>(
        '[data-slash-palette] button[role="option"]'
      )
    ).find((btn) => btn.textContent?.includes('Template'))
    expect(templateOption).toBeTruthy()
    await fireEvent.click(templateOption as HTMLButtonElement)

    await waitFor(() => {
      expect(
        document.querySelector('[role="dialog"][aria-label="Template picker"]')
      ).toBeTruthy()
    })
    // The slash trigger text was deleted by handleSlashSelect.
    expect(docText(editor)).toBe('')

    unmount()
  })
})
