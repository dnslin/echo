import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  applyOutputDevice,
  calculateLevelFromSamples,
  createLevelMeter,
  listMediaDevices,
  requestMicrophone,
} from './mediaDevices'

describe('mediaDevices wrapper', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('projects audio input and output devices after enumeration', async () => {
    const mediaDevices = {
      enumerateDevices: vi.fn().mockResolvedValue([
        fakeDevice('audioinput', 'mic_1', 'Desk Mic'),
        fakeDevice('audiooutput', 'speaker_1', 'Headset'),
        fakeDevice('videoinput', 'camera_1', 'Camera'),
      ]),
    } as unknown as MediaDevices

    const devices = await listMediaDevices(mediaDevices)

    expect(devices.microphones).toEqual([{ deviceId: 'mic_1', label: 'Desk Mic', kind: 'audioinput' }])
    expect(devices.outputs).toEqual([{ deviceId: 'speaker_1', label: 'Headset', kind: 'audiooutput' }])
  })

  it('returns empty lists when no audio devices are exposed', async () => {
    const mediaDevices = {
      enumerateDevices: vi.fn().mockResolvedValue([]),
    } as unknown as MediaDevices

    await expect(listMediaDevices(mediaDevices)).resolves.toEqual({ microphones: [], outputs: [] })
  })

  it('requests a specific microphone by exact device id', async () => {
    const stream = fakeStream()
    const mediaDevices = {
      getUserMedia: vi.fn().mockResolvedValue(stream),
    } as unknown as MediaDevices

    await expect(requestMicrophone('mic_2', mediaDevices)).resolves.toBe(stream)

    expect(mediaDevices.getUserMedia).toHaveBeenCalledWith({ audio: { deviceId: { exact: 'mic_2' } } })
  })

  it('maps silent and non-silent samples into a bounded 0-100 input level', () => {
    expect(calculateLevelFromSamples(new Float32Array([0, 0, 0]))).toBe(0)
    expect(calculateLevelFromSamples(new Float32Array([0.5, -0.5]))).toBe(50)
    expect(calculateLevelFromSamples(new Float32Array([2, -2]))).toBe(100)
  })

  it('creates an input level meter and cleans up Web Audio resources', () => {
    const close = vi.fn().mockResolvedValue(undefined)
    const sourceDisconnect = vi.fn()
    const analyserDisconnect = vi.fn()
    const requestFrame = vi.fn().mockReturnValue(7)
    const cancelFrame = vi.fn()
    const source = { connect: vi.fn(), disconnect: sourceDisconnect } as unknown as MediaStreamAudioSourceNode
    const analyser = {
      fftSize: 0,
      disconnect: analyserDisconnect,
      getFloatTimeDomainData: (samples: Float32Array) => samples.fill(0.25),
    } as unknown as AnalyserNode

    class MockAudioContext {
      createMediaStreamSource = vi.fn(() => source)
      createAnalyser = vi.fn(() => analyser)
      close = close
    }

    Object.defineProperty(window, 'AudioContext', { value: MockAudioContext, configurable: true })
    Object.defineProperty(window, 'requestAnimationFrame', { value: requestFrame, configurable: true })
    Object.defineProperty(window, 'cancelAnimationFrame', { value: cancelFrame, configurable: true })

    const onLevel = vi.fn()
    const cleanup = createLevelMeter(fakeStream(), onLevel)

    expect(onLevel).toHaveBeenCalledWith(25)
    expect(source.connect).toHaveBeenCalledWith(analyser)

    cleanup()

    expect(cancelFrame).toHaveBeenCalledWith(7)
    expect(sourceDisconnect).toHaveBeenCalled()
    expect(analyserDisconnect).toHaveBeenCalled()
    expect(close).toHaveBeenCalled()
  })

  it('reports unsupported output device switching without throwing', async () => {
    const audio = document.createElement('audio')

    await expect(applyOutputDevice(audio, 'speaker_1')).resolves.toEqual({
      supported: false,
      applied: false,
      message: '当前 WebView2 不支持指定输出设备，已跟随系统默认输出设备。',
    })
  })

  it('applies supported output device switching successfully', async () => {
    const audio = document.createElement('audio') as HTMLMediaElement & {
      setSinkId: (sinkId: string) => Promise<void>
    }
    audio.setSinkId = vi.fn().mockResolvedValue(undefined)

    await expect(applyOutputDevice(audio, 'speaker_1')).resolves.toEqual({
      supported: true,
      applied: true,
      message: '输出设备切换验证成功。',
    })

    expect(audio.setSinkId).toHaveBeenCalledWith('speaker_1')
  })

  it('reports setSinkId failures while keeping the UI path alive', async () => {
    const audio = document.createElement('audio') as HTMLMediaElement & {
      setSinkId: (sinkId: string) => Promise<void>
    }
    audio.setSinkId = vi.fn().mockRejectedValue(new Error('blocked'))

    const result = await applyOutputDevice(audio, 'speaker_1')

    expect(audio.setSinkId).toHaveBeenCalledWith('speaker_1')
    expect(result.supported).toBe(true)
    expect(result.applied).toBe(false)
    expect(result.message).toContain('blocked')
  })
})

function fakeDevice(kind: MediaDeviceKind, deviceId: string, label: string): MediaDeviceInfo {
  return {
    deviceId,
    groupId: '',
    kind,
    label,
    toJSON: () => ({ deviceId, groupId: '', kind, label }),
  } as MediaDeviceInfo
}

function fakeStream(stop: () => void = vi.fn()): MediaStream {
  return {
    getTracks: () => [{ stop }],
  } as unknown as MediaStream
}
