import { type CSSProperties, useEffect, useState } from 'react'
import { Events } from '@wailsio/runtime'

import {
  KEYBOARD_EVENT_NAME,
  KEYBOARD_HOOK_STATUS_EVENT_NAME,
  KEYBOARD_HOOK_STATUS_REQUEST_EVENT_NAME,
  applyHookStatus,
  applyKeyboardEvent,
  createInitialKeyboardSpikeState,
  keyboardDomEventToInput,
  normalizeHookStatusData,
  normalizeKeyboardEventData,
  resetKeyboardStats,
  type KeyboardEventRecord,
  type KeyboardEventSource,
  type KeyboardHookStatus,
  type KeyboardSourceState,
} from './keyboardState'

const SOURCE_TITLES: Record<KeyboardEventSource, string> = {
  native: 'Windows native hook（游戏前台验证）',
  dom: 'WebView DOM fallback（仅 echo 聚焦对照）',
}

export function KeyboardSpike() {
  const [state, setState] = useState(() => createInitialKeyboardSpikeState('V'))

  useEffect(() => {
    const unsubscribeKeyboard = Events.On(KEYBOARD_EVENT_NAME, (event) => {
      const keyboardEvent = normalizeKeyboardEventData(event.data)
      if (!keyboardEvent) return
      setState((current) => applyKeyboardEvent(current, keyboardEvent))
    })
    const unsubscribeHookStatus = Events.On(KEYBOARD_HOOK_STATUS_EVENT_NAME, (event) => {
      const hookStatus = normalizeHookStatusData(event.data)
      if (!hookStatus) return
      setState((current) => applyHookStatus(current, hookStatus))
    })

    void Events.Emit(KEYBOARD_HOOK_STATUS_REQUEST_EVENT_NAME)

    return () => {
      unsubscribeKeyboard()
      unsubscribeHookStatus()
    }
  }, [])

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.repeat) return
      const keyboardEvent = keyboardDomEventToInput(event, true)
      if (!keyboardEvent) return
      setState((current) => applyKeyboardEvent(current, keyboardEvent))
    }

    function handleKeyUp(event: KeyboardEvent) {
      const keyboardEvent = keyboardDomEventToInput(event, false)
      if (!keyboardEvent) return
      setState((current) => applyKeyboardEvent(current, keyboardEvent))
    }

    window.addEventListener('keydown', handleKeyDown)
    window.addEventListener('keyup', handleKeyUp)

    return () => {
      window.removeEventListener('keydown', handleKeyDown)
      window.removeEventListener('keyup', handleKeyUp)
    }
  }, [])

  return (
    <main style={styles.shell} aria-labelledby="keyboard-spike-title">
      <section style={styles.card}>
        <div style={styles.sectionHeader}>
          <div>
            <p style={styles.eyebrow}>Issue #9 Spike</p>
            <h1 id="keyboard-spike-title" style={styles.title}>按键说话 press/release 验证</h1>
          </div>
          <button type="button" style={styles.button} onClick={() => setState(resetKeyboardStats)}>
            重置统计
          </button>
        </div>
        <p style={styles.description}>
          验证 Wails Go 原生键盘路径和 WebView 聚焦时 DOM fallback 是否能分别观察默认按键说话快捷键的按下/释放。
        </p>
        <p style={styles.targetKey}>当前目标键：{state.targetKey}</p>
      </section>

      <SourceCard
        source="native"
        sourceState={state.sources.native}
        hookStatus={state.hookStatus}
        pendingSequence={state.nativeOrdering.nextSequence}
        pendingEventCount={Object.keys(state.nativeOrdering.pending).length}
        description="游戏前台验证只读取 native 路径计数；每轮开始前请先点击“重置统计”。"
      />

      <SourceCard
        source="dom"
        sourceState={state.sources.dom}
        description="仅用于 echo/WebView 聚焦时的对照，不代表游戏前台可用。"
      />

      <section style={styles.card} aria-labelledby="manual-steps-title">
        <h2 id="manual-steps-title" style={styles.sectionTitle}>手动验证说明</h2>
        <ol style={styles.list}>
          <li><strong>每轮开始：</strong>先点击“重置统计”；如果不能重置，则记录 native 与 DOM 当前基线后再测试。</li>
          <li><strong>普通桌面对照：</strong>聚焦 echo 窗口，按下/释放 V 10 次；分别记录 native 与 DOM 两张卡片的完整循环。</li>
          <li><strong>游戏前台验证：</strong>切换到游戏前台前再次重置统计；只读取 Windows native hook 卡片计数，不用 DOM fallback 或累计总数判定。</li>
          <li>DOM fallback 只证明 WebView 聚焦时的键盘事件，游戏前台能力必须看 native 事件和 HITL 文档记录。</li>
          <li>管理员权限游戏或反作弊限制只记录为兼容性边界，不实现提权或规避。</li>
        </ol>
      </section>
    </main>
  )
}

