import { type CSSProperties, useEffect, useRef, useState } from 'react'
import { Room, RoomEvent, Track } from 'livekit-client'

type ConnectionStatus = 'idle' | 'connecting' | 'connected' | 'failed' | 'disconnected'

type EventEntry = {
  id: number
  text: string
}

type DetachableAudioTrack = {
  detach: () => HTMLElement[]
}

type RemoteAudioAttachment = {
  track: DetachableAudioTrack
  element: HTMLMediaElement
}

const statusText: Record<ConnectionStatus, string> = {
  idle: '未连接',
  connecting: '连接中',
  connected: '已连接',
  failed: '失败',
  disconnected: '已断开',
}

export function LiveKitAudioSpike() {
  const [url, setUrl] = useState('wss://livekit.example.com')
  const [token, setToken] = useState('')
  const [status, setStatus] = useState<ConnectionStatus>('idle')
  const [error, setError] = useState('')
  const [events, setEvents] = useState<EventEntry[]>([])
  const roomRef = useRef<Room | null>(null)
  const remoteAudioContainerRef = useRef<HTMLDivElement | null>(null)
  const remoteAudioAttachmentsRef = useRef<RemoteAudioAttachment[]>([])
  const activeTokenRef = useRef('')
  const nextEventIDRef = useRef(1)

  useEffect(() => {
    return () => {
      void cleanupCurrentRoom('disconnected', false)
    }
  }, [])

  async function connectAndPublish() {
    const livekitUrl = url.trim()
    const joinToken = token.trim()
    if (!livekitUrl) {
      setStatus('failed')
      setError('请输入 LiveKit URL')
      return
    }
    if (!joinToken) {
      setStatus('failed')
      setError('请输入短期 join token')
      return
    }

    await cleanupCurrentRoom('disconnected', false)
    activeTokenRef.current = joinToken
    setStatus('connecting')
    setError('')
    addEvent('开始连接 LiveKit。')

    const room = new Room({ adaptiveStream: false, dynacast: false })
    roomRef.current = room
    registerRoomEvents(room, joinToken)

    try {
      await room.connect(livekitUrl, joinToken, { autoSubscribe: true })
      addEvent('LiveKit 房间已连接。')
      await room.localParticipant.setMicrophoneEnabled(true)
      addEvent('本地麦克风已发布。')
      if (roomRef.current === room) setStatus('connected')
    } catch (caught) {
      if (roomRef.current === room) {
        roomRef.current = null
        setStatus('failed')
        const message = formatError(caught, [joinToken, activeTokenRef.current])
        setError(message)
        addEvent(`连接失败：${message}`)
      }
      cleanupRemoteAudioElements()
      try {
        await room.disconnect(true)
      } catch {
        // Ignore cleanup errors so the visible failure remains the original connection error.
      } finally {
        activeTokenRef.current = ''
      }
    }
  }

  async function disconnect() {
    await cleanupCurrentRoom('disconnected', true)
  }

  async function cleanupCurrentRoom(nextStatus: ConnectionStatus, emitEvent: boolean) {
    const room = roomRef.current
    roomRef.current = null
    cleanupRemoteAudioElements()
    if (!room) {
      activeTokenRef.current = ''
      setStatus(nextStatus)
      return
    }

    try {
      await room.disconnect(true)
      if (emitEvent) addEvent('已断开房间并释放麦克风。')
    } catch (caught) {
      const message = formatError(caught, [activeTokenRef.current, token.trim()])
      setError(message)
      if (emitEvent) addEvent(`断开时出现错误：${message}`)
    } finally {
      activeTokenRef.current = ''
      setStatus(nextStatus)
    }
  }

  async function resumePlayback() {
    const room = roomRef.current
    if (!room) {
      setError('当前没有已连接的 LiveKit 房间。')
      return
    }

    try {
      await room.startAudio()
      addEvent('已请求恢复远端音频播放。')
    } catch (caught) {
      const message = formatError(caught, [activeTokenRef.current, token.trim()])
      setError(message)
      addEvent(`恢复播放失败：${message}`)
    }
  }

  function registerRoomEvents(room: Room, joinToken: string) {
    room.on(RoomEvent.Connected, () => {
      if (roomRef.current !== room) return
      setStatus('connected')
      addEvent('连接事件：已连接。')
    })
    room.on(RoomEvent.Disconnected, (reason) => {
      if (roomRef.current !== room) return
      setStatus('disconnected')
      addEvent(reason ? `连接事件：已断开（${String(reason)}）。` : '连接事件：已断开。')
      cleanupRemoteAudioElements()
    })
    room.on(RoomEvent.Reconnecting, () => {
      if (roomRef.current !== room) return
      setStatus('connecting')
      addEvent('连接事件：正在重连。')
    })
    room.on(RoomEvent.Reconnected, () => {
      if (roomRef.current !== room) return
      setStatus('connected')
      addEvent('连接事件：重连成功。')
    })
    room.on(RoomEvent.TrackSubscribed, (track) => {
      if (roomRef.current !== room) return
      if (track.kind !== Track.Kind.Audio) return
      const element = track.attach()
      element.autoplay = true
      element.controls = true
      element.dataset.echoLiveKitSpike = 'remote-audio'
      remoteAudioAttachmentsRef.current.push({ track, element })
      remoteAudioContainerRef.current?.appendChild(element)
      addEvent('已订阅并挂载远端音频。')
    })
    room.on(RoomEvent.TrackUnsubscribed, (track) => {
      if (roomRef.current !== room) return
      if (track.kind !== Track.Kind.Audio) return
      removeRemoteTrackAudio(track)
      addEvent('远端音频已取消订阅。')
    })
    room.on(RoomEvent.AudioPlaybackStatusChanged, () => {
      if (roomRef.current !== room) return
      addEvent(room.canPlaybackAudio ? '远端音频播放已启用。' : '远端音频播放可能被阻止，请点击恢复播放。')
    })
    room.on(RoomEvent.MediaDevicesChanged, () => {
      if (roomRef.current !== room) return
      addEvent('检测到媒体设备变化。')
    })
    room.on(RoomEvent.MediaDevicesError, (mediaError) => {
      if (roomRef.current !== room) return
      const message = formatError(mediaError, [joinToken, activeTokenRef.current])
      setError(message)
      addEvent(`媒体设备错误：${message}`)
    })
  }

  function cleanupRemoteAudioElements() {
    const attachments = remoteAudioAttachmentsRef.current
    remoteAudioAttachmentsRef.current = []

    attachments.forEach(({ track, element }) => {
      let detachedElements: HTMLElement[] = []
      try {
        detachedElements = track.detach()
      } catch {
        detachedElements = []
      }
      detachedElements.forEach(removeAudioElement)
      if (!detachedElements.includes(element)) removeAudioElement(element)
    })

    if (remoteAudioContainerRef.current) remoteAudioContainerRef.current.replaceChildren()
  }

  function removeRemoteTrackAudio(track: DetachableAudioTrack) {
    const matchingAttachments = remoteAudioAttachmentsRef.current.filter((attachment) => attachment.track === track)
    let detachedElements: HTMLElement[] = []
    try {
      detachedElements = track.detach()
    } catch {
      detachedElements = []
    }
    detachedElements.forEach(removeAudioElement)
    matchingAttachments.forEach(({ element }) => {
      if (!detachedElements.includes(element)) removeAudioElement(element)
    })
    remoteAudioAttachmentsRef.current = remoteAudioAttachmentsRef.current.filter((attachment) => (
      attachment.track !== track && !detachedElements.includes(attachment.element)
    ))
  }

  function addEvent(text: string) {
    const id = nextEventIDRef.current
    nextEventIDRef.current += 1
    setEvents((current) => [{ id, text }, ...current].slice(0, 20))
  }

  const connectDisabled = status === 'connecting' || status === 'connected'

  return (
    <main style={styles.shell} aria-labelledby="livekit-spike-title">
      <section style={styles.card}>
        <p style={styles.eyebrow}>Issue #8 Spike</p>
        <h1 id="livekit-spike-title" style={styles.title}>LiveKit 音频路径验证</h1>
        <p style={styles.description}>验证 Wails 3 WebView2 中的 LiveKit JS 连接、麦克风发布和远端音频播放。</p>
      </section>

      <section style={styles.card} aria-labelledby="connection-title">
        <div style={styles.sectionHeader}>
          <h2 id="connection-title" style={styles.sectionTitle}>连接信息</h2>
          <span aria-live="polite" style={styles.badge}>{statusText[status]}</span>
        </div>

        <label style={styles.fieldLabel} htmlFor="livekit-url">LiveKit URL</label>
        <input
          id="livekit-url"
          style={styles.input}
          value={url}
          onChange={(event) => setUrl(event.target.value)}
          placeholder="wss://livekit.example.com"
          type="url"
        />

        <label style={styles.fieldLabel} htmlFor="livekit-token">短期 join token</label>
        <textarea
          id="livekit-token"
          style={styles.textarea}
          value={token}
          onChange={(event) => setToken(event.target.value)}
          placeholder="仅粘贴本次 HITL 使用的短期 token，不要写入文档或日志。"
          rows={5}
        />

        <div style={styles.actions}>
          <button
            type="button"
            style={connectDisabled ? styles.disabledButton : styles.primaryButton}
            onClick={() => void connectAndPublish()}
            disabled={connectDisabled}
          >
            连接并发布麦克风
          </button>
          <button type="button" style={styles.secondaryButton} onClick={() => void resumePlayback()}>
            恢复远端播放
          </button>
          <button type="button" style={styles.secondaryButton} onClick={() => void disconnect()}>
            断开并清理
          </button>
        </div>

        <p aria-live="polite" style={styles.statusText}>当前状态：{statusText[status]}</p>
        {error && <p role="alert" style={styles.errorText}>{error}</p>}
      </section>

      <section style={styles.card} aria-labelledby="remote-audio-title">
        <h2 id="remote-audio-title" style={styles.sectionTitle}>远端音频</h2>
        <p style={styles.statusText}>订阅到远端 audio track 后，会使用 WebView2 浏览器路径挂载到下方容器。</p>
        <div ref={remoteAudioContainerRef} style={styles.remoteAudioBox} aria-label="远端音频元素容器" />
      </section>

      <section style={styles.card} aria-labelledby="event-log-title">
        <h2 id="event-log-title" style={styles.sectionTitle}>事件记录</h2>
        <p style={styles.statusText}>只记录非 secret 状态，不记录 token、密钥或音频内容。</p>
        {events.length === 0 ? (
          <p style={styles.emptyText}>暂无事件。</p>
        ) : (
          <ol style={styles.eventList} aria-live="polite">
            {events.map((event) => <li key={event.id}>{event.text}</li>)}
          </ol>
        )}
      </section>
    </main>
  )
}

