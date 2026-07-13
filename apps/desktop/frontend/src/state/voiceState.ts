import type { VoiceMode } from '../settings/settings'

export type { VoiceMode } from '../settings/settings'

export type VoiceStateInput = {
  connected: boolean
  micAvailable: boolean
  muted: boolean
  voiceMode: VoiceMode
  pttPressed: boolean
  freeTalkEnabledInRoom: boolean
}

export function canSendAudio(input: VoiceStateInput): boolean {
  if (!input.connected || !input.micAvailable || input.muted) {
    return false
  }

  if (input.voiceMode === 'push_to_talk') {
    return input.pttPressed
  }

  return input.freeTalkEnabledInRoom
}
