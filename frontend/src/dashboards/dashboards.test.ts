import { describe, expect, it } from 'vitest'
import {
  binByProperty,
  formatMultiValueDisplay,
  splitMultiValueText,
  type TypeDashboardRow
} from './dashboards'
import type { PropertyDef } from '../properties/types'

function row(page: string, tags: string): TypeDashboardRow {
  return {
    source: 'vault',
    notebook: 'N',
    section: '',
    page,
    properties: [{ name: 'tags', valueText: tags, valueType: 'multiselect' }]
  }
}

describe('splitMultiValueText / binByProperty', () => {
  it('parses JSON arrays so an entry may contain commas', () => {
    expect(splitMultiValueText('["a, b","c"]')).toEqual(['a, b', 'c'])
    expect(splitMultiValueText('[]')).toEqual([])
    expect(splitMultiValueText('')).toEqual([])
  })

  it('still accepts legacy ", "-joined projections', () => {
    expect(splitMultiValueText('x, y, z')).toEqual(['x', 'y', 'z'])
    expect(splitMultiValueText('solo')).toEqual(['solo'])
  })

  it('formats multi-values for human display', () => {
    expect(formatMultiValueDisplay('["a, b","c"]')).toBe('a, b, c')
  })

  it('bins multiselect with comma-containing options without phantom buckets', () => {
    const prop: PropertyDef = {
      name: 'tags',
      type: 'multiselect',
      options: ['a, b', 'c']
    }
    const sections = binByProperty(
      [row('P1', '["a, b","c"]'), row('P2', '["a, b"]')],
      prop,
      'tags'
    )
    const labels = sections.map((s) => s.label).sort()
    expect(labels).toEqual(['a, b', 'c'])
    const ab = sections.find((s) => s.label === 'a, b')?.rows.map((r) => r.page)
    expect(ab?.sort()).toEqual(['P1', 'P2'])
    const cOnly = sections.find((s) => s.label === 'c')?.rows.map((r) => r.page)
    expect(cOnly).toEqual(['P1'])
  })
})
