import { useEffect, useState } from 'react'
import './App.css'

import type { Settings } from '../bindings/echo/apps/desktop/internal/config/models'
import { Load, ResetAvatar, Save } from '../bindings/echo/apps/desktop/internal/app/settingsservice'
import {
  enumerateAudioDevices,
  requestAudioDevicePermission,
  supportsAudioOutputSelection,
  type AudioDeviceInventory,
  type AudioDeviceStatus,
} from './media/devices'

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

const initialDeviceInventory: AudioDeviceInventory = {
  status: 'unavailable',
  inputs: [],
  outputs: [],
}

type SettingsError = 'load' | 'save' | 'reset-avatar'

function inputDeviceStatusMessage(status: AudioDeviceStatus): string {
  switch (status) {
    case 'ready':
      return '已读取当前系统音频设备；加入临时房间后将应用保存的设备偏好。'
    case 'permission-required':
      return '请授权麦克风后刷新设备名称。'
    case 'permission-denied':
      return '未获得麦克风权限，当前使用系统默认设备。'
    case 'no-devices':
      return '未检测到可用音频设备，当前使用系统默认设备。'
    case 'unavailable':
      return '当前 WebView 无法读取设备，已使用系统默认设备。'
  }
}

function deviceStatusMessage(status: AudioDeviceStatus, outputSelectionSupported: boolean): string {
  const inputStatus = inputDeviceStatusMessage(status)
  return outputSelectionSupported ? inputStatus : `${inputStatus} 输出设备将跟随系统默认。`
}

function settingsErrorMessage(error: SettingsError): string {
  switch (error) {
    case 'load':
      return '无法加载本地设置，请检查配置目录权限。'
    case 'save':
      return '无法保存本地设置，请检查配置目录权限。'
    case 'reset-avatar':
      return '无法重置随机头像，请稍后重试。'
  }
}

