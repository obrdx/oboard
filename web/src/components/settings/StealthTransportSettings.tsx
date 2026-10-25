import { useEffect, useId, useState } from 'react'
import { FormField } from '../ui/form-field'
import { SettingsGroup, SettingsSwitchRow } from './SettingsLayout'

export type StealthTransportConfig = {
  enabled: boolean
  listen_address: string
  public_address: string
}
type Status = Partial<StealthTransportConfig> & { active?: boolean; error?: string; source?: string }

export function StealthTransportSettings({ value, onSave, notify, defaultOpen = false }: {
  value?: Status
  onSave: (config: StealthTransportConfig) => Promise<void>
  notify: (message: string, tone?: 'success' | 'error') => void
  defaultOpen?: boolean
}) {
  const enabled = Boolean(value?.enabled)
  const listen = value?.listen_address || '0.0.0.0:24443'
  const address = value?.public_address || ''
  const [draft, setDraft] = useState<StealthTransportConfig>({ enabled, listen_address: listen, public_address: address })
  const [saving, setSaving] = useState(false)
  const id = useId()
  useEffect(() => {
    setDraft({ enabled, listen_address: listen, public_address: address })
  }, [enabled, listen, address])
  const update = (changes: Partial<StealthTransportConfig>) => {
    setDraft(current => ({ ...current, ...changes }))
  }
  const save = async (event: React.FormEvent) => {
    event.preventDefault()
    if (saving) return
    setSaving(true)
    try {
      await onSave({ ...draft, listen_address: draft.listen_address.trim(), public_address: draft.public_address.trim() })
      notify('AgentLink 传输设置已保存并生效', 'success')
    } catch (error: any) {
      notify(error?.message || String(error), 'error')
    } finally { setSaving(false) }
  }
  return <SettingsGroup collapsible defaultOpen={defaultOpen} className="signal-stealth-settings" title="AgentLink 传输" description="所有模式的 Agent 均使用独立 TCP 端口和证书指纹连接主控。旧 Agent 更新后自动切换；已有 Agent 使用 AgentLink 时禁止改址或关闭端口。"
    actions={<span className={`status-pill ${value?.active ? 'ok' : value?.enabled ? 'warning' : ''}`}>{value?.active ? '监听中' : value?.enabled ? '未生效' : '未启用'}</span>}>
    <form className="form settings-form single-field" onSubmit={save} aria-busy={saving}>
      <SettingsSwitchRow label="启用AgentLink 传输" description="所有模式共用此传输；更新 Agent 前须启用。" checked={draft.enabled} ariaLabel="启用AgentLink 传输" onChange={enabled => update({ enabled })} disabled={saving} />
      <FormField label="监听地址" hint="主控本机 IP:端口。0.0.0.0 监听全部 IPv4 网卡；[::] 监听 IPv6。端口范围 1–65535。" required>
        <input aria-label="AgentLink 监听地址" aria-describedby={value?.error ? `${id}-error` : undefined}
          value={draft.listen_address} onChange={event => update({ listen_address: event.target.value })} disabled={saving} required placeholder="0.0.0.0:24443" />
      </FormField>
      <FormField label="专用地址" hint="Agent 可访问的域名或 IP:端口，不含 https:// 或面板路径。IPv6 使用 [地址]:端口。请放行对应 TCP 端口；NAT 环境需配置端口转发。专用地址必须填写；NAT 转发需保持此地址可达。" required>
        <input aria-label="AgentLink 专用地址" aria-describedby={value?.error ? `${id}-error` : undefined}
          value={draft.public_address} onChange={event => update({ public_address: event.target.value })} disabled={saving} required placeholder="agent.example.com:24443" />
      </FormField>
      {value?.source === 'environment' && value.enabled && <p className="muted">当前配置来自主控环境变量；保存后以面板配置为准。</p>}
      {value?.error && <p id={`${id}-error`} className="settings-feedback" role="alert">{value.error}</p>}
      <div className="settings-actions"><button type="submit" disabled={saving}>{saving ? '保存中...' : '保存安全传输设置'}</button></div>
    </form>
  </SettingsGroup>
}
