import React, { useRef, useState } from 'react'
import { Check, X } from 'lucide-react'
import { MotionDialogPanel } from '../ui/motion'
import { ADDRESS_FAMILY_OPTIONS, type AddressFamily } from './TransportDialog'

const HINTS: Record<AddressFamily, string> = {
  auto: '跟随目标服务器的入口策略。',
  prefer_ipv4: '优先用 IPv4 连接，不可用时改用 IPv6。',
  prefer_ipv6: '优先用 IPv6 连接，不可用时改用 IPv4。',
  ipv4_only: '只用 IPv4 连接，目标没有 IPv4 时链路无效。',
  ipv6_only: '只用 IPv6 连接，目标没有 IPv6 时链路无效。',
}

export function NextHopFamilyDialog({
  nodeLabel,
  targetLabels,
  current,
  onCancel,
  onSubmit,
}: {
  nodeLabel: string
  targetLabels: string[]
  current: AddressFamily | 'mixed'
  onCancel: () => void
  onSubmit: (family: AddressFamily) => Promise<void> | void
}) {
  const [family, setFamily] = useState<AddressFamily | 'mixed'>(current)
  const [saving, setSaving] = useState(false)
  const savingRef = useRef(false)
  const [error, setError] = useState('')
  const submit = async () => {
    if (family === 'mixed' || savingRef.current) return
    savingRef.current = true
    setSaving(true)
    setError('')
    try {
      await onSubmit(family)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err || '保存失败，请重试。'))
    } finally {
      savingRef.current = false
      setSaving(false)
    }
  }
  return (
    <MotionDialogPanel onCancel={() => { if (!savingRef.current) onCancel() }} className="transport-dialog next-hop-family-dialog">
      <header className="dialog-head">
        <div><h2>后续连接地址族</h2><p className="muted">{nodeLabel} 连接后续节点时使用的 IP 版本</p></div>
        <button type="button" className="ghost dialog-close icon-button" onClick={onCancel} disabled={saving} aria-label="关闭" title="关闭"><X size={16} /></button>
      </header>
      <fieldset disabled={saving} className="dialog-body transport-dialog-body" style={{ border: 0, margin: 0, minWidth: 0 }}>
        <div className="transport-mode-options next-hop-family-options" role="radiogroup" aria-label="后续连接地址族">
          {ADDRESS_FAMILY_OPTIONS.map(option => {
            const active = family === option.value
            return <button key={option.value} type="button" role="radio" aria-checked={active} className={`transport-mode-option${active ? ' is-active' : ''}`} onClick={() => setFamily(option.value)}>
              <span className="transport-mode-head"><strong>{option.label}</strong></span>
              <span className="transport-mode-hint">{HINTS[option.value]}</span>
              <Check className="transport-mode-check" size={14} aria-hidden />
            </button>
          })}
        </div>
        {current === 'mixed' && <small className="muted">这些后续连接目前设置不一致，保存后会统一。</small>}
        <div className="transport-preview">
          <span>将应用到 {targetLabels.length} 段连接</span>
          <strong>{targetLabels.join('、')}</strong>
          <small className="muted">只改变这些连接使用的 IP 版本；之后从这个节点新连出的链路会沿用此设置。</small>
        </div>
      </fieldset>
      {error && <p role="alert" className="transport-field-error">保存失败：{error}</p>}
      <footer className="dialog-actions"><button type="button" className="ghost" onClick={onCancel} disabled={saving}>取消</button><button type="button" onClick={() => void submit()} disabled={saving || family === 'mixed'}>{saving ? '保存中...' : '确定'}</button></footer>
    </MotionDialogPanel>
  )
}
