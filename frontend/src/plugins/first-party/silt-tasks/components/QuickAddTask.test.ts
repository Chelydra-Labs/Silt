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
})
