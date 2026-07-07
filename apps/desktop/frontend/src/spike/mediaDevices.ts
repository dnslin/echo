export type AudioDeviceKind = 'audioinput' | 'audiooutput'

export type EchoAudioDevice = {
  deviceId: string
  label: string
  kind: AudioDeviceKind
}

export type EchoAudioDeviceList = {
  microphones: EchoAudioDevice[]
  outputs: EchoAudioDevice[]
}

export type OutputDeviceResult = {
  supported: boolean
  applied: boolean
  message: string
}

type SinkSelectableElement = HTMLMediaElement & {
  setSinkId?: (sinkId: string) => Promise<void>
}

type AudioContextConstructor = new () => AudioContext

type WebkitAudioWindow = Window & {
  AudioContext?: AudioContextConstructor
  webkitAudioContext?: AudioContextConstructor
}

const unsupportedMediaDevicesMessage = '当前 WebView2 不支持媒体设备 API'

export async function listMediaDevices(mediaDevices: MediaDevices = getMediaDevices()): Promise<EchoAudioDeviceList> {
  const devices = await mediaDevices.enumerateDevices()
  const audioDevices = devices
    .filter((device) => device.kind === 'audioinput' || device.kind === 'audiooutput')
    .map(toEchoAudioDevice)

  return {
    microphones: audioDevices.filter((device) => device.kind === 'audioinput'),
    outputs: audioDevices.filter((device) => device.kind === 'audiooutput'),
  }
}

export function requestMicrophone(deviceId?: string, mediaDevices: MediaDevices = getMediaDevices()): Promise<MediaStream> {
  const audio: boolean | MediaTrackConstraints = deviceId ? { deviceId: { exact: deviceId } } : true
  return mediaDevices.getUserMedia({ audio })
}

export function calculateLevelFromSamples(samples: Float32Array): number {
  if (samples.length === 0) return 0

  let squareSum = 0
  for (const sample of samples) {
    squareSum += sample * sample
  }

  const rms = Math.sqrt(squareSum / samples.length)
  return clamp(Math.round(rms * 100), 0, 100)
}

export function createLevelMeter(stream: MediaStream, onLevel: (level: number) => void): () => void {
  const win = globalThis.window as WebkitAudioWindow | undefined
  const AudioContextConstructor = win?.AudioContext ?? win?.webkitAudioContext

  if (!win || !AudioContextConstructor) {
    onLevel(0)
    return () => undefined
  }

  const audioContext = new AudioContextConstructor()
  const source = audioContext.createMediaStreamSource(stream)
  const analyser = audioContext.createAnalyser()
  analyser.fftSize = 1024
  source.connect(analyser)

  const samples = new Float32Array(analyser.fftSize)
  let stopped = false
  let frameID: number | undefined

  const requestFrame = win.requestAnimationFrame
    ? win.requestAnimationFrame.bind(win)
    : (callback: FrameRequestCallback) => win.setTimeout(() => callback(Date.now()), 100)
  const cancelFrame = win.cancelAnimationFrame
    ? win.cancelAnimationFrame.bind(win)
    : (id: number) => win.clearTimeout(id)

  function tick() {
    if (stopped) return
    analyser.getFloatTimeDomainData(samples)
    onLevel(calculateLevelFromSamples(samples))
    frameID = requestFrame(tick)
  }

  tick()

  return () => {
    stopped = true
    if (frameID !== undefined) {
      cancelFrame(frameID)
    }
    safeDisconnect(source)
    safeDisconnect(analyser)
    void audioContext.close()
  }
}

export function canSelectOutputDevice(audio: HTMLMediaElement): boolean {
  return typeof (audio as SinkSelectableElement).setSinkId === 'function'
}

export async function applyOutputDevice(audio: HTMLMediaElement, sinkId: string): Promise<OutputDeviceResult> {
  const selectableAudio = audio as SinkSelectableElement
  if (!canSelectOutputDevice(audio)) {
    return {
      supported: false,
      applied: false,
      message: '当前 WebView2 不支持指定输出设备，已跟随系统默认输出设备。',
    }
  }

  try {
    await selectableAudio.setSinkId?.(sinkId)
    return {
      supported: true,
      applied: true,
      message: '输出设备切换验证成功。',
    }
  } catch (error) {
    return {
      supported: true,
      applied: false,
      message: `输出设备切换失败：${errorMessage(error)}`,
    }
  }
}

function getMediaDevices(): MediaDevices {
  if (!navigator.mediaDevices) {
    throw new Error(unsupportedMediaDevicesMessage)
  }
  return navigator.mediaDevices
}

function toEchoAudioDevice(device: MediaDeviceInfo): EchoAudioDevice {
  return {
    deviceId: device.deviceId,
    label: device.label || '(未授权或未命名设备)',
    kind: device.kind as AudioDeviceKind,
  }
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value))
}

function safeDisconnect(node: AudioNode): void {
  try {
    node.disconnect()
  } catch {
    // Some Web Audio nodes throw if already disconnected. Cleanup must stay idempotent.
  }
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
