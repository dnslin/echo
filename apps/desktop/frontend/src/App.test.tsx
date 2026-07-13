import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Settings } from '../bindings/echo/apps/desktop/internal/config/models'

const settingsBinding = vi.hoisted(() => ({
  load: vi.fn(),
  save: vi.fn(),
  resetAvatar: vi.fn(),
}))

vi.mock('../bindings/echo/apps/desktop/internal/app/settingsservice', () => ({
  Load: settingsBinding.load,
  Save: settingsBinding.save,
  ResetAvatar: settingsBinding.resetAvatar,
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn(() => () => undefined),
    Emit: vi.fn(() => Promise.resolve(true)),
  },
}))

import App from './App'

const firstLaunchSettings: Settings = {
  anonymous_id: 'anonymous-1',
  nickname: '',
  avatar_id: 'avatar-1',
  push_to_talk_key: 'V',
  microphone_device: '',
  output_device: '',
  voice_mode: 'push_to_talk',
  output_volume: 100,
}

describe('App local settings entry', () => {
  beforeEach(() => {
    settingsBinding.load.mockResolvedValue(firstLaunchSettings)
    settingsBinding.save.mockResolvedValue({ ...firstLaunchSettings, nickname: '小王' })
    settingsBinding.resetAvatar.mockResolvedValue({ ...firstLaunchSettings, avatar_id: 'avatar-2' })
  })

  afterEach(() => {
    cleanup()
    settingsBinding.load.mockReset()
    settingsBinding.save.mockReset()
    settingsBinding.resetAvatar.mockReset()
  })

  it('shows and saves the first nickname page', async () => {
    render(<App />)

    expect(await screen.findByRole('heading', { name: '欢迎使用 echo' })).toBeVisible()
    expect(screen.getByText('头像：avatar-1')).toBeVisible()
    const nickname = screen.getByLabelText('昵称')
    const continueButton = screen.getByRole('button', { name: '继续' })
    expect(continueButton).toBeDisabled()

    fireEvent.change(nickname, { target: { value: '   ' } })
    await waitFor(() => {
      expect(continueButton).toBeDisabled()
    })
    expect(settingsBinding.save).not.toHaveBeenCalled()

    fireEvent.change(nickname, { target: { value: '小王' } })
    await waitFor(() => {
      expect(nickname).toHaveValue('小王')
      expect(screen.getByRole('button', { name: '继续' })).toBeEnabled()
    })
    fireEvent.click(screen.getByRole('button', { name: '继续' }))

    await waitFor(() => {
      expect(settingsBinding.save).toHaveBeenCalledWith({ ...firstLaunchSettings, nickname: '小王' })
    })
    expect(await screen.findByText('你好，小王')).toBeVisible()
  })

  it('matches Store Unicode whitespace when enabling save', async () => {
    render(<App />)

    const nickname = await screen.findByLabelText('昵称')
    const continueButton = screen.getByRole('button', { name: '继续' })
    fireEvent.change(nickname, { target: { value: '\u0085' } })
    await waitFor(() => {
      expect(continueButton).toBeDisabled()
    })

    fireEvent.change(nickname, { target: { value: '\uFEFF' } })
    await waitFor(() => {
      expect(continueButton).toBeEnabled()
    })
  })

  it('keeps restored users on settings form while nickname edits are unsaved', async () => {
    settingsBinding.load.mockResolvedValue({ ...firstLaunchSettings, nickname: '小李' })
    render(<App />)

    expect(await screen.findByRole('heading', { name: '你好，小李' })).toBeVisible()
    fireEvent.change(screen.getByLabelText('昵称'), { target: { value: '' } })

    await waitFor(() => {
      expect(screen.queryByRole('heading', { name: '欢迎使用 echo' })).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: '保存设置' })).toBeDisabled()
    })
  })

  it('rejects an overlong nickname before saving', async () => {
    render(<App />)

    const nickname = await screen.findByLabelText('昵称')
    const continueButton = screen.getByRole('button', { name: '继续' })
    fireEvent.change(nickname, { target: { value: 'a'.repeat(17) } })

    expect(continueButton).toBeDisabled()
    expect(settingsBinding.save).not.toHaveBeenCalled()
  })

  it('allows sixteen Unicode code points on the first nickname page', async () => {
    const nicknameValue = '😀'.repeat(16)
    render(<App />)

    const nickname = await screen.findByLabelText('昵称')
    expect(nickname).not.toHaveAttribute('maxLength')
    fireEvent.change(nickname, { target: { value: nicknameValue } })

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '继续' })).toBeEnabled()
    })
    fireEvent.click(screen.getByRole('button', { name: '继续' }))
    expect(settingsBinding.save).toHaveBeenCalledWith({ ...firstLaunchSettings, nickname: nicknameValue })
  })

  it('allows sixteen Unicode code points in restored settings', async () => {
    const nicknameValue = '😀'.repeat(16)
    settingsBinding.load.mockResolvedValue({ ...firstLaunchSettings, nickname: '小李' })
    render(<App />)

    const nickname = await screen.findByLabelText('昵称')
    expect(nickname).not.toHaveAttribute('maxLength')
    fireEvent.change(nickname, { target: { value: nicknameValue } })

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '保存设置' })).toBeEnabled()
    })
  })

  it('persists all restored setting fields through the settings service', async () => {
    const restoredSettings = {
      ...firstLaunchSettings,
      nickname: '小李',
      microphone_device: 'mic-1',
      output_device: 'speaker-1',
      voice_mode: 'free_talk',
      output_volume: 37,
    }
    const persistedSettings = {
      ...restoredSettings,
      push_to_talk_key: 'B',
      microphone_device: 'mic-2',
      output_device: 'speaker-2',
      voice_mode: 'push_to_talk',
      output_volume: 42,
    }
    settingsBinding.load.mockResolvedValue(restoredSettings)
    settingsBinding.save.mockResolvedValue(persistedSettings)
    render(<App />)

    await screen.findByText('你好，小李')
    fireEvent.change(screen.getByLabelText('快捷键'), { target: { value: 'B' } })
    fireEvent.change(screen.getByLabelText('麦克风设备'), { target: { value: 'mic-2' } })
    fireEvent.change(screen.getByLabelText('输出设备'), { target: { value: 'speaker-2' } })
    fireEvent.change(screen.getByLabelText('语音模式'), { target: { value: 'push_to_talk' } })
    fireEvent.change(screen.getByRole('slider'), { target: { value: '42' } })

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '保存设置' })).toBeEnabled()
    })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => {
      expect(settingsBinding.save).toHaveBeenCalledWith(persistedSettings)
    })
  })

  it('shows a local settings save error', async () => {
    settingsBinding.save.mockRejectedValue(new Error('save settings failed'))
    render(<App />)

    const nickname = await screen.findByLabelText('昵称')
    fireEvent.change(nickname, { target: { value: '小王' } })
    fireEvent.click(screen.getByRole('button', { name: '继续' }))

    expect(await screen.findByText('无法保存本地设置，请检查配置目录权限。')).toBeVisible()
  })

  it('shows restored settings and persists a reset avatar', async () => {
    settingsBinding.load.mockResolvedValue({
      ...firstLaunchSettings,
      nickname: '小李',
      avatar_id: 'avatar-old',
      microphone_device: 'mic-1',
      output_device: 'speaker-1',
      voice_mode: 'free_talk',
      output_volume: 37,
    })
    settingsBinding.resetAvatar.mockResolvedValue({
      ...firstLaunchSettings,
      nickname: '小李',
      avatar_id: 'avatar-new',
      microphone_device: 'mic-1',
      output_device: 'speaker-1',
      voice_mode: 'free_talk',
      output_volume: 37,
    })

    render(<App />)

    expect(await screen.findByText('你好，小李')).toBeVisible()
    expect(screen.getByDisplayValue('mic-1')).toBeVisible()
    expect(screen.getByDisplayValue('speaker-1')).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: '重新随机头像' }))

    await waitFor(() => {
      expect(settingsBinding.resetAvatar).toHaveBeenCalledOnce()
    })
    expect(await screen.findByText('头像：avatar-new')).toBeVisible()
  })

  it('shows a reset avatar error', async () => {
    settingsBinding.load.mockResolvedValue({ ...firstLaunchSettings, nickname: '小李' })
    settingsBinding.resetAvatar.mockRejectedValue(new Error('reset avatar failed'))
    render(<App />)

    await screen.findByText('你好，小李')
    fireEvent.click(screen.getByRole('button', { name: '重新随机头像' }))

    expect(await screen.findByText('无法重置随机头像，请稍后重试。')).toBeVisible()
  })

  it('shows a local settings load error', async () => {
    settingsBinding.load.mockRejectedValue(new Error('read settings failed'))

    render(<App />)

    expect(await screen.findByText('无法加载本地设置，请检查配置目录权限。')).toBeVisible()
  })
})
