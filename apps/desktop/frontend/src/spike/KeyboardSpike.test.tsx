import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react'
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
  const emitToGo = vi.fn(() => Promise.resolve(true))

  return {
    handlers,
    on,
    emitToGo,
    dispatchFromGo(eventName: string, data: unknown) {
      ;(handlers.get(eventName) ?? []).forEach((handler) => handler({ data }))
    },
    reset() {
      handlers.clear()
      on.mockClear()
      emitToGo.mockClear()
    },
  }
})

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: runtimeMock.on,
    Emit: runtimeMock.emitToGo,
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

  it('renders separate native and DOM controls, subscribes to native events, and requests hook status', () => {
    render(<KeyboardSpike />)

    expect(screen.getByRole('heading', { name: '按键说话 press/release 验证' })).toBeVisible()
    expect(screen.getByText('当前目标键：V')).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Windows native hook（游戏前台验证）' })).toBeVisible()
    expect(screen.getByRole('heading', { name: 'WebView DOM fallback（仅 echo 聚焦对照）' })).toBeVisible()
    expect(runtimeMock.on).toHaveBeenCalledWith('keyboard:push-to-talk', expect.any(Function))
    expect(runtimeMock.on).toHaveBeenCalledWith('keyboard:hook-status', expect.any(Function))
    expect(runtimeMock.emitToGo).toHaveBeenCalledWith('keyboard:hook-status-request')
  })

  it('counts native and DOM fallback cycles independently when echo is focused', () => {
    render(<KeyboardSpike />)

    fireEvent.keyDown(window, { key: 'v' })
    fireEvent.keyUp(window, { key: 'v' })
    act(() => {
      runtimeMock.dispatchFromGo('keyboard:push-to-talk', { key: 'V', pressed: true, source: 'native', sequence: 1 })
      runtimeMock.dispatchFromGo('keyboard:push-to-talk', { key: 'V', pressed: false, source: 'native', sequence: 2 })
    })

    const nativeCard = screen.getByRole('region', { name: 'Windows native hook（游戏前台验证）' })
    const domCard = screen.getByRole('region', { name: 'WebView DOM fallback（仅 echo 聚焦对照）' })
    expect(within(nativeCard).getByText('完整循环：1')).toBeVisible()
    expect(within(domCard).getByText('完整循环：1')).toBeVisible()
    expect(within(nativeCard).getByText('native · V · 释放 · seq 2')).toBeVisible()
    expect(within(domCard).getByText('dom · V · 释放')).toBeVisible()
  })

  it('reorders native release-before-down payloads by sequence without a missing-release warning', () => {
    render(<KeyboardSpike />)

    act(() => {
      runtimeMock.dispatchFromGo('keyboard:push-to-talk', { key: 'V', pressed: false, source: 'native', sequence: 2 })
      runtimeMock.dispatchFromGo('keyboard:push-to-talk', { key: 'V', pressed: true, source: 'native', sequence: 1 })
    })

    const nativeCard = screen.getByRole('region', { name: 'Windows native hook（游戏前台验证）' })
    expect(within(nativeCard).getByText('完整循环：1')).toBeVisible()
    expect(within(nativeCard).queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows pending native sequence gaps while waiting for earlier events', () => {
    render(<KeyboardSpike />)

    act(() => {
      runtimeMock.dispatchFromGo('keyboard:push-to-talk', { key: 'V', pressed: false, source: 'native', sequence: 2 })
    })

    const nativeCard = screen.getByRole('region', { name: 'Windows native hook（游戏前台验证）' })
    expect(within(nativeCard).getByText('等待 native seq 1，已缓冲 1 个乱序事件。')).toBeVisible()
  })

  it('shows missing release warnings per source without letting DOM hide native state', () => {
    render(<KeyboardSpike />)

    fireEvent.keyDown(window, { key: 'V' })
    fireEvent.keyUp(window, { key: 'V' })
    act(() => {
      runtimeMock.dispatchFromGo('keyboard:push-to-talk', { key: 'V', pressed: true, source: 'native', sequence: 1 })
    })

    const nativeCard = screen.getByRole('region', { name: 'Windows native hook（游戏前台验证）' })
    const domCard = screen.getByRole('region', { name: 'WebView DOM fallback（仅 echo 聚焦对照）' })
    expect(within(nativeCard).getByRole('alert')).toHaveTextContent('native 路径检测到按下未释放')
    expect(within(domCard).queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows disabled hook status and keeps DOM fallback labeled as a focused-window control path', () => {
    render(<KeyboardSpike />)

    act(() => {
      runtimeMock.dispatchFromGo('keyboard:hook-status', { status: 'disabled', message: 'install low-level keyboard hook: access denied' })
    })

    expect(screen.getByText('Native hook 状态：不可用')).toBeVisible()
    expect(screen.getByText('install low-level keyboard hook: access denied')).toBeVisible()
    expect(screen.getByText('仅用于 echo/WebView 聚焦时的对照，不代表游戏前台可用。')).toBeVisible()
  })

  it('resets both path counters before each HITL round while preserving hook status', () => {
    render(<KeyboardSpike />)

    act(() => {
      runtimeMock.dispatchFromGo('keyboard:hook-status', { status: 'enabled' })
      runtimeMock.dispatchFromGo('keyboard:push-to-talk', { key: 'V', pressed: true, source: 'native', sequence: 1 })
      runtimeMock.dispatchFromGo('keyboard:push-to-talk', { key: 'V', pressed: false, source: 'native', sequence: 2 })
    })
    fireEvent.keyDown(window, { key: 'V' })
    fireEvent.keyUp(window, { key: 'V' })

    fireEvent.click(screen.getByRole('button', { name: '重置统计' }))

    expect(screen.getByText('Native hook 状态：可用')).toBeVisible()
    expect(screen.getAllByText('完整循环：0')).toHaveLength(2)
  })

  it('continues counting native hook events after reset with the next monotonic sequence', () => {
    render(<KeyboardSpike />)

    act(() => {
      runtimeMock.dispatchFromGo('keyboard:push-to-talk', { key: 'V', pressed: true, source: 'native', sequence: 1 })
      runtimeMock.dispatchFromGo('keyboard:push-to-talk', { key: 'V', pressed: false, source: 'native', sequence: 2 })
    })
    fireEvent.click(screen.getByRole('button', { name: '重置统计' }))
    act(() => {
      runtimeMock.dispatchFromGo('keyboard:push-to-talk', { key: 'V', pressed: true, source: 'native', sequence: 3 })
      runtimeMock.dispatchFromGo('keyboard:push-to-talk', { key: 'V', pressed: false, source: 'native', sequence: 4 })
    })

    const nativeCard = screen.getByRole('region', { name: 'Windows native hook（游戏前台验证）' })
    expect(within(nativeCard).getByText('完整循环：1')).toBeVisible()
    expect(within(nativeCard).queryByText(/等待 native seq/)).not.toBeInTheDocument()
  })
})
