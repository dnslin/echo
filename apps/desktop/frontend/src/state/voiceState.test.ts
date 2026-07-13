import { describe, expect, it } from 'vitest'

import { canSendAudio, type VoiceStateInput } from './voiceState'

const readyPushToTalk: VoiceStateInput = {
  connected: true,
  micAvailable: true,
  muted: false,
  voiceMode: 'push_to_talk',
  pttPressed: true,
  freeTalkEnabledInRoom: false,
}

describe('canSendAudio', () => {
  it('requires push-to-talk to be pressed in push-to-talk mode', () => {
    expect(canSendAudio(readyPushToTalk)).toBe(true)
    expect(canSendAudio({ ...readyPushToTalk, pttPressed: false })).toBe(false)
    expect(canSendAudio({ ...readyPushToTalk, pttPressed: false, freeTalkEnabledInRoom: true })).toBe(false)
  })

  it.each([
    ['disconnected', { connected: false }],
    ['microphone unavailable', { micAvailable: false }],
    ['muted', { muted: true }],
  ])('never sends audio while %s', (_condition, change) => {
    expect(canSendAudio({ ...readyPushToTalk, ...change })).toBe(false)
  })

  it('requires explicit in-room free-talk enablement', () => {
    const persistedFreeTalkPreference: VoiceStateInput = {
      ...readyPushToTalk,
      voiceMode: 'free_talk',
      pttPressed: false,
      freeTalkEnabledInRoom: false,
    }

    expect(canSendAudio(persistedFreeTalkPreference)).toBe(false)
    expect(canSendAudio({ ...persistedFreeTalkPreference, freeTalkEnabledInRoom: true })).toBe(true)
  })
})
