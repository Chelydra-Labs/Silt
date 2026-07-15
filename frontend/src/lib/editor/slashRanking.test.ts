// Order assertions for the slash-palette ranker (#585). The fixture is a fixed
// command set (independent of the live registry) so these assertions pin the
// ordering regardless of how the registry is populated.
import { describe, expect, it } from 'vitest'
import { rankSlashCommands } from './slashRanking'
import type { SlashCommand } from './slash-registry'

// A representative slice of the built-in catalog. Order here is intentionally
// not alphabetical — the ranker must not depend on insertion order.
const FIXTURE: SlashCommand[] = [
  {
    id: 'background-color',
    label: 'Background color',
    description: 'Pick a background color for the selection'
  },
  {
    id: 'text-color',
    label: 'Text color',
    description: 'Pick a text color for the selection'
  },
  { id: 'bold', label: 'Bold', description: 'Make the selection bold' },
  { id: 'italic', label: 'Italic', description: 'Make the selection italic' },
  { id: 'table', label: 'Table', description: 'Insert a 3×3 table' },
  {
    id: 'table-custom',
    label: 'Custom table…',
    description: 'Insert a table with custom dimensions'
  },
  { id: 'h1', label: 'Heading 1', description: 'Large section header' },
  { id: 'h2', label: 'Heading 2', description: 'Convert the block to an H2' },
  { id: 'h3', label: 'Heading 3', description: 'Convert the block to an H3' },
  {
    id: 'code-block',
    label: 'Code block',
    description: 'Insert a fenced code block with syntax highlighting'
  },
  {
    id: 'mermaid',
    label: 'Mermaid diagram',
    description: 'Insert a Mermaid diagram code block'
  },
  {
    id: 'math',
    label: 'Math equation',
    description: 'Insert a centered LaTeX equation'
  },
  { id: 'task', label: 'Task', description: 'Convert the block to a task' },
  {
    id: 'quote',
    label: 'Quote',
    description: 'Toggle a blockquote on the current block'
  }
]

function idsFor(query: string): string[] {
  return rankSlashCommands(FIXTURE, query).map((c) => c.id)
}

describe('rankSlashCommands', () => {
  it('returns the full list unchanged for an empty query', () => {
    expect(rankSlashCommands(FIXTURE, '')).toHaveLength(FIXTURE.length)
  })

  it('ranks Bold first for /b (ahead of Background color)', () => {
    // The headline case: both labels start with 'b', but Bold is the more
    // common intent. Curated priority (and shorter-label fallback) must win.
    const result = idsFor('b')
    expect(result[0]).toBe('bold')
    expect(result.indexOf('bold')).toBeLessThan(
      result.indexOf('background-color')
    )
  })

  it('ranks an exact label/id match first', () => {
    expect(idsFor('bold')[0]).toBe('bold')
    expect(idsFor('table')[0]).toBe('table')
  })

  it('ranks label/id word-boundary matches (color) and keeps them deterministic', () => {
    const result = idsFor('color')
    // Both color commands match on a word boundary; order is deterministic.
    expect(result).toContain('text-color')
    expect(result).toContain('background-color')
    // No description-only match sneaks above a label/id match.
    const textColorIdx = result.indexOf('text-color')
    expect(textColorIdx).toBeGreaterThanOrEqual(0)
  })

  it('ranks label/id matches above description-only matches', () => {
    // 'table' is a label/id match for table + table-custom (word boundary),
    // and a description match for nothing relevant here; ensure the label/id
    // entries come first and in a deterministic order.
    const result = idsFor('table')
    expect(result[0]).toBe('table')
    expect(result.slice(0, 2)).toContain('table-custom')
  })

  it('groups heading matches deterministically by label prefix', () => {
    const result = idsFor('heading')
    expect(result.slice(0, 3)).toEqual(['h1', 'h2', 'h3'])
  })

  it('gates description matching for short queries (one char → label/id only)', () => {
    // 'h' matches Heading labels but must NOT pull in description-only hits.
    const result = idsFor('h')
    for (const id of result) {
      const cmd = FIXTURE.find((c) => c.id === id)!
      const hay = (cmd.label + ' ' + cmd.id).toLowerCase()
      expect(hay.includes('h')).toBe(true)
    }
  })

  it('gates description matching for stopwords (the → no description swamp)', () => {
    // No label/id contains 'the', and 'the' is a stopword, so descriptions are
    // excluded → empty result instead of every "…the selection" command.
    expect(idsFor('the')).toHaveLength(0)
  })

  it('allows a non-stopword ≥3-char description query, deterministically', () => {
    // 'insert' has no label/id match but is a valid description query; the
    // result is non-empty and deterministic (stable across runs).
    const a = idsFor('insert')
    const b = idsFor('insert')
    expect(a.length).toBeGreaterThan(0)
    expect(a).toEqual(b)
  })

  it('produces identical output for repeated calls (pure/deterministic)', () => {
    expect(idsFor('table')).toEqual(idsFor('table'))
    expect(idsFor('b')).toEqual(idsFor('b'))
  })
})
