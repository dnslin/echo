import { describe, expect, it, type Mock, vi } from 'vitest'

import {
  enumerateAudioDevices,
  requestAudioDevicePermission,
  supportsAudioOutputSelection,
} from './devices'

function mediaDevice(kind: MediaDeviceKind, deviceId: string, label: string): MediaDeviceInfo {
  return { deviceId, groupId: '', kind, label, toJSON: () => ({}) }
}

type MockMediaDevices = {
  enumerateDevices: Mock<() => Promise<MediaDeviceInfo[]>>
  getUserMedia: Mock<(constraints?: MediaStreamConstraints) => Promise<MediaStream>>
}

function mediaDevicesWith(devices: MediaDeviceInfo[]): MockMediaDevices {
  return {
    enumerateDevices: vi.fn().mockResolvedValue(devices),
    getUserMedia: vi.fn(),
  }
}

describe('audio device discovery', () => {
  it('lists only actual audio inputs and outputs', async () => {
    const mediaDevices = mediaDevicesWith([
      mediaDevice('audioinput', 'mic-1', 'Microphone One'),
      mediaDevice('audiooutput', 'speaker-1', 'Speaker One'),
      mediaDevice('videoinput', 'camera-1', 'Camera One'),
    ])

    await expect(enumerateAudioDevices(mediaDevices)).resolves.toEqual({
      status: 'ready',
      inputs: [{ deviceId: 'mic-1', label: 'Microphone One' }],
      outputs: [{ deviceId: 'speaker-1', label: 'Speaker One' }],
    })
    expect(mediaDevices.getUserMedia).not.toHaveBeenCalled()
  })

  it('marks an empty enumerated inventory as no devices', async () => {
    await expect(enumerateAudioDevices(mediaDevicesWith([]))).resolves.toEqual({
      status: 'no-devices',
      inputs: [],
      outputs: [],
    })
  })

  it('requests audio permission, stops its temporary tracks, and refreshes labels', async () => {
    const stop = vi.fn()
    const mediaDevices = mediaDevicesWith([
      mediaDevice('audioinput', 'mic-1', 'Microphone One'),
      mediaDevice('audiooutput', 'speaker-1', 'Speaker One'),
    ])
    mediaDevices.getUserMedia.mockResolvedValue({ getTracks: () => [{ stop }] } as unknown as MediaStream)

    await expect(requestAudioDevicePermission(mediaDevices)).resolves.toMatchObject({
      status: 'ready',
      inputs: [{ deviceId: 'mic-1', label: 'Microphone One' }],
      outputs: [{ deviceId: 'speaker-1', label: 'Speaker One' }],
    })
    expect(mediaDevices.getUserMedia).toHaveBeenCalledWith({ audio: true })
    expect(stop).toHaveBeenCalledOnce()
  })

  it('returns an actionable state when permission is denied or media APIs are unavailable', async () => {
    const denied = mediaDevicesWith([])
    denied.getUserMedia.mockRejectedValue(new Error('Permission denied'))

    await expect(requestAudioDevicePermission(denied)).resolves.toEqual({
      status: 'permission-denied',
      inputs: [],
      outputs: [],
    })
    await expect(enumerateAudioDevices(undefined)).resolves.toEqual({
      status: 'unavailable',
      inputs: [],
      outputs: [],
    })
  })

  it('marks output selection only when setSinkId is available', () => {
    expect(supportsAudioOutputSelection({ setSinkId: vi.fn() })).toBe(true)
    expect(supportsAudioOutputSelection({})).toBe(false)
  })
})