type SourceCardProps = {
  source: KeyboardEventSource
  sourceState: KeyboardSourceState
  description: string
  hookStatus?: KeyboardHookStatus
  pendingSequence?: number
  pendingEventCount?: number
}

function SourceCard({
  source,
  sourceState,
  description,
  hookStatus,
  pendingSequence,
  pendingEventCount = 0,
}: SourceCardProps) {
  const title = SOURCE_TITLES[source]

  return (
    <section style={styles.card} aria-labelledby={`${source}-state-title`}>
      <div style={styles.sectionHeader}>
        <h2 id={`${source}-state-title`} style={styles.sectionTitle}>{title}</h2>
        <span aria-live="polite" style={sourceState.isPressed ? styles.activeBadge : styles.badge}>
          按键状态：{sourceState.isPressed ? '按下' : '释放'}
        </span>
      </div>
      <p style={styles.description}>{description}</p>
      {hookStatus && <HookStatusView hookStatus={hookStatus} />}
      {pendingEventCount > 0 && pendingSequence && (
        <p style={styles.pendingText}>等待 native seq {pendingSequence}，已缓冲 {pendingEventCount} 个乱序事件。</p>
      )}

      <dl style={styles.statsGrid} aria-label={`${title}键盘事件计数`}>
        <div style={styles.statCard}>
          <dt style={styles.statLabel}>按下次数</dt>
          <dd style={styles.statValue}>按下次数：{sourceState.downCount}</dd>
        </div>
        <div style={styles.statCard}>
          <dt style={styles.statLabel}>释放次数</dt>
          <dd style={styles.statValue}>释放次数：{sourceState.upCount}</dd>
        </div>
        <div style={styles.statCard}>
          <dt style={styles.statLabel}>完整循环</dt>
          <dd style={styles.statValue}>完整循环：{sourceState.completedCycles}</dd>
        </div>
      </dl>

      {sourceState.missingRelease && (
        <p role="alert" style={styles.warningText}>{source} 路径检测到按下未释放，请检查 release 事件是否丢失。</p>
      )}

      <h3 style={styles.eventLogTitle}>最近{source === 'native' ? ' native' : ' DOM'}事件</h3>
      {sourceState.events.length === 0 ? (
        <p style={styles.emptyText}>暂无{source === 'native' ? ' native' : ' DOM'}事件。</p>
      ) : (
        <ol style={styles.eventList} aria-live="polite">
          {sourceState.events.map((event) => (
            <li key={`${event.source}-${event.logIndex}-${event.sequence ?? 'dom'}`}>{formatEvent(event)}</li>
          ))}
        </ol>
      )}
    </section>
  )
}

function HookStatusView({ hookStatus }: { hookStatus: KeyboardHookStatus }) {
  return (
    <div style={styles.statusBlock}>
      <p style={styles.statusText}>Native hook 状态：{formatHookStatus(hookStatus.status)}</p>
      {hookStatus.message && <p style={styles.statusMessage}>{hookStatus.message}</p>}
    </div>
  )
}

