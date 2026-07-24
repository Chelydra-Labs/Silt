import { describe, expect, it } from 'vitest'
import { classifySlashCommand, FORMAT_COMMANDS } from './builtinSlashCommands'

describe('classifySlashCommand', () => {
  it('classifies the calendar command', () => {
    expect(classifySlashCommand('calendar')).toEqual({ kind: 'calendar' })
  })

  it('classifies the shortcuts command', () => {
    expect(classifySlashCommand('shortcuts')).toEqual({ kind: 'shortcuts' })
  })

  it('still classifies the existing today command', () => {
    expect(classifySlashCommand('today')).toEqual({ kind: 'today' })
  })

  it('still classifies embed and template', () => {
    expect(classifySlashCommand('embed')).toEqual({ kind: 'embed' })
    expect(classifySlashCommand('template')).toEqual({ kind: 'template' })
  })

  it('still classifies heading conversions', () => {
    expect(classifySlashCommand('h2')).toEqual({
      kind: 'convert',
      blockType: 'headerBlock',
      depth: 2
    })
  })

  it('still classifies inline format commands', () => {
    expect(classifySlashCommand('bold')).toEqual({
      kind: 'format',
      mark: FORMAT_COMMANDS.bold
    })
  })

  it('returns null for an unknown / plugin command id', () => {
    expect(classifySlashCommand('my-plugin:custom')).toBeNull()
  })
})
