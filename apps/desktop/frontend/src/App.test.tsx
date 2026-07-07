import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import App from './App'

describe('App device tray spike smoke', () => {
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it('renders visible echo device tray spike content', async () => {
    Object.defineProperty(navigator, 'mediaDevices', {
      value: {
        enumerateDevices: vi.fn().mockResolvedValue([]),
        getUserMedia: vi.fn(),
      },
      configurable: true,
    })

    render(<App />)

    expect(await screen.findByRole('heading', { name: '音频设备和托盘验证' })).toBeVisible()
    expect(screen.getByText(/系统托盘手动验证/)).toBeVisible()
  })
})
