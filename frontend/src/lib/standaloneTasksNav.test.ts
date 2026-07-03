import { describe, it, expect } from 'vitest'
import {
  STANDALONE_TASKS_NOTEBOOK,
  isStandaloneTaskRef
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
