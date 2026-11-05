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
import {
  SiltBlockExtensions,
  SiltInlineMarkExtensions,
  SiltColorMarkExtensions,
  SiltTableExtensions,
  SiltHardBreak
} from './index'
import {
  BlockReferenceNode,
  CalloutBlock,
  CodeBlock,
  InlineMathNode,
  MentionNode,
  normalizeCalloutVariant,
  siltPasteProps
} from './schema'
import { docToBlocks } from './converters'
import { PlainPaste, _getShiftHeldForTests } from './plainPaste'
import { PASTE_HTML_SIZE_CAP } from './paste/normalizePastedHTML'
import type { DocJSON } from './types'

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
        attrs: { ...node.attrs }
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

// ---- Underline mark (#1030) -------------------------------------------------
// StarterKit 3.x registers Underline unless explicitly disabled, and every Silt
// editor surface leaves that default intact. These tests pin the resulting mark
// through the real schema and the real paste pipeline so a future
// StarterKit/editor-config change cannot silently drop underline support (or
// tempt someone into registering a second Underline extension).
function makeUnderlineEditor(): Editor {
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
          trailingNode: false,
          hardBreak: false
        }),
        SiltHardBreak,
        ...SiltBlockExtensions,
        ...SiltInlineMarkExtensions,
        ...SiltColorMarkExtensions
      ],
      content: {
        type: 'doc',
        content: [
          {
            type: 'noteBlock',
            attrs: { id: 'underline-test', depth: 0, bullet: '' }
          }
        ]
      }
    })
  )
}

function underlinedText(editor: Editor): string[] {
  const found: string[] = []
  editor.state.doc.descendants((node) => {
    if (node.isText && node.marks.some((m) => m.type.name === 'underline')) {
      found.push(node.text ?? '')
    }
  })
  return found
}

describe('underline mark (#1030)', () => {
  it('exists in the real schema via StarterKit (single registration)', () => {
    const editor = makeUnderlineEditor()
    expect(editor.schema.marks.underline).toBeDefined()
    expect(Object.keys(editor.schema.marks)).toContain('underline')
    // Silt's own mark extension bundles must not re-register it: a duplicate
    // would warn and could shift schema mark rank / clean_text ordering.
    const siltMarkNames = [
      ...SiltInlineMarkExtensions,
      ...SiltColorMarkExtensions
    ].map((ext) => ext.name)
    expect(siltMarkNames).not.toContain('underline')
  })

  it('toggles on a selection and clears on a second toggle', () => {
    const editor = makeUnderlineEditor()
    editor.commands.insertContent('underlined')
    editor.commands.setTextSelection({ from: 1, to: 11 })
    expect(editor.isActive('underline')).toBe(false)

    editor.chain().focus().toggleMark('underline').run()
    expect(editor.isActive('underline')).toBe(true)
    const marked = (editor.getJSON() as DocJSON).content[0].content
    expect(marked?.find((n) => n.text === 'underlined')?.marks).toEqual([
      { type: 'underline' }
    ])

    editor.chain().focus().toggleMark('underline').run()
    expect(editor.isActive('underline')).toBe(false)
    const cleared = (editor.getJSON() as DocJSON).content[0].content
    expect(cleared?.find((n) => n.text === 'underlined')?.marks).toBeUndefined()
  })

  it('parses pasted <u>…</u> through the real paste pipeline', () => {
    const editor = makeUnderlineEditor()
    editor.view.pasteHTML('<u>from tag</u>')
    expect(underlinedText(editor)).toEqual(['from tag'])
  })

  it('parses pasted text-decoration: underline through the real paste pipeline', () => {
    const editor = makeUnderlineEditor()
    editor.view.pasteHTML(
      '<span style="text-decoration: underline">from style</span>'
    )
    expect(underlinedText(editor)).toEqual(['from style'])
  })
})

