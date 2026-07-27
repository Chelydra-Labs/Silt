import { describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { AIErrorKind } from '../../../../generated/enums'
import { completeStreaming, isStreamUnsupportedError } from './runChat'

describe('isStreamUnsupportedError', () => {
  it('matches bad-request code', () => {
    expect(isStreamUnsupportedError({ code: AIErrorKind.ErrBadRequest })).toBe(
      true
    )
  })
  it('matches stream in message', () => {
    expect(
      isStreamUnsupportedError({ message: 'streaming not supported' })
    ).toBe(true)
  })
  it('rejects auth/rate-limit', () => {
    expect(
      isStreamUnsupportedError({ code: AIErrorKind.ErrUnauthorized })
    ).toBe(false)
    expect(isStreamUnsupportedError({ code: AIErrorKind.ErrRateLimited })).toBe(
      false
    )
    expect(isStreamUnsupportedError({ code: AIErrorKind.ErrTimeout })).toBe(
      false
    )
  })
})

describe('completeStreaming fallback gate', () => {
  const messages = [{ role: 'user' as const, content: 'hi' }]

  it('falls back to buffered when provider rejects streaming', async () => {
    const complete = vi
      .fn()
      .mockRejectedValueOnce({
        code: AIErrorKind.ErrBadRequest,
        message: 'streaming not supported'
      })
      .mockResolvedValueOnce({ content: 'buffered ok', model: 'm' })

    const ctx = { ai: { complete } } as unknown as PluginContext
    const deltas: string[] = []
    const res = await completeStreaming(ctx, messages, (c) => deltas.push(c))

    expect(res.content).toBe('buffered ok')
    expect(complete).toHaveBeenCalledTimes(2)
    expect(complete.mock.calls[0][0].stream).toBe(true)
    expect(complete.mock.calls[1][0].stream).toBeUndefined()
    expect(deltas).toContain('buffered ok')
  })

  it('does not retry buffered on rate-limited', async () => {
    const complete = vi.fn().mockRejectedValue({
      code: AIErrorKind.ErrRateLimited,
      message: 'slow down'
    })
    const ctx = { ai: { complete } } as unknown as PluginContext

    await expect(
      completeStreaming(ctx, messages, () => {})
    ).rejects.toMatchObject({ code: AIErrorKind.ErrRateLimited })
    expect(complete).toHaveBeenCalledTimes(1)
  })

  it('does not retry buffered on unauthorized', async () => {
    const complete = vi.fn().mockRejectedValue({
      code: AIErrorKind.ErrUnauthorized,
      message: 'bad key'
    })
    const ctx = { ai: { complete } } as unknown as PluginContext

    await expect(
      completeStreaming(ctx, messages, () => {})
    ).rejects.toMatchObject({ code: AIErrorKind.ErrUnauthorized })
    expect(complete).toHaveBeenCalledTimes(1)
  })
})
