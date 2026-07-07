import { describe, expect, it, vi, afterEach } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

// jsdom polyfill: Svelte 5 inlines a Web Animations shim at component mount.
// ErrorBanner itself doesn't animate, but the render path touches rAF.
if (!Element.prototype.animate) {
  Element.prototype.animate = function () {
    return {
      cancel() {},
      finish() {},
      play() {},
      pause() {},
      reverse() {},
      addEventListener() {},
      removeEventListener() {},
      onfinish: null,
      oncancel: null
    } as unknown as Animation
  }
}

import ErrorBanner from './ErrorBanner.svelte'

afterEach(cleanup)

describe('ErrorBanner', () => {
  it('renders the message', () => {
    render(ErrorBanner, { message: 'Something broke' })
    expect(screen.getByText('Something broke')).toBeTruthy()
  })

  it('uses role=alert + assertive live region for errors (kind defaults to error)', () => {
    render(ErrorBanner, { message: 'boom' })
    const banner = screen.getByRole('alert')
    expect(banner.getAttribute('aria-live')).toBe('assertive')
    expect(banner.getAttribute('aria-atomic')).toBe('true')
  })

  it('uses role=status + polite live region for warnings', () => {
    render(ErrorBanner, { message: 'heads up', kind: 'warning' })
    const banner = screen.getByRole('status')
    expect(banner.getAttribute('aria-live')).toBe('polite')
  })

  it('does not render a dismiss button when onDismiss is absent', () => {
    render(ErrorBanner, { message: 'no dismiss' })
    expect(screen.queryByRole('button', { name: 'Dismiss' })).toBeNull()
  })

  it('renders a dismiss button and fires onDismiss on click', async () => {
    const onDismiss = vi.fn()
    render(ErrorBanner, { message: 'dismissible', onDismiss })
    const btn = screen.getByRole('button', { name: 'Dismiss' })
    await fireEvent.click(btn)
    expect(onDismiss).toHaveBeenCalledTimes(1)
  })

  it('renders a "Try again" button and fires onRetry on click', async () => {
    const onRetry = vi.fn()
    render(ErrorBanner, { message: 'retryable', onRetry })
    const btn = screen.getByRole('button', { name: 'Try again' })
    await fireEvent.click(btn)
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it('does not render a retry button when onRetry is absent', () => {
    render(ErrorBanner, { message: 'no retry' })
    expect(screen.queryByRole('button', { name: 'Try again' })).toBeNull()
  })

  it('surfaces dataTestId on the banner root and derives testids for the buttons', () => {
    render(ErrorBanner, {
      message: 'tagged',
      dataTestId: 'tasks-mark-done-error',
      onDismiss: () => {},
      onRetry: () => {}
    })
    expect(screen.getByTestId('tasks-mark-done-error')).toBeTruthy()
    expect(screen.getByTestId('tasks-mark-done-error-dismiss')).toBeTruthy()
    expect(screen.getByTestId('tasks-mark-done-error-retry')).toBeTruthy()
  })

  it('renders nothing interactive when message is empty (caller-controlled)', () => {
    // Callers gate {#if errorMsg} themselves; the component itself just renders
    // whatever message it receives. Empty message still renders the banner —
    // guarding is the caller's job. This test pins that contract.
    const { container } = render(ErrorBanner, { message: '' })
    expect(container.querySelector('[role="alert"]')).toBeTruthy()
  })
})