// ---- External leaf-tag parse rules (#1031) ---------------------------------
// External rich HTML names blocks by element (`<p>`, `<h1>`-`<h6>`, `<pre>`),
// not by Silt's serialized `div[data-type=…]` shape. These rules run through
// the real paste pipeline (`view.pasteHTML`) against the production-shaped
// schema: paragraph stays enabled because table cells require it. The shared
// paste helpers are wired in exactly as the two editor surfaces wire them, and
// PlainPaste is registered as on the main editor, so `pasteText`/`dispatchPaste`
// exercise the same code path (and handler precedence) as the real Ctrl+V.
function makeLeafTagEditor(
  content: DocJSON = {
    type: 'doc',
    content: [
      {
        type: 'noteBlock',
        attrs: { id: 'leaf-tag-test', depth: 0, bullet: '' }
      }
    ]
  }
): Editor {
  return track(
    new Editor({
      editorProps: siltPasteProps,
      extensions: [
        StarterKit.configure({
          // paragraph deliberately enabled — the `<p>` rule must beat it on
          // priority, and td/th must still get paragraph nodes.
          heading: false,
          bulletList: false,
          orderedList: false,
          listItem: false,
          blockquote: false,
          codeBlock: false,
          horizontalRule: false,
          trailingNode: false,
          hardBreak: false
        }),
        SiltHardBreak,
        ...SiltBlockExtensions,
        CalloutBlock,
        CodeBlock,
        ...SiltTableExtensions,
        ...SiltInlineMarkExtensions,
        ...SiltColorMarkExtensions,
        PlainPaste
      ],
      content
    })
  )
}

type FlatBlock = {
  type: string
  attrs: Record<string, unknown>
  text: string
  parent: string | null
}

function flatBlocks(editor: Editor): FlatBlock[] {
  const out: FlatBlock[] = []
  editor.state.doc.descendants((node, _pos, parent) => {
    if (!node.isBlock) return
    out.push({
      type: node.type.name,
      attrs: { ...node.attrs },
      text: node.textContent,
      parent: parent?.type.name ?? null
    })
  })
  return out
}

function findBlockByText(
  editor: Editor,
  type: string,
  text: string
): FlatBlock | undefined {
  return flatBlocks(editor).find((b) => b.type === type && b.text === text)
}

function firstNodeOfType(editor: Editor, type: string): PmNode | null {
  let found: PmNode | null = null
  editor.state.doc.descendants((node) => {
    if (!found && node.type.name === type) found = node
  })
  return found
}

// The editor starts from a seed noteBlock, so text assertions must select the
// pasted block by its content rather than by being the first of its type.
function nodeWithText(
  editor: Editor,
  type: string,
  text: string
): PmNode | null {
  let found: PmNode | null = null
  editor.state.doc.descendants((node) => {
    if (!found && node.type.name === type && node.textContent === text) {
      found = node
    }
  })
  return found
}

function inlineMarkNames(block: PmNode | null): string[][] {
  const marks: string[][] = []
  block?.forEach((node) => {
    if (node.isText) marks.push(node.marks.map((m) => m.type.name))
  })
  return marks
}

