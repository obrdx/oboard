import React, { useMemo, useState } from 'react'
import { Activity, Link as LinkIcon, Plus, Search, Settings as SettingsIcon, Trash2, X } from 'lucide-react'
import { MotionDialogPanel } from '../ui/motion'
import { FormField } from '../ui/form-field'
import { Select } from '../ui/select'
import { NumericInput } from '../ui/numeric-input'
import { RegionFlag } from '../ui/RegionFlag'
import type { ExternalOutbound, Server } from './types'

export type ExternalOutboundCheck = {
  external_outbound_id: number
  status: 'reachable' | 'unreachable' | 'failed'
  target: string
  exit_ip?: string
  region_code?: string
  region_status?: string
  connect_ms?: number
  error?: string
  checked_at: string
}

type RequestFn = (path: string, init?: RequestInit) => Promise<unknown>

export type ThirdPartyFormProtocol = 'socks' | 'http' | 'https' | 'shadowsocks'

export type ThirdPartyNodeDraft = {
  mode: 'form' | 'link'
  scope: 'global' | 'server'
  server_id: number
  expose_to_users: boolean
  content: string
  name: string
  protocol: ThirdPartyFormProtocol
  address: string
  port: number | undefined
  username: string
  password: string
  method: string
  server_name: string
  insecure: boolean
}

export const SHADOWSOCKS_METHODS = ['2022-blake3-aes-128-gcm', '2022-blake3-aes-256-gcm', '2022-blake3-chacha20-poly1305', 'aes-128-gcm', 'aes-256-gcm', 'chacha20-ietf-poly1305']

export function newThirdPartyNodeDraft(serverID: number): ThirdPartyNodeDraft {
  return { mode: 'form', scope: 'global', server_id: serverID, expose_to_users: false, content: '', name: '', protocol: 'socks', address: '', port: 1080, username: '', password: '', method: '2022-blake3-aes-128-gcm', server_name: '', insecure: false }
}

export function externalOutboundIsHTTPS(node: Pick<ExternalOutbound, 'protocol' | 'config_json'>) {
  if (node.protocol !== 'http') return false
  try {
    const cfg = JSON.parse(node.config_json || '{}')
    return Boolean(cfg?.tls?.enabled)
  } catch {
    return false
  }
}

export function externalProtocolLabel(node: Pick<ExternalOutbound, 'protocol' | 'config_json'>) {
  switch (node.protocol) {
    case 'http': return externalOutboundIsHTTPS(node) ? 'HTTPS' : 'HTTP'
    case 'shadowsocks': return 'SS'
    case 'hy2': return 'HY2'
    case 'anytls': return 'AnyTLS'
    case 'mieru': return 'Mieru'
    case 'vless': return 'VLESS'
    case 'socks': return 'SOCKS'
    case 'snell': return 'Snell'
    default: return String(node.protocol)
  }
}

function hostPort(address: string, port: number) {
  if (!address) return '地址待填写'
  return address.includes(':') && !address.startsWith('[') ? `[${address}]:${port}` : `${address}:${port}`
}

// thirdPartyFormPayload turns the structured form into the external outbound
// create body. Only the fields the Controller allowlists are sent.
export function thirdPartyFormPayload(draft: ThirdPartyNodeDraft) {
  const config: Record<string, unknown> = {}
  let protocol: string = draft.protocol
  if (draft.protocol === 'shadowsocks') {
    config.method = draft.method
    config.password = draft.password
  } else {
    if (draft.username.trim()) config.username = draft.username.trim()
    if (draft.password) config.password = draft.password
  }
  if (draft.protocol === 'socks') config.version = '5'
  if (draft.protocol === 'http' || draft.protocol === 'https') {
    protocol = 'http'
    if (draft.protocol === 'https') {
      const tls: Record<string, unknown> = { enabled: true }
      if (draft.server_name.trim()) tls.server_name = draft.server_name.trim()
      if (draft.insecure) tls.insecure = true
      config.tls = tls
    }
  }
  const address = draft.address.trim()
  const port = draft.port || 0
  return {
    name: draft.name.trim() || `${draft.protocol.toUpperCase()} ${address}`,
    protocol,
    scope: draft.scope,
    server_id: draft.scope === 'server' && draft.server_id ? draft.server_id : undefined,
    target_address: address,
    target_port: port,
    config_json: JSON.stringify(config),
    region_mode: 'auto',
    region_code: '',
    expose_to_users: protocol === 'http' ? false : draft.expose_to_users,
    enabled: true,
  }
}

