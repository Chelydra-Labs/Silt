// Unit coverage for the built-in slash-command classifier. The id → intent
// surface was previously an inline if/else in TipTapEditor with no direct test;
// these cases pin every built-in command id and the null (plugin/unknown) path.
import { describe, expect, it } from 'vitest'
import { classifySlashCommand, FORMAT_COMMANDS } from './builtinSlashCommands'

describe('classifySlashCommand', () => {
  it('classifies task conversions (todo + task alias)', () => {
    expect(classifySlashCommand('todo')).toEqual({
      kind: 'convert',
      blockType: 'taskBlock'
    })
    expect(classifySlashCommand('task')).toEqual({
      kind: 'convert',
      blockType: 'taskBlock'
    })
  })

  it('classifies heading levels with depth', () => {
    expect(classifySlashCommand('h1')).toEqual({
      kind: 'convert',
      blockType: 'headerBlock',
      depth: 1
    })
    expect(classifySlashCommand('h2')).toEqual({
      kind: 'convert',
      blockType: 'headerBlock',
      depth: 2
    })
    expect(classifySlashCommand('h3')).toEqual({
      kind: 'convert',
      blockType: 'headerBlock',
      depth: 3
    })
  })

  it('classifies a note conversion (no depth)', () => {
    const r = classifySlashCommand('note')
    expect(r?.kind).toBe('convert')
    expect((r as { blockType: string }).blockType).toBe('noteBlock')
    expect((r as { depth?: number }).depth).toBeUndefined()
  })

  it('classifies the four alignment commands', () => {
    expect(classifySlashCommand('align-left')).toEqual({
      kind: 'align',
      align: 'left'
    })
    expect(classifySlashCommand('align-center')).toEqual({
      kind: 'align',
      align: 'center'
    })
    expect(classifySlashCommand('align-right')).toEqual({
      kind: 'align',
      align: 'right'
    })
    expect(classifySlashCommand('align-justify')).toEqual({
      kind: 'align',
      align: 'justify'
    })
  })

  it('classifies the default callout and callout-<variant> ids', () => {
    expect(classifySlashCommand('callout')).toEqual({
      kind: 'callout',
      variant: 'note'
    })
    expect(classifySlashCommand('callout-warning')).toEqual({
      kind: 'callout',
      variant: 'warning'
    })
  })

  it('classifies structural inserts', () => {
    expect(classifySlashCommand('quote')?.kind).toBe('quote')
    expect(classifySlashCommand('code-block')?.kind).toBe('codeBlock')
    expect(classifySlashCommand('math')?.kind).toBe('math')
    expect(classifySlashCommand('details')?.kind).toBe('details')
  })

  it('classifies the two table presets and the custom picker', () => {
    expect(classifySlashCommand('table')).toEqual({
      kind: 'table',
      rows: 3,
      cols: 3
    })
    expect(classifySlashCommand('table-5x4')).toEqual({
      kind: 'table',
      rows: 5,
      cols: 4
    })
    expect(classifySlashCommand('table-custom')?.kind).toBe('tableCustom')
  })

  it('classifies color set/remove', () => {
    expect(classifySlashCommand('text-color')).toEqual({
      kind: 'color',
      markType: 'textColor'
    })
    expect(classifySlashCommand('background-color')).toEqual({
      kind: 'color',
      markType: 'backgroundColor'
    })
    expect(classifySlashCommand('remove-color')).toEqual({
      kind: 'removeColor',
      markType: 'textColor'
    })
    expect(classifySlashCommand('remove-background')).toEqual({
      kind: 'removeColor',
      markType: 'backgroundColor'
    })
  })

  it('classifies today/embed/template (no payload — execution computes it)', () => {
    expect(classifySlashCommand('today')).toEqual({ kind: 'today' })
    expect(classifySlashCommand('embed')).toEqual({ kind: 'embed' })
    expect(classifySlashCommand('template')).toEqual({ kind: 'template' })
  })

  it('classifies inline-format commands via FORMAT_COMMANDS', () => {
    expect(classifySlashCommand('bold')).toEqual({
      kind: 'format',
      mark: 'bold'
    })
    expect(classifySlashCommand('clear-formatting')).toEqual({
      kind: 'format',
      mark: 'clear'
    })
    expect(classifySlashCommand('link')).toEqual({
      kind: 'format',
      mark: 'link'
    })
    // every entry in FORMAT_COMMANDS must resolve to a format intent
    for (const id of Object.keys(FORMAT_COMMANDS)) {
      const r = classifySlashCommand(id)
      expect(r?.kind).toBe('format')
    }
  })

  it('returns null for plugin / unknown ids', () => {
    expect(classifySlashCommand('my-plugin-command')).toBe(null)
    expect(classifySlashCommand('')).toBe(null)
    expect(classifySlashCommand('totally-made-up')).toBe(null)
  })
})
