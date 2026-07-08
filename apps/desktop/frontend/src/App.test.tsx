import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn(() => () => undefined),
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
    expect(screen.getByText(/普通桌面对照/)).toBeVisible()
    expect(screen.getByText(/游戏前台验证/)).toBeVisible()
  })
})
