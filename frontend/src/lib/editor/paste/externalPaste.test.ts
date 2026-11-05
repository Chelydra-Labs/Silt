// Real-editor pastes: the normalizer is only useful if the full Silt schema +
// ProseMirror clipboard pipeline turn the normalized HTML into the block and
// mark model the rest of the app stores. `view.pasteHTML` is the same pipeline
// native Ctrl+V uses (transformPastedHTML included), so these assertions cover
// the contract without a browser e2e run.

import { afterEach, describe, expect, it } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import type { JSONContent } from '@tiptap/core'
import {
  SiltBlockExtensions,
  SiltInlineMarkExtensions,
  SiltColorMarkExtensions,
  SiltDetailsExtensions,
  SiltTableExtensions,
  SiltHardBreak,
  UniqueBlockIds,
  ExternalPasteNormalizer,
  docToBlocks
} from '../index'
import { CodeBlock, siltPasteProps } from '../schema'
import type { DocJSON } from '../types'
import {
  BLOCKQUOTE_BODY,
  CODE_BLOCK,
  CSS_ONLY_EMPHASIS,
  GENUINE_TABLE,
  LAYOUT_TABLE_BODY,
  MSO_PSEUDO_LIST,
  NESTED_ORDERED_LIST,
  OUTLOOK_BODY,
  SHADED_CONTAINER,
  SILT_INTERNAL_CLIPBOARD
} from './fixtures'

const editors: Editor[] = []

function makeEditor(): Editor {
  const editor = new Editor({
    extensions: [
      StarterKit.configure({
        heading: false,
        bulletList: false,
        orderedList: false,
        listItem: false,
        blockquote: false,
        codeBlock: false,
        horizontalRule: false,
        trailingNode: false,
        hardBreak: false,
        link: { openOnClick: false, autolink: true }
      }),
      SiltHardBreak,
      ...SiltBlockExtensions,
      CodeBlock,
      ...SiltInlineMarkExtensions,
      ...SiltColorMarkExtensions,
      ...SiltDetailsExtensions,
      ...SiltTableExtensions,
      ExternalPasteNormalizer,
      UniqueBlockIds
    ],
    editorProps: siltPasteProps,
    content: {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'seed', depth: 0, bullet: '', quote: '' },
          content: [{ type: 'text', text: 'seed' }]
        }
      ]
    }
  })
  editors.push(editor)
  return editor
}

afterEach(() => {
  while (editors.length) {
    const editor = editors.pop()
    if (editor && !editor.isDestroyed) editor.destroy()
  }
})

function paste(editor: Editor, html: string): JSONContent[] {
  editor.commands.selectAll()
  editor.view.pasteHTML(html)
  return editor.getJSON().content ?? []
}

function noteAttrs(node: JSONContent): Record<string, unknown> {
  return node.attrs ?? {}
}

function textOf(node: JSONContent): string {
  if (node.text) return node.text
  return (node.content ?? []).map(textOf).join('')
}

// Word bodies often end in one empty paragraph (kept by the spacer rule).
function meaningful(nodes: JSONContent[]): JSONContent[] {
  return nodes.filter(
    (node) =>
      (node.type !== 'paragraph' && node.type !== 'noteBlock') ||
      textOf(node)
        .replace(/\u00a0/g, ' ')
        .trim() !== ''
  )
}

function marksOf(nodes: JSONContent[]): { type: string; attrs?: unknown }[] {
  const marks: { type: string; attrs?: unknown }[] = []
  for (const node of nodes) {
    for (const mark of node.marks ?? []) marks.push(mark)
    if (node.content) marks.push(...marksOf(node.content))
  }
  return marks
}

function textRuns(nodes: JSONContent[]): { text: string; marks: string[] }[] {
  const runs: { text: string; marks: string[] }[] = []
  const walk = (list: JSONContent[]) => {
    for (const node of list) {
      if (typeof node.text === 'string') {
        runs.push({
          text: node.text,
          marks: (node.marks ?? []).map((mark) => mark.type)
        })
      }
      if (node.content) walk(node.content)
    }
  }
  walk(nodes)
  return runs
}

function blockIds(nodes: JSONContent[]): (string | null)[] {
  return nodes.map((node) => (node.attrs?.id as string | null) ?? null)
}