describe('external leaf-tag parse rules (#1031)', () => {
  it('parses <h1>…<h6> as headerBlock at the matching depth', () => {
    for (const level of [1, 2, 3, 4, 5, 6] as const) {
      const editor = makeLeafTagEditor()
      editor.view.pasteHTML(`<h${level}>Heading ${level}</h${level}>`)
      const header = findBlockByText(editor, 'headerBlock', `Heading ${level}`)
      expect(header, `expected h${level} to become a headerBlock`).toBeDefined()
      expect(header!.attrs.depth).toBe(level)
    }
  })

  it('keeps the serialized div[data-type=header] depth intact', () => {
    const editor = makeLeafTagEditor()
    editor.view.pasteHTML(
      '<div data-type="header" data-depth="4">Silt heading</div>'
    )
    const header = findBlockByText(editor, 'headerBlock', 'Silt heading')
    expect(header).toBeDefined()
    expect(header!.attrs.depth).toBe(4)
  })

  it('parses a top-level <p> as a noteBlock with default attrs', () => {
    const editor = makeLeafTagEditor()
    editor.view.pasteHTML('<p>plain paragraph</p>')
    const note = findBlockByText(editor, 'noteBlock', 'plain paragraph')
    expect(note).toBeDefined()
    expect(note!.attrs).toMatchObject({
      depth: 0,
      bullet: '',
      quote: '',
      align: 'left',
      file_date: ''
    })
    // The paragraph rule must not have produced a textual paragraph sibling.
    expect(
      flatBlocks(editor).filter(
        (b) => b.type === 'paragraph' && b.text.trim() !== ''
      )
    ).toEqual([])
  })

  it('keeps <p> inside table cells as paragraph nodes', () => {
    const editor = makeLeafTagEditor()
    editor.view.pasteHTML(
      '<table><tbody><tr><td><p>cell text</p></td>' +
        '<th><p>head text</p></th></tr></tbody></table>'
    )
    expect(firstNodeOfType(editor, 'tableCell')?.firstChild?.type.name).toBe(
      'paragraph'
    )
    expect(firstNodeOfType(editor, 'tableHeader')?.firstChild?.type.name).toBe(
      'paragraph'
    )
    // Cell text did not leak out as a top-level note.
    expect(findBlockByText(editor, 'noteBlock', 'cell text')).toBeUndefined()
  })

  it('keeps the serialized div[data-type=note] shape intact', () => {
    const editor = makeLeafTagEditor()
    editor.view.pasteHTML(
      '<div data-type="note" data-depth="2" data-bullet="- ">Silt note</div>'
    )
    const note = findBlockByText(editor, 'noteBlock', 'Silt note')
    expect(note).toBeDefined()
    expect(note!.attrs).toMatchObject({ depth: 2, bullet: '- ' })
  })

  it('parses <pre><code class="language-ts"> as a codeBlock with language', () => {
    const editor = makeLeafTagEditor()
    editor.view.pasteHTML(
      '<pre><code class="language-ts">const x = 1</code></pre>'
    )
    const code = findBlockByText(editor, 'codeBlock', 'const x = 1')
    expect(code, 'expected <pre> to become a codeBlock').toBeDefined()
    expect(code!.attrs.language).toBe('ts')
  })

  it('extracts the lang- class variant too', () => {
    const editor = makeLeafTagEditor()
    editor.view.pasteHTML(
      '<pre><code class="lang-rust">fn main() {}</code></pre>'
    )
    const code = findBlockByText(editor, 'codeBlock', 'fn main() {}')
    expect(code).toBeDefined()
    expect(code!.attrs.language).toBe('rust')
  })

  it('parses a bare <pre> as a codeBlock with no language', () => {
    const editor = makeLeafTagEditor()
    editor.view.pasteHTML('<pre>plain pre</pre>')
    const code = findBlockByText(editor, 'codeBlock', 'plain pre')
    expect(code).toBeDefined()
    expect(code!.attrs.language).toBe('')
  })

  it('keeps the serialized div[data-type=code] language intact', () => {
    const editor = makeLeafTagEditor()
    editor.view.pasteHTML(
      '<div data-type="code" data-language="go">fmt.Println()</div>'
    )
    const code = findBlockByText(editor, 'codeBlock', 'fmt.Println()')
    expect(code).toBeDefined()
    expect(code!.attrs.language).toBe('go')
  })

  it('keeps inline marks inside a pasted <p>', () => {
    const editor = makeLeafTagEditor()
    editor.view.pasteHTML(
      '<p>plain <strong>bold</strong> <em>italic</em> <u>under</u></p>'
    )
    const note = nodeWithText(editor, 'noteBlock', 'plain bold italic under')
    expect(note, 'expected the <p> to become a noteBlock').not.toBeNull()
    const marks = inlineMarkNames(note)
    expect(marks).toContainEqual(['bold'])
    expect(marks).toContainEqual(['italic'])
    expect(marks).toContainEqual(['underline'])
  })

  it('parses a span carrying both color and background-color as both marks', () => {
    // Tag rules are first-match-wins, so the old `span[style]` rules could
    // only ever produce one of the two marks. Style rules match per property.
    for (const style of [
      'color: #dc2626; background-color: #facc15',
      'background-color: #facc15; color: #dc2626'
    ]) {
      const editor = makeLeafTagEditor()
      editor.view.pasteHTML(`<p><span style="${style}">both</span></p>`)
      const note = nodeWithText(editor, 'noteBlock', 'both')
      const text = note?.child(0)
      expect(text?.text).toBe('both')
      const markNames = (text?.marks ?? []).map((m) => m.type.name)
      expect(markNames, `style="${style}"`).toContain('textColor')
      expect(markNames, `style="${style}"`).toContain('highlight')
      const color = text?.marks.find((m) => m.type.name === 'textColor')
      const background = text?.marks.find((m) => m.type.name === 'highlight')
      expect(color?.attrs.color).toBeTruthy()
      expect(background?.attrs.color).toBeTruthy()
    }
  })

  it('does not lose the color mark when only color is present', () => {
    const editor = makeLeafTagEditor()
    editor.view.pasteHTML('<p><span style="color: #dc2626">red</span></p>')
    const text = nodeWithText(editor, 'noteBlock', 'red')?.child(0)
    expect(text?.marks.map((m) => m.type.name)).toEqual(['textColor'])
  })

  it('does not lose the highlight mark when only background-color is present', () => {
    const editor = makeLeafTagEditor()
    editor.view.pasteHTML(
      '<p><span style="background-color: #facc15">marked</span></p>'
    )
    const text = nodeWithText(editor, 'noteBlock', 'marked')?.child(0)
    expect(text?.marks.map((m) => m.type.name)).toEqual(['highlight'])
  })

  it('does not turn inert CSS color keywords into marks', () => {
    // Styled page wrappers commonly carry inherited/transparent values; a mark
    // for them would render nothing useful (or hide the text) in Silt.
    for (const style of [
      'color: inherit; background-color: transparent',
      'color: currentColor; background-color: initial',
      'color: auto; background-color: windowtext'
    ]) {
      const editor = makeLeafTagEditor()
      editor.view.pasteHTML(
        `<div style="${style}"><p><span style="${style}">inert</span></p></div>`
      )
      const text = nodeWithText(editor, 'noteBlock', 'inert')?.child(0)
      expect(text?.text, `style="${style}"`).toBe('inert')
      expect(
        text?.marks.map((m) => m.type.name),
        `style="${style}"`
      ).toEqual([])
    }
  })

  it('gives each generic leaf tag to a single Silt block type', () => {
    // Duplicate generic tags would silently re-parse pasted blocks as the
    // wrong node type (the failure mode the inline-chip tests above guard).
    const schema = makeLeafTagEditor().schema
    const siltNames = new Set(
      [...SiltBlockExtensions, CalloutBlock, CodeBlock].map((ext) => ext.name)
    )
    const owners = (tag: string) =>
      Object.entries(schema.nodes)
        .filter(
          ([name, type]) =>
            siltNames.has(name) &&
            type.spec.parseDOM?.some(
              (rule) => 'tag' in rule && rule.tag === tag
            )
        )
        .map(([name]) => name)
    expect(owners('p')).toEqual(['noteBlock'])
    expect(owners('pre')).toEqual(['codeBlock'])
    expect(owners('h3')).toEqual(['headerBlock'])
  })
})