function App() {
  const [settings, setSettings] = useState<Settings | null>(null)
  const [nicknameDraft, setNicknameDraft] = useState('')
  const [requiresNickname, setRequiresNickname] = useState(false)
  const [error, setError] = useState<SettingsError | null>(null)
  const [saving, setSaving] = useState(false)
  const [devices, setDevices] = useState<AudioDeviceInventory>(initialDeviceInventory)
  const [refreshingDevices, setRefreshingDevices] = useState(false)
  const [saveNotice, setSaveNotice] = useState<string | null>(null)
  const [deviceFallbackNotice, setDeviceFallbackNotice] = useState<string | null>(null)
  const outputSelectionSupported = supportsAudioOutputSelection()

  const loadSettings = async () => {
    setError(null)
    setSaveNotice(null)
    setDeviceFallbackNotice(null)
    try {
      const loaded = await Load()
      setSettings(loaded)
      setNicknameDraft(loaded.nickname)
      setRequiresNickname(loaded.nickname === '')
    } catch {
      setError('load')
    }
  }

  useEffect(() => {
    let cancelled = false
    void loadSettings()
    void enumerateAudioDevices().then((inventory) => {
      if (!cancelled) setDevices(inventory)
    })

    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    if (!settings || (devices.status !== 'ready' && devices.status !== 'no-devices')) return

    const microphoneDevice = settings.microphone_device === '' || devices.inputs.some((device) => device.deviceId === settings.microphone_device)
      ? settings.microphone_device
      : ''
    const outputDevice = outputSelectionSupported && (settings.output_device === '' || devices.outputs.some((device) => device.deviceId === settings.output_device))
      ? settings.output_device
      : ''
    if (microphoneDevice === settings.microphone_device && outputDevice === settings.output_device) return

    setSettings({ ...settings, microphone_device: microphoneDevice, output_device: outputDevice })
    setDeviceFallbackNotice('检测到已保存的设备不可用，已改为跟随系统默认。')
  }, [devices, outputSelectionSupported, settings])

  const refreshDevices = async () => {
    setRefreshingDevices(true)
    setSaveNotice(null)
    setDeviceFallbackNotice(null)
    try {
      setDevices(await requestAudioDevicePermission())
    } finally {
      setRefreshingDevices(false)
    }
  }

  const isFirstRun = requiresNickname
  const nickname = trimStoreWhitespace(isFirstRun ? nicknameDraft : settings?.nickname ?? '')
  const nicknameLength = countCodePoints(nickname)
  const canSave = nicknameLength > 0 && nicknameLength <= 16 && !saving

  const updateSettings = (patch: Partial<Settings>) => {
    setSaveNotice(null)
    setDeviceFallbackNotice(null)
    setSettings((current) => (current ? { ...current, ...patch } : current))
  }

  const saveSettings = async () => {
    if (!settings || !canSave) return

    setSaving(true)
    setError(null)
    setSaveNotice(null)
    try {
      const persisted = await Save({ ...settings, nickname })
      setSettings(persisted)
      setNicknameDraft(persisted.nickname)
      setRequiresNickname(false)
      setSaveNotice('本地设置已保存')
    } catch {
      setError('save')
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
      setError('reset-avatar')
    } finally {
      setSaving(false)
    }
  }

  if (error) {
    const retryAction = error === 'load' ? loadSettings : error === 'save' ? saveSettings : resetAvatar
    const retryLabel = error === 'load' ? '重试' : error === 'save' ? '再次保存' : '再次重置头像'

    return (
      <main className="settings-app settings-app--viewport">
        <section className="settings-card settings-card--error" aria-labelledby="settings-error-title">
          <p className="settings-brand">echo</p>
          <h1 id="settings-error-title" className="settings-card__title">本地设置不可用</h1>
          <p className="settings-error" role="alert">{settingsErrorMessage(error)}</p>
          <div className="settings-actions">
            <button className="settings-button settings-button--secondary" type="button" onClick={() => void retryAction()}>{retryLabel}</button>
          </div>
        </section>
      </main>
    )
  }

  if (!settings) {
    return (
      <main className="settings-app settings-app--viewport" aria-busy="true">
        <section className="settings-card">
          <p className="settings-brand">echo</p>
          <p className="settings-loading">正在加载本地设置…</p>
        </section>
      </main>
    )
  }

  if (isFirstRun) {
    return (
      <main className="settings-app settings-app--viewport">
        <section className="settings-card" aria-labelledby="first-nickname-title">
          <p className="settings-brand">echo</p>
          <div className="settings-card__heading">
            <h1 id="first-nickname-title" className="settings-card__title">欢迎使用 echo</h1>
            <p className="settings-card__description">设置一个在临时房间中展示的昵称。</p>
          </div>
          <div className="settings-avatar-row">
            <div className="settings-avatar" aria-hidden="true">e</div>
            <div className="settings-avatar-copy">
              <p className="settings-avatar-label">随机头像</p>
              <code className="settings-avatar-id">{settings.avatar_id}</code>
            </div>
          </div>
          <label className="settings-field">
            <span className="settings-field__label">昵称</span>
            <input
              className="settings-field__control"
              name="nickname"
              autoComplete="nickname"
              value={nicknameDraft}
              onChange={(event) => setNicknameDraft(event.target.value)}
            />
          </label>
          <div className="settings-actions">
            <button className="settings-button settings-button--primary" type="button" disabled={!canSave} onClick={() => void saveSettings()}>继续</button>
          </div>
        </section>
      </main>
    )
  }

  return (
    <main className="settings-app settings-app--viewport">
      <section className="settings-card settings-card--wide" aria-labelledby="restored-settings-title">
        <header className="settings-card__header">
          <div>
            <p className="settings-brand">echo</p>
            <h1 id="restored-settings-title" className="settings-card__title">你好，{settings.nickname}</h1>
          </div>
          <button className="settings-button settings-button--secondary" type="button" disabled={saving} onClick={() => void resetAvatar()}>重新随机头像</button>
        </header>
        <div className="settings-avatar-row">
          <div className="settings-avatar" aria-hidden="true">e</div>
          <div className="settings-avatar-copy">
            <p className="settings-avatar-label">随机头像</p>
            <code className="settings-avatar-id">{settings.avatar_id}</code>
          </div>
        </div>
        <div className="settings-form">
          <div className="settings-device-controls settings-field--wide">
            <p className="settings-device-status" role="status" aria-live="polite">{deviceFallbackNotice ?? deviceStatusMessage(devices.status, outputSelectionSupported)}</p>
            <button className="settings-button settings-button--secondary" type="button" disabled={refreshingDevices} onClick={() => void refreshDevices()}>{refreshingDevices ? '正在刷新…' : '授权并刷新设备'}</button>
          </div>
          <label className="settings-field settings-field--wide">
            <span className="settings-field__label">昵称</span>
            <input className="settings-field__control" name="nickname" autoComplete="nickname" value={settings.nickname} onChange={(event) => updateSettings({ nickname: event.target.value })} />
          </label>
          <label className="settings-field">
            <span className="settings-field__label">快捷键</span>
            <input className="settings-field__control" name="push-to-talk-key" value={settings.push_to_talk_key} onChange={(event) => updateSettings({ push_to_talk_key: event.target.value })} />
          </label>
          <label className="settings-field">
            <span className="settings-field__label">麦克风设备</span>
            <select className="settings-field__control" name="microphone-device" value={settings.microphone_device} onChange={(event) => updateSettings({ microphone_device: event.target.value })}>
              <option value="">跟随系统默认</option>
              {devices.inputs.map((device) => <option key={device.deviceId} value={device.deviceId}>{device.label}</option>)}
            </select>
          </label>
          <label className="settings-field">
            <span className="settings-field__label">输出设备</span>
            <select className="settings-field__control" name="output-device" disabled={!outputSelectionSupported} value={settings.output_device} onChange={(event) => updateSettings({ output_device: event.target.value })}>
              <option value="">跟随系统默认</option>
              {devices.outputs.map((device) => <option key={device.deviceId} value={device.deviceId}>{device.label}</option>)}
            </select>
          </label>
          <label className="settings-field">
            <span className="settings-field__label">语音模式</span>
            <select className="settings-field__control" name="voice-mode" value={settings.voice_mode} onChange={(event) => updateSettings({ voice_mode: event.target.value })}>
              <option value="push_to_talk">按键说话</option>
              <option value="free_talk">自由说话</option>
            </select>
          </label>
          <label className="settings-field settings-field--wide">
            <span className="settings-field__label">房间音量</span>
            <input className="settings-field__control settings-field__control--range" name="output-volume" type="range" min="0" max="100" value={settings.output_volume} onChange={(event) => updateSettings({ output_volume: Number(event.target.value) })} />
          </label>
        </div>
        <div className="settings-actions">
          {saveNotice && <p className="settings-save-notice" role="status">{saveNotice}</p>}
          <button className="settings-button settings-button--primary" type="button" disabled={!canSave} onClick={() => void saveSettings()}>{saving ? '保存中…' : '保存设置'}</button>
        </div>
      </section>
    </main>
  )
}

export default App

