// Regression for the settings save diff (#103). The per-field write loop must
// compare field VALUES (not references): a `list` field re-entered with the
// same content produces a fresh array reference that is equal by value to the
// upstream one. The old `!==` check flagged it as changed and wrote it on every
// save; JSON.stringify comparison (mirroring the `dirty` computation) skips it.

import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

const mocks = vi.hoisted(() => ({
  updatePluginSetting: vi.fn()
}))

vi.mock('../../../wailsjs/go/main/App.js', () => ({
  UpdatePluginSetting: mocks.updatePluginSetting
}))

import SettingsForm from './SettingsForm.svelte'

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

const schema = [
  { key: 'tags', label: 'Tags', type: 'list' },
  { key: 'name', label: 'Name', type: 'string' }
] as any

describe('SettingsForm save diff', () => {
  beforeEach(() => mocks.updatePluginSetting.mockReset())
  afterEach(() => cleanup())

  it('skips unchanged list settings even when their array reference differs', async () => {
    const values = { tags: ['a', 'b'], name: 'x' }
    render(SettingsForm, { props: { pluginID: 'p1', schema, values } })
    await flush()

    // Re-enter identical list content: oninput splits into a NEW array that is
    // equal to values.tags by value but a distinct reference — exactly the case
    // the old `!==` check wrongly treated as a change.
    const listInput = screen.getByLabelText('Tags') as HTMLInputElement
    await fireEvent.input(listInput, { target: { value: 'a, b' } })

    // Make the form genuinely dirty via the string field so Save renders.
    const nameInput = screen.getByLabelText('Name') as HTMLInputElement
    await fireEvent.input(nameInput, { target: { value: 'y' } })
    await flush()

    await fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await flush()

    // Only the changed string field is written; the equal-by-value list is not.
    expect(mocks.updatePluginSetting).toHaveBeenCalledTimes(1)
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith('p1', 'name', 'y')
  })
})
