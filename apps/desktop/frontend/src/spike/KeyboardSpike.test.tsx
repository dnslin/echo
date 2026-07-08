import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const runtimeMock = vi.hoisted(() => {
  const handlers = new Map<string, Array<(event: { data: unknown }) => void>>()
  const on = vi.fn((eventName: string, handler: (event: { data: unknown }) => void) => {
    const currentHandlers = handlers.get(eventName) ?? []
    currentHandlers.push(handler)
    handlers.set(eventName, currentHandlers)

    return () => {
      handlers.set(eventName, (handlers.get(eventName) ?? []).filter((candidate) => candidate !== handler))
    }
  })

  return {
    handlers,
    on,
    emit(eventName: string, data: unknown) {
      ;(handlers.get(eventName) ?? []).forEach((handler) => handler({ data }))
    },
    reset() {
      handlers.clear()
      on.mockClear()
    },
  }
})

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: runtimeMock.on,
  },
}))

import { KeyboardSpike } from './KeyboardSpike'

describe('KeyboardSpike', () => {
  beforeEach(() => {
    runtimeMock.reset()
  })

  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it('renders the keyboard spike controls and subscribes to native Wails events', () => {
    render(<KeyboardSpike />)

    expect(screen.getByRole('heading', { name: '按键说话 press/release 验证' })).toBeVisible()
    expect(screen.getByText('当前目标键：V')).toBeVisible()
    expect(screen.getByText(/游戏前台验证/)).toBeVisible()
    expect(runtimeMock.on).toHaveBeenCalledWith('keyboard:push-to-talk', expect.any(Function))
  })

  it('counts 10 DOM fallback press/release cycles while the app is focused', () => {
    render(<KeyboardSpike />)

    for (let index = 0; index < 10; index += 1) {
      fireEvent.keyDown(window, { key: 'v' })
      fireEvent.keyUp(window, { key: 'v' })
    }

    expect(screen.getByText('按下次数：10')).toBeVisible()
    expect(screen.getByText('释放次数：10')).toBeVisible()
    expect(screen.getByText('完整循环：10')).toBeVisible()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows a clear missing-release warning', () => {
    render(<KeyboardSpike />)

    fireEvent.keyDown(window, { key: 'V' })

    expect(screen.getByRole('alert')).toHaveTextContent('检测到按下未释放')
    expect(screen.getByText('按键状态：按下')).toBeVisible()
  })

  it('applies native Wails event payloads and ignores malformed payloads', () => {
    render(<KeyboardSpike />)

    act(() => {
      runtimeMock.emit('keyboard:push-to-talk', { key: 'V', pressed: true, source: 'native' })
      runtimeMock.emit('keyboard:push-to-talk', { key: 'V', pressed: false, source: 'native' })
      runtimeMock.emit('keyboard:push-to-talk', { key: 'V', pressed: 'false', source: 'native' })
    })

    expect(screen.getByText('按下次数：1')).toBeVisible()
    expect(screen.getByText('释放次数：1')).toBeVisible()
    expect(screen.getByText('完整循环：1')).toBeVisible()
    expect(screen.getByText('native · V · 释放')).toBeVisible()
  })
})
