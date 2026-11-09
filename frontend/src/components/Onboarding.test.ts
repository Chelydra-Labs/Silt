import { describe, expect, it, vi, afterEach } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'
import Onboarding from './Onboarding.svelte'

describe('Onboarding (#1080)', () => {
  afterEach(() => {
    cleanup()
  })

  it('renders loading status when loading is true', () => {
    render(Onboarding, {
      loading: true,
      initialized: false,
      onSelectFolder: vi.fn()
    })

    const status = screen.getByRole('status')
    expect(status).toHaveTextContent('Initializing Silt Core…')
    expect(status).toHaveAttribute('aria-live', 'polite')
    expect(
      screen.queryByRole('button', { name: /initialize workspace/i })
    ).not.toBeInTheDocument()
  })

  it('renders nothing when already initialized', () => {
    const { container } = render(Onboarding, {
      loading: false,
      initialized: true,
      onSelectFolder: vi.fn()
    })

    expect(container.children.length).toBe(0)
  })

  it('renders first-run onboarding with CTA button', async () => {
    const onSelect = vi.fn()
    render(Onboarding, {
      loading: false,
      initialized: false,
      onSelectFolder: onSelect
    })

    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Silt')
    expect(screen.getByAltText('Silt Logo')).toBeInTheDocument()

    const btn = screen.getByRole('button', {
      name: 'Initialize Workspace Folder'
    })
    expect(btn).toBeInTheDocument()
    expect(btn.className).toContain('onboarding-btn')
    expect(btn.className).toContain('font-label-sm-bold')

    await fireEvent.click(btn)
    expect(onSelect).toHaveBeenCalledTimes(1)
  })

  it('disables CTA button and shows busy indicator when initializing', () => {
    render(Onboarding, {
      loading: false,
      initialized: false,
      initializing: true,
      onSelectFolder: vi.fn()
    })

    const btn = screen.getByRole('button', { name: 'Initializing…' })
    expect(btn).toBeDisabled()
    expect(btn).toHaveAttribute('aria-busy', 'true')
  })

  it('renders inline error alert with retry button when error is present', async () => {
    const onRetry = vi.fn()
    render(Onboarding, {
      loading: false,
      initialized: false,
      error: 'Permission denied to selected directory',
      onRetry,
      onSelectFolder: vi.fn()
    })

    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent('Permission denied to selected directory')
    expect(alert).toHaveAttribute('aria-live', 'assertive')

    const retryBtn = screen.getByRole('button', { name: 'Retry' })
    await fireEvent.click(retryBtn)
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it('disables retry button and sets aria-busy when initializing', () => {
    render(Onboarding, {
      loading: false,
      initialized: false,
      initializing: true,
      error: 'Disk failure',
      onRetry: vi.fn(),
      onSelectFolder: vi.fn()
    })

    const retryBtn = screen.getByRole('button', { name: 'Retry' })
    expect(retryBtn).toBeDisabled()
    expect(retryBtn).toHaveAttribute('aria-busy', 'true')
  })
})
