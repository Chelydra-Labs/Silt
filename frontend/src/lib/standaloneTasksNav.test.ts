import { describe, it, expect } from 'vitest'
import {
  STANDALONE_TASKS_NOTEBOOK,
  isStandaloneTaskRef,
  routeJumpTarget
} from './standaloneTasksNav'

describe('STANDALONE_TASKS_NOTEBOOK (#374)', () => {
  it('matches the backend constant parser.StandaloneTasksNotebook', () => {
    expect(STANDALONE_TASKS_NOTEBOOK).toBe('.silt')
  })

  it('isStandaloneTaskRef returns true only for the standalone notebook', () => {
    expect(isStandaloneTaskRef('.silt')).toBe(true)
    expect(isStandaloneTaskRef('Work')).toBe(false)
    expect(isStandaloneTaskRef('')).toBe(false)
    expect(isStandaloneTaskRef('.silt-something-else')).toBe(false)
  })
})

describe('routeJumpTarget (#374)', () => {
  it('routes a .silt locator to the tasks-view (#374 AC1)', () => {
    const target = routeJumpTarget({
      notebook: '.silt',
      section: '',
      page: 'tasks',
      blockTarget: { blockId: 'abc', fileDate: '2026-07-02' }
    })
    expect(target).toEqual({
      kind: 'tasks-view',
      notebook: '.silt',
      blockTarget: { blockId: 'abc', fileDate: '2026-07-02' }
    })
  })

  it('routes a normal page locator to open-page', () => {
    const target = routeJumpTarget({
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      blockTarget: { blockId: 'xyz', fileDate: '2026-07-02' }
    })
    expect(target).toEqual({
      kind: 'open-page',
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      blockTarget: { blockId: 'xyz', fileDate: '2026-07-02' }
    })
  })

  it('forwards the blockTarget unchanged so the Tasks view can scroll+highlight (#374 AC4)', () => {
    const target = routeJumpTarget({
      notebook: '.silt',
      section: '',
      page: 'tasks',
      blockTarget: { blockId: 'task-uuid-1' }
    })
    if (target.kind !== 'tasks-view') {
      throw new Error('expected tasks-view')
    }
    expect(target.blockTarget?.blockId).toBe('task-uuid-1')
  })

  it('omits blockTarget when the caller did not pass one', () => {
    const target = routeJumpTarget({
      notebook: '.silt',
      section: '',
      page: 'tasks'
    })
    expect(target.kind).toBe('tasks-view')
    if (target.kind === 'tasks-view') {
      expect(target.blockTarget).toBeUndefined()
    }
  })

  it('never produces an open-page target when notebook is the synthetic .silt (#374 AC6)', () => {
    // Contract guard: even a future caller mistake cannot create a
    // `.silt` page tab — the router must always classify as tasks-view.
    const targets = [
      routeJumpTarget({ notebook: '.silt', section: '', page: 'tasks' }),
      routeJumpTarget({
        notebook: '.silt',
        section: 'anything',
        page: 'anything'
      }),
      routeJumpTarget({
        notebook: '.silt',
        section: '',
        page: 'tasks',
        blockTarget: { blockId: 'x' }
      })
    ]
    for (const t of targets) {
      expect(t.kind).toBe('tasks-view')
    }
  })
})
