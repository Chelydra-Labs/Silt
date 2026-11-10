import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor
} from '@testing-library/svelte'
import QuickAddTask from './QuickAddTask.svelte'

afterEach(cleanup)

describe('QuickAddTask', () => {
  it('offers a pointer submit action and preserves the title-only create contract', async () => {
    const createTask = vi.fn().mockResolvedValue('task-1')
    const onCreated = vi.fn()
    render(QuickAddTask, {
      props: {
        createTask,
        dueDate: '2026-08-06',
        status: 'DOING',
        keepOpenAfterCreate: true,
        onCreated
      }
    })

    const input = screen.getByTestId('quick-add-task-input')
    const submit = screen.getByRole('button', { name: 'Add task' })
    expect(submit).toBeDisabled()

    await fireEvent.input(input, {
      target: { value: '  Draft release notes  ' }
    })
    expect(submit).toBeEnabled()
    await fireEvent.click(submit)

    await waitFor(() => {
      expect(createTask).toHaveBeenCalledWith({
        title: 'Draft release notes',
        dueDate: '2026-08-06',
        status: 'DOING'
      })
      expect(onCreated).toHaveBeenCalledWith('task-1')
      expect(input).toHaveValue('')
    })
  })

  it('keeps the draft available when creation fails', async () => {
    const createTask = vi.fn().mockRejectedValue(new Error('Vault is locked'))
    render(QuickAddTask, { props: { createTask } })

    const input = screen.getByTestId('quick-add-task-input')
    await fireEvent.input(input, { target: { value: 'Retry this task' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Add task' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Vault is locked'
    )
    expect(input).toHaveValue('Retry this task')
    expect(input).toHaveAttribute('aria-invalid', 'true')
  })

  it('keeps focus in the input and clears the field after an Enter submit', async () => {
    const createTask = vi.fn().mockResolvedValue('task-1')
    render(QuickAddTask, { props: { createTask } })

    const input = screen.getByTestId('quick-add-task-input') as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'Ship it' } })
    await fireEvent.keyDown(input, { key: 'Enter' })

    await waitFor(() => {
      expect(createTask).toHaveBeenCalledTimes(1)
      expect(input).toHaveValue('')
    })
    expect(document.activeElement).toBe(input)
  })

  it('supports chained quick entry: two Enter submits create two tasks', async () => {
    const createTask = vi
      .fn()
      .mockResolvedValueOnce('task-1')
      .mockResolvedValueOnce('task-2')
    render(QuickAddTask, { props: { createTask } })

    const input = screen.getByTestId('quick-add-task-input') as HTMLInputElement

    await fireEvent.input(input, { target: { value: 'First' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(createTask).toHaveBeenCalledTimes(1))

    await fireEvent.input(input, { target: { value: 'Second' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(createTask).toHaveBeenCalledTimes(2))

    expect(createTask).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({ title: 'First' })
    )
    expect(createTask).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({ title: 'Second' })
    )
    expect(input).toHaveValue('')
    expect(document.activeElement).toBe(input)
  })

  it('ignores extra Enter presses while a create is in flight', async () => {
    let resolveCreate!: (id: string) => void
    const createTask = vi.fn(
      () =>
        new Promise<string>((resolve) => {
          resolveCreate = resolve
        })
    )
    render(QuickAddTask, { props: { createTask } })

    const input = screen.getByTestId('quick-add-task-input') as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'Slow create' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await fireEvent.keyDown(input, { key: 'Enter' })

    expect(createTask).toHaveBeenCalledTimes(1)
    expect(input).toHaveAttribute('aria-busy', 'true')
    expect(document.activeElement).toBe(input)

    resolveCreate('task-1')
    await waitFor(() => expect(input).toHaveValue(''))
    expect(createTask).toHaveBeenCalledTimes(1)
    expect(input).toHaveAttribute('aria-busy', 'false')
    expect(document.activeElement).toBe(input)
  })

  it('does not treat the post-submit clear as an empty blur cancel', async () => {
    let resolveCreate!: (id: string) => void
    const createTask = vi.fn(
      () =>
        new Promise<string>((resolve) => {
          resolveCreate = resolve
        })
    )
    const onCancel = vi.fn()
    render(QuickAddTask, { props: { createTask, onCancel } })

    const input = screen.getByTestId('quick-add-task-input') as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'Pending' } })
    await fireEvent.keyDown(input, { key: 'Enter' })

    resolveCreate('task-1')
    // One microtask lets the submit continuation clear the title; the refocus
    // is still scheduled, so a blur here is a race, not a click-away.
    await Promise.resolve()
    fireEvent.blur(input)
    expect(onCancel).not.toHaveBeenCalled()

    await waitFor(() => expect(input).toHaveValue(''))
    expect(document.activeElement).toBe(input)
  })

  it('Escape clears the draft when clearOnEscape is set', async () => {
    const onCancel = vi.fn()
    render(QuickAddTask, {
      props: { createTask: vi.fn(), clearOnEscape: true, onCancel }
    })

    const input = screen.getByTestId('quick-add-task-input') as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'Discard me' } })
    await fireEvent.keyDown(input, { key: 'Escape' })

    expect(input).toHaveValue('')
    expect(onCancel).not.toHaveBeenCalled()
  })

  it('Escape cancels via onCancel when clearOnEscape is not set', async () => {
    const onCancel = vi.fn()
    render(QuickAddTask, { props: { createTask: vi.fn(), onCancel } })

    const input = screen.getByTestId('quick-add-task-input') as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'Keep me' } })
    await fireEvent.keyDown(input, { key: 'Escape' })

    expect(onCancel).toHaveBeenCalledTimes(1)
    expect(input).toHaveValue('Keep me')
  })
})
