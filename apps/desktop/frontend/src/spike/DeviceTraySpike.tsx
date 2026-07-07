import { type ChangeEvent, type CSSProperties, useEffect, useRef, useState } from 'react'

import {
  applyOutputDevice,
  canSelectOutputDevice,
  createLevelMeter,
  listMediaDevices,
  requestMicrophone,
  type EchoAudioDevice,
} from './mediaDevices'

type PermissionStatus = 'idle' | 'requesting' | 'granted' | 'failed'

const permissionStatusText: Record<PermissionStatus, string> = {
  idle: '未请求',
  requesting: '请求中',
  granted: '已授权',
  failed: '授权失败',
}

export function DeviceTraySpike() {
  const [microphones, setMicrophones] = useState<EchoAudioDevice[]>([])
  const [outputs, setOutputs] = useState<EchoAudioDevice[]>([])
  const [selectedMicrophoneID, setSelectedMicrophoneID] = useState('')
  const [selectedOutputID, setSelectedOutputID] = useState('')
  const [permissionStatus, setPermissionStatus] = useState<PermissionStatus>('idle')
  const [deviceMessage, setDeviceMessage] = useState('正在读取设备列表...')
  const [microphoneMessage, setMicrophoneMessage] = useState('')
  const [outputMessage, setOutputMessage] = useState('')
  const [inputLevel, setInputLevel] = useState(0)
  const [runtimeSeconds, setRuntimeSeconds] = useState(0)

  const streamRef = useRef<MediaStream | null>(null)
  const cleanupLevelMeterRef = useRef<() => void>(() => undefined)
  const microphoneRequestIDRef = useRef(0)
  const outputAudioRef = useRef<HTMLAudioElement | null>(null)

  useEffect(() => {
    void refreshDevices()
  }, [])

  useEffect(() => {
    const startedAt = Date.now()
    const timerID = window.setInterval(() => {
      setRuntimeSeconds(Math.floor((Date.now() - startedAt) / 1000))
    }, 1000)

    return () => window.clearInterval(timerID)
  }, [])

  useEffect(() => {
    return () => {
      microphoneRequestIDRef.current += 1
      stopCurrentMicrophone()
    }
  }, [])

  useEffect(() => {
    const audio = outputAudioRef.current
    if (audio && !canSelectOutputDevice(audio)) {
      setOutputMessage('当前 WebView2 不支持指定输出设备，已跟随系统默认输出设备。')
    }
  }, [])

  async function refreshDevices() {
    setDeviceMessage('正在读取设备列表...')
    try {
      const devices = await listMediaDevices()
      setMicrophones(devices.microphones)
      setOutputs(devices.outputs)
      setSelectedMicrophoneID((current) => selectAvailableDeviceID(current, devices.microphones))
      setSelectedOutputID((current) => selectAvailableDeviceID(current, devices.outputs))

      const microphoneText = devices.microphones.length > 0 ? '' : '未检测到可用麦克风'
      const outputText = devices.outputs.length > 0 ? '' : '未检测到输出设备，将跟随系统默认输出设备。'
      setDeviceMessage([microphoneText, outputText].filter(Boolean).join(' ') || '设备列表已刷新')
    } catch (error) {
      setDeviceMessage(error instanceof Error ? error.message : String(error))
    }
  }

  async function requestPermissionAndStartMicrophone() {
    stopCurrentMicrophone()
    await startMicrophone(selectedMicrophoneID || undefined)
    await refreshDevices()
  }

  async function startMicrophone(deviceId?: string) {
    const requestID = microphoneRequestIDRef.current + 1
    microphoneRequestIDRef.current = requestID
    setPermissionStatus('requesting')
    setMicrophoneMessage('正在请求麦克风权限...')
    try {
      const stream = await requestMicrophone(deviceId)
      if (!isCurrentMicrophoneRequest(requestID)) {
        stopMediaStream(stream)
        return
      }

      stopCurrentMicrophone()
      try {
        const cleanupLevelMeter = createLevelMeter(stream, setInputLevel)
        streamRef.current = stream
        cleanupLevelMeterRef.current = cleanupLevelMeter
        setPermissionStatus('granted')
        setMicrophoneMessage(deviceId ? '麦克风已切换，输入电平正在更新。' : '麦克风已授权，输入电平正在更新。')
      } catch (error) {
        stopMediaStream(stream)
        if (!isCurrentMicrophoneRequest(requestID)) return
        setPermissionStatus('failed')
        setInputLevel(0)
        setMicrophoneMessage(`无法使用麦克风，请检查系统权限。${formatOptionalError(error)}`)
      }
    } catch (error) {
      if (!isCurrentMicrophoneRequest(requestID)) return
      stopCurrentMicrophone()
      setPermissionStatus('failed')
      setInputLevel(0)
      setMicrophoneMessage(`无法使用麦克风，请检查系统权限。${formatOptionalError(error)}`)
    }
  }

  async function handleMicrophoneChange(event: ChangeEvent<HTMLSelectElement>) {
    const deviceId = event.target.value
    setSelectedMicrophoneID(deviceId)
    stopCurrentMicrophone()
    await startMicrophone(deviceId || undefined)
  }

  async function handleOutputChange(event: ChangeEvent<HTMLSelectElement>) {
    const sinkId = event.target.value
    setSelectedOutputID(sinkId)
    await applySelectedOutputDevice(sinkId)
  }

  async function applySelectedOutputDevice(sinkId = selectedOutputID) {
    const audio = outputAudioRef.current
    if (!audio) {
      setOutputMessage('输出设备验证元素未就绪。')
      return
    }
    if (!outputs.some((device) => device.deviceId === sinkId)) {
      setOutputMessage('未检测到输出设备，将跟随系统默认输出设备。')
      return
    }

    const result = await applyOutputDevice(audio, sinkId)
    setOutputMessage(result.message)
  }

  function stopCurrentMicrophone() {
    cleanupLevelMeterRef.current()
    cleanupLevelMeterRef.current = () => undefined
    if (streamRef.current) {
      stopMediaStream(streamRef.current)
    }
    streamRef.current = null
    setInputLevel(0)
  }

  function isCurrentMicrophoneRequest(requestID: number) {
    return microphoneRequestIDRef.current === requestID
  }

  const levelBars = Array.from({ length: 20 }, (_, index) => index < Math.round(inputLevel / 5))

  return (
    <main style={styles.shell} aria-labelledby="device-tray-title">
      <section style={styles.card}>
        <p style={styles.eyebrow}>Issue #7 Spike</p>
        <h1 id="device-tray-title" style={styles.title}>音频设备和托盘验证</h1>
        <p style={styles.description}>验证 WebView2 媒体设备能力和关闭窗口进入系统托盘后的页面保持状态。</p>
      </section>

      <section style={styles.card} aria-labelledby="microphone-title">
        <div style={styles.sectionHeader}>
          <h2 id="microphone-title" style={styles.sectionTitle}>麦克风</h2>
          <span aria-live="polite" style={styles.badge}>{permissionStatusText[permissionStatus]}</span>
        </div>

        <div style={styles.actions}>
          <button type="button" style={styles.primaryButton} onClick={requestPermissionAndStartMicrophone}>
            请求麦克风权限
          </button>
          <button type="button" style={styles.secondaryButton} onClick={refreshDevices}>
            刷新设备
          </button>
        </div>

        <label style={styles.fieldLabel} htmlFor="microphone-device">麦克风设备</label>
        <select
          id="microphone-device"
          style={styles.select}
          value={selectedMicrophoneID}
          onChange={handleMicrophoneChange}
          disabled={microphones.length === 0}
        >
          {microphones.length === 0 ? (
            <option value="">未检测到可用麦克风</option>
          ) : microphones.map((device) => (
            <option key={device.deviceId} value={device.deviceId}>{device.label}</option>
          ))}
        </select>

        <div style={styles.meterRow}>
          <span style={styles.fieldLabel}>输入电平</span>
          <div
            aria-label="当前输入电平"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={inputLevel}
            role="meter"
            style={styles.meter}
          >
            {levelBars.map((active, index) => (
              <span key={index} style={{ ...styles.meterBar, ...(active ? styles.meterBarActive : undefined) }} />
            ))}
          </div>
          <strong style={styles.levelText}>{inputLevel}</strong>
        </div>

        {microphoneMessage && <p aria-live="polite" style={styles.statusText}>{microphoneMessage}</p>}
      </section>

      <section style={styles.card} aria-labelledby="output-title">
        <h2 id="output-title" style={styles.sectionTitle}>输出设备</h2>
        <label style={styles.fieldLabel} htmlFor="output-device">输出设备</label>
        <select
          id="output-device"
          style={styles.select}
          value={selectedOutputID}
          onChange={handleOutputChange}
          disabled={outputs.length === 0}
        >
          {outputs.length === 0 ? (
            <option value="">未检测到输出设备</option>
          ) : outputs.map((device) => (
            <option key={device.deviceId} value={device.deviceId}>{device.label}</option>
          ))}
        </select>
        <div style={styles.actionsCompact}>
          <button
            type="button"
            style={outputs.length === 0 ? styles.disabledButton : styles.secondaryButton}
            onClick={() => void applySelectedOutputDevice()}
            disabled={outputs.length === 0}
          >
            验证输出设备切换
          </button>
        </div>
        <p aria-live="polite" style={styles.statusText}>{outputMessage || '仅验证 setSinkId 支持性，不播放输出测试音。'}</p>
        <audio ref={outputAudioRef} aria-hidden="true" />
      </section>

      <section style={styles.card} aria-labelledby="tray-title">
        <h2 id="tray-title" style={styles.sectionTitle}>系统托盘手动验证</h2>
        <p style={styles.statusText}>运行时间：{runtimeSeconds} 秒</p>
        <ol style={styles.list}>
          <li>点击窗口 X，应隐藏到系统托盘而不是退出。</li>
          <li>从托盘菜单点击“显示主窗口”，本页面运行时间和媒体状态应继续保留。</li>
          <li>从托盘菜单点击“退出 echo”，应用应真正退出。</li>
        </ol>
      </section>

      <p aria-live="polite" style={styles.footerStatus}>{deviceMessage}</p>
    </main>
  )
}