export function thirdPartyFormIssue(draft: ThirdPartyNodeDraft): string {
  if (draft.mode === 'link') return draft.content.trim() ? '' : '请粘贴节点链接或出站 JSON'
  if (draft.scope === 'server' && !draft.server_id) return '请选择服务器'
  if (!draft.address.trim()) return '请填写节点地址'
  if (!draft.port || draft.port < 1 || draft.port > 65535) return '端口需在 1-65535 之间'
  if (draft.protocol === 'shadowsocks' && !draft.password) return 'Shadowsocks 需要密码'
  if (draft.username.includes(':')) return '用户名不能包含冒号'
  return ''
}

function checkText(check: ExternalOutboundCheck) {
  switch (check.status) {
    case 'reachable': return `可用 · ${Math.round(check.connect_ms || 0)} ms`
    case 'unreachable': return `不可达 · ${check.error || '连接失败'}`
    default: return check.error ? `检测失败 · ${check.error}` : '检测失败'
  }
}

function CheckBadge({ check, busy }: { check?: ExternalOutboundCheck; busy: boolean }) {
  if (busy) return <span className="third-party-check is-busy">检测中…</span>
  if (!check) return null
  const title = ['检测位置：主控', check.exit_ip ? `实际出口 IP：${check.exit_ip}` : '', checkText(check)].filter(Boolean).join('\n')
  const regionText = check.region_code ? `出口 ${check.region_code}` : check.status === 'reachable' ? '出口地区未识别' : ''
  return <span className={`third-party-check is-${check.status}`} title={title || undefined}>{checkText(check)}{regionText && ` · ${regionText}`}</span>
}

