// Regression tests for the inline atomic nodes' HTML identity. The converter
// (NodeJSON) path is covered in converters.test.ts, but the HTML clipboard /
// DnD path runs through ProseMirror's DOMParser, which matches each node's
// parseHTML() tag. Two nodes must NOT share a tag — a past review slipped a
// `data-type="mention"` collision onto BlockReferenceNode, which silently
// re-parsed every block-ref chip as a mention on paste. These tests pin the
// distinct tags via the real DOMParser so it can't recur.

import { describe, it, expect, afterEach } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import { DOMParser as PmDOMParser, Node as PmNode } from '@tiptap/pm/model'
import { SiltBlockExtensions } from './index'
import {
  BlockReferenceNode,
  CalloutBlock,
  CodeBlock,
  InlineMathNode,
  MentionNode,
  normalizeCalloutVariant
} from './schema'

function schemaFor() {
  const ed = new Editor({
    extensions: [
      StarterKit.configure({
        paragraph: false,
        heading: false,
        bulletList: false,
        orderedList: false,
        listItem: false,
        blockquote: false,
        codeBlock: false,
        horizontalRule: false,
        trailingNode: false
      }),
      ...SiltBlockExtensions,
      BlockReferenceNode,
      MentionNode
    ]
  })
  const schema = ed.schema
  ed.destroy()
  return schema
}

// Parse an inline chip wrapped in a NOTE block (the parse context the editor
// uses) and return the first inline child of that block.
function parseInlineChip(
  schema: ReturnType<typeof schemaFor>,
  chipHtml: string
) {
  const dom = document.createElement('div')
  dom.innerHTML = `<div data-type="note">${chipHtml}</div>`
  const doc = PmDOMParser.fromSchema(schema).parse(dom)
  const note = doc.firstChild
  return note?.firstChild ?? null
}

describe('inline atomic node HTML identity (no data-type collision)', () => {
  it('parses a mention chip as a mentionNode, not a blockReferenceNode (#184)', () => {
    const node = parseInlineChip(
      schemaFor(),
      '<span data-type="mention" data-name="Alice"></span>'
    )
    expect(node?.type.name).toBe('mentionNode')
    expect(node?.attrs.name).toBe('Alice')
  })

  it('parses a block-reference chip as a blockReferenceNode, not a mentionNode (#85)', () => {
    const UUID = 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee'
    const node = parseInlineChip(
      schemaFor(),
      `<span data-type="block-ref" data-uuid="${UUID}"></span>`
    )
    expect(node?.type.name).toBe('blockReferenceNode')
    expect(node?.attrs.uuid).toBe(UUID)
  })
})

// ---- InlineMathNode input rule (#328) ---------------------------------------
// Drives the inline `$...$` auto-trigger by simulating per-character typing
// through the same `handleTextInput` prop the real editor uses, so the input
// rule fires (or doesn't) at every keystroke — the same path a user takes.
// The matrix here is the load-bearing correctness contract: real inline math
// converts to an atomic inlineMathNode chip; currency, lone, and stray-``$``
// patterns stay literal.
const editors: Editor[] = []
function track(editor: Editor): Editor {
  editors.push(editor)
  return editor
}
afterEach(() => {
  while (editors.length) {
    const e = editors.pop()
    if (e && !e.isDestroyed) e.destroy()
  }
})

function makeMathEditor(): Editor {
  return track(
    new Editor({
      extensions: [
        StarterKit.configure({
          paragraph: false,
          heading: false,
          bulletList: false,
          orderedList: false,
          listItem: false,
          blockquote: false,
          codeBlock: false,
          horizontalRule: false,
          trailingNode: false
        }),
        ...SiltBlockExtensions,
        InlineMathNode
      ],
      content: {
        type: 'doc',
        content: [
          { type: 'noteBlock', attrs: { id: 'test-id', depth: 0, bullet: '' } }
        ]
      }
    })
  )
}

// Simulate typing text one character at a time, firing each plugin's
// `handleTextInput` (which is where TipTap's InputRule plugin lives). If a
// handler consumes the char (returns truthy), the input rule did the insert;
// otherwise we insert the char ourselves. This mirrors real keyboard input.
function typeText(editor: Editor, text: string): void {
  editor.commands.focus()
  for (const ch of text) {
    const pos = editor.state.selection.from
    const handled = editor.view.someProp('handleTextInput', (f) =>
      (
        f as (
          view: unknown,
          from: number,
          to: number,
          text: string
        ) => boolean | null
      )(editor.view, pos, pos, ch)
    )
    if (!handled) {
      editor.commands.insertContent(ch)
    }
  }
}

function countMathNodes(editor: Editor): number {
  let count = 0
  editor.state.doc.descendants((node) => {
    if (node.type.name === 'inlineMathNode') count += 1
  })
  return count
}

type FoundMath = { node: PmNode; pos: number }