function selectAvailableDeviceID(current: string, devices: EchoAudioDevice[]) {
  if (devices.some((device) => device.deviceId === current)) return current
  return devices[0]?.deviceId ?? ''
}

function stopMediaStream(stream: MediaStream) {
  stream.getTracks().forEach((track) => track.stop())
}

function formatOptionalError(error: unknown): string {
  if (error instanceof Error && error.message) return `（${error.message}）`
  return ''
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
  actions: {
    display: 'flex',
    gap: 12,
    marginBottom: 16,
  },
  actionsCompact: {
    display: 'flex',
    gap: 12,
    marginTop: 12,
  },
  primaryButton: {
    minHeight: 40,
    border: 0,
    borderRadius: 8,
    padding: '0 16px',
    background: '#0B63F6',
    color: '#FFFFFF',
    fontWeight: 600,
    cursor: 'pointer',
  },
  secondaryButton: {
    minHeight: 40,
    border: '1px solid #D7DEE5',
    borderRadius: 8,
    padding: '0 16px',
    background: '#FFFFFF',
    color: '#1F2933',
    fontWeight: 600,
    cursor: 'pointer',
  },
  disabledButton: {
    minHeight: 40,
    border: '1px solid #D7DEE5',
    borderRadius: 8,
    padding: '0 16px',
    background: '#EEF2F5',
    color: '#8B97A4',
    fontWeight: 600,
    cursor: 'not-allowed',
  },
  fieldLabel: {
    display: 'block',
    marginBottom: 8,
    color: '#647282',
    fontSize: 12,
    fontWeight: 600,
  },
  select: {
    width: '100%',
    minHeight: 44,
    boxSizing: 'border-box',
    border: '1px solid #D7DEE5',
    borderRadius: 8,
    padding: '0 12px',
    background: '#FFFFFF',
    color: '#1F2933',
  },
  meterRow: {
    display: 'grid',
    gridTemplateColumns: '80px 1fr 48px',
    alignItems: 'center',
    gap: 12,
    marginTop: 16,
  },
  meter: {
    display: 'grid',
    gridTemplateColumns: 'repeat(20, 1fr)',
    gap: 3,
    height: 28,
    alignItems: 'end',
  },
  meterBar: {
    display: 'block',
    height: 18,
    borderRadius: 2,
    background: '#D7DEE5',
  },
  meterBarActive: {
    background: '#0B63F6',
  },
  levelText: {
    fontVariantNumeric: 'tabular-nums',
  },
  statusText: {
    margin: '12px 0 0',
    color: '#647282',
    lineHeight: '22px',
  },
  list: {
    margin: 0,
    paddingLeft: 20,
    color: '#647282',
    lineHeight: '24px',
  },
  footerStatus: {
    maxWidth: 760,
    margin: '0 auto',
    color: '#647282',
    fontSize: 12,
  },
} satisfies Record<string, CSSProperties>
