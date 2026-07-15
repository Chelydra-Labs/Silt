import { describe, it, expect, vi } from 'vitest'
import { runPluginCommand } from './runPluginCommand'
import type { SlashCommand } from './slash-registry'

const editor = {} as unknown
const pos = 0

describe('runPluginCommand (#581)', () => {
  it('catches a synchronous throw and routes it to report', () => {
    const report = vi.fn()
    const cmd = {
      id: 'p:throw',
      onSelect: () => {
        throw new Error('boom')
      }
    } as unknown as SlashCommand

    expect(() => runPluginCommand(cmd, editor, pos, report)).not.toThrow()
    expect(report).toHaveBeenCalledTimes(1)
    expect((report.mock.calls[0][0] as Error).message).toBe('boom')
  })

  it('catches an async rejection and routes it to report', async () => {
    const report = vi.fn()
    const cmd = {
      id: 'p:reject',
      onSelect: () => Promise.reject(new Error('async boom'))
    } as unknown as SlashCommand

    runPluginCommand(cmd, editor, pos, report)
    // The .catch handler runs on the next microtask.
    await Promise.resolve()
    await Promise.resolve()

    expect(report).toHaveBeenCalledTimes(1)
    expect((report.mock.calls[0][0] as Error).message).toBe('async boom')
  })

  it('does not report when the handler resolves normally', async () => {
    const report = vi.fn()
    const onSelect = vi.fn(() => Promise.resolve())
    const cmd = { id: 'p:ok', onSelect } as unknown as SlashCommand

    runPluginCommand(cmd, editor, pos, report)
    await Promise.resolve()
    await Promise.resolve()

    expect(onSelect).toHaveBeenCalledWith(editor, pos)
    expect(report).not.toHaveBeenCalled()
  })

  it('does not report when the handler returns a synchronous value', () => {
    const report = vi.fn()
    const cmd = {
      id: 'p:sync',
      onSelect: () => undefined
    } as unknown as SlashCommand

    runPluginCommand(cmd, editor, pos, report)
    expect(report).not.toHaveBeenCalled()
  })

  it('does not throw when onSelect is absent', () => {
    const report = vi.fn()
    const cmd = { id: 'p:none' } as unknown as SlashCommand
    expect(() => runPluginCommand(cmd, editor, pos, report)).not.toThrow()
    expect(report).not.toHaveBeenCalled()
  })
})