function firstMathNode(editor: Editor): FoundMath | null {
  let found: FoundMath | null = null
  editor.state.doc.descendants((node, pos) => {
    if (!found && node.type.name === 'inlineMathNode') {
      found = { node, pos }
    }
  })
  return found
}

describe('InlineMathNode input rule — $...$ auto-trigger (#328)', () => {
  describe('converts real inline math', () => {
    const cases: Array<{ name: string; input: string; latex: string }> = [
      { name: 'simple equation $E=mc^2$', input: '$E=mc^2$', latex: 'E=mc^2' },
      { name: 'subscript $x_i$', input: '$x_i$', latex: 'x_i' },
      {
        name: 'fraction with braces $\\frac{a}{b}$',
        input: '$\\frac{a}{b}$',
        latex: '\\frac{a}{b}'
      }
    ]
    for (const c of cases) {
      it(`converts ${c.name} on the closing $`, () => {
        const editor = makeMathEditor()
        typeText(editor, c.input)

        const found = firstMathNode(editor)
        expect(found, 'expected an inlineMathNode to be created').not.toBeNull()
        // It's a NODE (atomic inline atom), not a Mark.
        expect(found!.node.type.name).toBe('inlineMathNode')
        expect(found!.node.isAtom).toBe(true)
        expect(found!.node.attrs.latex).toBe(c.latex)

        // The original `$...$` text is gone — only the chip remains.
        expect(editor.getText()).toBe('')
        expect(countMathNodes(editor)).toBe(1)

        // Caret lands just past the inserted chip (pos = chip pos + nodeSize).
        expect(editor.state.selection.empty).toBe(true)
        expect(editor.state.selection.from).toBe(
          found!.pos + found!.node.nodeSize
        )
      })
    }
  })

  describe('leaves currency / stray $ literal', () => {
    const cases: Array<{ name: string; input: string }> = [
      { name: 'trailing currency "5$ cash"', input: '5$ cash' },
      { name: 'lone opening "$5"', input: '$5' },
      {
        name: 'two currency amounts "cost $5 and $3"',
        input: 'cost $5 and $3'
      },
      { name: 'non-boundary dollars "a$b$c"', input: 'a$b$c' },
      { name: 'lone opening with text "$100 only"', input: '$100 only' }
    ]
    for (const c of cases) {
      it(`leaves ${c.name} as literal text`, () => {
        const editor = makeMathEditor()
        typeText(editor, c.input)

        expect(countMathNodes(editor)).toBe(0)
        expect(editor.getText()).toBe(c.input)
      })
    }

    it('leaves "a$b$c$" literal even with a closing $ (boundary check)', () => {
      // `a$b$c$` has a closing `$` so the rule DOES fire its regex engine —
      // but the opening `$` is preceded by a letter (`b`), so the lookbehind
      // `(?<![\p{L}\p{N}])` rejects it. This is the load-bearing boundary case.
      const editor = makeMathEditor()
      typeText(editor, 'a$b$c$')

      expect(countMathNodes(editor)).toBe(0)
      expect(editor.getText()).toBe('a$b$c$')
    })

    it('leaves "5$ cash$" literal even with a closing $ (digit boundary)', () => {
      // Closing `$` present, but the opening `$` follows a digit (`5`) —
      // rejected by the boundary lookbehind.
      const editor = makeMathEditor()
      typeText(editor, '5$ cash$')

      expect(countMathNodes(editor)).toBe(0)
      expect(editor.getText()).toBe('5$ cash$')
    })

    it('leaves math with internal spaces literal (heuristic 1: no spaces)', () => {
      // `$\int_0^1 x\,dx$` is real LaTeX but contains a literal space inside
      // the delimiters. The `[^\s$]+` content guard rejects it on purpose:
      // allowing spaces would let currency runs like `cost $5 and $3$` (with
      // a closing `$`) span the gap. Users type `/math` for spaced math.
      // Pinning this behavior so a future regex loosening shows up in review.
      const editor = makeMathEditor()
      typeText(editor, '$\\int_0^1 x\\,dx$')

      expect(countMathNodes(editor)).toBe(0)
      expect(editor.getText()).toBe('$\\int_0^1 x\\,dx$')
    })
  })

  it('converts math that follows a word + space (boundary passes on whitespace)', () => {
    // `energy is $E=mc^2$ here` — the opening `$` follows a space, which is a
    // valid token boundary. Confirms the chip can sit mid-sentence.
    const editor = makeMathEditor()
    typeText(editor, 'energy is $E=mc^2$ here')

    const found = firstMathNode(editor)
    expect(found).not.toBeNull()
    expect(found!.node.type.name).toBe('inlineMathNode')
    expect(found!.node.attrs.latex).toBe('E=mc^2')
    // Surrounding prose survives.
    expect(editor.getText()).toBe('energy is  here')
    expect(countMathNodes(editor)).toBe(1)
  })
})