// ---- Text-only paste and paragraph-required containers ---------------------
// Ctrl+V of a clipboard that has only text/plain used to synthesize `<p>` per
// line, which the p rule turned into isolating noteBlocks (splitting the caret
// block, or leaving an empty block behind). These tests drive the real
// `view.pasteText`/`pasteHTML` paths with the shared editorProps wired in.
function textPos(editor: Editor, text: string): number {
  let pos = -1
  editor.state.doc.descendants((node, p) => {
    if (pos < 0 && node.isText && node.text === text) pos = p
  })
  if (pos < 0) throw new Error(`text not found: ${text}`)
  return pos
}

function noteDoc(text: string): DocJSON {
  return {
    type: 'doc',
    content: [
      {
        type: 'noteBlock',
        attrs: { id: 'paste-test', depth: 0, bullet: '' },
        content: text ? [{ type: 'text', text }] : []
      }
    ]
  }
}

function tableDoc(cellText: string): DocJSON {
  return {
    type: 'doc',
    content: [
      {
        type: 'table',
        content: [
          {
            type: 'tableRow',
            content: [
              {
                type: 'tableCell',
                content: [
                  {
                    type: 'paragraph',
                    content: cellText ? [{ type: 'text', text: cellText }] : []
                  }
                ]
              }
            ]
          }
        ]
      }
    ]
  }
}

