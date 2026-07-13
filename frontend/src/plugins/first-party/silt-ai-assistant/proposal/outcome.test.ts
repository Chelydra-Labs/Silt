import { describe, expect, it } from 'vitest'
import { createProposal } from './model'
import { acceptLabel } from './outcome'

describe('acceptLabel', () => {
  it('labels replace selection', () => {
    const p = createProposal({
      actionId: 'improve-clarity',
      kind: 'replace-selection',
      scope: {
        notebook: 'n',
        section: '',
        page: 'p',
        inputText: 'x',
        truncated: false,
        targetBlockId: 'b',
        targetBlockText: 'hello world',
        selectionText: 'world',
        replaceFullBlock: false
      },
      proposedMarkdown: 'planet'
    })
    expect(acceptLabel(p)).toBe('Replace selection in block')
  })

  it('labels task insert count', () => {
    const p = createProposal({
      actionId: 'extract-tasks',
      kind: 'insert-tasks',
      scope: {
        notebook: 'n',
        section: '',
        page: 'p',
        inputText: 'x',
        truncated: false
      },
      tasks: ['a', 'b']
    })
    expect(acceptLabel(p)).toBe('Insert 2 tasks')
  })
})
