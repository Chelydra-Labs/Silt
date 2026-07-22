// Pure unit tests for outerNodeViewAttrs + the NODE_DATA_TYPE completeness
// guard. Integration coverage lives in blockDepthDom.test.ts; these pin the
// TipTap getRenderedAttributes gap (no node-level data-type) without mounting
// an editor.

import { describe, it, expect } from 'vitest'
import {
  outerNodeViewAttrs,
  syncOuterDomAttrs,
  NODE_DATA_TYPE,
  NODE_VIEW_TYPE_NAMES
} from './nodeViews'
import type { Node as ProseMirrorNode } from '@tiptap/pm/model'

function fakeNode(typeName: string): ProseMirrorNode {
  return { type: { name: typeName } } as ProseMirrorNode
}

describe('outerNodeViewAttrs', () => {
  it('merges data-type onto attribute-level HTMLAttributes for noteBlock', () => {
    const result = outerNodeViewAttrs({
      node: fakeNode('noteBlock'),
      HTMLAttributes: {
        'data-depth': '2',
        'data-id': 'abc',
        'data-bullet': '- '
      }
    })
    expect(result).toEqual({
      'data-depth': '2',
      'data-id': 'abc',
      'data-bullet': '- ',
      'data-type': 'note'
    })
  })

  it('maps every known NodeView type to its schema data-type string', () => {
    for (const typeName of NODE_VIEW_TYPE_NAMES) {
      const expected = NODE_DATA_TYPE[typeName]
      expect(expected, `missing NODE_DATA_TYPE for ${typeName}`).toBeTruthy()
      const result = outerNodeViewAttrs({
        node: fakeNode(typeName),
        HTMLAttributes: { 'data-depth': '0' }
      })
      expect(result['data-type']).toBe(expected)
      expect(result['data-depth']).toBe('0')
    }
  })

  it('passes through HTMLAttributes unchanged for unknown node types', () => {
    const html = { 'data-depth': '1', 'data-custom': 'x' }
    expect(
      outerNodeViewAttrs({
        node: fakeNode('unknownBlock'),
        HTMLAttributes: html
      })
    ).toEqual(html)
  })

  it('does not mutate the input HTMLAttributes object', () => {
    const html = { 'data-depth': '0' }
    outerNodeViewAttrs({ node: fakeNode('taskBlock'), HTMLAttributes: html })
    expect(html).toEqual({ 'data-depth': '0' })
    expect('data-type' in html).toBe(false)
  })
})

describe('NODE_DATA_TYPE completeness vs NodeView list', () => {
  it('has a data-type entry for every NodeView type name', () => {
    const missing = NODE_VIEW_TYPE_NAMES.filter((n) => !NODE_DATA_TYPE[n])
    expect(missing).toEqual([])
  })

  it('does not carry orphan NODE_DATA_TYPE keys outside the NodeView list', () => {
    const known = new Set<string>(NODE_VIEW_TYPE_NAMES)
    const orphans = Object.keys(NODE_DATA_TYPE).filter((k) => !known.has(k))
    expect(orphans).toEqual([])
  })
})

describe('syncOuterDomAttrs', () => {
  it('sets new attrs and removes stale data-* keys', () => {
    const el = document.createElement('div')
    el.setAttribute('data-depth', '2')
    el.setAttribute('data-id', 'old-id')
    el.setAttribute('data-owner', 'alice')
    el.className = 'node-noteBlock'

    syncOuterDomAttrs(el, {
      'data-depth': '1',
      'data-type': 'note'
    })

    expect(el.getAttribute('data-depth')).toBe('1')
    expect(el.getAttribute('data-type')).toBe('note')
    expect(el.hasAttribute('data-id')).toBe(false)
    expect(el.hasAttribute('data-owner')).toBe(false)
    // Non-data attributes are left alone.
    expect(el.className).toBe('node-noteBlock')
  })
})