export function ThirdPartyNodesDialog({
  nodes,
  servers,
  checks,
  checking,
  onCheck,
  onCheckAll,
  onAdd,
  onAddToTopology,
  onConnectToEntry,
  onInspect,
  onDelete,
  onClose,
}: {
  nodes: ExternalOutbound[]
  servers: Server[]
  checks: Record<number, ExternalOutboundCheck>
  checking: number[]
  onCheck: (node: ExternalOutbound) => void
  onCheckAll: (nodes: ExternalOutbound[]) => void
  onAdd: () => void
  onAddToTopology: (node: ExternalOutbound) => void
  onConnectToEntry: (node: ExternalOutbound) => void
  onInspect: (node: ExternalOutbound) => void
  onDelete: (node: ExternalOutbound) => Promise<void>
  onClose: () => void
}) {
  const [query, setQuery] = useState('')
  const [deleting, setDeleting] = useState<number | null>(null)
  const [deleteError, setDeleteError] = useState('')
  const deleteNode = async (node: ExternalOutbound) => {
    setDeleteError('')
    setDeleting(node.id)
    try { await onDelete(node) } catch (error) { setDeleteError(error instanceof Error ? error.message : '删除失败，请重试') } finally { setDeleting(null) }
  }
  const serverName = useMemo(() => new Map(servers.map(server => [server.id, server.name])), [servers])
  const normalized = query.trim().toLowerCase()
  const filtered = useMemo(() => nodes.filter(node => !normalized || [node.name, node.protocol, externalProtocolLabel(node), node.target_address, String(node.target_port), node.effective_region_code]
    .some(value => String(value || '').toLowerCase().includes(normalized))), [nodes, normalized])
  const busy = checking.length > 0 || deleting !== null
  return <MotionDialogPanel onCancel={onClose} className="graph-node-picker-dialog third-party-nodes-dialog" placement="right" drawerSize="wide" ariaLabel="第三方节点">
    <header className="dialog-head">
      <div><h2>第三方节点</h2><p className="muted">导入的第三方出口。可检测连通性、放入拓扑或连接到当前入口。</p></div>
      <button className="ghost dialog-close icon-button" onClick={onClose} aria-label="关闭" title="关闭"><X /></button>
    </header>
    <div className="dialog-body graph-node-picker-body">
      <div className="third-party-toolbar">
        <label className="graph-palette-search">
          <Search size={13} aria-hidden="true" />
          <input value={query} onChange={event => setQuery(event.target.value)} placeholder="搜索名称、协议或地址" aria-label="搜索第三方节点" autoFocus />
          {query && <button type="button" className="ghost icon-button graph-palette-search-clear" onClick={() => setQuery('')} aria-label="清空搜索"><X size={12} /></button>}
        </label>
        <button type="button" className="ghost" onClick={() => onCheckAll(filtered)} disabled={busy || !filtered.length}><Activity size={14} aria-hidden="true" />全部检测</button>
        <button type="button" onClick={onAdd}><Plus size={14} aria-hidden="true" />添加节点</button>
      </div>
      {deleteError && <p role="alert" className="error">{deleteError}</p>}
      {!nodes.length ? <div className="graph-palette-empty third-party-empty">
        <p>还没有第三方节点。</p>
        <button type="button" onClick={onAdd}><Plus size={14} aria-hidden="true" />添加第一个节点</button>
      </div> : !filtered.length ? <div className="graph-palette-empty">没有匹配的节点</div> : <div className="graph-imported-list third-party-list">
        {filtered.map(node => {
          const isChecking = checking.includes(node.id)
          const scope = node.scope === 'server' && node.server_id ? `仅 ${serverName.get(node.server_id) || `服务器 ${node.server_id}`}` : '全部服务器'
          return <div className="graph-palette-item third-party-item" key={node.id}>
            <span className="graph-palette-kind imported"><RegionFlag code={checks[node.id]?.region_code || node.effective_region_code || node.region_code} size={20} /></span>
            <span className="graph-palette-copy">
              <strong>{node.name || `第三方节点 ${node.id}`}</strong>
              <small>{externalProtocolLabel(node)} · {hostPort(node.target_address, node.target_port)} · {scope}{node.enabled === false ? ' · 已禁用' : ''}</small>
              <CheckBadge check={checks[node.id]} busy={isChecking} />
            </span>
            <div className="graph-palette-actions">
              <button type="button" className="ghost icon-button" onClick={() => onCheck(node)} disabled={busy} title="检测可用性" aria-label={`检测 ${node.name} 可用性`}><Activity size={14} /></button>
              <button type="button" className="ghost icon-button" onClick={() => onConnectToEntry(node)} disabled={node.enabled === false || deleting === node.id} title="连接到当前入口" aria-label={`将 ${node.name} 连接到当前入口`}><LinkIcon size={14} /></button>
              <button type="button" className="ghost icon-button" onClick={() => onAddToTopology(node)} disabled={node.enabled === false || deleting === node.id} title="添加到拓扑" aria-label={`将 ${node.name} 添加到拓扑`}><Plus size={15} /></button>
              <button type="button" className="ghost icon-button" onClick={() => onInspect(node)} title="节点设置" aria-label={`${node.name} 设置`}><SettingsIcon size={14} /></button>
              <button type="button" className="ghost icon-button" onClick={() => void deleteNode(node)} disabled={busy} title="删除节点" aria-label={`删除 ${node.name}`}><Trash2 size={14} aria-hidden="true" /></button>
            </div>
          </div>
        })}
      </div>}
      <p className="muted third-party-note">主控通过该代理访问 IP 查询服务，验证连接并保存真实出口地区。无需先创建链路，加入链路后直接复用结果；修改连接配置后请重新检测。</p>
    </div>
    <footer className="dialog-actions"><button type="button" className="ghost" onClick={onClose}>完成</button></footer>
  </MotionDialogPanel>
}

