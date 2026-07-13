import { describe, expect, it } from 'vitest'

import { defaultSettings } from './settings'

describe('defaultSettings', () => {
  it('mirrors the safe persisted-settings defaults', () => {
    expect(defaultSettings).toEqual({
      anonymousId: '',
      nickname: '',
      avatarId: '',
      pushToTalkKey: 'V',
      microphoneDevice: '',
      outputDevice: '',
      voiceMode: 'push_to_talk',
      outputVolume: 100,
    })
  })
})
