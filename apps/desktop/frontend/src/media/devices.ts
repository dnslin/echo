export type AudioDevice = {
  deviceId: string
  label: string
}

export type AudioDeviceStatus = 'ready' | 'permission-required' | 'permission-denied' | 'no-devices' | 'unavailable'

export type AudioDeviceInventory = {
  status: AudioDeviceStatus
  inputs: AudioDevice[]
  outputs: AudioDevice[]
}

type MediaDevicesApi = Pick<MediaDevices, 'enumerateDevices' | 'getUserMedia'>
type OutputSelectionTarget = { setSinkId?: unknown }

const emptyInventory = (status: AudioDeviceStatus): AudioDeviceInventory => ({
  status,
  inputs: [],
  outputs: [],
})

function browserMediaDevices(): MediaDevicesApi | undefined {
  if (typeof navigator === 'undefined') return undefined
  return navigator.mediaDevices
}

export async function enumerateAudioDevices(mediaDevices = browserMediaDevices()): Promise<AudioDeviceInventory> {
  if (!mediaDevices) return emptyInventory('unavailable')

  try {
    const devices = await mediaDevices.enumerateDevices()
    const inputs = devices
      .filter((device) => device.kind === 'audioinput')
      .map(({ deviceId, label }) => ({ deviceId, label }))
    const outputs = devices
      .filter((device) => device.kind === 'audiooutput')
      .map(({ deviceId, label }) => ({ deviceId, label }))

    if (inputs.length === 0 && outputs.length === 0) return emptyInventory('no-devices')
    if ([...inputs, ...outputs].some((device) => device.label === '')) {
      return { status: 'permission-required', inputs, outputs }
    }
    return { status: 'ready', inputs, outputs }
  } catch {
    return emptyInventory('unavailable')
  }
}

export async function requestAudioDevicePermission(mediaDevices = browserMediaDevices()): Promise<AudioDeviceInventory> {
  if (!mediaDevices) return emptyInventory('unavailable')

  try {
    const stream = await mediaDevices.getUserMedia({ audio: true })
    try {
      return await enumerateAudioDevices(mediaDevices)
    } finally {
      for (const track of stream.getTracks()) track.stop()
    }
  } catch {
    return emptyInventory('permission-denied')
  }
}

export function supportsAudioOutputSelection(target: OutputSelectionTarget | undefined = typeof HTMLMediaElement === 'undefined' ? undefined : HTMLMediaElement.prototype): boolean {
  return typeof target?.setSinkId === 'function'
}
