import { describe, expect, it } from 'vitest'

import {
  applyKeyboardEvent,
  createInitialKeyboardSpikeState,
  normalizeKeyboardEventData,
} from './keyboardState'

describe('keyboardState', () => {
  it('counts one down/up cycle for the target key', () => {
    let state = createInitialKeyboardSpikeState('V')

    state = applyKeyboardEvent(state, { key: 'v', pressed: true, source: 'dom' })
    state = applyKeyboardEvent(state, { key: 'V', pressed: false, source: 'dom' })

    expect(state.downCount).toBe(1)
    expect(state.upCount).toBe(1)
    expect(state.completedCycles).toBe(1)
    expect(state.isPressed).toBe(false)
    expect(state.missingRelease).toBe(false)
  })

  it('counts 10 consecutive target-key press/release cycles', () => {
    let state = createInitialKeyboardSpikeState('V')

    for (let index = 0; index < 10; index += 1) {
      state = applyKeyboardEvent(state, { key: 'V', pressed: true, source: 'native' })
      state = applyKeyboardEvent(state, { key: 'V', pressed: false, source: 'native' })
    }

    expect(state.downCount).toBe(10)
    expect(state.upCount).toBe(10)
    expect(state.completedCycles).toBe(10)
    expect(state.missingRelease).toBe(false)
  })

  it('ignores repeated keydown while already pressed', () => {
    let state = createInitialKeyboardSpikeState('V')

    state = applyKeyboardEvent(state, { key: 'V', pressed: true, source: 'native' })
    state = applyKeyboardEvent(state, { key: 'V', pressed: true, source: 'native' })
    state = applyKeyboardEvent(state, { key: 'V', pressed: false, source: 'native' })

    expect(state.downCount).toBe(1)
    expect(state.upCount).toBe(1)
    expect(state.completedCycles).toBe(1)
    expect(state.events.map((event) => event.pressed)).toEqual([false, true])
  })

  it('marks missing release after a down without a matching up', () => {
    const state = applyKeyboardEvent(createInitialKeyboardSpikeState('V'), {
      key: 'V',
      pressed: true,
      source: 'native',
    })

    expect(state.isPressed).toBe(true)
    expect(state.downCount).toBe(1)
    expect(state.upCount).toBe(0)
    expect(state.missingRelease).toBe(true)
  })

  it('ignores non-target keys', () => {
    const initial = createInitialKeyboardSpikeState('V')
    const state = applyKeyboardEvent(initial, { key: 'B', pressed: true, source: 'native' })

    expect(state).toBe(initial)
  })

  it('normalizes valid native event payloads and rejects malformed payloads', () => {
    expect(normalizeKeyboardEventData({ key: 'v', pressed: true, source: 'native' })).toEqual({
      key: 'V',
      pressed: true,
      source: 'native',
    })
    expect(normalizeKeyboardEventData({ key: 'V', pressed: 'true', source: 'native' })).toBeNull()
    expect(normalizeKeyboardEventData({ key: 'V', pressed: true, source: 'unknown' })).toBeNull()
  })
})