function calloutDoc(title: string, body: string): DocJSON {
  return {
    type: 'doc',
    content: [
      {
        type: 'calloutBlock',
        attrs: { variant: 'note' },
        content: [
          { type: 'paragraph', content: [{ type: 'text', text: title }] },
          { type: 'paragraph', content: [{ type: 'text', text: body }] }
        ]
      }
    ]
  }
}

function childTypeNames(node: PmNode | null): string[] {
  const names: string[] = []
  node?.forEach((child) => names.push(child.type.name))
  return names
}

/** Dispatch a real DOM `paste` event so ProseMirror's own pipeline runs:
 *  `handleDOMEvents` hooks first, then `doPaste`'s `handlePaste` chain. jsdom
 *  has neither ClipboardEvent nor DataTransfer, so attach a minimal stand-in. */
function dispatchPaste(
  editor: Editor,
  data: { text?: string; html?: string }
): void {
  const event = new Event('paste', { bubbles: true, cancelable: true })
  Object.defineProperty(event, 'clipboardData', {
    value: {
      getData: (type: string) =>
        type === 'text/plain' || type === 'Text'
          ? (data.text ?? '')
          : (data.html ?? '')
    }
  })
  editor.view.dom.dispatchEvent(event)
}

/** jsdom's KeyboardEvent init drops the non-standard `keyCode`, which
 *  ProseMirror uses to track the shift modifier, so pin it explicitly. */
function dispatchKey(
  editor: Editor,
  type: 'keydown' | 'keyup',
  keyCode: number,
  shiftKey: boolean
): void {
  const event = new KeyboardEvent(type, { shiftKey, bubbles: true })
  Object.defineProperty(event, 'keyCode', { value: keyCode })
  editor.view.dom.dispatchEvent(event)
}

function hasMarkedText(editor: Editor, text: string, mark: string): boolean {
  let found = false
  editor.state.doc.descendants((node) => {
    if (
      !found &&
      node.isText &&
      node.text?.includes(text) &&
      node.marks.some((m) => m.type.name === mark)
    ) {
      found = true
    }
  })
  return found
}

