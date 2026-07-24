// Unit coverage for the built-in slash-command classifier. The id → intent
// surface was previously an inline if/else in TipTapEditor with no direct test;
// these cases pin every built-in command id and the null (plugin/unknown) path.
import { describe, expect, it } from 'vitest'
import { classifySlashCommand, FORMAT_COMMANDS } from './builtinSlashCommands'

describe('classifySlashCommand', () => {
  it('classifies the task conversion', () => {
    expect(classifySlashCommand('task')).toEqual({
      kind: 'convert',
      blockType: 'taskBlock'
    })
  })

  it('classifies heading levels with depth (H1–H6)', () => {
    for (let depth = 1; depth <= 6; depth++) {
      expect(classifySlashCommand(`h${depth}`)).toEqual({
        kind: 'convert',
        blockType: 'headerBlock',
        depth
      })
    }
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
    expect(classifySlashCommand('math')?.kind).toBe('math')
    expect(classifySlashCommand('details')?.kind).toBe('details')
  })

  it('classifies code blocks (default and mermaid language)', () => {
    expect(classifySlashCommand('code-block')).toEqual({ kind: 'codeBlock' })
    expect(classifySlashCommand('mermaid')).toEqual({
      kind: 'codeBlock',
      language: 'mermaid'
    })
  })

  it('classifies the table preset and the custom picker', () => {
    expect(classifySlashCommand('table')).toEqual({
      kind: 'table',
      rows: 3,
      cols: 3
    })
    expect(classifySlashCommand('table-custom')?.kind).toBe('tableCustom')
  })

  it('classifies color set', () => {
    expect(classifySlashCommand('text-color')).toEqual({
      kind: 'color',
      markType: 'textColor'
    })
    expect(classifySlashCommand('background-color')).toEqual({
      kind: 'color',
      markType: 'backgroundColor'
    })
  })

  it('classifies today/embed/template/calendar/shortcuts (no payload — execution computes it)', () => {
    expect(classifySlashCommand('today')).toEqual({ kind: 'today' })
    expect(classifySlashCommand('embed')).toEqual({ kind: 'embed' })
    expect(classifySlashCommand('template')).toEqual({ kind: 'template' })
    expect(classifySlashCommand('calendar')).toEqual({ kind: 'calendar' })
    expect(classifySlashCommand('shortcuts')).toEqual({ kind: 'shortcuts' })
  })

  it('classifies inline-format commands via FORMAT_COMMANDS', () => {
    expect(classifySlashCommand('bold')).toEqual({
      kind: 'format',
      mark: 'bold'
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
