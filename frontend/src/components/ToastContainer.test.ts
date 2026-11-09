import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'
import ToastContainer from './ToastContainer.svelte'
import { pushNotification, _resetForTests } from '../notifications/store.svelte'

describe('ToastContainer (#86)', () => {
  beforeEach(() => {
    _resetForTests()
  })

  afterEach(() => {
    cleanup()
    _resetForTests()
  })

  it('renders nothing when there are no notifications', () => {
    const { container } = render(ToastContainer)
    expect(
      container.querySelectorAll('[role="status"], [role="alert"]')
    ).toHaveLength(0)
  })

  it('renders an info notification as role=status', async () => {
    pushNotification({ kind: 'info', message: 'Saved' })
    render(ToastContainer)
    const status = await screen.findByRole('status')
    expect(status).toHaveTextContent('Saved')
  })

  it('renders an error notification as role=alert', async () => {
    pushNotification({ kind: 'error', message: 'Boom' })
    render(ToastContainer)
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Boom')
  })

  it('renders an action button when an action is attached', async () => {
    const action = vi.fn()
    pushNotification({
      kind: 'error',
      message: 'Try again',
      action: { label: 'Retry', run: action }
    })
    render(ToastContainer)
    const btn = await screen.findByRole('button', { name: 'Retry' })
    await fireEvent.click(btn)
    expect(action).toHaveBeenCalledTimes(1)
    // After action + dismiss, the toast is gone.
    expect(screen.queryByText('Try again')).not.toBeInTheDocument()
  })

  it('invokes an action only once after its toast leaves the notification state', async () => {
    const action = vi.fn(() => {
      pushNotification({ kind: 'success', message: 'Completed' })
    })
    pushNotification({
      kind: 'error',
      message: 'Try again',
      action: { label: 'Retry', run: action }
    })
    render(ToastContainer)
    const btn = await screen.findByRole('button', { name: 'Retry' })

    await fireEvent.click(btn)
    await fireEvent.click(btn)

    expect(action).toHaveBeenCalledTimes(1)
    expect(screen.getByText('Completed')).toBeInTheDocument()
  })

  it('dismiss button removes the notification', async () => {
    pushNotification({ kind: 'info', message: 'Dismissable' })
    render(ToastContainer)
    const dismissBtn = screen.getByRole('button', {
      name: 'Dismiss notification'
    })
    await fireEvent.click(dismissBtn)
    expect(screen.queryByText('Dismissable')).not.toBeInTheDocument()
  })

  it('renders multiple notifications in the stack', async () => {
    pushNotification({ kind: 'info', message: 'First' })
    pushNotification({ kind: 'error', message: 'Second' })
    render(ToastContainer)
    expect(screen.getByText('First')).toBeInTheDocument()
    expect(screen.getByText('Second')).toBeInTheDocument()
  })

  it('live region is aria-live=assertive for errors and polite for info', async () => {
    pushNotification({ kind: 'info', message: 'Info' })
    pushNotification({ kind: 'error', message: 'Err' })
    render(ToastContainer)
    expect(screen.getByRole('status')).toHaveAttribute('aria-live', 'polite')
    expect(screen.getByRole('alert')).toHaveAttribute('aria-live', 'assertive')
  })

  it('dismisses latest notification on Escape when no overlay is open', async () => {
    pushNotification({ kind: 'info', message: 'First' })
    pushNotification({ kind: 'info', message: 'Second' })
    render(ToastContainer)
    expect(screen.getByText('First')).toBeInTheDocument()
    expect(screen.getByText('Second')).toBeInTheDocument()

    await fireEvent.keyDown(window, { key: 'Escape' })
    // Second (latest) dismissed
    expect(screen.getByText('First')).toBeInTheDocument()
    expect(screen.queryByText('Second')).not.toBeInTheDocument()

    await fireEvent.keyDown(window, { key: 'Escape' })
    // First dismissed
    expect(screen.queryByText('First')).not.toBeInTheDocument()
  })

  it('does not dismiss notification on Escape when a modal overlay is open', async () => {
    pushNotification({ kind: 'info', message: 'Protected' })
    render(ToastContainer)

    const dialog = document.createElement('div')
    dialog.setAttribute('role', 'dialog')
    document.body.appendChild(dialog)

    try {
      await fireEvent.keyDown(window, { key: 'Escape' })
      expect(screen.getByText('Protected')).toBeInTheDocument()
    } finally {
      document.body.removeChild(dialog)
    }
  })

  it('dismisses notification on Escape when an inline non-modal listbox is present', async () => {
    pushNotification({ kind: 'info', message: 'Tasks view toast' })
    render(ToastContainer)

    const listbox = document.createElement('ul')
    listbox.setAttribute('role', 'listbox')
    document.body.appendChild(listbox)

    try {
      await fireEvent.keyDown(window, { key: 'Escape' })
      expect(screen.queryByText('Tasks view toast')).not.toBeInTheDocument()
    } finally {
      document.body.removeChild(listbox)
    }
  })

  it('dismisses notification on Escape when a non-modal dialog (aria-modal=false) is present and unfocused', async () => {
    pushNotification({ kind: 'info', message: 'Props open toast' })
    render(ToastContainer)

    const panel = document.createElement('div')
    panel.setAttribute('role', 'dialog')
    panel.setAttribute('aria-modal', 'false')
    document.body.appendChild(panel)

    try {
      await fireEvent.keyDown(window, { key: 'Escape' })
      expect(screen.queryByText('Props open toast')).not.toBeInTheDocument()
    } finally {
      document.body.removeChild(panel)
    }
  })

  it('does not dismiss notification on Escape when focus is inside a menu or listbox', async () => {
    pushNotification({ kind: 'info', message: 'Focused menu toast' })
    render(ToastContainer)

    const menu = document.createElement('div')
    menu.setAttribute('role', 'menu')
    const item = document.createElement('button')
    item.setAttribute('role', 'menuitem')
    menu.appendChild(item)
    document.body.appendChild(menu)
    item.focus()

    try {
      await fireEvent.keyDown(window, { key: 'Escape' })
      expect(screen.getByText('Focused menu toast')).toBeInTheDocument()
    } finally {
      document.body.removeChild(menu)
    }
  })
})
