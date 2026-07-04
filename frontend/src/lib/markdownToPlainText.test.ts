import { describe, expect, it } from 'vitest'
import { stripMarkdown } from './markdownToPlainText'

describe('stripMarkdown (#378 release-notes plain text)', () => {
  it('passes plain text through unchanged', () => {
    expect(stripMarkdown('just some words')).toBe('just some words')
  })

  it('returns empty string for empty / whitespace-only input', () => {
    expect(stripMarkdown('')).toBe('')
    expect(stripMarkdown('   \n\t  ')).toBe('')
  })

  it('strips bold', () => {
    expect(stripMarkdown('**bold**')).toBe('bold')
    expect(stripMarkdown('__bold__')).toBe('bold')
    expect(stripMarkdown('a **b** c')).toBe('a b c')
  })

  it('strips italic with arithmetic safety', () => {
    expect(stripMarkdown('*italic*')).toBe('italic')
    expect(stripMarkdown('_italic_')).toBe('italic')
    // Arithmetic / currency / identifiers must stay literal.
    expect(stripMarkdown('5*3=15')).toBe('5*3=15')
    expect(stripMarkdown('5 * 3 = 15')).toBe('5 * 3 = 15')
    expect(stripMarkdown('snake_case_name')).toBe('snake_case_name')
    expect(stripMarkdown('$5')).toBe('$5')
    expect(stripMarkdown('5$ cash')).toBe('5$ cash')
  })

  it('strips strikethrough', () => {
    expect(stripMarkdown('~~done~~')).toBe('done')
    expect(stripMarkdown('a ~~b~~ c')).toBe('a b c')
  })

  it('strips inline code', () => {
    expect(stripMarkdown('`code`')).toBe('code')
    expect(stripMarkdown('use `npm test` now')).toBe('use npm test now')
  })

  it('strips fenced code blocks, keeping the inner content', () => {
    expect(stripMarkdown('```js\nconst x = 1\n```')).toBe('const x = 1')
    expect(stripMarkdown('```ts\na\nb\n```')).toBe('a\nb')
  })

  it('unwraps images to their alt text', () => {
    expect(stripMarkdown('![a logo](https://x/y.png)')).toBe('a logo')
    expect(stripMarkdown('![alt](url) before')).toBe('alt before')
  })

  it('unwraps links to their label text', () => {
    expect(stripMarkdown('[text](https://x/y)')).toBe('text')
    expect(stripMarkdown('see [docs](url) now')).toBe('see docs now')
  })

  it('unwraps a link whose label is itself bold', () => {
    // Order-sensitivity: links unwrap before emphasis, so the bold inside the
    // label collapses too.
    expect(stripMarkdown('[**bold link**](url)')).toBe('bold link')
  })

  it('strips leading heading hashes from mixed lines', () => {
    // Heading-only lines are filtered upstream by notesExcerpt; this covers a
    // mixed line that survives the filter (defense-in-depth).
    expect(stripMarkdown('## A title')).toBe('A title')
    expect(stripMarkdown('###### deep')).toBe('deep')
  })

  it('renders unordered bullets as "• "', () => {
    expect(stripMarkdown('- item one')).toBe('• item one')
    expect(stripMarkdown('* item two')).toBe('• item two')
    expect(stripMarkdown('+ item three')).toBe('• item three')
  })

  it('removes blockquote prefixes', () => {
    expect(stripMarkdown('> quoted line')).toBe('quoted line')
    expect(stripMarkdown('>nested')).toBe('nested')
  })

  it('handles nested syntax', () => {
    expect(stripMarkdown('- **Harden recurring tasks**')).toBe(
      '• Harden recurring tasks'
    )
    expect(stripMarkdown('- [Parse and index](url) metadata')).toBe(
      '• Parse and index metadata'
    )
  })

  it('reproduces the #378 issue example as clean text', () => {
    const notes = [
      '- **Harden recurring tasks engine (idempotency, anchor retention, a11y, UX)**',
      '- **Address review feedback (comment cleanup, test time-bomb, overdue preview)**',
      '- **Parse and index [recur::] metadata tokens (#295)**',
      '- **Recurrence resolution and task auto-recreation (#296)**',
      '- **Expose recurrence over IPC and plugin SDK (#296)**',
      '- **Task recurrence cards & settings panel integration (#297)**'
    ].join('\n')
    const out = stripMarkdown(notes)
    // No raw markdown syntax survives. Note: `[recur::]` is a Silt metadata
    // token (no trailing `(url)`), not a markdown link, so it stays literal —
    // the `](` link-syntax guard below confirms no actual links leak through.
    expect(out).not.toContain('**')
    expect(out).not.toMatch(/\]\(/)
    expect(out).not.toContain('`')
    // Each line is a clean bullet with bold stripped.
    expect(out).toContain('• Harden recurring tasks engine')
    expect(out).toContain('• Parse and index [recur::] metadata tokens (#295)')
    expect(out).toContain(
      '• Task recurrence cards & settings panel integration (#297)'
    )
  })

  it('preserves internal single newlines and trims outer blank lines', () => {
    expect(stripMarkdown('\n\na\nb\n\n')).toBe('a\nb')
    expect(stripMarkdown('a\n\n\n\nb')).toBe('a\n\nb')
  })
})
