import { describe, expect, it } from 'vitest'
import {
  binByProperty,
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
  it('splits on ", " matching the projection join (not bare commas)', () => {
    expect(splitMultiValueText('x, y, z')).toEqual(['x', 'y', 'z'])
    expect(splitMultiValueText('solo')).toEqual(['solo'])
    expect(splitMultiValueText('')).toEqual([])
  })

  it('bins multiselect without fracturing on bare commas inside a lone value', () => {
    // Projection joins with ", ". A single stored value "alpha, beta" (one
    // option) is indistinguishable from two options after join — that is the
    // documented limit. Two clean options must still multi-membership bin.
    const prop: PropertyDef = {
      name: 'tags',
      type: 'multiselect',
      options: ['x', 'y']
    }
    const sections = binByProperty(
      [row('P1', 'x, y'), row('P2', 'x')],
      prop,
      'tags'
    )
    const keys = sections.map((s) => s.label).sort()
    expect(keys).toEqual(['x', 'y'])
    const xRows = sections.find((s) => s.label === 'x')?.rows.map((r) => r.page)
    expect(xRows?.sort()).toEqual(['P1', 'P2'])
  })
})
