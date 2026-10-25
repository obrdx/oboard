import React, { useEffect, useMemo, useState } from 'react'
import { AnimatePresence, m, useReducedMotion } from 'motion/react'
import { AlertTriangle, Check, ChevronRight, CirclePlay, Minus, X } from 'lucide-react'
import { MotionDialogPanel } from '../../components/ui/motion'
import { Select } from '../../components/ui/select'
import { SettingsRow, SettingsSwitchRow } from '../../components/settings/SettingsLayout'
import { bbrQdiscHints, bbrQdiscOptions, parseBBRQdisc, type BBRQdisc } from '../../components/server/bbr-options'
import { itemsByLevel, onboardingContent, onboardingTours, firstOpenRequired, type OnboardingTarget } from './content'
import type { GuideSection } from './OnboardingProvider'
import type { OnboardingAction, OnboardingClient, OnboardingItem, OnboardingItemKey, OnboardingView } from './types'

type GuideProps = {
  view: OnboardingView
  client: OnboardingClient
  initialSection: GuideSection
  initialItem?: OnboardingItemKey
  act: (action: OnboardingAction, item?: OnboardingItemKey) => Promise<OnboardingView>
  refresh: () => Promise<OnboardingView | undefined>
  go: (target: OnboardingTarget) => void
  openDemo: () => void
  onClose: () => void
}

const sections: Array<{ key: GuideSection; label: string }> = [
  { key: 'required', label: '必要配置' },
  { key: 'recommended', label: '推荐配置' },
  { key: 'tutorials', label: '使用教程' },
]

const statusLabel: Record<OnboardingItem['status'], string> = { done: '已完成', pending: '待完成', skipped: '已跳过' }

export function SetupGuide({ view, client, initialSection, initialItem, act, refresh, go, openDemo, onClose }: GuideProps) {
  const [section, setSection] = useState<GuideSection>(initialSection)
  const required = itemsByLevel(view, 'required')
  const recommended = itemsByLevel(view, 'recommended')
  const [active, setActive] = useState<OnboardingItemKey>(() => initialItem || firstOpenRequired(view) || required[0]?.key || 'password')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const completed = view.state.status === 'completed'

  const run = async (work: () => Promise<unknown>) => {
    if (busy) return
    setBusy(true)
    setError('')
    try {
      await work()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy(false)
    }
  }

  const advance = (next: OnboardingView) => {
    const open = firstOpenRequired(next)
    if (open) setActive(open)
  }

  return <MotionDialogPanel onCancel={onClose} className="onboarding-guide" ariaLabel="初始化与教程">
    <header className="dialog-head onboarding-guide-head">
      <div>
        <h2>初始化与教程</h2>
        <p className="muted">{completed ? '初始化配置已完成。推荐配置与使用教程可随时查看。' : `必要配置已完成 ${view.required_done} / ${view.required_total} 项，进度自动保存。`}</p>
      </div>
      <button type="button" className="ghost dialog-close icon-button" onClick={onClose} aria-label="关闭" title="关闭"><X size={16} /></button>
    </header>
    <div className="onboarding-tabs" role="tablist" aria-label="初始化内容">
      {sections.map(item => <button key={item.key} type="button" role="tab" aria-selected={section === item.key} className={section === item.key ? 'is-active' : ''} onClick={() => setSection(item.key)}>
        {item.label}
        {item.key === 'required' && <small>{view.required_done}/{view.required_total}</small>}
      </button>)}
    </div>
    <div className="onboarding-guide-body">
      {section === 'required' && <div className="onboarding-required">
        <ol className="onboarding-steps" aria-label="必要配置">
          {required.map((item, index) => <li key={item.key}>
            <button type="button" className={`onboarding-step is-${item.status}${active === item.key ? ' is-active' : ''}`} aria-current={active === item.key ? 'step' : undefined} onClick={() => { setActive(item.key); setError('') }}>
              <span className="onboarding-step-index">{item.status === 'done' ? <Check size={12} /> : item.status === 'skipped' ? <Minus size={12} /> : index + 1}</span>
              <span className="onboarding-step-copy"><strong>{onboardingContent[item.key].title}</strong><small>{statusLabel[item.status]}</small></span>
            </button>
          </li>)}
        </ol>
        <AnimatePresence mode="wait" initial={false}>
          <StepDetail
            key={active}
            item={required.find(item => item.key === active) || required[0]}
            view={view}
            client={client}
            busy={busy}
            error={error}
            run={run}
            act={act}
            refresh={refresh}
            go={go}
            openDemo={openDemo}
            onAdvance={advance}
          />
        </AnimatePresence>
      </div>}
      {section === 'recommended' && <ul className="onboarding-checklist" aria-label="推荐配置">
        {recommended.map(item => {
          const content = onboardingContent[item.key]
          return <li key={item.key} className={`is-${item.status}`}>
            <span className="onboarding-check-mark" aria-hidden="true">{item.status === 'done' ? <Check size={13} /> : item.status === 'skipped' ? <Minus size={13} /> : null}</span>
            <span className="onboarding-check-copy"><strong>{content.title}</strong><small>{content.description}</small></span>
            <span className="onboarding-check-actions">
              <span className={`onboarding-status is-${item.status}`}>{statusLabel[item.status]}</span>
              {content.target && <button type="button" className="ghost" onClick={() => go(content.target!)}>{content.targetLabel}<ChevronRight size={14} /></button>}
              {item.status === 'pending' && content.confirmLabel && <button type="button" className="ghost" disabled={busy} onClick={() => void run(() => act('confirm', item.key))}>{content.confirmLabel}</button>}
              {item.status === 'pending' && <button type="button" className="ghost" disabled={busy} onClick={() => void run(() => act('skip', item.key))}>暂不配置</button>}
              {item.status === 'skipped' && <button type="button" className="ghost" disabled={busy} onClick={() => void run(() => act('unskip', item.key))}>撤销跳过</button>}
            </span>
          </li>
        })}
      </ul>}
      {section === 'tutorials' && <div className="onboarding-tutorials">
        {onboardingTours.map(tour => {
          const status = view.tours[tour.key]
          return <article key={tour.key} className="onboarding-tutorial">
            <span className="onboarding-tutorial-icon"><CirclePlay size={18} /></span>
            <span className="onboarding-tutorial-copy"><strong>{tour.title}</strong><small>{tour.summary}</small><small>{tour.duration} · {status === 'completed' ? '已观看' : status === 'started' ? '未看完' : '未观看'}</small></span>
            <button type="button" onClick={openDemo}>{status === 'completed' ? '重新观看' : '开始观看'}</button>
          </article>
        })}
      </div>}
    </div>
    <footer className="dialog-actions onboarding-guide-foot">
      {error && section !== 'required' && <span className="onboarding-error" role="alert">{error}</span>}
      <span className="muted">{completed ? `初始化完成于 ${formatDate(view.state.completed_at)}` : view.can_complete ? '必要配置均已处理，可以完成初始化。' : '完成或跳过全部必要配置后，即可完成初始化。'}</span>
      {!completed && <button type="button" disabled={!view.can_complete || busy} onClick={() => void run(async () => { await act('complete') })}>完成初始化</button>}
    </footer>
  </MotionDialogPanel>
}

