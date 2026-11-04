import { useMemo, useState } from 'react'
import { ChevronDown, Loader2, Search } from 'lucide-react'
import { FormField } from '../ui/form-field'
import { Select } from '../ui/select'
import { Switch } from '../ui/switch'
import type { LatencyProbePreset, LatencyProbeTask, NetworkProbeMethod, Server } from '../proxy-path/types'

const INTERVAL_PRESETS = [60, 300, 900, 1800, 3600]
const CARRIER_PRIORITY = ['中国电信', '中国联通', '中国移动', '中国广电', '教育网']

export const targetLabel = (province: string, carrier: string) => {
  const left = province.trim()
  const right = carrier.trim()
  if (!left) return right
  if (!right) return left
  return `${left} · ${right}`
}

const familyLabel = (preset: LatencyProbePreset) => {
  const v4 = preset.targets.filter(target => target.family === 'ipv4').length
  const v6 = preset.targets.filter(target => target.family === 'ipv6').length
  return [v4 ? `IPv4${v4 > 1 ? ` ×${v4}` : ''}` : '', v6 ? `IPv6${v6 > 1 ? ` ×${v6}` : ''}` : ''].filter(Boolean).join(' · ')
}

export function ReturnLatencyTaskForm({ task, presets = [], servers, loading, error, disabled, onSubmit, onCancel }: {
  task?: LatencyProbeTask | null
  presets?: LatencyProbePreset[]
  servers: Server[]
  loading?: boolean
  error?: string
  disabled?: boolean
  onSubmit: (payload: Omit<LatencyProbeTask, 'id' | 'created_at' | 'updated_at'>) => void | Promise<void>
  onCancel: () => void
}) {
  const [method, setMethod] = useState<NetworkProbeMethod>(task?.method || 'tcp')
  const [address, setAddress] = useState(task?.address || '')
  const [port, setPort] = useState<number | ''>(task?.port || 80)
  const [presetOpen, setPresetOpen] = useState(Boolean(task && !task.address))
  const [targetQuery, setTargetQuery] = useState('')
  const [presetID, setPresetID] = useState(task?.address ? '' : task?.preset_id || '')
  const [province, setProvince] = useState('')
  const [carrier, setCarrier] = useState('')
  const [name, setName] = useState(task?.name || '')
  const [nameTouched, setNameTouched] = useState(Boolean(task?.name))
  const [interval, setInterval] = useState<number | ''>(task?.interval_seconds || 300)
  const [enabled, setEnabled] = useState(task?.enabled !== false)
  const [serverIDs, setServerIDs] = useState<number[]>(task?.server_ids || [])
  const [serverQuery, setServerQuery] = useState('')
  const [saving, setSaving] = useState(false)
  const [submitError, setSubmitError] = useState('')

  const provinces = useMemo(() => Array.from(new Set(presets.map(preset => preset.province))).sort((a, b) => a.localeCompare(b, 'zh-CN')), [presets])
  const carriers = useMemo(() => {
    const pool = province ? presets.filter(preset => preset.province === province) : presets
    return Array.from(new Set(pool.map(region => region.carrier))).sort((a, b) => {
      const ia = CARRIER_PRIORITY.indexOf(a)
      const ib = CARRIER_PRIORITY.indexOf(b)
      if (ia !== -1 && ib !== -1) return ia - ib
      if (ia !== -1) return -1
      if (ib !== -1) return 1
      return a.localeCompare(b, 'zh-CN')
    })
  }, [presets, province])

  const selectedPreset = presets.find(preset => preset.id === presetID)
  // A stored task keeps its names even when the resource is not loaded yet.
  const presetLabel = selectedPreset ? targetLabel(selectedPreset.province, selectedPreset.carrier) : presetID && task?.preset_id === presetID ? targetLabel(task.province, task.carrier) : ''
  const presetMode = Boolean(presetID && !address.trim() && method !== 'http')
  const presetMissing = presetMode && !loading && !error && !selectedPreset
  const effectiveName = nameTouched && name.trim() ? name.trim() : Array.from(address.trim() || (presetMode ? presetLabel || presetID : '')).slice(0, 60).join('')
  const online = (server: Server) => Boolean(server.agent_id) && server.status === 'online'
  const suggested = useMemo(() => servers.filter(server => online(server) && server.latency_probe_enabled), [servers])
  const visibleServers = useMemo(() => {
    const q = serverQuery.trim().toLowerCase()
    if (!q) return servers
    return servers.filter(server => `${server.name} ${server.id} ${server.public_ipv4 || ''} ${server.public_ipv6 || ''}`.toLowerCase().includes(q))
  }, [servers, serverQuery])

  const toggleServer = (id: number) => setServerIDs(current => current.includes(id) ? current.filter(item => item !== id) : [...current, id])
  const intervalInvalid = interval === '' || !Number.isInteger(Number(interval)) || Number(interval) < 30 || Number(interval) > 86400
  const addressError = (() => {
    const value = address.trim()
    if (!value) return presetMissing ? '该运营商预设已下线，请重新选择或填写地址。' : presetMode ? '' : method === 'http' ? '请填写完整 URL。' : '请填写目标地址或从预设中选择。'
    if (method === 'http') {
      try { const url = new URL(value); if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.hash) return '请输入不含凭据或片段的 HTTP / HTTPS URL。' } catch { return '请输入完整 URL，例如 https://example.com/health。' }
    } else if (/[\s/?#]/.test(value) || (value.includes(':') && !/^[0-9a-fA-F:.]+(?:%[a-zA-Z0-9_.-]+)?$/.test(value))) return '请只填写 IP 地址或主机名，端口单独设置。'
    return ''
  })()
  const portInvalid = method === 'tcp' && (!Number.isInteger(port) || Number(port) < 1 || Number(port) > 65535)
  const canSubmit = !addressError && !portInvalid && !intervalInvalid && !saving && !disabled
  const visiblePresets = presets.filter(preset => (!province || preset.province === province) && (!carrier || preset.carrier === carrier) && `${preset.province} ${preset.carrier} ${preset.id} ${preset.targets.map(target => target.host).join(' ')}`.toLowerCase().includes(targetQuery.trim().toLowerCase()))
  // Choosing a preset binds its id: the task probes every target the preset
  // publishes and follows later address updates. HTTP needs a concrete URL,
  // so there it copies the first address instead.
  const selectPreset = (preset: LatencyProbePreset) => {
    if (method === 'http') {
      const host = preset.targets[0]?.host
      if (host) setAddress(`https://${host}`)
      setPresetID('')
    } else {
      setPresetID(preset.id)
      setAddress('')
    }
    if (!nameTouched) setName(targetLabel(preset.province, preset.carrier))
    setNameTouched(true)
    setPresetOpen(false)
  }

  const submit = async () => {
    if (!canSubmit) return
    setSaving(true)
    setSubmitError('')
    try {
      await onSubmit({ name: effectiveName, method, address: address.trim(), port: method === 'tcp' ? Number(port) : 0, preset_id: presetMode ? presetID : '', province: '', carrier: '', interval_seconds: Number(interval), enabled, server_ids: serverIDs })
    } catch (err: any) {
      setSubmitError(err?.message || '保存失败，请重试')
    } finally {
      setSaving(false)
    }
  }

  return (
    <form className={`probe-task-form${presetOpen ? ' is-preset-open' : ''}`} onSubmit={event => { event.preventDefault(); void submit() }}>
      <fieldset disabled={saving || disabled} className="return-latency-fields">
        <div className="probe-task-form-scroll">
          <div className="probe-task-layout">
            <div className="probe-task-main">
              <div className="probe-task-primary">
                <div className="probe-task-form-grid">
                  <FormField label="探测方式" required full>
                    <Select variant="segmented" className="full-width" aria-label="探测方式" value={method} onChange={event => { setMethod(event.target.value as NetworkProbeMethod) }}>
                      <option value="tcp">TCP</option><option value="icmp">Ping</option><option value="http">HTTP</option>
                    </Select>
                    <p className="muted probe-method-hint">{method === 'tcp' ? '检测指定端口能否建立连接。' : method === 'icmp' ? '通过 ICMP 检测目标的可达性与延迟。' : '发送 GET 请求，跟随最多 3 次跳转，以 2xx 响应为成功。'}</p>
                  </FormField>
                  <FormField label={method === 'http' ? '目标 URL' : '目标地址'} required full={method !== 'tcp'}>
                    <input aria-label="目标地址" required={!presetMode} aria-describedby="probe-address-hint" aria-invalid={Boolean(address && addressError)} type="text" maxLength={2048} placeholder={method === 'http' ? 'https://example.com/health' : '例如 example.com 或 1.1.1.1'} value={address} onChange={event => setAddress(event.target.value)} />
                    <small id="probe-address-hint" className={(address || presetMissing) && addressError ? 'danger-text' : 'muted'}>{addressError || (presetMode ? `已绑定预设：${presetLabel || presetID}（${selectedPreset ? familyLabel(selectedPreset) : '全部目标'}），地址随预设更新。填写地址可改为固定目标。` : '由所选节点直接探测，支持 IPv4、IPv6 和内网目标。')}</small>
                  </FormField>
                  <div className="probe-preset-picker">
                    <button type="button" className="probe-preset-toggle" aria-expanded={presetOpen} aria-controls="probe-presets" onClick={() => setPresetOpen(!presetOpen)}>
                      {presetOpen ? '收起预设目标' : '从预设中选择'}<ChevronDown size={16} aria-hidden="true" />
                    </button>
                    {presetOpen && <section id="probe-presets" aria-label="预设目标" className="probe-preset-panel">
                      {(loading || presets.length > 0) && <>
                        <div className="probe-preset-filters">
                          <Select aria-label="探测目标省份" value={province} onChange={event => { setProvince(event.target.value); setCarrier('') }}><option value="">全部省份</option>{provinces.map(item => <option key={item} value={item}>{item}</option>)}</Select>
                          <Select aria-label="探测目标运营商" value={carrier} onChange={event => setCarrier(event.target.value)}><option value="">全部运营商</option>{carriers.map(item => <option key={item} value={item}>{item}</option>)}</Select>
                        </div>
                        <div className="return-latency-search">
                          <Search size={15} aria-hidden="true" />
                          <input type="search" aria-label="搜索预设目标" placeholder="搜索省份、运营商或地址" value={targetQuery} onChange={event => setTargetQuery(event.target.value)} />
                        </div>
                      </>}
                      <div className="probe-preset-results">
                        {loading ? <p className="muted" role="status">正在加载预设…</p> : !visiblePresets.length ? <p className="muted">{error ? '预设暂不可用，仍可手动填写地址。' : '没有匹配的预设目标。'}</p> : visiblePresets.map(preset => <button type="button" className="probe-preset-option" key={preset.id} aria-pressed={presetMode && preset.id === presetID} title={preset.targets.map(target => target.host).join('\n')} onClick={() => selectPreset(preset)}><strong>{targetLabel(preset.province, preset.carrier)}</strong><span>{familyLabel(preset)}</span></button>)}
                      </div>
                    </section>}
                  </div>
                  {method === 'tcp' && <FormField label="端口" required>
                    <input aria-label="目标端口" required aria-invalid={portInvalid} aria-describedby="probe-port-hint" type="number" min={1} max={65535} value={port} onChange={event => setPort(event.target.value === '' ? '' : Number(event.target.value))} />
                    <small id="probe-port-hint" className={portInvalid ? 'danger-text' : 'muted'}>1–65535，例如 HTTPS 服务使用 443。</small>
                  </FormField>}
                </div>
              </div>
              <div className="probe-task-secondary">
                <div className="probe-task-form-grid">
                  <FormField label="任务名称" hint="留空自动使用目标地址。同名任务不可重复。">
                    <input aria-label="任务名称" type="text" maxLength={60} placeholder={address || (presetMode ? presetLabel : '') || '例如：网站可用性'} value={name} onChange={event => { setName(event.target.value); setNameTouched(true) }} />
                  </FormField>
                  <FormField label="探测间隔（秒）" required hint="该任务多久执行一次（30–86400 秒）。">
                    <div className="probe-task-interval">
                      <input aria-label="探测间隔（秒）" required aria-invalid={intervalInvalid} type="number" min={30} max={86400} value={interval} onChange={event => setInterval(event.target.value === '' ? '' : Number(event.target.value))} />
                      <div className="probe-task-interval-presets">
                        {INTERVAL_PRESETS.map(preset => <button key={preset} type="button" className={`latency-pill-btn${Number(interval) === preset ? ' active' : ''}`} onClick={() => setInterval(preset)}>{preset >= 3600 ? `${preset / 3600} 小时` : preset >= 60 ? `${preset / 60} 分钟` : `${preset} 秒`}</button>)}
                      </div>
                    </div>
                  </FormField>
                </div>
                <label className="return-latency-switch-row">
                  <span><strong>启用该任务</strong><small className="muted">停用后保留配置，但不再下发给服务器。</small></span>
                  <Switch checked={enabled} ariaLabel="启用该任务" onChange={setEnabled} />
                </label>
              </div>
            </div>
            <section className="probe-task-servers" aria-label="执行服务器">
              <header className="return-latency-section-head">
                <h3>执行服务器</h3>
                <span className="muted">已选 {serverIDs.length} 台</span>
              </header>
              <p className="muted">选择执行节点；未选择时任务不会执行。快捷选择应用于全部节点，不受搜索影响。</p>
              <div className="probe-task-server-tools">
                <div className="return-latency-search">
                  <Search size={15} aria-hidden="true" />
                  <input type="search" aria-label="搜索执行服务器" placeholder="搜索名称、IP 或编号" value={serverQuery} onChange={event => setServerQuery(event.target.value)} />
                </div>
                <div className="probe-task-server-quick">
                  <button type="button" className="latency-pill-btn" disabled={!servers.length} onClick={() => setServerIDs(servers.map(server => server.id))}>选择全部</button>
                  <button type="button" className="latency-pill-btn" disabled={!suggested.length} onClick={() => setServerIDs(suggested.map(server => server.id))}>选择在线 {suggested.length} 台</button>
                  <button type="button" className="latency-pill-btn" disabled={!serverIDs.length} onClick={() => setServerIDs([])}>清空</button>
                </div>
              </div>
              <div className="probe-task-server-list">
                {!servers.length ? <p className="muted">暂无服务器，请先在服务器管理中添加。</p> : !visibleServers.length ? <p className="muted">没有匹配的服务器。</p> : visibleServers.map(server => (
                  <label key={server.id} className={`probe-task-server${serverIDs.includes(server.id) ? ' is-selected' : ''}`}>
                    <input type="checkbox" aria-label={`选择 ${server.name}`} checked={serverIDs.includes(server.id)} onChange={() => toggleServer(server.id)} />
                    <span className="probe-task-server-main">
                      <strong>{server.name}</strong>
                      <small className="muted">{server.public_ipv4 || server.public_ipv6 || `#${server.id}`}</small>
                    </span>
                    <span className="probe-task-server-state muted">{online(server) ? (server.latency_probe_enabled ? '推荐' : '未启用探测') : server.agent_id ? '离线' : '未接入'}</span>
                  </label>
                ))}
              </div>
            </section>
          </div>
          {submitError && <p className="danger-text" role="alert">{submitError}</p>}
        </div>
        <div className="return-latency-form-actions">
          <button type="button" className="ghost" onClick={onCancel}>取消</button>
          <button type="submit" className="primary" disabled={!canSubmit}>
            {saving && <Loader2 size={15} className="spin" aria-hidden="true" />}{task ? '保存任务' : '创建任务'}
          </button>
        </div>
      </fieldset>
    </form>
  )
}