describe('text-only paste (Ctrl+V) into paragraph-required containers', () => {
  it('inserts a single plain-text line inline at the caret', () => {
    const editor = makeLeafTagEditor(noteDoc('abcdef'))
    editor.commands.setTextSelection(textPos(editor, 'abcdef') + 3)
    editor.view.pasteText('X')

    expect(editor.state.doc.childCount).toBe(1)
    expect(editor.state.doc.firstChild?.type.name).toBe('noteBlock')
    expect(editor.state.doc.firstChild?.textContent).toBe('abcXdef')
  })

  it('inserts a single plain-text line into an empty note without a leftover block', () => {
    const editor = makeLeafTagEditor()
    editor.commands.setTextSelection(1)
    editor.view.pasteText('X')

    expect(editor.state.doc.childCount).toBe(1)
    expect(editor.state.doc.firstChild?.type.name).toBe('noteBlock')
    expect(editor.state.doc.firstChild?.textContent).toBe('X')
  })

  it('produces one block per line for multi-line plain text', () => {
    const editor = makeLeafTagEditor()
    editor.commands.setTextSelection(1)
    editor.view.pasteText('one\ntwo')

    const notes = flatBlocks(editor).filter((b) => b.type === 'noteBlock')
    expect(notes.map((b) => b.text)).toEqual(['one', 'two'])
    expect(editor.state.doc.childCount).toBe(2)
  })

  it('strips the trailing newline a terminal copy appends', () => {
    const editor = makeLeafTagEditor(noteDoc('abcdef'))
    editor.commands.setTextSelection(textPos(editor, 'abcdef') + 3)
    editor.view.pasteText('X\n')

    expect(editor.state.doc.firstChild?.textContent).toBe('abcXdef')
  })

  it('keeps plain-text paste inside a table cell paragraph-based', () => {
    const editor = makeLeafTagEditor(tableDoc('cell'))
    editor.commands.setTextSelection(textPos(editor, 'cell') + 2)
    editor.view.pasteText('X')

    const cell = firstNodeOfType(editor, 'tableCell')
    expect(cell?.firstChild?.type.name).toBe('paragraph')
    expect(cell?.textContent).toBe('ceXll')
    expect(firstNodeOfType(editor, 'noteBlock')).toBeNull()
  })

  it('keeps multi-line plain text inside a table cell paragraph-based', () => {
    const editor = makeLeafTagEditor(tableDoc(''))
    let paragraphPos = -1
    editor.state.doc.descendants((node, pos) => {
      if (paragraphPos < 0 && node.type.name === 'paragraph') paragraphPos = pos
    })
    editor.commands.setTextSelection(paragraphPos + 1)
    editor.view.pasteText('one\ntwo')

    const cell = firstNodeOfType(editor, 'tableCell')
    expect(childTypeNames(cell)).toEqual(['paragraph', 'paragraph'])
    expect(cell?.textContent).toBe('onetwo')
    expect(firstNodeOfType(editor, 'noteBlock')).toBeNull()
  })

  it('keeps the empty note marker when block content replaces it', () => {
    const editor = makeLeafTagEditor({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: {
            id: 'marked-note',
            depth: 1,
            bullet: '- ',
            quote: '',
            align: 'center',
            file_date: '2026-01-01'
          }
        }
      ]
    })
    editor.commands.setTextSelection(1)
    editor.view.pasteText('one\ntwo')

    const notes = flatBlocks(editor).filter((b) => b.type === 'noteBlock')
    expect(notes.map((b) => b.text)).toEqual(['one', 'two'])
    for (const note of notes) {
      expect(note.attrs.bullet).toBe('- ')
      expect(note.attrs.depth).toBe(1)
      expect(note.attrs.align).toBe('center')
      expect(note.attrs.file_date).toBe('2026-01-01')
    }
  })

  it('keeps a note whose only marker is its file_date', () => {
    // `file_date` is persisted identity, so it alone must route through the
    // inheritance branch; otherwise the replace drops it silently.
    const editor = makeLeafTagEditor({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'dated-note', file_date: '2026-01-01' }
        }
      ]
    })
    editor.commands.setTextSelection(1)
    editor.view.pasteText('one\ntwo')

    const notes = flatBlocks(editor).filter((b) => b.type === 'noteBlock')
    expect(notes.map((b) => b.text)).toEqual(['one', 'two'])
    for (const note of notes) {
      expect(note.attrs.file_date).toBe('2026-01-01')
    }
  })
})

describe('callout copy/paste keeps paragraph children', () => {
  it('parses internally serialized callout HTML as paragraphs and round-trips the title', () => {
    const editor = makeLeafTagEditor()
    // The shape ProseMirror's clipboard serializer produces for a callout.
    editor.view.pasteHTML(
      '<div data-type="callout" data-variant="warning"><p>Title</p><p>Body</p></div>'
    )

    const callout = firstNodeOfType(editor, 'calloutBlock')
    expect(callout, 'expected the callout to survive paste').not.toBeNull()
    expect(childTypeNames(callout)).toEqual(['paragraph', 'paragraph'])
    expect(
      flatBlocks(editor).filter(
        (b) => b.type === 'noteBlock' && b.text.trim() !== ''
      )
    ).toEqual([])

    const saved = docToBlocks(editor.getJSON() as DocJSON).find(
      (b) => b.type === 'CALLOUT'
    )
    expect(saved?.raw_text).toBe('> [!warning] Title\n> Body')
  })

  it('parses nested callout paragraphs as paragraphs too', () => {
    const editor = makeLeafTagEditor()
    editor.view.pasteHTML(
      '<div data-type="callout"><div data-type="callout"><p>Inner</p></div></div>'
    )

    const outer = firstNodeOfType(editor, 'calloutBlock')
    expect(childTypeNames(outer)).toEqual(['calloutBlock'])
    const inner = outer?.firstChild ?? null
    expect(childTypeNames(inner)).toEqual(['paragraph'])
    expect(
      flatBlocks(editor).filter(
        (b) => b.type === 'noteBlock' && b.text.trim() !== ''
      )
    ).toEqual([])
  })

  it('keeps partial HTML pasted into an existing callout paragraph-based', () => {
    const editor = makeLeafTagEditor(calloutDoc('Title', 'Body'))
    editor.commands.setTextSelection(textPos(editor, 'Body') + 4)
    editor.view.pasteHTML('<p>More</p>')

    const callout = firstNodeOfType(editor, 'calloutBlock')
    expect(childTypeNames(callout)).toEqual([
      'paragraph',
      'paragraph',
      'paragraph'
    ])
    expect(callout?.textContent).toBe('TitleBodyMore')
    expect(
      flatBlocks(editor).filter(
        (b) => b.type === 'noteBlock' && b.text.trim() !== ''
      )
    ).toEqual([])
  })
})