function formatDate(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '' : date.toLocaleString('zh-CN', { hour12: false })
}

type DetailProps = {
  item: OnboardingItem
  view: OnboardingView
  client: OnboardingClient
  busy: boolean
  error: string
  run: (work: () => Promise<unknown>) => Promise<void>
  act: GuideProps['act']
  refresh: GuideProps['refresh']
  go: GuideProps['go']
  openDemo: () => void
  onAdvance: (next: OnboardingView) => void
}

function StepDetail(props: DetailProps) {
  const { item, view, busy, error, run, act, go, openDemo, onAdvance } = props
  const reduced = useReducedMotion()
  const content = onboardingContent[item.key]
  const skippable = !['password', 'controller_url'].includes(item.key)
  const controllerURLReady = view.items.find(entry => entry.key === 'controller_url')?.status === 'done'
  return <m.section
    className="onboarding-detail"
    aria-labelledby={`onboarding-${item.key}`}
    initial={reduced ? false : { opacity: 0, x: 8 }}
    animate={{ opacity: 1, x: 0 }}
    exit={reduced ? undefined : { opacity: 0, x: -6, transition: { duration: 0.1 } }}
    transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
  >
    <div className="onboarding-detail-head">
      <h3 id={`onboarding-${item.key}`}>{content.title}</h3>
      <span className={`onboarding-status is-${item.status}`}>{statusLabel[item.status]}</span>
    </div>
    <p className="onboarding-detail-text">{content.description}</p>

    {item.key === 'password' && <PasswordStep {...props} />}
    {item.key === 'controller_url' && <ControllerURLStep {...props} />}
    {item.key === 'node_defaults' && <NodeDefaultsStep {...props} />}
    {item.key === 'server' && <>
      {!controllerURLReady && <p className="onboarding-warning"><AlertTriangle size={15} />请先完成「设置面板公开地址」，否则 Agent 无法连接面板。</p>}
      <p className="onboarding-fact">已添加 {item.detail?.servers ?? 0} 台服务器，其中 {item.detail?.online ?? 0} 台在线。</p>
    </>}
    {item.key === 'entry' && <p className="onboarding-fact">已启用 {item.detail?.inbounds ?? 0} 个入口。</p>}
    {item.key === 'access' && <p className="onboarding-fact">包含节点的套餐 {item.detail?.plans ?? 0} 个，已分配的用户套餐 {item.detail?.bindings ?? 0} 个。</p>}

    {error && <p className="onboarding-error" role="alert">{error}</p>}

    <div className="onboarding-detail-actions">
      {content.target && !['password', 'node_defaults', 'controller_url'].includes(item.key) && <button type="button" onClick={() => go(content.target!)}>{content.targetLabel}<ChevronRight size={14} /></button>}
      {item.key === 'access' && <button type="button" className="ghost" onClick={() => go({ tab: 'users' })}>前往用户与分组<ChevronRight size={14} /></button>}
      {item.key === 'entry' && <button type="button" className="ghost" onClick={openDemo}><CirclePlay size={14} />观看代理链路演示</button>}
      {item.key === 'subscription' && item.status !== 'done' && <button type="button" className="ghost" disabled={busy} onClick={() => void run(async () => onAdvance(await act('confirm', item.key)))}>{content.confirmLabel}</button>}
      {skippable && item.status === 'pending' && <button type="button" className="ghost onboarding-skip" disabled={busy} onClick={() => void run(async () => onAdvance(await act('skip', item.key)))}>跳过此项</button>}
      {item.status === 'skipped' && <button type="button" className="ghost onboarding-skip" disabled={busy} onClick={() => void run(() => act('unskip', item.key))}>撤销跳过</button>}
    </div>
  </m.section>
}

