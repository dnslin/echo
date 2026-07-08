import { type CSSProperties, useEffect, useState } from 'react'
import { Events } from '@wailsio/runtime'

import {
  KEYBOARD_EVENT_NAME,
  applyKeyboardEvent,
  createInitialKeyboardSpikeState,
  keyboardDomEventToInput,
  normalizeKeyboardEventData,
  type KeyboardEventRecord,
} from './keyboardState'

export function KeyboardSpike() {
  const [state, setState] = useState(() => createInitialKeyboardSpikeState('V'))

  useEffect(() => {
    return Events.On(KEYBOARD_EVENT_NAME, (event) => {
      const keyboardEvent = normalizeKeyboardEventData(event.data)
      if (!keyboardEvent) return
      setState((current) => applyKeyboardEvent(current, keyboardEvent))
    })
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
        <p style={styles.eyebrow}>Issue #9 Spike</p>
        <h1 id="keyboard-spike-title" style={styles.title}>按键说话 press/release 验证</h1>
        <p style={styles.description}>
          验证 Wails Go 原生键盘路径和 WebView 聚焦时 DOM fallback 是否能观察默认按键说话快捷键的按下/释放。
        </p>
      </section>

      <section style={styles.card} aria-labelledby="keyboard-state-title">
        <div style={styles.sectionHeader}>
          <h2 id="keyboard-state-title" style={styles.sectionTitle}>事件状态</h2>
          <span aria-live="polite" style={state.isPressed ? styles.activeBadge : styles.badge}>
            按键状态：{state.isPressed ? '按下' : '释放'}
          </span>
        </div>

        <p style={styles.targetKey}>当前目标键：{state.targetKey}</p>
        <dl style={styles.statsGrid} aria-label="键盘事件计数">
          <div style={styles.statCard}>
            <dt style={styles.statLabel}>按下次数</dt>
            <dd style={styles.statValue}>按下次数：{state.downCount}</dd>
          </div>
          <div style={styles.statCard}>
            <dt style={styles.statLabel}>释放次数</dt>
            <dd style={styles.statValue}>释放次数：{state.upCount}</dd>
          </div>
          <div style={styles.statCard}>
            <dt style={styles.statLabel}>完整循环</dt>
            <dd style={styles.statValue}>完整循环：{state.completedCycles}</dd>
          </div>
        </dl>

        {state.missingRelease && (
          <p role="alert" style={styles.warningText}>检测到按下未释放，请检查 release 事件是否丢失。</p>
        )}
      </section>

      <section style={styles.card} aria-labelledby="manual-steps-title">
        <h2 id="manual-steps-title" style={styles.sectionTitle}>手动验证说明</h2>
        <ol style={styles.list}>
          <li><strong>普通桌面对照：</strong>聚焦 echo 窗口，按下/释放 V 10 次；应得到 10 个完整循环且没有缺失 release 提示。</li>
          <li><strong>游戏前台验证：</strong>切换到游戏前台，连续按下/释放 V 10 次；观察 native 事件是否仍计数为 10 个完整循环。</li>
          <li>DOM fallback 只证明 WebView 聚焦时的键盘事件，游戏前台能力必须看 native 事件和 HITL 文档记录。</li>
          <li>管理员权限游戏或反作弊限制只记录为兼容性边界，不实现提权或规避。</li>
        </ol>
      </section>

      <section style={styles.card} aria-labelledby="event-log-title">
        <h2 id="event-log-title" style={styles.sectionTitle}>最近事件</h2>
        {state.events.length === 0 ? (
          <p style={styles.emptyText}>暂无按键事件。</p>
        ) : (
          <ol style={styles.eventList} aria-live="polite">
            {state.events.map((event) => <li key={event.sequence}>{formatEvent(event)}</li>)}
          </ol>
        )}
      </section>
    </main>
  )
}

function formatEvent(event: KeyboardEventRecord) {
  return `${event.source} · ${event.key} · ${event.pressed ? '按下' : '释放'}`
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
    margin: '8px 0 0',
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
  },
  activeBadge: {
    border: '1px solid #0B63F6',
    borderRadius: 4,
    padding: '4px 8px',
    color: '#0B63F6',
    fontSize: 12,
  },
  targetKey: {
    margin: '12px 0',
    color: '#1F2933',
    fontWeight: 600,
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
  emptyText: {
    margin: '12px 0 0',
    color: '#8B97A4',
  },
  eventList: {
    margin: '12px 0 0',
    paddingLeft: 20,
    color: '#647282',
    lineHeight: '24px',
  },
} satisfies Record<string, CSSProperties>
