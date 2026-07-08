import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('livekit-client', () => ({
  Room: vi.fn(),
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

import App from './App'

describe('App LiveKit audio spike smoke', () => {
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it('renders visible echo LiveKit audio spike content', () => {
    render(<App />)

    expect(screen.getByRole('heading', { name: 'LiveKit 音频路径验证' })).toBeVisible()
    expect(screen.getByLabelText('LiveKit URL')).toBeVisible()
    expect(screen.getByLabelText('短期 join token')).toBeVisible()
    expect(screen.getByRole('button', { name: '连接并发布麦克风' })).toBeVisible()
    expect(screen.getByText(/只记录非 secret 状态/)).toBeVisible()
  })
})