function PasswordStep({ item, view, client, busy, run }: DetailProps) {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [changed, setChanged] = useState(false)
  if (changed) return <div className="onboarding-notice" role="status">
    <strong>密码已更新</strong>
    <span>当前会话已失效，请使用新密码重新登录。初始化进度已保存，登录后将继续。</span>
    <button type="button" onClick={() => window.location.reload()}>重新登录</button>
  </div>
  if (item.status === 'done') return <p className="onboarding-fact">{view.bootstrap_admin ? '初始密码已更换。' : '当前账号不是首个管理员账号，无需更换初始密码。'}</p>
  const mismatch = confirm !== '' && confirm !== next
  const tooShort = next !== '' && next.length < 8
  const ready = current !== '' && next.length >= 8 && confirm === next
  return <form className="onboarding-form" onSubmit={event => { event.preventDefault(); if (ready) void run(async () => { await client.request('/auth/password', { method: 'POST', body: JSON.stringify({ current_password: current, new_password: next }) }); setChanged(true) }) }}>
    <label><span>当前密码</span><input type="password" autoComplete="current-password" value={current} onChange={event => setCurrent(event.target.value)} /></label>
    <label><span>新密码</span><input type="password" autoComplete="new-password" value={next} onChange={event => setNext(event.target.value)} aria-invalid={tooShort} /></label>
    <label><span>确认新密码</span><input type="password" autoComplete="new-password" value={confirm} onChange={event => setConfirm(event.target.value)} aria-invalid={mismatch} /></label>
    {(tooShort || mismatch) && <small className="onboarding-error">{tooShort ? '新密码至少需要 8 个字符。' : '两次输入的新密码不一致。'}</small>}
    <div><button type="submit" disabled={!ready || busy}>{busy ? '正在保存…' : '更新密码'}</button></div>
  </form>
}

function suggestedControllerURL() {
  try {
    return new URL('./', window.location.href).href.replace(/\/$/, '')
  } catch {
    return window.location.origin
  }
}

function isLocalHost(value: string) {
  try {
    const host = new URL(value).hostname
    return host === 'localhost' || host === '127.0.0.1' || host === '[::1]' || host.endsWith('.localhost')
  } catch {
    return false
  }
}

