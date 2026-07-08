export const KEYBOARD_EVENT_NAME = 'keyboard:push-to-talk'
export const DEFAULT_TARGET_KEY = 'V'
const MAX_EVENT_RECORDS = 20

export type KeyboardEventSource = 'native' | 'dom'

export type KeyboardInputEvent = {
  key: string
  pressed: boolean
  source: KeyboardEventSource
}

export type KeyboardEventRecord = KeyboardInputEvent & {
  sequence: number
}

export type KeyboardSpikeState = {
  targetKey: string
  isPressed: boolean
  downCount: number
  upCount: number
  completedCycles: number
  missingRelease: boolean
  events: KeyboardEventRecord[]
}

export function createInitialKeyboardSpikeState(targetKey = DEFAULT_TARGET_KEY): KeyboardSpikeState {
  return {
    targetKey: normalizeKey(targetKey) || DEFAULT_TARGET_KEY,
    isPressed: false,
    downCount: 0,
    upCount: 0,
    completedCycles: 0,
    missingRelease: false,
    events: [],
  }
}

export function applyKeyboardEvent(state: KeyboardSpikeState, event: KeyboardInputEvent): KeyboardSpikeState {
  const key = normalizeKey(event.key)
  if (key !== state.targetKey) return state

  if (event.pressed) {
    if (state.isPressed) return state

    return withDerivedFlags({
      ...state,
      isPressed: true,
      downCount: state.downCount + 1,
      events: prependEvent(state, { ...event, key, pressed: true }),
    })
  }

  if (!state.isPressed) return state

  return withDerivedFlags({
    ...state,
    isPressed: false,
    upCount: state.upCount + 1,
    completedCycles: state.completedCycles + 1,
    events: prependEvent(state, { ...event, key, pressed: false }),
  })
}

export function normalizeKeyboardEventData(data: unknown): KeyboardInputEvent | null {
  if (!isKeyboardPayload(data)) return null

  const key = normalizeKey(data.key)
  if (!key) return null

  return {
    key,
    pressed: data.pressed,
    source: data.source,
  }
}

export function keyboardDomEventToInput(event: KeyboardEvent, pressed: boolean): KeyboardInputEvent | null {
  const key = normalizeKey(event.key)
  if (!key) return null

  return { key, pressed, source: 'dom' }
}

function withDerivedFlags(state: KeyboardSpikeState): KeyboardSpikeState {
  return {
    ...state,
    missingRelease: state.isPressed || state.downCount > state.upCount,
  }
}

function prependEvent(state: KeyboardSpikeState, event: KeyboardInputEvent): KeyboardEventRecord[] {
  const sequence = (state.events[0]?.sequence ?? 0) + 1
  return [{ ...event, sequence }, ...state.events].slice(0, MAX_EVENT_RECORDS)
}

function normalizeKey(key: string): string {
  return key.trim().toUpperCase()
}

function isKeyboardPayload(data: unknown): data is KeyboardInputEvent {
  if (!data || typeof data !== 'object') return false
  const candidate = data as Partial<Record<keyof KeyboardInputEvent, unknown>>

  return (
    typeof candidate.key === 'string' &&
    typeof candidate.pressed === 'boolean' &&
    (candidate.source === 'native' || candidate.source === 'dom')
  )
}