describe('HTML pasted into an existing table cell stays paragraph-based', () => {
  it('converts the parsed noteBlock into a paragraph at the insertion context', () => {
    const editor = makeLeafTagEditor(tableDoc('cell'))
    editor.commands.setTextSelection(textPos(editor, 'cell') + 2)
    editor.view.pasteHTML('<p>X</p>')

    const cell = firstNodeOfType(editor, 'tableCell')
    expect(childTypeNames(cell)).toEqual([
      'paragraph',
      'paragraph',
      'paragraph'
    ])
    expect(cell?.textContent).toBe('ceXll')
    expect(firstNodeOfType(editor, 'noteBlock')).toBeNull()
  })
})

// Ctrl+Shift+V × the shared block-paste path. `siltHandlePaste` is a direct
// editorProp, so ProseMirror consults it before PlainPaste's plugin handler;
// when it claims the paste PlainPaste never runs. The shift flag must still be
// consumed by the paste event itself, or the *next* keydown-less paste
// (context menu, middle click) is silently downgraded to plain text.
describe('plain paste flag × shared paste path', () => {
  it('multi-line Ctrl+Shift+V into an empty note does not make the next paste plain', () => {
    const editor = makeLeafTagEditor()
    editor.commands.setTextSelection(1)

    // Ctrl+Shift+V: keydown sets the shift flag, then the paste event carries
    // text-only multi-line clipboard data (no text/html).
    dispatchKey(editor, 'keydown', 86, true)
    dispatchPaste(editor, { text: 'one\ntwo' })

    expect(
      flatBlocks(editor)
        .filter((b) => b.type === 'noteBlock')
        .map((b) => b.text)
    ).toEqual(['one', 'two'])
    // The paste was claimed by siltHandlePaste, so PlainPaste's handlePaste
    // never ran — the flag still has to be clear at this point.
    expect(_getShiftHeldForTests()).toBe(false)
    dispatchKey(editor, 'keyup', 16, false)

    // Context-menu-style rich paste, no preceding keydown: the bold mark must
    // survive rather than being inserted as raw plain text.
    dispatchPaste(editor, { text: 'bolded', html: '<b>bolded</b>' })
    expect(hasMarkedText(editor, 'bolded', 'bold')).toBe(true)
  })
})

// ProseMirror parses text/html before handlePaste runs, so the size cap must be
// enforced by a DOM `paste` hook. Without it a pathological payload is still
// parsed on the main thread even though the normalizer returns it untouched.
describe('oversized clipboard HTML falls back to plain text', () => {
  const HUGE_HTML = `<h1>${'x'.repeat(PASTE_HTML_SIZE_CAP)}</h1>`

  it('inserts the text/plain payload instead of parsing the HTML', () => {
    const editor = makeLeafTagEditor()
    editor.commands.setTextSelection(1)
    dispatchPaste(editor, { text: 'plain fallback', html: HUGE_HTML })

    expect(editor.state.doc.textContent).toBe('plain fallback')
    expect(firstNodeOfType(editor, 'headerBlock')).toBeNull()
  })

  it('clears the shift flag so a following context-menu paste stays rich', () => {
    const editor = makeLeafTagEditor()
    editor.commands.setTextSelection(1)

    dispatchKey(editor, 'keydown', 86, true)
    dispatchPaste(editor, { text: 'plain fallback', html: HUGE_HTML })
    // The DOM hook that normally consumes the flag was preempted.
    expect(_getShiftHeldForTests()).toBe(false)
    dispatchKey(editor, 'keyup', 16, false)

    dispatchPaste(editor, { text: 'bolded', html: '<b>bolded</b>' })
    expect(hasMarkedText(editor, 'bolded', 'bold')).toBe(true)
  })
})