function ControllerURLStep({ item, client, busy, run, refresh, onAdvance }: DetailProps) {
  const saved = String(item.detail?.url || '')
  const [value, setValue] = useState(saved || suggestedControllerURL())
  const trimmed = value.trim()
  const insecure = trimmed.toLowerCase().startsWith('http://') && !isLocalHost(trimmed)
  return <form className="onboarding-form" onSubmit={event => { event.preventDefault(); void run(async () => {
    await client.request('/settings', { method: 'POST', body: JSON.stringify({ controller_url: trimmed }) })
    const next = await refresh()
    if (next) onAdvance(next)
  }) }}>
    <label><span>面板公开地址</span><input type="url" inputMode="url" value={value} onChange={event => setValue(event.target.value)} placeholder="https://panel.example.com" spellCheck={false} /></label>
    {insecure && <p className="onboarding-warning"><AlertTriangle size={15} />正式版本仅接受 HTTPS 地址。请先通过反向代理为面板启用 HTTPS，再填写对应的 https:// 地址。</p>}
    {saved && <small className="muted">当前已保存：{saved}</small>}
    <div><button type="submit" disabled={!trimmed || busy || trimmed === saved}>{busy ? '正在保存…' : '保存地址'}</button></div>
  </form>
}

function NodeDefaultsStep({ item, client, busy, run, act, onAdvance }: DetailProps) {
  const [settings, setSettings] = useState<Record<string, any> | null>(null)
  const [qdisc, setQdisc] = useState<BBRQdisc>('fq')
  const [tcpTuning, setTCPTuning] = useState(false)
  const [timeMode, setTimeMode] = useState('auto')
  useEffect(() => {
    let active = true
    void client.request('/settings').then((result: any) => {
      if (!active) return
      const values = result?.settings || {}
      setSettings(values)
      setQdisc(parseBBRQdisc(values.server_default_bbr_qdisc))
      setTCPTuning(String(values.server_default_tcp_tuning_enabled ?? 'false') === 'true')
      setTimeMode(String(values.server_default_time_correction_mode || 'auto'))
    }).catch(() => setSettings({}))
    return () => { active = false }
  }, [client])
  const changed = useMemo(() => settings !== null && (
    parseBBRQdisc(settings.server_default_bbr_qdisc) !== qdisc ||
    (String(settings.server_default_tcp_tuning_enabled ?? 'false') === 'true') !== tcpTuning ||
    String(settings.server_default_time_correction_mode || 'auto') !== timeMode
  ), [settings, qdisc, tcpTuning, timeMode])
  if (!settings) return <p className="onboarding-fact">正在读取当前设置…</p>
  return <div className="onboarding-form">
    <SettingsRow label="拥塞控制" description={bbrQdiscHints[qdisc]}>
      <Select aria-label="拥塞控制" value={qdisc} onChange={event => setQdisc(parseBBRQdisc(event.target.value))}>
        {bbrQdiscOptions.map(option => <option key={option.value} value={option.value}>{option.value === 'fq' ? `${option.label}（默认）` : option.label}</option>)}
      </Select>
    </SettingsRow>
    <SettingsSwitchRow label="TCP 调优" description="写入固定的 TCP/UDP 缓冲区与转发参数，当前系统不支持的项自动跳过。" checked={tcpTuning} onChange={setTCPTuning} ariaLabel="TCP 调优" />
    <SettingsRow label="时间校准" description="服务器校准时间的方式。">
      <Select aria-label="时间校准" variant="segmented" value={timeMode} onChange={event => setTimeMode(event.target.value)}>
        <option value="off">关闭</option><option value="auto">自动</option><option value="ntp">逻辑校时</option>
      </Select>
    </SettingsRow>
    {item.status !== 'done' && <div><button type="button" disabled={busy} onClick={() => void run(async () => {
      if (changed) await client.request('/settings', { method: 'POST', body: JSON.stringify({ server_default_bbr_qdisc: qdisc, server_default_tcp_tuning_enabled: tcpTuning, server_default_time_correction_mode: timeMode }) })
      onAdvance(await act('confirm', 'node_defaults'))
    })}>{busy ? '正在保存…' : changed ? '保存并确认' : '确认使用当前值'}</button></div>}
    {item.status === 'done' && changed && <div><button type="button" disabled={busy} onClick={() => void run(async () => {
      await client.request('/settings', { method: 'POST', body: JSON.stringify({ server_default_bbr_qdisc: qdisc, server_default_tcp_tuning_enabled: tcpTuning, server_default_time_correction_mode: timeMode }) })
      setSettings({ ...settings, server_default_bbr_qdisc: qdisc, server_default_tcp_tuning_enabled: String(tcpTuning), server_default_time_correction_mode: timeMode })
    })}>{busy ? '正在保存…' : '保存修改'}</button></div>}
  </div>
}
