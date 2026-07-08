import { describe, expect, it } from 'vitest'

import {
  applyHookStatus,
  applyKeyboardEvent,
  createInitialKeyboardSpikeState,
  normalizeHookStatusData,
  normalizeKeyboardEventData,
  resetKeyboardStats,
} from './keyboardState'

describe('keyboardState', () => {
  it('replays out-of-order native events by sequence', () => {
    let state = createInitialKeyboardSpikeState('V')

    state = applyKeyboardEvent(state, { key: 'V', pressed: false, source: 'native', sequence: 2 })
    expect(state.sources.native.completedCycles).toBe(0)
    expect(state.sources.native.missingRelease).toBe(false)

    state = applyKeyboardEvent(state, { key: 'V', pressed: true, source: 'native', sequence: 1 })

    expect(state.sources.native.downCount).toBe(1)
    expect(state.sources.native.upCount).toBe(1)
    expect(state.sources.native.completedCycles).toBe(1)
    expect(state.sources.native.isPressed).toBe(false)
    expect(state.sources.native.missingRelease).toBe(false)
    expect(state.sources.native.events.map((event) => event.sequence)).toEqual([2, 1])
  })

  it('keeps native and DOM fallback counts, state, and logs isolated', () => {
    let state = createInitialKeyboardSpikeState('V')

    state = applyKeyboardEvent(state, { key: 'V', pressed: true, source: 'dom' })
    state = applyKeyboardEvent(state, { key: 'V', pressed: false, source: 'dom' })
    state = applyKeyboardEvent(state, { key: 'V', pressed: true, source: 'native', sequence: 1 })
    state = applyKeyboardEvent(state, { key: 'V', pressed: false, source: 'native', sequence: 2 })

    expect(state.sources.dom.completedCycles).toBe(1)
    expect(state.sources.native.completedCycles).toBe(1)
    expect(state.sources.dom.events).toHaveLength(2)
    expect(state.sources.native.events).toHaveLength(2)
    expect(state.sources.dom.events.every((event) => event.source === 'dom')).toBe(true)
    expect(state.sources.native.events.every((event) => event.source === 'native')).toBe(true)
  })

  it('counts 10 consecutive target-key cycles for native and DOM paths independently', () => {
    let state = createInitialKeyboardSpikeState('V')

    for (let cycle = 0; cycle < 10; cycle += 1) {
      state = applyKeyboardEvent(state, { key: 'V', pressed: true, source: 'dom' })
      state = applyKeyboardEvent(state, { key: 'V', pressed: false, source: 'dom' })
      state = applyKeyboardEvent(state, { key: 'V', pressed: true, source: 'native', sequence: cycle * 2 + 1 })
      state = applyKeyboardEvent(state, { key: 'V', pressed: false, source: 'native', sequence: cycle * 2 + 2 })
    }

    expect(state.sources.dom.completedCycles).toBe(10)
    expect(state.sources.native.completedCycles).toBe(10)
    expect(state.sources.dom.missingRelease).toBe(false)
    expect(state.sources.native.missingRelease).toBe(false)
  })

  it('ignores non-target keys and release events without matching down events', () => {
    let state = createInitialKeyboardSpikeState('V')

    state = applyKeyboardEvent(state, { key: 'B', pressed: true, source: 'dom' })
    state = applyKeyboardEvent(state, { key: 'V', pressed: false, source: 'dom' })
    state = applyKeyboardEvent(state, { key: 'V', pressed: false, source: 'native', sequence: 1 })

    expect(state.sources.dom.downCount).toBe(0)
    expect(state.sources.dom.upCount).toBe(0)
    expect(state.sources.native.downCount).toBe(0)
    expect(state.sources.native.upCount).toBe(0)
    expect(state.sources.native.completedCycles).toBe(0)
  })

  it('suppresses repeated target-key down events per source', () => {
    let state = createInitialKeyboardSpikeState('V')

    state = applyKeyboardEvent(state, { key: 'V', pressed: true, source: 'dom' })
    state = applyKeyboardEvent(state, { key: 'V', pressed: true, source: 'dom' })
    state = applyKeyboardEvent(state, { key: 'V', pressed: false, source: 'dom' })

    expect(state.sources.dom.downCount).toBe(1)
    expect(state.sources.dom.upCount).toBe(1)
    expect(state.sources.dom.completedCycles).toBe(1)
  })

  it('ignores malformed native payloads without sequence and malformed hook status payloads', () => {
    expect(normalizeKeyboardEventData({ key: 'V', pressed: true, source: 'native' })).toBeNull()
    expect(normalizeKeyboardEventData({ key: 'V', pressed: true, source: 'native', sequence: 0 })).toBeNull()
    expect(normalizeKeyboardEventData({ key: 'V', pressed: true, source: 'native', sequence: 1 })).toEqual({
      key: 'V',
      pressed: true,
      source: 'native',
      sequence: 1,
    })
    expect(normalizeHookStatusData({ status: 'disabled', message: 'install failed' })).toEqual({
      status: 'disabled',
      message: 'install failed',
    })
    expect(normalizeHookStatusData({ status: 'broken', message: 'install failed' })).toBeNull()
  })

  it('updates hook status and resets counters without losing hook status', () => {
    let state = createInitialKeyboardSpikeState('V')

    state = applyHookStatus(state, { status: 'disabled', message: 'install failed' })
    state = applyKeyboardEvent(state, { key: 'V', pressed: true, source: 'dom' })
    state = resetKeyboardStats(state)

    expect(state.hookStatus).toEqual({ status: 'disabled', message: 'install failed' })
    expect(state.sources.dom.downCount).toBe(0)
    expect(state.sources.native.downCount).toBe(0)
  })

  it('preserves the next native sequence across reset so later hook events still replay', () => {
    let state = createInitialKeyboardSpikeState('V')

    state = applyKeyboardEvent(state, { key: 'V', pressed: true, source: 'native', sequence: 1 })
    state = applyKeyboardEvent(state, { key: 'V', pressed: false, source: 'native', sequence: 2 })
    state = resetKeyboardStats(state)
    state = applyKeyboardEvent(state, { key: 'V', pressed: true, source: 'native', sequence: 3 })
    state = applyKeyboardEvent(state, { key: 'V', pressed: false, source: 'native', sequence: 4 })

    expect(state.nativeOrdering.nextSequence).toBe(5)
    expect(state.nativeOrdering.pending).toEqual({})
    expect(state.sources.native.downCount).toBe(1)
    expect(state.sources.native.upCount).toBe(1)
    expect(state.sources.native.completedCycles).toBe(1)
    expect(state.sources.native.missingRelease).toBe(false)
  })

  it('advances past cleared native pending events on reset so a later round can start cleanly', () => {
    let state = createInitialKeyboardSpikeState('V')

    state = applyKeyboardEvent(state, { key: 'V', pressed: false, source: 'native', sequence: 2 })
    state = resetKeyboardStats(state)
    state = applyKeyboardEvent(state, { key: 'V', pressed: true, source: 'native', sequence: 3 })
    state = applyKeyboardEvent(state, { key: 'V', pressed: false, source: 'native', sequence: 4 })

    expect(state.nativeOrdering.nextSequence).toBe(5)
    expect(state.nativeOrdering.pending).toEqual({})
    expect(state.sources.native.completedCycles).toBe(1)
    expect(state.sources.native.missingRelease).toBe(false)
  })
})
