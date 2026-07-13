import { useEffect, useState } from 'react'

import type { Settings } from '../bindings/echo/apps/desktop/internal/config/models'
import { Load, ResetAvatar, Save } from '../bindings/echo/apps/desktop/internal/app/settingsservice'

function countCodePoints(value: string): number {
  let count = 0
  for (const _character of value) {
    count += 1
  }
  return count
}

// Matches Go strings.TrimSpace in config.Store.
function trimStoreWhitespace(value: string): string {
  return value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, '')
}

function App() {
  const [settings, setSettings] = useState<Settings | null>(null)
  const [nicknameDraft, setNicknameDraft] = useState('')
  const [requiresNickname, setRequiresNickname] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const loadSettings = async () => {
    setError(null)
    try {
      const loaded = await Load()
      setSettings(loaded)
      setNicknameDraft(loaded.nickname)
      setRequiresNickname(loaded.nickname === '')
    } catch {
      setError('无法加载本地设置，请检查配置目录权限。')
    }
  }

  useEffect(() => {
    void loadSettings()
  }, [])

  const isFirstRun = requiresNickname
  const nickname = trimStoreWhitespace(isFirstRun ? nicknameDraft : settings?.nickname ?? '')
  const nicknameLength = countCodePoints(nickname)
  const canSave = nicknameLength > 0 && nicknameLength <= 16 && !saving

  const updateSettings = (patch: Partial<Settings>) => {
    setSettings((current) => (current ? { ...current, ...patch } : current))
  }

  const saveSettings = async () => {
    if (!settings || !canSave) return

    setSaving(true)
    setError(null)
    try {
      const persisted = await Save({ ...settings, nickname })
      setSettings(persisted)
      setNicknameDraft(persisted.nickname)
      setRequiresNickname(false)
    } catch {
      setError('无法保存本地设置，请检查配置目录权限。')
    } finally {
      setSaving(false)
    }
  }

  const resetAvatar = async () => {
    if (!settings) return

    setSaving(true)
    setError(null)
    try {
      setSettings(await ResetAvatar())
    } catch {
      setError('无法重置随机头像，请稍后重试。')
    } finally {
      setSaving(false)
    }
  }

  if (error) {
    return (
      <main>
        <p role="alert">{error}</p>
        <button type="button" onClick={() => void loadSettings()}>重试</button>
      </main>
    )
  }

  if (!settings) {
    return <main aria-busy="true">正在加载本地设置…</main>
  }

  if (isFirstRun) {
    return (
      <main>
        <h1>欢迎使用 echo</h1>
        <p>头像：{settings.avatar_id}</p>
        <label>
          昵称
          <input
            value={nicknameDraft}
            onChange={(event) => setNicknameDraft(event.target.value)}
          />
        </label>
        <button type="button" disabled={!canSave} onClick={() => void saveSettings()}>继续</button>
      </main>
    )
  }

  return (
    <main>
      <h1>你好，{settings.nickname}</h1>
      <p>头像：{settings.avatar_id}</p>
      <button type="button" disabled={saving} onClick={() => void resetAvatar()}>重新随机头像</button>

      <label>
        昵称
        <input value={settings.nickname} onChange={(event) => updateSettings({ nickname: event.target.value })} />
      </label>
      <label>
        快捷键
        <input value={settings.push_to_talk_key} onChange={(event) => updateSettings({ push_to_talk_key: event.target.value })} />
      </label>
      <label>
        麦克风设备
        <input value={settings.microphone_device} onChange={(event) => updateSettings({ microphone_device: event.target.value })} />
      </label>
      <label>
        输出设备
        <input value={settings.output_device} onChange={(event) => updateSettings({ output_device: event.target.value })} />
      </label>
      <label>
        语音模式
        <select value={settings.voice_mode} onChange={(event) => updateSettings({ voice_mode: event.target.value })}>
          <option value="push_to_talk">按键说话</option>
          <option value="free_talk">自由说话</option>
        </select>
      </label>
      <label>
        房间音量
        <input type="range" min="0" max="100" value={settings.output_volume} onChange={(event) => updateSettings({ output_volume: Number(event.target.value) })} />
      </label>
      <button type="button" disabled={!canSave} onClick={() => void saveSettings()}>{saving ? '保存中…' : '保存设置'}</button>
    </main>
  )
}

export default App