// ---- Markdown-as-you-type input rules (#657) --------------------------------
function makeMarkdownRuleEditor(): Editor {
  return track(
    new Editor({
      extensions: [
        StarterKit.configure({
          // paragraph kept — calloutBlock content is block+ and seeds a paragraph.
          heading: false,
          bulletList: false,
          orderedList: false,
          listItem: false,
          blockquote: false,
          codeBlock: false,
          horizontalRule: false,
          trailingNode: false
        }),
        ...SiltBlockExtensions,
        CalloutBlock,
        CodeBlock
      ],
      content: {
        type: 'doc',
        content: [
          { type: 'noteBlock', attrs: { id: 'test-id', depth: 0, bullet: '' } }
        ]
      }
    })
  )
}

function firstBlockOfType(
  editor: Editor,
  type: string
): { name: string; attrs: Record<string, unknown> } | null {
  let found: { name: string; attrs: Record<string, unknown> } | null = null
  editor.state.doc.forEach((node) => {
    if (!found && node.type.name === type) {
      found = {
        name: node.type.name,
        attrs: { ...node.attrs } as Record<string, unknown>
      }
    }
  })
  return found
}

describe('markdown-as-you-type input rules (#657)', () => {
  it('normalizes callout variant aliases', () => {
    expect(normalizeCalloutVariant('WARNING')).toBe('warning')
    expect(normalizeCalloutVariant('error')).toBe('danger')
    expect(normalizeCalloutVariant('hint')).toBe('tip')
    expect(normalizeCalloutVariant('unknown-xyz')).toBe('note')
  })

  it('converts # / ## / ### prefixes to headerBlock depths', () => {
    for (const level of [1, 2, 3, 4, 5, 6] as const) {
      const editor = makeMarkdownRuleEditor()
      typeText(editor, `${'#'.repeat(level)} `)
      const header = firstBlockOfType(editor, 'headerBlock')
      expect(header, `expected header for level ${level}`).not.toBeNull()
      expect(header!.attrs.depth).toBe(level)
    }
  })

  it('undo after # space restores noteBlock (#657)', () => {
    const editor = makeMarkdownRuleEditor()
    typeText(editor, '# ')
    expect(firstBlockOfType(editor, 'headerBlock')).not.toBeNull()
    editor.commands.undo()
    const note = firstBlockOfType(editor, 'noteBlock')
    expect(note).not.toBeNull()
    expect(firstBlockOfType(editor, 'headerBlock')).toBeNull()
  })

  it('converts > space to a quote noteBlock', () => {
    const editor = makeMarkdownRuleEditor()
    typeText(editor, '> ')
    const note = firstBlockOfType(editor, 'noteBlock')
    expect(note).not.toBeNull()
    expect(note!.attrs.quote).toBe('> ')
    expect(note!.attrs.bullet).toBe('')
  })

  it('converts > then [!warning] space to calloutBlock (typed path)', () => {
    // Character-by-character: `> ` becomes a quote first, then `[!warning] `
    // promotes the quote note to a callout (#657).
    const editor = makeMarkdownRuleEditor()
    typeText(editor, '> [!warning] ')
    const callout = firstBlockOfType(editor, 'calloutBlock')
    expect(callout).not.toBeNull()
    expect(callout!.attrs.variant).toBe('warning')
  })

  it('converts pasted > [!tip] space to calloutBlock in one match', () => {
    const editor = makeMarkdownRuleEditor()
    editor.commands.focus()
    // Insert the full prefix then a trailing space via handleTextInput so the
    // full-line callout rule can match without intermediate quote conversion.
    editor.commands.insertContent('> [!tip]')
    const pos = editor.state.selection.from
    const handled = editor.view.someProp('handleTextInput', (f) =>
      (
        f as (
          view: unknown,
          from: number,
          to: number,
          text: string
        ) => boolean | null
      )(editor.view, pos, pos, ' ')
    )
    if (!handled) editor.commands.insertContent(' ')
    const callout = firstBlockOfType(editor, 'calloutBlock')
    expect(callout).not.toBeNull()
    expect(callout!.attrs.variant).toBe('tip')
  })

  it('converts triple-backtick fence to codeBlock with language', () => {
    const editor = makeMarkdownRuleEditor()
    typeText(editor, '```ts ')
    const code = firstBlockOfType(editor, 'codeBlock')
    expect(code).not.toBeNull()
    expect(code!.attrs.language).toBe('ts')
  })

  it('converts bare triple-backtick fence to codeBlock', () => {
    const editor = makeMarkdownRuleEditor()
    typeText(editor, '``` ')
    const code = firstBlockOfType(editor, 'codeBlock')
    expect(code).not.toBeNull()
    expect(code!.attrs.language).toBe('')
  })

  it('still converts bullet markers (regression)', () => {
    const editor = makeMarkdownRuleEditor()
    typeText(editor, '- ')
    const note = firstBlockOfType(editor, 'noteBlock')
    expect(note).not.toBeNull()
    expect(note!.attrs.bullet).toBe('- ')
  })
})