function formatHookStatus(status: KeyboardHookStatus['status']) {
  switch (status) {
    case 'enabled':
      return '可用'
    case 'disabled':
      return '不可用'
    case 'unsupported':
      return '当前平台不支持'
    case 'unknown':
      return '等待状态'
  }
}

function formatEvent(event: KeyboardEventRecord) {
  const base = `${event.source} · ${event.key} · ${event.pressed ? '按下' : '释放'}`
  return event.sequence ? `${base} · seq ${event.sequence}` : base
}

const styles = {
  shell: {
    minHeight: '100vh',
    boxSizing: 'border-box',
    padding: 24,
    background: '#F3F6F8',
    color: '#1F2933',
    fontFamily: '"Microsoft YaHei UI", "Segoe UI", sans-serif',
  },
  card: {
    maxWidth: 760,
    margin: '0 auto 16px',
    padding: 20,
    border: '1px solid #D7DEE5',
    borderRadius: 16,
    background: '#FFFFFF',
    boxShadow: '0 1px 2px rgb(31 41 51 / 0.08)',
  },
  eyebrow: {
    margin: '0 0 8px',
    color: '#647282',
    fontSize: 12,
    fontWeight: 500,
  },
  title: {
    margin: 0,
    fontSize: 28,
    lineHeight: '36px',
  },
  description: {
    margin: '8px 0 12px',
    color: '#647282',
    lineHeight: '22px',
  },
  sectionHeader: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 12,
  },
  sectionTitle: {
    margin: '0 0 12px',
    fontSize: 20,
    lineHeight: '28px',
  },
  badge: {
    border: '1px solid #D7DEE5',
    borderRadius: 4,
    padding: '4px 8px',
    color: '#647282',
    fontSize: 12,
    whiteSpace: 'nowrap',
  },
  activeBadge: {
    border: '1px solid #0B63F6',
    borderRadius: 4,
    padding: '4px 8px',
    color: '#0B63F6',
    fontSize: 12,
    whiteSpace: 'nowrap',
  },
  button: {
    border: '1px solid #0B63F6',
    borderRadius: 8,
    padding: '8px 12px',
    background: '#FFFFFF',
    color: '#0B63F6',
    fontSize: 14,
    fontWeight: 600,
    cursor: 'pointer',
  },
  targetKey: {
    margin: '12px 0 0',
    color: '#1F2933',
    fontWeight: 600,
  },
  statusBlock: {
    margin: '0 0 12px',
    padding: 12,
    border: '1px solid #D7DEE5',
    borderRadius: 8,
    background: '#F8FAFC',
  },
  statusText: {
    margin: 0,
    color: '#1F2933',
    fontWeight: 700,
  },
  statusMessage: {
    margin: '8px 0 0',
    color: '#B42318',
    lineHeight: '22px',
  },
  pendingText: {
    margin: '0 0 12px',
    color: '#B42318',
    lineHeight: '22px',
  },
  statsGrid: {
    display: 'grid',
    gridTemplateColumns: 'repeat(3, minmax(0, 1fr))',
    gap: 12,
    margin: 0,
  },
  statCard: {
    border: '1px solid #D7DEE5',
    borderRadius: 8,
    padding: 12,
    background: '#F8FAFC',
  },
  statLabel: {
    margin: 0,
    color: '#647282',
    fontSize: 12,
    fontWeight: 600,
  },
  statValue: {
    margin: '8px 0 0',
    color: '#1F2933',
    fontSize: 18,
    fontWeight: 700,
  },
  warningText: {
    margin: '12px 0 0',
    color: '#B42318',
    lineHeight: '22px',
  },
  list: {
    margin: 0,
    paddingLeft: 20,
    color: '#647282',
    lineHeight: '24px',
  },
  eventLogTitle: {
    margin: '16px 0 0',
    fontSize: 16,
    lineHeight: '24px',
  },
  emptyText: {
    margin: '8px 0 0',
    color: '#8B97A4',
  },
  eventList: {
    margin: '8px 0 0',
    paddingLeft: 20,
    color: '#647282',
    lineHeight: '24px',
  },
} satisfies Record<string, CSSProperties>
