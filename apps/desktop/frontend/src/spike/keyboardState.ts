export const KEYBOARD_EVENT_NAME = 'keyboard:push-to-talk'
export const KEYBOARD_HOOK_STATUS_EVENT_NAME = 'keyboard:hook-status'
export const KEYBOARD_HOOK_STATUS_REQUEST_EVENT_NAME = 'keyboard:hook-status-request'
export const DEFAULT_TARGET_KEY = 'V'

const MAX_EVENT_RECORDS = 20

export type KeyboardEventSource = 'native' | 'dom'
export type KeyboardHookStatusName = 'unknown' | 'enabled' | 'disabled' | 'unsupported'

export type KeyboardHookStatus = {
  status: KeyboardHookStatusName
  message?: string
}

export type KeyboardInputEvent = {
  key: string
  pressed: boolean
  source: KeyboardEventSource
  sequence?: number
}

export type KeyboardEventRecord = {
  key: string
  pressed: boolean
  source: KeyboardEventSource
  sequence?: number
  logIndex: number
}

export type KeyboardSourceState = {
  isPressed: boolean
  downCount: number
  upCount: number
  completedCycles: number
  missingRelease: boolean
  events: KeyboardEventRecord[]
}

export type NativeOrderingState = {
  nextSequence: number
  pending: Record<number, KeyboardInputEvent>
}

export type KeyboardSpikeState = {
  targetKey: string
  sources: Record<KeyboardEventSource, KeyboardSourceState>
  nativeOrdering: NativeOrderingState
  hookStatus: KeyboardHookStatus
}

export function createInitialKeyboardSpikeState(targetKey = DEFAULT_TARGET_KEY): KeyboardSpikeState {
  return {
    targetKey: normalizeKey(targetKey) || DEFAULT_TARGET_KEY,
    sources: {
      native: createEmptySourceState(),
      dom: createEmptySourceState(),
    },
    nativeOrdering: createInitialNativeOrdering(),
    hookStatus: { status: 'unknown', message: '等待 native hook 状态' },
  }
}

export function applyKeyboardEvent(state: KeyboardSpikeState, event: KeyboardInputEvent): KeyboardSpikeState {
  const key = normalizeKey(event.key)
  if (key !== state.targetKey) return state

  const normalizedEvent: KeyboardInputEvent = { ...event, key }
  if (normalizedEvent.source === 'native') {
    return applyNativeEventBySequence(state, normalizedEvent)
  }

  return applySourceTransition(state, normalizedEvent)
}

export function applyHookStatus(state: KeyboardSpikeState, hookStatus: KeyboardHookStatus): KeyboardSpikeState {
  return {
    ...state,
    hookStatus,
  }
}

export function resetKeyboardStats(state: KeyboardSpikeState): KeyboardSpikeState {
  return {
    ...state,
    sources: {
      native: createEmptySourceState(),
      dom: createEmptySourceState(),
    },
    nativeOrdering: {
      nextSequence: nextNativeSequenceAfterReset(state.nativeOrdering),
      pending: {},
    },
  }
}

export function normalizeKeyboardEventData(data: unknown): KeyboardInputEvent | null {
  if (!isKeyboardPayload(data)) return null

  const key = normalizeKey(data.key)
  if (!key) return null

  if (data.source === 'native') {
    if (!isPositiveInteger(data.sequence)) return null
    return {
      key,
      pressed: data.pressed,
      source: 'native',
      sequence: data.sequence,
    }
  }

  return {
    key,
    pressed: data.pressed,
    source: 'dom',
  }
}

export function normalizeHookStatusData(data: unknown): KeyboardHookStatus | null {
  if (!data || typeof data !== 'object') return null
  const candidate = data as { status?: unknown; message?: unknown }
  if (!isHookStatusName(candidate.status)) return null

  const status: KeyboardHookStatus = { status: candidate.status }
  if (typeof candidate.message === 'string' && candidate.message.trim() !== '') {
    status.message = candidate.message
  }
  return status
}

export function keyboardDomEventToInput(event: KeyboardEvent, pressed: boolean): KeyboardInputEvent | null {
  const key = normalizeKey(event.key)
  if (!key) return null

  return { key, pressed, source: 'dom' }
}

function nextNativeSequenceAfterReset(nativeOrdering: NativeOrderingState): number {
  const pendingSequences = Object.keys(nativeOrdering.pending).map(Number).filter(isPositiveInteger)
  if (pendingSequences.length === 0) return nativeOrdering.nextSequence

  return Math.max(nativeOrdering.nextSequence, Math.max(...pendingSequences) + 1)
}

function applyNativeEventBySequence(state: KeyboardSpikeState, event: KeyboardInputEvent): KeyboardSpikeState {
  const sequence = event.sequence
  if (!isPositiveInteger(sequence)) return state
  if (sequence < state.nativeOrdering.nextSequence) return state

  let nextState: KeyboardSpikeState = {
    ...state,
    nativeOrdering: {
      ...state.nativeOrdering,
      pending: {
        ...state.nativeOrdering.pending,
        [sequence]: event,
      },
    },
  }

  while (true) {
    const nextSequence = nextState.nativeOrdering.nextSequence
    const queuedEvent = nextState.nativeOrdering.pending[nextSequence]
    if (!queuedEvent) return nextState

    const pending = { ...nextState.nativeOrdering.pending }
    delete pending[nextSequence]
    nextState = applySourceTransition(
      {
        ...nextState,
        nativeOrdering: {
          nextSequence: nextSequence + 1,
          pending,
        },
      },
      queuedEvent,
    )
  }
}

function applySourceTransition(state: KeyboardSpikeState, event: KeyboardInputEvent): KeyboardSpikeState {
  const sourceState = state.sources[event.source]

  if (event.pressed) {
    if (sourceState.isPressed) return state

    return replaceSourceState(state, event.source, withDerivedFlags({
      ...sourceState,
      isPressed: true,
      downCount: sourceState.downCount + 1,
      events: prependEvent(sourceState, event),
    }))
  }

  if (!sourceState.isPressed) return state

  return replaceSourceState(state, event.source, withDerivedFlags({
    ...sourceState,
    isPressed: false,
    upCount: sourceState.upCount + 1,
    completedCycles: sourceState.completedCycles + 1,
    events: prependEvent(sourceState, event),
  }))
}

function replaceSourceState(
  state: KeyboardSpikeState,
  source: KeyboardEventSource,
  sourceState: KeyboardSourceState,
): KeyboardSpikeState {
  return {
    ...state,
    sources: {
      ...state.sources,
      [source]: sourceState,
    },
  }
}

function withDerivedFlags(state: KeyboardSourceState): KeyboardSourceState {
  return {
    ...state,
    missingRelease: state.isPressed || state.downCount > state.upCount,
  }
}

function prependEvent(sourceState: KeyboardSourceState, event: KeyboardInputEvent): KeyboardEventRecord[] {
  const logIndex = (sourceState.events[0]?.logIndex ?? 0) + 1
  return [{ ...event, logIndex }, ...sourceState.events].slice(0, MAX_EVENT_RECORDS)
}

function createEmptySourceState(): KeyboardSourceState {
  return {
    isPressed: false,
    downCount: 0,
    upCount: 0,
    completedCycles: 0,
    missingRelease: false,
    events: [],
  }
}

function createInitialNativeOrdering(): NativeOrderingState {
  return {
    nextSequence: 1,
    pending: {},
  }
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

function isPositiveInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value > 0
}

function isHookStatusName(value: unknown): value is KeyboardHookStatusName {
  return value === 'enabled' || value === 'disabled' || value === 'unsupported'
}
