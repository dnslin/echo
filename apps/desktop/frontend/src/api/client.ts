export type RoomEntry = {
  room: {
    id: string
    name: string
    inviteCode: string
  }
  member: {
    id: string
    nickname: string
    avatarId: string
    isHost: boolean
  }
  roomSessionToken: string
}

export type CreateRoomInput = {
  anonymousId: string
  nickname: string
  avatarId: string
  roomName?: string
}

export type JoinRoomInput = {
  inviteCode: string
  anonymousId: string
  nickname: string
  avatarId: string
}

export type RoomApiErrorCode =
  | 'connection_failed'
  | 'invalid_response'
  | string

export class RoomApiError extends Error {
  readonly code: RoomApiErrorCode
  readonly status: number | undefined

  constructor(code: RoomApiErrorCode, message: string, status?: number) {
    super(message)
    this.name = 'RoomApiError'
    this.code = code
    this.status = status
  }
}

export type FetchImplementation = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>

export type RoomClient = {
  createRoom(input: CreateRoomInput): Promise<RoomEntry>
  joinRoom(input: JoinRoomInput): Promise<RoomEntry>
}

export type RoomClientOptions = {
  baseURL: string
  fetchImpl?: FetchImplementation
}

type RoomEntryResponse = {
  room: {
    id: string
    name: string
    invite_code: string
  }
  member: {
    id: string
    nickname: string
    avatar_id: string
    is_host: boolean
  }
  room_session_token: string
}

type ErrorResponse = {
  error: {
    code: string
    message: string
  }
}

const connectionFailedMessage = '连接失败，请检查网络后重试'

export function createRoomClient(options: RoomClientOptions): RoomClient {
  const fetchImpl = options.fetchImpl ?? globalThis.fetch
  const baseURL = options.baseURL.replace(/\/+$/, '')

  return {
    createRoom: (input) => requestRoomEntry(fetchImpl, `${baseURL}/v1/rooms`, 201, {
      anonymous_id: input.anonymousId,
      nickname: input.nickname,
      avatar_id: input.avatarId,
      ...(input.roomName === undefined ? {} : { room_name: input.roomName }),
    }),
    joinRoom: (input) => requestRoomEntry(fetchImpl, `${baseURL}/v1/rooms/join`, 200, {
      invite_code: input.inviteCode,
      anonymous_id: input.anonymousId,
      nickname: input.nickname,
      avatar_id: input.avatarId,
    }),
  }
}

async function requestRoomEntry(
  fetchImpl: FetchImplementation,
  url: string,
  expectedStatus: number,
  body: Record<string, string>,
): Promise<RoomEntry> {
  let response: Response
  try {
    response = await fetchImpl(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
  } catch {
    throw new RoomApiError('connection_failed', connectionFailedMessage)
  }

  const payload = await readJSON(response)
  if (response.status !== expectedStatus) {
    throw toApiError(payload, response.status)
  }

  return toRoomEntry(payload)
}

async function readJSON(response: Response): Promise<unknown> {
  try {
    return await response.json()
  } catch {
    return undefined
  }
}

function toApiError(payload: unknown, status: number): RoomApiError {
  if (isErrorResponse(payload)) {
    return new RoomApiError(payload.error.code, payload.error.message, status)
  }
  return new RoomApiError('connection_failed', connectionFailedMessage, status)
}

function toRoomEntry(payload: unknown): RoomEntry {
  if (!isRoomEntryResponse(payload)) {
    throw new RoomApiError('invalid_response', connectionFailedMessage)
  }

  return {
    room: {
      id: payload.room.id,
      name: payload.room.name,
      inviteCode: payload.room.invite_code,
    },
    member: {
      id: payload.member.id,
      nickname: payload.member.nickname,
      avatarId: payload.member.avatar_id,
      isHost: payload.member.is_host,
    },
    roomSessionToken: payload.room_session_token,
  }
}

function isErrorResponse(payload: unknown): payload is ErrorResponse {
  if (!isRecord(payload) || !isRecord(payload.error)) return false
  return typeof payload.error.code === 'string' && typeof payload.error.message === 'string'
}

function isRoomEntryResponse(payload: unknown): payload is RoomEntryResponse {
  if (!isRecord(payload) || !isRecord(payload.room) || !isRecord(payload.member)) return false
  return typeof payload.room.id === 'string'
    && typeof payload.room.name === 'string'
    && typeof payload.room.invite_code === 'string'
    && typeof payload.member.id === 'string'
    && typeof payload.member.nickname === 'string'
    && typeof payload.member.avatar_id === 'string'
    && typeof payload.member.is_host === 'boolean'
    && typeof payload.room_session_token === 'string'
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}
