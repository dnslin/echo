import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { RoomClient, RoomEntry } from '../api/client'
import { RoomEntryView } from './RoomEntryView'

const createdEntry: RoomEntry = {
  room: { id: 'room-1', name: '今晚开黑', inviteCode: 'K7M9Q2' },
  member: { id: 'member-1', nickname: '小王', avatarId: 'avatar-1', isHost: true },
  roomSessionToken: 'room-session-token',
}

afterEach(cleanup)

describe('RoomEntryView', () => {
  it('creates a temporary room and shows its invite code before entering the room', async () => {
    const client: RoomClient = {
      createRoom: vi.fn().mockResolvedValue(createdEntry),
      joinRoom: vi.fn(),
    }
    const onEnterRoom = vi.fn()

    render(
      <RoomEntryView
        identity={{ anonymousId: 'anonymous-1', nickname: '小王', avatarId: 'avatar-1' }}
        client={client}
        onEnterRoom={onEnterRoom}
        onOpenSettings={vi.fn()}
      />,
    )

    fireEvent.change(screen.getByLabelText('房间名（可选）'), { target: { value: '今晚开黑' } })
    fireEvent.click(screen.getByRole('button', { name: '创建房间' }))

    await waitFor(() => {
      expect(client.createRoom).toHaveBeenCalledWith({
        anonymousId: 'anonymous-1',
        nickname: '小王',
        avatarId: 'avatar-1',
        roomName: '今晚开黑',
      })
    })
    expect(await screen.findByRole('heading', { name: '房间已创建' })).toBeVisible()
    expect(screen.getByText('今晚开黑')).toBeVisible()
    expect(screen.getByText('K7M9Q2')).toBeVisible()
    expect(onEnterRoom).not.toHaveBeenCalled()
  })

  it('shows the server-provided default room name after creation', async () => {
    const defaultNamedEntry: RoomEntry = {
      ...createdEntry,
      room: { ...createdEntry.room, name: '小王的房间' },
    }
    const client: RoomClient = {
      createRoom: vi.fn().mockResolvedValue(defaultNamedEntry),
      joinRoom: vi.fn(),
    }

    render(
      <RoomEntryView
        identity={{ anonymousId: 'anonymous-1', nickname: '小王', avatarId: 'avatar-1' }}
        client={client}
        onEnterRoom={vi.fn()}
        onOpenSettings={vi.fn()}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '创建房间' }))

    await screen.findByRole('heading', { name: '房间已创建' })
    expect(client.createRoom).toHaveBeenCalledWith({
      anonymousId: 'anonymous-1',
      nickname: '小王',
      avatarId: 'avatar-1',
    })
    expect(screen.getByText('小王的房间')).toBeVisible()
  })

  it('copies an invite message and confirms the copy without a system notification', async () => {
    const client: RoomClient = {
      createRoom: vi.fn().mockResolvedValue(createdEntry),
      joinRoom: vi.fn(),
    }
    const copyText = vi.fn().mockResolvedValue(undefined)

    render(
      <RoomEntryView
        identity={{ anonymousId: 'anonymous-1', nickname: '小王', avatarId: 'avatar-1' }}
        client={client}
        copyText={copyText}
        onEnterRoom={vi.fn()}
        onOpenSettings={vi.fn()}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '创建房间' }))
    await screen.findByRole('heading', { name: '房间已创建' })
    fireEvent.click(screen.getByRole('button', { name: '复制邀请码' }))

    await waitFor(() => {
      expect(copyText).toHaveBeenCalledWith('加入我的语音房间，邀请码：K7M9Q2\n请打开 echo 应用后输入邀请码加入。')
    })
    expect(await screen.findByRole('status')).toHaveTextContent('邀请码已复制')
  })

  it('normalizes an invite code and enters the room after a successful join', async () => {
    const joinedEntry: RoomEntry = {
      room: { id: 'room-2', name: '朋友的房间', inviteCode: 'K7M9Q2' },
      member: { id: 'member-2', nickname: '小王', avatarId: 'avatar-1', isHost: false },
      roomSessionToken: 'room-session-token',
    }
    const client: RoomClient = {
      createRoom: vi.fn(),
      joinRoom: vi.fn().mockResolvedValue(joinedEntry),
    }
    const onEnterRoom = vi.fn()

    render(
      <RoomEntryView
        identity={{ anonymousId: 'anonymous-1', nickname: '小王', avatarId: 'avatar-1' }}
        client={client}
        onEnterRoom={onEnterRoom}
        onOpenSettings={vi.fn()}
      />,
    )

    const inviteCode = screen.getByLabelText('邀请码')
    fireEvent.change(inviteCode, { target: { value: ' k7-m9 q2 ' } })
    expect(inviteCode).toHaveValue('K7M9Q2')
    fireEvent.click(screen.getByRole('button', { name: '加入房间' }))

    await waitFor(() => {
      expect(client.joinRoom).toHaveBeenCalledWith({
        inviteCode: 'K7M9Q2',
        anonymousId: 'anonymous-1',
        nickname: '小王',
        avatarId: 'avatar-1',
      })
    })
    expect(onEnterRoom).toHaveBeenCalledWith(joinedEntry)
  })

  it.each([
    ['邀请码无效，请检查后重试', 'invite_not_found'],
    ['该房间已过期，请让朋友重新创建', 'room_expired'],
    ['房间人数已满，暂时无法加入', 'room_full'],
  ])('shows the PRD message for %s and keeps the invite code editable', async (message, code) => {
    const client: RoomClient = {
      createRoom: vi.fn(),
      joinRoom: vi.fn().mockRejectedValue({ code }),
    }
    const onEnterRoom = vi.fn()

    render(
      <RoomEntryView
        identity={{ anonymousId: 'anonymous-1', nickname: '小王', avatarId: 'avatar-1' }}
        client={client}
        onEnterRoom={onEnterRoom}
        onOpenSettings={vi.fn()}
      />,
    )

    const inviteCode = screen.getByLabelText('邀请码')
    fireEvent.change(inviteCode, { target: { value: 'K7M9Q2' } })
    fireEvent.click(screen.getByRole('button', { name: '加入房间' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(message)
    expect(inviteCode).toHaveValue('K7M9Q2')
    expect(onEnterRoom).not.toHaveBeenCalled()
  })
})
