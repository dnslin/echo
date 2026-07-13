export type VoiceStateInput = {
  connected: boolean
  micAvailable: boolean
  muted: boolean
  voiceMode: string
  pttPressed: boolean
  freeTalkEnabledInRoom: boolean
}

export function canSendAudio(input: VoiceStateInput): boolean {
  if (!input.connected || !input.micAvailable || input.muted) {
    return false
  }

  if (input.voiceMode === 'free_talk') {
    return input.freeTalkEnabledInRoom
  }

  return input.pttPressed
}