export function AddThirdPartyNodeDialog({ draft, setDraft, servers, submitting, onCancel, onSubmit }: {
  draft: ThirdPartyNodeDraft
  setDraft: React.Dispatch<React.SetStateAction<ThirdPartyNodeDraft | null>>
  servers: Server[]
  submitting: boolean
  onCancel: () => void
  onSubmit: () => void
}) {
  const update = (patch: Partial<ThirdPartyNodeDraft>) => setDraft(old => old ? { ...old, ...patch } : old)
  const issue = thirdPartyFormIssue(draft)
  const isHTTP = draft.mode === 'form' && (draft.protocol === 'http' || draft.protocol === 'https')
  const changeProtocol = (protocol: ThirdPartyFormProtocol) => {
    const defaults: Record<ThirdPartyFormProtocol, number> = { socks: 1080, http: 8080, https: 443, shadowsocks: 8388 }
    const knownDefault = Object.values(defaults).includes(draft.port || 0)
    update({ protocol, port: !draft.port || knownDefault ? defaults[protocol] : draft.port, expose_to_users: protocol === 'http' || protocol === 'https' ? false : draft.expose_to_users })
  }
  return <MotionDialogPanel onCancel={onCancel} className="graph-form-dialog" aria-labelledby="add-third-party-title">
    <header className="dialog-head">
      <div><h2 id="add-third-party-title">添加第三方节点</h2><p className="muted">填写节点信息，或粘贴节点链接 / 出站 JSON。</p></div>
      <button className="ghost dialog-close icon-button" onClick={onCancel} aria-label="关闭" title="关闭"><X /></button>
    </header>
    <div className="dialog-body">
      <div className="form graph-dialog-form">
        <FormField label="添加方式"><Select variant="segmented" value={draft.mode} onChange={e => update({ mode: e.target.value as ThirdPartyNodeDraft['mode'] })} aria-label="添加方式"><option value="form">表单填写</option><option value="link">链接导入</option></Select></FormField>
        {draft.mode === 'form' ? <>
          <FormField label="协议"><Select value={draft.protocol} onChange={e => changeProtocol(e.target.value as ThirdPartyFormProtocol)} aria-label="协议"><option value="socks">SOCKS5</option><option value="http">HTTP</option><option value="https">HTTPS</option><option value="shadowsocks">Shadowsocks</option></Select></FormField>
          <FormField label="名称" hint="留空时按协议和地址命名。"><input value={draft.name} onChange={e => update({ name: e.target.value })} placeholder="例如 香港 HTTP" maxLength={128} /></FormField>
          <div className="third-party-address-row">
            <FormField label="地址" required><input value={draft.address} onChange={e => update({ address: e.target.value })} placeholder="proxy.example.com" autoComplete="off" spellCheck={false} /></FormField>
            <FormField label="端口" required><NumericInput value={draft.port} onValueChange={port => update({ port })} max={65535} aria-label="端口" /></FormField>
          </div>
          {draft.protocol === 'shadowsocks' ? <>
            <FormField label="加密方式"><Select value={draft.method} onChange={e => update({ method: e.target.value })} aria-label="加密方式">{SHADOWSOCKS_METHODS.map(method => <option key={method} value={method}>{method}</option>)}</Select></FormField>
            <FormField label="密码" required><input value={draft.password} onChange={e => update({ password: e.target.value })} autoComplete="off" spellCheck={false} /></FormField>
          </> : <>
            <FormField label="用户名" hint="无认证时留空。"><input value={draft.username} onChange={e => update({ username: e.target.value })} autoComplete="off" spellCheck={false} /></FormField>
            <FormField label="密码"><input value={draft.password} onChange={e => update({ password: e.target.value })} autoComplete="off" spellCheck={false} /></FormField>
          </>}
          {draft.protocol === 'https' && <>
            <FormField label="TLS 服务器名称（SNI）" hint="留空时使用节点域名。"><input value={draft.server_name} onChange={e => update({ server_name: e.target.value })} placeholder={draft.address || 'proxy.example.com'} spellCheck={false} /></FormField>
            <FormField label="证书校验" hint="仅在节点使用自签证书时跳过。"><Select variant="segmented" value={String(draft.insecure)} onChange={e => update({ insecure: e.target.value === 'true' })} aria-label="证书校验"><option value="false">校验证书</option><option value="true">跳过校验</option></Select></FormField>
          </>}
        </> : <FormField label="链接或 JSON" required hint="每行一个链接，支持 ss://、socks5://、http://、https://、vless://、mierus:// 和 sing-box 出站 JSON。"><textarea value={draft.content} onChange={e => update({ content: e.target.value })} rows={8} placeholder={'socks5://user:password@example.com:1080#SOCKS-A\nhttp://user:password@proxy.example.com:8080#HTTP-A'} spellCheck={false} /></FormField>}
        <FormField label="使用范围"><Select variant="segmented" value={draft.scope} onChange={e => update({ scope: e.target.value as 'global' | 'server' })} aria-label="使用范围"><option value="global">全部服务器</option><option value="server">指定服务器</option></Select></FormField>
        {draft.scope === 'server' && <FormField label="服务器" required><Select value={draft.server_id} onChange={e => update({ server_id: Number(e.target.value) })} aria-label="服务器"><option value={0}>选择服务器</option>{servers.map(s => <option value={s.id} key={s.id}>{s.name}</option>)}</Select></FormField>}
        {isHTTP
          ? <p className="muted third-party-note">HTTP / HTTPS 节点只能作为代理链路出口，不会下发到用户订阅。</p>
          : <FormField label="订阅展示" hint="链接导入的 HTTP / HTTPS 节点会忽略此项。"><Select variant="segmented" value={String(draft.expose_to_users)} onChange={e => update({ expose_to_users: e.target.value === 'true' })} aria-label="订阅展示"><option value="false">隐藏</option><option value="true">允许授权</option></Select></FormField>}
        {issue && <p className="muted" aria-live="polite">{issue}</p>}
      </div>
    </div>
    <footer className="dialog-actions"><button className="ghost" onClick={onCancel}>取消</button><button onClick={onSubmit} disabled={Boolean(issue) || submitting}>{submitting ? '保存中…' : draft.mode === 'link' ? '导入节点' : '添加节点'}</button></footer>
  </MotionDialogPanel>
}