function markdownOf(nodes: JSONContent[]): string {
  return docToBlocks({ type: 'doc', content: nodes } as unknown as DocJSON)
    .map((block) => block.clean_text)
    .join('\n')
}

describe('external paste through the real Silt schema', () => {
  it('pastes an Outlook body as header + marked prose + bullet notes', () => {
    const editor = makeEditor()
    const nodes = meaningful(paste(editor, OUTLOOK_BODY))

    expect(nodes[0].type).toBe('headerBlock')
    expect(noteAttrs(nodes[0]).depth).toBe(2)
    expect(textOf(nodes[0])).toBe('Q3 Planning')

    const body = nodes[1]
    expect(body.type).toBe('noteBlock')
    expect(textOf(body)).toContain('Bold launch')
    expect(textOf(body)).toContain('body text')
    const marks = marksOf([body]).map((m) => m.type)
    expect(marks).toContain('bold')
    expect(marks).toContain('italic')

    const bullets = nodes.filter((n) => noteAttrs(n).bullet === '- ')
    expect(bullets).toHaveLength(2)
    expect(bullets.map(textOf)).toEqual(['Ship the beta', 'Write the docs'])
    expect(bullets.map((n) => noteAttrs(n).depth)).toEqual([0, 0])
  })

  it('keeps ordered-list ordinals and nested bullets with depth', () => {
    const nodes = meaningful(paste(makeEditor(), NESTED_ORDERED_LIST))
    expect(
      nodes.map((n) => [noteAttrs(n).depth, noteAttrs(n).bullet, textOf(n)])
    ).toEqual([
      [0, '3. ', 'Install the app'],
      [0, '4. ', 'Configure it'],
      [1, '- ', 'Windows'],
      [1, '- ', 'Linux'],
      [0, '5. ', 'Restart']
    ])
  })

  it('unwraps a layout-table body instead of creating a table block', () => {
    const nodes = meaningful(paste(makeEditor(), LAYOUT_TABLE_BODY))
    expect(nodes.some((n) => n.type === 'table')).toBe(false)
    expect(nodes.map((n) => n.type)).toEqual([
      'headerBlock',
      'noteBlock',
      'noteBlock'
    ])
    expect(nodes.map(textOf)).toEqual([
      'Wrapped title',
      'Body living inside a layout cell.',
      'Nested layout row'
    ])
  })

  it('flattens mso-list pseudo paragraphs to notes at the right depth', () => {
    const nodes = meaningful(paste(makeEditor(), MSO_PSEUDO_LIST))
    expect(
      nodes.map((n) => [noteAttrs(n).depth, noteAttrs(n).bullet, textOf(n)])
    ).toEqual([
      [0, '- ', 'Alpha'],
      [1, '- ', 'Beta'],
      [0, '- ', 'Gamma'],
      [0, '1. ', 'First numbered'],
      [0, '2. ', 'Second numbered']
    ])
  })

  it('derives bold/italic/underline from CSS-only emphasis', () => {
    const nodes = meaningful(paste(makeEditor(), CSS_ONLY_EMPHASIS))
    const runs = (nodes[0].content ?? []).filter((n) => n.type === 'text')
    const byText = new Map(runs.map((n) => [n.text, n.marks ?? []]))
    expect(byText.get('Bold by CSS')?.map((m) => m.type)).toEqual(['bold'])
    expect(byText.get('Italic by CSS')?.map((m) => m.type)).toEqual(['italic'])
    expect(byText.get('Underlined by CSS')?.map((m) => m.type)).toEqual([
      'underline'
    ])
    expect(
      byText
        .get('All three')
        ?.map((m) => m.type)
        .sort()
    ).toEqual(['bold', 'italic', 'underline'])
  })

  it('maps blockquotes onto note quote attrs', () => {
    const nodes = meaningful(paste(makeEditor(), BLOCKQUOTE_BODY))
    expect(
      nodes.map((n) => [noteAttrs(n).quote, noteAttrs(n).bullet, textOf(n)])
    ).toEqual([
      ['> ', '', 'Quoted first'],
      ['> ', '', 'Quoted second with bold'],
      ['>> ', '', 'Nested quote']
    ])
  })

  it('pastes a fenced code block with its language', () => {
    const nodes = meaningful(paste(makeEditor(), CODE_BLOCK))
    expect(nodes).toHaveLength(1)
    expect(nodes[0].type).toBe('codeBlock')
    expect(noteAttrs(nodes[0]).language).toBe('ts')
    expect(textOf(nodes[0])).toBe('const answer = 42')
  })

  it('keeps inline run shading but drops container shading (#1034)', () => {
    const nodes = meaningful(paste(makeEditor(), SHADED_CONTAINER))
    const text = textOf({ type: 'doc', content: nodes })
    expect(text).toContain('Plain paragraph')
    expect(text).toContain('Shaded cell')

    const marks = marksOf(nodes)
    const highlights = marks.filter((m) => m.type === 'highlight')
    expect(
      highlights.map((m) => (m.attrs as { color?: string }).color)
    ).toEqual(['rgb(255, 242, 204)', 'rgb(217, 234, 211)'])
    const colors = marks.filter((m) => m.type === 'textColor')
    expect(colors.map((m) => (m.attrs as { color?: string }).color)).toEqual([
      'rgb(192, 0, 0)'
    ])
    // Container shading must not leak as a mark on plain prose/cells.
    const runs = textRuns(nodes)
    expect(runs.find((r) => r.text === 'Plain paragraph')?.marks).toEqual([])
    expect(runs.find((r) => r.text === 'Shaded cell')?.marks).toEqual([])
  })

  it('keeps genuine tables and serializes a rectangular GFM grid', () => {
    const editor = makeEditor()
    const nodes = meaningful(paste(editor, GENUINE_TABLE))
    expect(nodes).toHaveLength(1)
    expect(nodes[0].type).toBe('table')
    const rows = nodes[0].content ?? []
    expect(rows).toHaveLength(3)
    expect((rows[0].content ?? []).map(textOf)).toEqual(['Name', 'Values', ''])

    const markdown = markdownOf(nodes)
    expect(
      markdown
        .split('\n')[0]
        .split('|')
        .map((c) => c.trim())
    ).toEqual(['', 'Name', 'Values', '', ''])
    expect(markdown).not.toContain('colspan')
    expect(markdown).not.toContain('rowspan')
  })

  it('mints fresh unique ids for every pasted block', () => {
    const nodes = meaningful(paste(makeEditor(), OUTLOOK_BODY))
    const ids = blockIds(nodes)
    expect(ids.every((id) => !!id)).toBe(true)
    expect(new Set(ids).size).toBe(ids.length)
    expect(ids).not.toContain('seed')
  })

  it('serializes every fixture to markdown without Word artefacts', () => {
    const fixtures = [
      OUTLOOK_BODY,
      NESTED_ORDERED_LIST,
      LAYOUT_TABLE_BODY,
      MSO_PSEUDO_LIST,
      CSS_ONLY_EMPHASIS,
      BLOCKQUOTE_BODY,
      CODE_BLOCK,
      SHADED_CONTAINER,
      GENUINE_TABLE
    ]
    for (const fixture of fixtures) {
      const nodes = paste(makeEditor(), fixture)
      const markdown = markdownOf(nodes)
      expect(markdown).not.toMatch(/mso-/i)
      expect(markdown).not.toMatch(/<o:p/i)
      expect(markdown).not.toContain('<!--')
      expect(markdown).not.toMatch(/<table|data-pm-slice/i)
    }
    // A layout-table body must not become a TABLE block.
    const layoutNodes = paste(makeEditor(), LAYOUT_TABLE_BODY)
    expect(layoutNodes.some((n) => n.type === 'table')).toBe(false)
  })

  it('leaves internal Silt clipboard payloads (data-pm-slice) untouched', () => {
    const nodes = meaningful(paste(makeEditor(), SILT_INTERNAL_CLIPBOARD))
    // The normalizer would unwrap this 1x1 table as a layout wrapper. The
    // data-pm-slice passthrough keeps it as a real table instead.
    expect(nodes.some((n) => n.type === 'table')).toBe(true)
    expect(textOf({ type: 'doc', content: nodes })).toContain(
      'Internal table must stay a table'
    )
  })
})
