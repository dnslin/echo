import { useState } from 'react'

import type { RoomClient, RoomEntry } from '../api/client'
import { normalizeInviteCode } from './inviteCode'

export type RoomIdentity = {
  anonymousId: string
  nickname: string
  avatarId: string
}

type RoomEntryViewProps = {
  identity: RoomIdentity
  client: RoomClient
  copyText?: (text: string) => Promise<void>
  onEnterRoom: (entry: RoomEntry) => void
  onOpenSettings: () => void
}

export function RoomEntryView({ identity, client, copyText, onEnterRoom, onOpenSettings }: RoomEntryViewProps) {
  const [roomName, setRoomName] = useState('')
  const [inviteCode, setInviteCode] = useState('')
  const [inviteCodeIsValid, setInviteCodeIsValid] = useState(true)
  const [createdEntry, setCreatedEntry] = useState<RoomEntry | null>(null)
  const [creating, setCreating] = useState(false)
  const [joining, setJoining] = useState(false)
  const [errorMessage, setErrorMessage] = useState<string | null>(null)
  const [copyNotice, setCopyNotice] = useState<string | null>(null)
  const canJoin = inviteCodeIsValid && inviteCode.length === 6 && !joining
  const visibleErrorMessage = inviteCodeIsValid ? errorMessage : '邀请码应为 6 位字母或数字'

  const createRoom = async () => {
    setCreating(true)
    setErrorMessage(null)
    try {
      const name = roomName.trim()
      const entry = await client.createRoom({
        anonymousId: identity.anonymousId,
        nickname: identity.nickname,
        avatarId: identity.avatarId,
        ...(name === '' ? {} : { roomName: name }),
      })
      setCreatedEntry(entry)
    } catch (error) {
      setErrorMessage(roomEntryErrorMessage(error))
    } finally {
      setCreating(false)
    }
  }

  const changeInviteCode = (value: string) => {
    const normalized = normalizeInviteCode(value)
    setInviteCode(normalized.value)
    setInviteCodeIsValid(normalized.isValid)
    setErrorMessage(null)
  }

  const joinRoom = async () => {
    if (!canJoin) {
      setErrorMessage(inviteCode === '' ? '请输入邀请码' : '邀请码应为 6 位字母或数字')
      return
    }

    setJoining(true)
    setErrorMessage(null)
    try {
      const entry = await client.joinRoom({
        inviteCode,
        anonymousId: identity.anonymousId,
        nickname: identity.nickname,
        avatarId: identity.avatarId,
      })
      onEnterRoom(entry)
    } catch (error) {
      setErrorMessage(roomEntryErrorMessage(error))
    } finally {
      setJoining(false)
    }
  }

  const copyInviteCode = async () => {
    if (!createdEntry) return

    try {
      await (copyText ?? copyToClipboard)(inviteMessage(createdEntry.room.inviteCode))
      setCopyNotice('邀请码已复制')
    } catch {
      setCopyNotice('复制失败，请手动复制邀请码')
    }
  }

  if (createdEntry) {
    return (
      <main className="room-entry-app room-entry-app--viewport">
        <section className="room-entry-card" aria-labelledby="room-created-title">
          <p className="room-entry-brand">echo</p>
          <h1 id="room-created-title" className="room-entry-card__title">房间已创建</h1>
          <p className="room-entry-room-name">{createdEntry.room.name}</p>
          <p className="room-entry-card__description">邀请朋友输入邀请码，一起进入临时房间。</p>
          <div className="room-entry-invite-code" aria-label={`邀请码 ${createdEntry.room.inviteCode}`}>
            <code>{createdEntry.room.inviteCode}</code>
          </div>
          {copyNotice && <p className="room-entry-copy-notice" role="status">{copyNotice}</p>}
          <div className="room-entry-actions">
            <button className="settings-button settings-button--secondary" type="button" onClick={() => void copyInviteCode()}>复制邀请码</button>
            <button className="settings-button settings-button--primary" type="button" onClick={() => onEnterRoom(createdEntry)}>进入房间</button>
          </div>
        </section>
      </main>
    )
  }

  return (
    <main className="room-entry-app room-entry-app--viewport">
      <section className="room-entry-card" aria-labelledby="room-entry-title">
        <header className="room-entry-card__header">
          <div>
            <p className="room-entry-brand">echo</p>
            <h1 id="room-entry-title" className="room-entry-card__title">创建或加入临时房间</h1>
          </div>
          <button className="settings-button settings-button--secondary" type="button" onClick={onOpenSettings}>设置</button>
        </header>
        <p className="room-entry-card__description">创建一个临时房间，或输入邀请码加入。</p>
        <div className="room-entry-identity">
          <span className="settings-avatar" aria-hidden="true">e</span>
          <span>{identity.nickname}</span>
        </div>
        <label className="settings-field">
          <span className="settings-field__label">邀请码</span>
          <div className="room-entry-invite-input">
            <input
              aria-describedby={visibleErrorMessage ? 'room-entry-error' : undefined}
              aria-invalid={!inviteCodeIsValid}
              autoComplete="off"
              className="room-entry-invite-input__control"
              inputMode="text"
              value={inviteCode}
              onChange={(event) => changeInviteCode(event.target.value)}
            />
            <span className="room-entry-invite-input__slots" aria-hidden="true">
              {Array.from({ length: 6 }, (_, index) => <span key={index}>{inviteCode[index] ?? ''}</span>)}
            </span>
          </div>
        </label>
        <div className="room-entry-actions">
          <button className="settings-button settings-button--primary" type="button" disabled={!canJoin || creating} onClick={() => void joinRoom()}>{joining ? '正在加入…' : '加入房间'}</button>
        </div>
        <label className="settings-field">
          <span className="settings-field__label">房间名（可选）</span>
          <input className="settings-field__control" value={roomName} onChange={(event) => setRoomName(event.target.value)} />
        </label>
        {visibleErrorMessage && <p id="room-entry-error" className="room-entry-error" role="alert">{visibleErrorMessage}</p>}
        <div className="room-entry-actions">
          <button className="settings-button settings-button--secondary" type="button" disabled={creating || joining} onClick={() => void createRoom()}>{creating ? '正在创建…' : '创建房间'}</button>
        </div>
      </section>
    </main>
  )
}

function inviteMessage(inviteCode: string): string {
  return `加入我的语音房间，邀请码：${inviteCode}\n请打开 echo 应用后输入邀请码加入。`
}

async function copyToClipboard(text: string): Promise<void> {
  if (!navigator.clipboard?.writeText) throw new Error('clipboard is unavailable')
  await navigator.clipboard.writeText(text)
}

function roomEntryErrorMessage(error: unknown): string {
  const code = typeof error === 'object' && error !== null && 'code' in error
    ? (error as { code?: unknown }).code
    : undefined

  switch (code) {
    case 'empty_invite_code':
      return '请输入邀请码'
    case 'invalid_invite_format':
      return '邀请码应为 6 位字母或数字'
    case 'invite_not_found':
      return '邀请码无效，请检查后重试'
    case 'room_expired':
      return '该房间已过期，请让朋友重新创建'
    case 'room_full':
      return '房间人数已满，暂时无法加入'
    case 'room_name_too_long':
      return '房间名称最多 24 个字符'
    default:
      return '连接失败，请检查网络后重试'
  }
}