// submitThirdPartyNode creates the node(s) from either form or link input and
// returns the raw mutation result plus the created nodes.
export async function submitThirdPartyNode(request: RequestFn, draft: ThirdPartyNodeDraft): Promise<{ result: Record<string, unknown>; created: ExternalOutbound[] }> {
  if (draft.mode === 'link') {
    const result = await request('/external-outbounds/import', { method: 'POST', body: JSON.stringify({ content: draft.content, scope: draft.scope, server_id: draft.scope === 'server' && draft.server_id ? draft.server_id : undefined, expose_to_users: draft.expose_to_users }) }) as { external_outbounds?: ExternalOutbound[] }
    return { result: result as Record<string, unknown>, created: result.external_outbounds || [] }
  }
  const result = await request('/external-outbounds', { method: 'POST', body: JSON.stringify(thirdPartyFormPayload(draft)) }) as { external_outbound?: ExternalOutbound }
  return { result: result as Record<string, unknown>, created: result.external_outbound ? [result.external_outbound] : [] }
}

export async function checkThirdPartyNode(request: RequestFn, node: ExternalOutbound): Promise<ExternalOutboundCheck> {
  const result = await request(`/external-outbounds/${node.id}/check`, { method: 'POST', body: '{}' }) as { check: ExternalOutboundCheck }
  return result.check
}

