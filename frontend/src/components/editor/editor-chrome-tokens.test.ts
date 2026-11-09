import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

describe('Editor chrome typography tokens guard (#1076)', () => {
  const editorDir = join(__dirname, '.')
  const tipTapEditorPath = join(__dirname, '..', 'TipTapEditor.svelte')

  // Content typography exceptions explicitly permitted by Issue #1076:
  // - HeaderBlockView: note heading sizes (em / sizes lookup)
  // - MathNodeView: rendered equation scale (em)
  // - MarkdownSourceViewer: raw markdown / diff viewer source code content
  const contentExceptions = new Set([
    'HeaderBlockView.svelte',
    'MathNodeView.svelte',
    'MarkdownSourceViewer.svelte'
  ])

  function getEditorFiles(dir: string): string[] {
    const files: string[] = []
    for (const entry of readdirSync(dir)) {
      const fullPath = join(dir, entry)
      if (statSync(fullPath).isDirectory()) {
        files.push(...getEditorFiles(fullPath))
      } else if (entry.endsWith('.svelte')) {
        files.push(fullPath)
      }
    }
    return files
  }

  const auditedFiles = [tipTapEditorPath, ...getEditorFiles(editorDir)].filter(
    (file) => {
      const basename = file.split(/[/\\]/).pop() ?? ''
      return !contentExceptions.has(basename)
    }
  )

  it('audits all editor chrome components for hardcoded font-size pixels or rems', () => {
    const rawPattern =
      /font-size:\s*(?:(?:\d*\.?\d+)(?:px|rem)|(?:1[0-9]|2[0-9])px)/gi
    const violations: Array<{ file: string; line: number; match: string }> = []

    for (const filePath of auditedFiles) {
      const content = readFileSync(filePath, 'utf-8')
      const lines = content.split('\n')
      lines.forEach((lineText, idx) => {
        // Ignore lines that reference design tokens
        if (
          lineText.includes('var(--text-') ||
          lineText.includes('var(--editor-')
        ) {
          return
        }
        const match = lineText.match(rawPattern)
        if (match) {
          violations.push({
            file: filePath.split(/[/\\]/).slice(-2).join('/'),
            line: idx + 1,
            match: match[0]
          })
        }
      })
    }

    expect(violations).toEqual([])
  })
})
