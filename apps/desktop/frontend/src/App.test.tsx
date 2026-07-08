import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn(() => () => undefined),
    Emit: vi.fn(() => Promise.resolve(true)),
  },
}))

import App from './App'

describe('App keyboard spike smoke', () => {
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it('renders visible echo keyboard spike content', () => {
    render(<App />)

    expect(screen.getByRole('heading', { name: '按键说话 press/release 验证' })).toBeVisible()
    expect(screen.getByText('当前目标键：V')).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Windows native hook（游戏前台验证）' })).toBeVisible()
    expect(screen.getByRole('heading', { name: 'WebView DOM fallback（仅 echo 聚焦对照）' })).toBeVisible()
  })
})
