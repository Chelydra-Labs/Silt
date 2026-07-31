import { describe, it, expect, beforeEach } from 'vitest'
import {
  clampNoteZoom,
  noteZoom,
  NOTE_ZOOM_DEFAULT,
  NOTE_ZOOM_MAX,
  NOTE_ZOOM_MIN,
  NOTE_ZOOM_STEP
} from './noteZoom.svelte'

describe('noteZoom (#843)', () => {
  beforeEach(() => {
    noteZoom.reset()
  })

  it('defaults to 1 (100%)', () => {
    expect(noteZoom.factor).toBe(NOTE_ZOOM_DEFAULT)
    expect(noteZoom.percent).toBe(100)
  })

  it('clamps below min and above max', () => {
    expect(clampNoteZoom(0.1)).toBe(NOTE_ZOOM_MIN)
    expect(clampNoteZoom(5)).toBe(NOTE_ZOOM_MAX)
  })

  it('steps by 0.1 via zoomIn / zoomOut', () => {
    noteZoom.zoomIn()
    expect(noteZoom.factor).toBe(NOTE_ZOOM_DEFAULT + NOTE_ZOOM_STEP)
    expect(noteZoom.percent).toBe(110)
    noteZoom.zoomOut()
    expect(noteZoom.factor).toBe(NOTE_ZOOM_DEFAULT)
  })

  it('does not zoom past bounds', () => {
    noteZoom.setFactor(NOTE_ZOOM_MAX)
    noteZoom.zoomIn()
    expect(noteZoom.factor).toBe(NOTE_ZOOM_MAX)
    noteZoom.setFactor(NOTE_ZOOM_MIN)
    noteZoom.zoomOut()
    expect(noteZoom.factor).toBe(NOTE_ZOOM_MIN)
  })

  it('reset restores default', () => {
    noteZoom.setFactor(1.5)
    noteZoom.reset()
    expect(noteZoom.factor).toBe(NOTE_ZOOM_DEFAULT)
  })
})
