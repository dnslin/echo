import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { DeviceTraySpike } from './DeviceTraySpike'

describe('DeviceTraySpike', () => {
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it('shows empty device states when WebView2 exposes no audio devices', async () => {
    setMediaDevices({
      enumerateDevices: vi.fn().mockResolvedValue([]),
      getUserMedia: vi.fn(),
    })

    render(<DeviceTraySpike />)

    expect(await screen.findByRole('heading', { name: '音频设备和托盘验证' })).toBeVisible()
    expect((await screen.findAllByText(/未检测到可用麦克风/)).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/未检测到输出设备/).length).toBeGreaterThan(0)
  })

  it('shows a clear permission failure message when getUserMedia rejects', async () => {
    setMediaDevices({
      enumerateDevices: vi.fn().mockResolvedValue([fakeDevice('audioinput', 'mic_1', 'Desk Mic')]),
      getUserMedia: vi.fn().mockRejectedValue(new Error('denied')),
    })

    render(<DeviceTraySpike />)

    fireEvent.click(screen.getByRole('button', { name: '请求麦克风权限' }))

    expect(await screen.findByText(/无法使用麦克风，请检查系统权限/)).toBeVisible()
    expect(screen.getByText('授权失败')).toBeVisible()
  })

  it('stops the old stream when switching microphones successfully', async () => {
    const firstTrackStop = vi.fn()
    const secondTrackStop = vi.fn()
    const getUserMedia = vi
      .fn()
      .mockResolvedValueOnce(fakeStream(firstTrackStop))
      .mockResolvedValueOnce(fakeStream(secondTrackStop))

    setMediaDevices({
      enumerateDevices: vi.fn().mockResolvedValue([
        fakeDevice('audioinput', 'mic_1', 'Desk Mic'),
        fakeDevice('audioinput', 'mic_2', 'Headset Mic'),
        fakeDevice('audiooutput', 'speaker_1', 'Headset'),
      ]),
      getUserMedia,
    })

    render(<DeviceTraySpike />)

    await screen.findByText('Desk Mic')
    fireEvent.click(screen.getByRole('button', { name: '请求麦克风权限' }))

    await waitFor(() => expect(getUserMedia).toHaveBeenCalledTimes(1))
    fireEvent.change(screen.getByLabelText('麦克风设备'), { target: { value: 'mic_2' } })

    await waitFor(() => expect(getUserMedia).toHaveBeenCalledTimes(2))
    expect(getUserMedia).toHaveBeenLastCalledWith({ audio: { deviceId: { exact: 'mic_2' } } })
    expect(firstTrackStop).toHaveBeenCalled()
    expect(secondTrackStop).not.toHaveBeenCalled()
  })

  it('applies the selected output device from the page', async () => {
    const setSinkId = vi.fn().mockResolvedValue(undefined)
    const originalSetSinkId = Object.getOwnPropertyDescriptor(HTMLMediaElement.prototype, 'setSinkId')
    Object.defineProperty(HTMLMediaElement.prototype, 'setSinkId', {
      value: setSinkId,
      configurable: true,
    })

    try {
      setMediaDevices({
        enumerateDevices: vi.fn().mockResolvedValue([
          fakeDevice('audiooutput', 'speaker_1', 'Headset'),
        ]),
        getUserMedia: vi.fn(),
      })

      render(<DeviceTraySpike />)

      await screen.findByText('Headset')
      fireEvent.click(screen.getByRole('button', { name: '验证输出设备切换' }))

      await waitFor(() => expect(setSinkId).toHaveBeenCalledWith('speaker_1'))
      expect(await screen.findByText('输出设备切换验证成功。')).toBeVisible()
    } finally {
      if (originalSetSinkId) {
        Object.defineProperty(HTMLMediaElement.prototype, 'setSinkId', originalSetSinkId)
      } else {
        delete (HTMLMediaElement.prototype as { setSinkId?: unknown }).setSinkId
      }
    }
  })
})

function setMediaDevices(mediaDevices: Partial<MediaDevices>) {
  Object.defineProperty(navigator, 'mediaDevices', {
    value: mediaDevices,
    configurable: true,
  })
}

function fakeDevice(kind: MediaDeviceKind, deviceId: string, label: string): MediaDeviceInfo {
  return {
    deviceId,
    groupId: '',
    kind,
    label,
    toJSON: () => ({ deviceId, groupId: '', kind, label }),
  } as MediaDeviceInfo
}

function fakeStream(stop: () => void): MediaStream {
  return {
    getTracks: () => [{ stop }],
  } as unknown as MediaStream
}