function removeAudioElement(element: HTMLElement) {
  if (element instanceof HTMLMediaElement) {
    element.pause()
    element.removeAttribute('src')
  }
  element.remove()
}

function formatError(error: unknown, tokensToRedact: string | string[]) {
  const rawMessage = error instanceof Error ? error.message : String(error)
  const tokens = Array.isArray(tokensToRedact) ? tokensToRedact : [tokensToRedact]

  return tokens
    .filter((tokenToRedact) => tokenToRedact.length > 0)
    .reduce((message, tokenToRedact) => message.split(tokenToRedact).join('[token已隐藏]'), rawMessage)
}

const styles = {
  shell: {
    minHeight: '100vh',
    boxSizing: 'border-box',
    padding: 24,
    background: '#F3F6F8',
    color: '#1F2933',
    fontFamily: '"Microsoft YaHei UI", "Segoe UI", sans-serif',
  },
  card: {
    maxWidth: 760,
    margin: '0 auto 16px',
    padding: 20,
    border: '1px solid #D7DEE5',
    borderRadius: 16,
    background: '#FFFFFF',
    boxShadow: '0 1px 2px rgb(31 41 51 / 0.08)',
  },
  eyebrow: {
    margin: '0 0 8px',
    color: '#647282',
    fontSize: 12,
    fontWeight: 500,
  },
  title: {
    margin: 0,
    fontSize: 28,
    lineHeight: '36px',
  },
  description: {
    margin: '8px 0 0',
    color: '#647282',
    lineHeight: '22px',
  },
  sectionHeader: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 12,
  },
  sectionTitle: {
    margin: '0 0 12px',
    fontSize: 20,
    lineHeight: '28px',
  },
  badge: {
    border: '1px solid #D7DEE5',
    borderRadius: 4,
    padding: '4px 8px',
    color: '#647282',
    fontSize: 12,
  },
  fieldLabel: {
    display: 'block',
    margin: '12px 0 8px',
    color: '#647282',
    fontSize: 12,
    fontWeight: 600,
  },
  input: {
    width: '100%',
    minHeight: 44,
    boxSizing: 'border-box',
    border: '1px solid #D7DEE5',
    borderRadius: 8,
    padding: '0 12px',
    background: '#FFFFFF',
    color: '#1F2933',
  },
  textarea: {
    width: '100%',
    boxSizing: 'border-box',
    border: '1px solid #D7DEE5',
    borderRadius: 8,
    padding: 12,
    background: '#FFFFFF',
    color: '#1F2933',
    resize: 'vertical',
    fontFamily: 'Consolas, "Microsoft YaHei UI", monospace',
  },
  actions: {
    display: 'flex',
    flexWrap: 'wrap',
    gap: 12,
    marginTop: 16,
  },
  primaryButton: {
    minHeight: 40,
    border: 0,
    borderRadius: 8,
    padding: '0 16px',
    background: '#0B63F6',
    color: '#FFFFFF',
    fontWeight: 600,
    cursor: 'pointer',
  },
  secondaryButton: {
    minHeight: 40,
    border: '1px solid #D7DEE5',
    borderRadius: 8,
    padding: '0 16px',
    background: '#FFFFFF',
    color: '#1F2933',
    fontWeight: 600,
    cursor: 'pointer',
  },
  disabledButton: {
    minHeight: 40,
    border: '1px solid #D7DEE5',
    borderRadius: 8,
    padding: '0 16px',
    background: '#EEF2F5',
    color: '#8B97A4',
    fontWeight: 600,
    cursor: 'not-allowed',
  },
  statusText: {
    margin: '12px 0 0',
    color: '#647282',
    lineHeight: '22px',
  },
  errorText: {
    margin: '12px 0 0',
    color: '#B42318',
    lineHeight: '22px',
  },
  remoteAudioBox: {
    minHeight: 64,
    marginTop: 12,
    padding: 12,
    border: '1px dashed #AEB8C2',
    borderRadius: 8,
    background: '#F8FAFC',
  },
  emptyText: {
    margin: '12px 0 0',
    color: '#8B97A4',
  },
  eventList: {
    margin: '12px 0 0',
    paddingLeft: 20,
    color: '#647282',
    lineHeight: '24px',
  },
} satisfies Record<string, CSSProperties>
