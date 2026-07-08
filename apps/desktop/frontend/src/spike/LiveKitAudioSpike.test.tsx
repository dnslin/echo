import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const liveKitMock = vi.hoisted(() => {
  const roomInstances: MockRoom[] = []
  const controls: { connectError?: Error; disconnectError?: Error; startAudioError?: Error } = {}

  class MockRoom {
    handlers = new Map<string, Array<(...args: unknown[]) => void>>()
    localParticipant = {
      setMicrophoneEnabled: vi.fn(async () => undefined),
    }
    connect = vi.fn(async () => {
      if (controls.connectError) throw controls.connectError
    })
    disconnect = vi.fn(async () => {
      if (controls.disconnectError) throw controls.disconnectError
    })
    startAudio = vi.fn(async () => {
      if (controls.startAudioError) throw controls.startAudioError
    })

    constructor() {
      roomInstances.push(this)
    }

    on(event: string, handler: (...args: unknown[]) => void) {
      const handlers = this.handlers.get(event) ?? []
      handlers.push(handler)
      this.handlers.set(event, handlers)
      return this
    }

    emit(event: string, ...args: unknown[]) {
      this.handlers.get(event)?.forEach((handler) => handler(...args))
    }
  }

  return { controls, MockRoom, roomInstances }
})

vi.mock('livekit-client', () => ({
  Room: liveKitMock.MockRoom,
  RoomEvent: {
    Connected: 'connected',
    Disconnected: 'disconnected',
    Reconnecting: 'reconnecting',
    Reconnected: 'reconnected',
    TrackSubscribed: 'trackSubscribed',
    TrackUnsubscribed: 'trackUnsubscribed',
    AudioPlaybackStatusChanged: 'audioPlaybackChanged',
    MediaDevicesChanged: 'mediaDevicesChanged',
    MediaDevicesError: 'mediaDevicesError',
  },
  Track: {
    Kind: {
      Audio: 'audio',
    },
  },
}))

import { LiveKitAudioSpike } from './LiveKitAudioSpike'

describe('LiveKitAudioSpike', () => {
  beforeEach(() => {
    liveKitMock.roomInstances.length = 0
    liveKitMock.controls.connectError = undefined
    liveKitMock.controls.disconnectError = undefined
    liveKitMock.controls.startAudioError = undefined
  })

  afterEach(() => {
    cleanup()
    vi.clearAllMocks()
  })

  it('connects to LiveKit and publishes the local microphone after user action', async () => {
    const room = await renderAndConnect()

    expect(room.connect).toHaveBeenCalledWith('wss://livekit.test', 'short-lived-token', { autoSubscribe: true })
    expect(room.localParticipant.setMicrophoneEnabled).toHaveBeenCalledWith(true)
    expect(screen.getByText('当前状态：已连接')).toBeVisible()
  })

  it('attaches subscribed remote audio and detaches it during disconnect cleanup', async () => {
    const room = await renderAndConnect()
    const audioElement = document.createElement('audio')
    vi.spyOn(audioElement, 'pause').mockImplementation(() => undefined)
    const remoteTrack = {
      kind: 'audio',
      attach: vi.fn(() => audioElement),
      detach: vi.fn(() => [audioElement]),
    }

    room.emit('trackSubscribed', remoteTrack)

    const remoteContainer = screen.getByLabelText('远端音频元素容器')
    expect(remoteTrack.attach).toHaveBeenCalledTimes(1)
    expect(remoteContainer.querySelectorAll('audio')).toHaveLength(1)

    fireEvent.click(screen.getByRole('button', { name: '断开并清理' }))

    await waitFor(() => expect(room.disconnect).toHaveBeenCalledWith(true))
    expect(remoteTrack.detach).toHaveBeenCalledTimes(1)
    expect(remoteContainer.querySelectorAll('audio')).toHaveLength(0)
  })

  it('redacts the join token from visible connection errors', async () => {
    liveKitMock.controls.connectError = new Error('denied for secret-token-value')
    render(<LiveKitAudioSpike />)

    fillConnectionForm('wss://livekit.test', 'secret-token-value')
    fireEvent.click(screen.getByRole('button', { name: '连接并发布麦克风' }))

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('denied for [token已隐藏]'))
    expect(screen.getByRole('alert')).not.toHaveTextContent('secret-token-value')
    expect(liveKitMock.roomInstances[0].disconnect).toHaveBeenCalledWith(true)
  })
})

async function renderAndConnect() {
  render(<LiveKitAudioSpike />)
  fillConnectionForm('wss://livekit.test', 'short-lived-token')
  fireEvent.click(screen.getByRole('button', { name: '连接并发布麦克风' }))

  await waitFor(() => expect(liveKitMock.roomInstances).toHaveLength(1))
  const [room] = liveKitMock.roomInstances
  await waitFor(() => expect(room.connect).toHaveBeenCalled())
  await waitFor(() => expect(room.localParticipant.setMicrophoneEnabled).toHaveBeenCalled())

  return room
}

function fillConnectionForm(url: string, token: string) {
  fireEvent.change(screen.getByLabelText('LiveKit URL'), { target: { value: url } })
  fireEvent.change(screen.getByLabelText('短期 join token'), { target: { value: token } })
}
