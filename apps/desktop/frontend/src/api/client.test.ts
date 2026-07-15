import { describe, expect, it, vi } from 'vitest'

import { RoomApiError, createRoomClient } from './client'

const successfulRoomResponse = {
  room: {
    id: 'room-1',
    name: '今晚开黑',
    invite_code: 'K7M9Q2',
  },
  member: {
    id: 'member-1',
    nickname: '小王',
    avatar_id: 'avatar-1',
    is_host: true,
  },
  room_session_token: 'room-session-token',
  livekit_url: 'wss://livekit.example.com',
  livekit_token: 'livekit-token',
}

describe('Room API client', () => {
  it('creates a temporary room with the OpenAPI request shape and returns a safe room entry', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(new Response(JSON.stringify(successfulRoomResponse), {
      status: 201,
      headers: { 'Content-Type': 'application/json' },
    }))
    const client = createRoomClient({ baseURL: 'https://api.example.com/', fetchImpl })

    const entry = await client.createRoom({
      anonymousId: 'anonymous-1',
      nickname: '小王',
      avatarId: 'avatar-1',
      roomName: '今晚开黑',
    })

    expect(fetchImpl).toHaveBeenCalledWith('https://api.example.com/v1/rooms', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        anonymous_id: 'anonymous-1',
        nickname: '小王',
        avatar_id: 'avatar-1',
        room_name: '今晚开黑',
      }),
    })
    expect(entry).toEqual({
      room: { id: 'room-1', name: '今晚开黑', inviteCode: 'K7M9Q2' },
      member: { id: 'member-1', nickname: '小王', avatarId: 'avatar-1', isHost: true },
      roomSessionToken: 'room-session-token',
    })
    expect(JSON.stringify(entry)).not.toContain('livekit-token')
  })

  it('joins a temporary room with the normalized invite code', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(new Response(JSON.stringify(successfulRoomResponse), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }))
    const client = createRoomClient({ baseURL: 'https://api.example.com', fetchImpl })

    await client.joinRoom({
      inviteCode: 'K7M9Q2',
      anonymousId: 'anonymous-2',
      nickname: '小李',
      avatarId: 'avatar-2',
    })

    expect(fetchImpl).toHaveBeenCalledWith('https://api.example.com/v1/rooms/join', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        invite_code: 'K7M9Q2',
        anonymous_id: 'anonymous-2',
        nickname: '小李',
        avatar_id: 'avatar-2',
      }),
    })
  })

  it('maps a structured API error to a typed error without changing its contract', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      error: { code: 'room_expired', message: '该房间已过期，请让朋友重新创建' },
    }), {
      status: 410,
      headers: { 'Content-Type': 'application/json' },
    }))
    const client = createRoomClient({ baseURL: 'https://api.example.com', fetchImpl })

    const promise = client.joinRoom({
      inviteCode: 'K7M9Q2',
      anonymousId: 'anonymous-2',
      nickname: '小李',
      avatarId: 'avatar-2',
    })

    await expect(promise).rejects.toEqual(expect.objectContaining({
      name: 'RoomApiError',
      code: 'room_expired',
      status: 410,
      message: '该房间已过期，请让朋友重新创建',
    }))
  })

  it.each([
    ['network failure', () => Promise.reject(new TypeError('network unavailable')), 'connection_failed'],
    ['malformed success response', () => Promise.resolve(new Response(JSON.stringify({ room: {} }), { status: 201 })), 'invalid_response'],
  ])('does not expose transport details for a %s', async (_scenario, fetchResponse, code) => {
    const client = createRoomClient({ baseURL: 'https://api.example.com', fetchImpl: vi.fn(fetchResponse) })

    const promise = client.createRoom({
      anonymousId: 'anonymous-1',
      nickname: '小王',
      avatarId: 'avatar-1',
    })

    await expect(promise).rejects.toBeInstanceOf(RoomApiError)
    await expect(promise).rejects.toEqual(expect.objectContaining({
      code,
      message: '连接失败，请检查网络后重试',
    }))
  })
})
