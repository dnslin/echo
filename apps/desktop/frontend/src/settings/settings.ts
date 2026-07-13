export type VoiceMode = 'push_to_talk' | 'free_talk'

export type LocalSettings = {
  anonymousId: string
  nickname: string
  avatarId: string
  pushToTalkKey: string
  microphoneDevice: string
  outputDevice: string
  voiceMode: VoiceMode
  outputVolume: number
}

export const defaultSettings: LocalSettings = {
  anonymousId: '',
  nickname: '',
  avatarId: '',
  pushToTalkKey: 'V',
  microphoneDevice: '',
  outputDevice: '',
  voiceMode: 'push_to_talk',
  outputVolume: 100,
}
