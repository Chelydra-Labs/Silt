import { describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import ViewState from './ViewState.svelte'

describe('ViewState component', () => {
  it('renders loading state with role="status" and polite live region', () => {
    render(ViewState, {
      kind: 'loading',
      title: 'Loading custom view…'
    })
    const status = screen.getByRole('status')
    expect(status).toHaveAttribute('aria-live', 'polite')
    expect(status).toHaveTextContent('Loading custom view…')
  })

  it('renders error state with role="alert" and invokes onRetry', async () => {
    const onRetry = vi.fn()
    render(ViewState, {
      kind: 'error',
      title: 'Failed to load',
      description: 'Network error occurred',
      retryLabel: 'Try again',
      onRetry
    })
    const alert = screen.getByRole('alert')
    expect(alert).toBeInTheDocument()
    expect(screen.getByText('Failed to load')).toBeInTheDocument()
    expect(screen.getByText('Network error occurred')).toBeInTheDocument()

    const retryBtn = screen.getByRole('button', { name: /Try again/ })
    await fireEvent.click(retryBtn)
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it('renders empty state with icon, title, and description', () => {
    render(ViewState, {
      kind: 'empty',
      icon: 'inbox',
      title: 'No items yet',
      description: 'Create an item to see it here.'
    })
    expect(screen.getByText('No items yet')).toBeInTheDocument()
    expect(
      screen.getByText('Create an item to see it here.')
    ).toBeInTheDocument()
    expect(screen.getByText('inbox')).toBeInTheDocument()
  })

  it('supports compact mode with smaller spacing', () => {
    const { container } = render(ViewState, {
      kind: 'empty',
      compact: true,
      title: 'Compact empty'
    })
    const root = container.firstElementChild as HTMLElement
    expect(root.className).toContain('py-6')
  })
})
