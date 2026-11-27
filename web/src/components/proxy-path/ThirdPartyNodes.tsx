import React, { useMemo, useState } from 'react'
import { Activity, ChevronDown, ClipboardPaste, Info, Link as LinkIcon, PenLine, Plus, Search, Settings as SettingsIcon, Trash2, X } from 'lucide-react'
import { MotionDialogPanel } from '../ui/motion'
import { FormField } from '../ui/form-field'
import { Select } from '../ui/select'
import { NumericInput } from '../ui/numeric-input'
import { RegionFlag } from '../ui/RegionFlag'
import type { ExternalOutbound, Server } from './types'

export type ExternalOutboundCheck = {
  external_outbound_id: number
  status: 'reachable' | 'unreachable' | 'failed' | 'skipped'
  server_id?: number
  server_name?: string
  target: string
  exit_ip?: string
  region_code?: string
  region_status?: string
  connect_ms?: number
  error?: string
  checked_at: string
}

type RequestFn = (path: string, init?: RequestInit) => Promise<unknown>

export type ThirdPartyFormProtocol = 'socks' | 'http' | 'https' | 'shadowsocks' | 'wireguard'

// Protocols that are only proxy-path exits and never reach a user subscription.
export function externalProtocolChainOnly(protocol: string) {
  return protocol === 'http' || protocol === 'https' || protocol === 'wireguard'
}

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
  remote_dns: boolean
  method: string
  server_name: string
  insecure: boolean
  private_key: string
  wg_address: string
  public_key: string
  pre_shared_key: string
  reserved: string
  mtu: number | undefined
}

export const SHADOWSOCKS_METHODS = ['2022-blake3-aes-128-gcm', '2022-blake3-aes-256-gcm', '2022-blake3-chacha20-poly1305', 'aes-128-gcm', 'aes-256-gcm', 'chacha20-ietf-poly1305']

export function newThirdPartyNodeDraft(serverID: number): ThirdPartyNodeDraft {
  return { mode: 'form', scope: 'global', server_id: serverID, expose_to_users: false, content: '', name: '', protocol: 'socks', address: '', port: 1080, username: '', password: '', remote_dns: true, method: '2022-blake3-aes-128-gcm', server_name: '', insecure: false, private_key: '', wg_address: '', public_key: '', pre_shared_key: '', reserved: '', mtu: undefined }
}

export function externalOutboundSocksRemoteDNS(node: Pick<ExternalOutbound, 'protocol' | 'config_json'>) {
  if (node.protocol !== 'socks') return false
  try {
    const cfg = JSON.parse(node.config_json || '{}')
    const version = String(cfg?.version || '5').trim()
    if (version === '4') return false
    if (typeof cfg?.remote_dns === 'boolean') return cfg.remote_dns
    return true
  } catch {
    return true
  }
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
    case 'socks': {
      try {
        const cfg = JSON.parse(node.config_json || '{}')
        const version = String(cfg?.version || '5').trim()
        if (version === '4') return 'SOCKS4'
        if (version === '4a') return 'SOCKS4a'
      } catch { /* default SOCKS5H */ }
      return externalOutboundSocksRemoteDNS(node) ? 'SOCKS5H' : 'SOCKS5'
    }
    case 'snell': return 'Snell'
    case 'wireguard': return 'WireGuard'
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
  } else if (draft.protocol === 'wireguard') {
    config.private_key = draft.private_key.trim()
    config.address = splitList(draft.wg_address)
    config.peer_public_key = draft.public_key.trim()
    if (draft.pre_shared_key.trim()) config.pre_shared_key = draft.pre_shared_key.trim()
    const reserved = splitList(draft.reserved).map(Number)
    if (reserved.length) config.reserved = reserved
    if (draft.mtu) config.mtu = draft.mtu
  } else {
    if (draft.username.trim()) config.username = draft.username.trim()
    if (draft.password) config.password = draft.password
  }
  if (draft.protocol === 'socks') {
    config.version = '5'
    config.remote_dns = draft.remote_dns
  }
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
  const label = draft.protocol === 'wireguard' ? 'WireGuard' : draft.protocol === 'socks' ? (draft.remote_dns ? 'SOCKS5H' : 'SOCKS5') : draft.protocol.toUpperCase()
  return {
    name: draft.name.trim() || `${label} ${address}`,
    protocol,
    scope: draft.scope,
    server_id: draft.scope === 'server' && draft.server_id ? draft.server_id : undefined,
    target_address: address,
    target_port: port,
    config_json: JSON.stringify(config),
    region_mode: 'auto',
    region_code: '',
    expose_to_users: externalProtocolChainOnly(protocol) ? false : draft.expose_to_users,
    enabled: true,
  }
}

export function thirdPartyFormIssue(draft: ThirdPartyNodeDraft): string {
  if (draft.mode === 'link') return draft.content.trim() ? (draft.scope === 'server' && !draft.server_id ? '请选择服务器' : '') : '粘贴节点链接或配置后即可导入'
  if (draft.scope === 'server' && !draft.server_id) return '请选择服务器'
  if (!draft.address.trim()) return '请填写节点地址'
  if (!draft.port || draft.port < 1 || draft.port > 65535) return '端口需在 1-65535 之间'
  if (draft.protocol === 'shadowsocks' && !draft.password) return 'Shadowsocks 需要密码'
  if (draft.protocol === 'wireguard') {
    if (!WIREGUARD_KEY.test(draft.private_key.trim())) return '请填写 Base64 格式的本地私钥'
    if (!WIREGUARD_KEY.test(draft.public_key.trim())) return '请填写 Base64 格式的对端公钥'
    if (draft.pre_shared_key.trim() && !WIREGUARD_KEY.test(draft.pre_shared_key.trim())) return '预共享密钥格式无效'
    if (!splitList(draft.wg_address).length) return '请填写隧道本地地址'
    const reserved = splitList(draft.reserved)
    if (reserved.length && (reserved.length !== 3 || reserved.some(item => !/^\d{1,3}$/.test(item) || Number(item) > 255))) return 'Reserved 需为 3 个 0-255 的整数'
    if (draft.mtu && (draft.mtu < 576 || draft.mtu > 9000)) return 'MTU 需在 576-9000 之间'
    return ''
  }
  if (draft.username.includes(':')) return '用户名不能包含冒号'
  return ''
}

const WIREGUARD_KEY = /^[A-Za-z0-9+/]{43}=$/

function splitList(value: string) {
  return value.split(',').map(item => item.trim()).filter(Boolean)
}

function checkText(check: ExternalOutboundCheck) {
  switch (check.status) {
    case 'reachable': return `可用${check.server_name ? `（经 ${check.server_name}）` : ''} · ${Math.round(check.connect_ms || 0)} ms`
    case 'unreachable': return `不可达 · ${check.error || '连接失败'}`
    case 'skipped': return '仅服务器可达 · 主控不检测，部署后由路径探测出口'
    default: return check.error ? `检测失败 · ${check.error}` : '检测失败'
  }
}

function CheckBadge({ check, busy }: { check?: ExternalOutboundCheck; busy: boolean }) {
  if (busy) return <span className="third-party-check is-busy">检测中…</span>
  if (!check) return null
  const where = check.server_name ? `检测位置：主控失败后由服务器 ${check.server_name} 复检` : '检测位置：主控'
  const title = [where, check.exit_ip ? `实际出口 IP：${check.exit_ip}` : '', checkText(check)].filter(Boolean).join('\n')
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

const PROTOCOL_CHOICES: { protocol: ThirdPartyFormProtocol; label: string; summary: string }[] = [
  { protocol: 'socks', label: 'SOCKS5H', summary: '远端 DNS · 通用代理' },
  { protocol: 'http', label: 'HTTP', summary: 'CONNECT 代理' },
  { protocol: 'https', label: 'HTTPS', summary: 'TLS 加密' },
  { protocol: 'shadowsocks', label: 'Shadowsocks', summary: 'SS / SS2022' },
  { protocol: 'wireguard', label: 'WireGuard', summary: 'UDP 隧道' },
]

const IMPORT_FORMATS: { pattern: RegExp; label: string }[] = [
  { pattern: /^ss:\/\//i, label: 'Shadowsocks' },
  { pattern: /^socks(?:5h?)?:\/\//i, label: 'SOCKS5H' },
  { pattern: /^https:\/\//i, label: 'HTTPS' },
  { pattern: /^http:\/\//i, label: 'HTTP' },
  { pattern: /^vless:\/\//i, label: 'VLESS' },
  { pattern: /^mierus:\/\//i, label: 'Mieru' },
  { pattern: /^(wireguard|wg):\/\//i, label: 'WireGuard' },
]

// describeImportContent recognises pasted content before it is submitted so
// the operator sees what will be imported. The Controller remains the only
// parser; this is a preview of the formats, not validation.
export function describeImportContent(content: string): { summary: string; unknown: number } {
  const text = content.trim()
  if (!text) return { summary: '', unknown: 0 }
  const firstLine = text.split('\n').map(line => line.trim()).find(line => line && !line.startsWith('#') && !line.startsWith(';')) || ''
  if (/^\[interface\]$/i.test(firstLine)) return { summary: '识别为 WireGuard 配置文件，将导入 1 个节点', unknown: 0 }
  if (text.startsWith('{') || text.startsWith('[')) return { summary: '识别为 sing-box 出站 JSON', unknown: 0 }
  const counts = new Map<string, number>()
  let unknown = 0
  for (const raw of text.split('\n')) {
    const line = raw.trim()
    if (!line || line.startsWith('#') || line.startsWith('//')) continue
    const format = line.startsWith('{') ? { label: 'JSON' } : IMPORT_FORMATS.find(item => item.pattern.test(line))
    if (!format) { unknown++; continue }
    counts.set(format.label, (counts.get(format.label) || 0) + 1)
  }
  const total = Array.from(counts.values()).reduce((sum, count) => sum + count, 0)
  if (!total) return { summary: '', unknown }
  const parts = Array.from(counts, ([label, count]) => count > 1 ? `${label} ×${count}` : label)
  return { summary: `识别到 ${total} 个节点：${parts.join('、')}`, unknown }
}

function advancedSummary(draft: ThirdPartyNodeDraft) {
  if (draft.protocol === 'socks') return draft.remote_dns ? '远端 DNS（SOCKS5H）' : '本地 DNS（SOCKS5）'
  if (draft.protocol === 'https') return [draft.server_name.trim() && 'SNI', draft.insecure && '跳过证书校验'].filter(Boolean).join(' · ')
  if (draft.protocol === 'wireguard') return [draft.pre_shared_key.trim() && '预共享密钥', draft.reserved.trim() && 'Reserved', draft.mtu && `MTU ${draft.mtu}`].filter(Boolean).join(' · ')
  return ''
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
  const isForm = draft.mode === 'form'
  const chainOnly = isForm && externalProtocolChainOnly(draft.protocol)
  const detected = useMemo(() => describeImportContent(draft.content), [draft.content])
  const changeProtocol = (protocol: ThirdPartyFormProtocol) => {
    const defaults: Record<ThirdPartyFormProtocol, number> = { socks: 1080, http: 8080, https: 443, shadowsocks: 8388, wireguard: 51820 }
    const knownDefault = Object.values(defaults).includes(draft.port || 0)
    update({ protocol, port: !draft.port || knownDefault ? defaults[protocol] : draft.port, expose_to_users: externalProtocolChainOnly(protocol) ? false : draft.expose_to_users })
  }
  const choice = PROTOCOL_CHOICES.find(item => item.protocol === draft.protocol) || PROTOCOL_CHOICES[0]
  const hasAdvanced = draft.protocol === 'socks' || draft.protocol === 'https' || draft.protocol === 'wireguard'
  const advanced = advancedSummary(draft)
  const secret = { autoComplete: 'off', spellCheck: false } as const
  return <MotionDialogPanel onCancel={onCancel} className="graph-form-dialog third-party-dialog" aria-labelledby="add-third-party-title">
    <header className="dialog-head">
      <div><h2 id="add-third-party-title">添加第三方节点</h2><p className="muted">第三方节点可作为代理链路的出口，也可授权到用户订阅。</p></div>
      <button className="ghost dialog-close icon-button" onClick={onCancel} aria-label="关闭" title="关闭"><X /></button>
    </header>
    <div className="dialog-body">
      <div className="third-party-form">
        <div className="third-party-mode" role="tablist" aria-label="添加方式">
          <button type="button" role="tab" aria-selected={isForm} className={isForm ? 'is-active' : ''} onClick={() => update({ mode: 'form' })}><PenLine aria-hidden="true" />填写信息</button>
          <button type="button" role="tab" aria-selected={!isForm} className={!isForm ? 'is-active' : ''} onClick={() => update({ mode: 'link' })}><ClipboardPaste aria-hidden="true" />粘贴链接或配置</button>
        </div>

        {isForm ? <>
          <section className="third-party-section" aria-labelledby="third-party-protocol-title">
            <h3 id="third-party-protocol-title">协议</h3>
            <div className="third-party-protocols" role="radiogroup" aria-label="协议">
              {PROTOCOL_CHOICES.map(item => {
                const active = item.protocol === draft.protocol
                return <button key={item.protocol} type="button" role="radio" aria-checked={active} className={`third-party-protocol${active ? ' is-active' : ''}`} onClick={() => changeProtocol(item.protocol)}>
                  <strong>{item.label}</strong><small>{item.summary}</small>
                </button>
              })}
            </div>
          </section>

          <section className="third-party-section" aria-labelledby="third-party-connection-title">
            <h3 id="third-party-connection-title">{draft.protocol === 'wireguard' ? '对端' : '连接'}</h3>
            <div className="third-party-grid is-endpoint">
              <FormField label={draft.protocol === 'wireguard' ? 'Endpoint 地址' : '地址'} required><input value={draft.address} onChange={e => update({ address: e.target.value })} placeholder={draft.protocol === 'wireguard' ? 'wg.example.com' : 'proxy.example.com'} {...secret} /></FormField>
              <FormField label="端口" required><NumericInput value={draft.port} onValueChange={port => update({ port })} max={65535} aria-label="端口" /></FormField>
            </div>
            <FormField label="名称" hint="留空时按协议和地址自动命名。"><input value={draft.name} onChange={e => update({ name: e.target.value })} placeholder={`${choice.label} ${draft.address.trim() || '节点地址'}`} maxLength={128} /></FormField>
          </section>

          <section className="third-party-section" aria-labelledby="third-party-auth-title">
            <h3 id="third-party-auth-title">{draft.protocol === 'wireguard' ? '密钥与隧道' : '认证'}</h3>
            {draft.protocol === 'shadowsocks' ? <div className="third-party-grid">
              <FormField label="加密方式"><Select value={draft.method} onChange={e => update({ method: e.target.value })} aria-label="加密方式">{SHADOWSOCKS_METHODS.map(method => <option key={method} value={method}>{method}</option>)}</Select></FormField>
              <FormField label="密码" required><input value={draft.password} onChange={e => update({ password: e.target.value })} {...secret} /></FormField>
            </div> : draft.protocol === 'wireguard' ? <>
              <div className="third-party-grid">
                <FormField label="本地私钥" required hint="配置文件 [Interface] 中的 PrivateKey。"><input className="is-mono" value={draft.private_key} onChange={e => update({ private_key: e.target.value })} placeholder="Base64，44 个字符" {...secret} /></FormField>
                <FormField label="对端公钥" required hint="配置文件 [Peer] 中的 PublicKey。"><input className="is-mono" value={draft.public_key} onChange={e => update({ public_key: e.target.value })} placeholder="Base64，44 个字符" {...secret} /></FormField>
              </div>
              <FormField label="隧道地址" required hint="配置文件 [Interface] 中的 Address，多个用逗号分隔。"><input className="is-mono" value={draft.wg_address} onChange={e => update({ wg_address: e.target.value })} placeholder="172.16.0.2/32, fd00::2/128" {...secret} /></FormField>
            </> : <>
              <div className="third-party-grid">
                <FormField label="用户名"><input value={draft.username} onChange={e => update({ username: e.target.value })} placeholder="无认证时留空" {...secret} /></FormField>
                <FormField label="密码"><input value={draft.password} onChange={e => update({ password: e.target.value })} placeholder="无认证时留空" {...secret} /></FormField>
              </div>
            </>}
          </section>

          {hasAdvanced && <details className="third-party-advanced">
            <summary><span>高级设置</span><small>{advanced || '可选'}</small><ChevronDown aria-hidden="true" /></summary>
            <div className="third-party-advanced-body">
              {draft.protocol === 'socks' ? <FormField label="DNS 解析方式" hint="默认使用远端 DNS（SOCKS5H），将域名交给远端 SOCKS5 代理在出口侧解析；仅在第三方代理不支持域名目标时切回本地 DNS（需新版内核）。">
                <Select variant="segmented" value={String(draft.remote_dns)} onChange={e => update({ remote_dns: e.target.value === 'true' })} aria-label="DNS 解析方式">
                  <option value="true">远端 DNS（SOCKS5H）</option>
                  <option value="false">本地 DNS（SOCKS5）</option>
                </Select>
              </FormField> : draft.protocol === 'https' ? <div className="third-party-grid">
                <FormField label="TLS 服务器名称（SNI）" hint="留空时使用节点域名。"><input value={draft.server_name} onChange={e => update({ server_name: e.target.value })} placeholder={draft.address || 'proxy.example.com'} spellCheck={false} /></FormField>
                <FormField label="证书校验" hint="仅在节点使用自签证书时跳过。"><Select variant="segmented" value={String(draft.insecure)} onChange={e => update({ insecure: e.target.value === 'true' })} aria-label="证书校验"><option value="false">校验</option><option value="true">跳过</option></Select></FormField>
              </div> : <>
                <FormField label="预共享密钥" hint="配置文件中的 PresharedKey，没有时留空。"><input className="is-mono" value={draft.pre_shared_key} onChange={e => update({ pre_shared_key: e.target.value })} {...secret} /></FormField>
                <div className="third-party-grid">
                  <FormField label="Reserved" hint="部分服务（如 WARP）需要的 3 个字节，例如 1,2,3。"><input value={draft.reserved} onChange={e => update({ reserved: e.target.value })} placeholder="1,2,3" {...secret} /></FormField>
                  <FormField label="MTU" hint="留空时使用 1408。"><NumericInput value={draft.mtu} onValueChange={mtu => update({ mtu })} max={9000} aria-label="MTU" placeholder="1408" /></FormField>
                </div>
              </>}
            </div>
          </details>}
        </> : <section className="third-party-section" aria-labelledby="third-party-paste-title">
          <h3 id="third-party-paste-title">链接或配置</h3>
          <textarea className="third-party-paste" value={draft.content} onChange={e => update({ content: e.target.value })} rows={9} aria-label="链接或配置" placeholder={'每行一个节点链接，例如：\nsocks5h://user:password@example.com:1080#香港-SOCKS5H\nwireguard://私钥@wg.example.com:51820?publickey=…&address=10.0.0.2/32\n\n也可以直接粘贴完整的 WireGuard 配置文件或 sing-box 出站 JSON。'} spellCheck={false} />
          {detected.summary || detected.unknown
            ? <p className={`third-party-detect${detected.unknown ? ' is-warning' : ''}`} aria-live="polite">{detected.summary}{detected.unknown ? `${detected.summary ? '；' : ''}${detected.unknown} 行无法识别` : ''}</p>
            : <div className="third-party-formats" aria-label="支持的格式">{['ss://', 'socks5h://', 'socks5://', 'http(s)://', 'vless://', 'mierus://', 'wireguard://', 'WireGuard 配置文件', 'sing-box JSON'].map(item => <span key={item}>{item}</span>)}</div>}
        </section>}

        <section className="third-party-section" aria-labelledby="third-party-usage-title">
          <h3 id="third-party-usage-title">使用范围</h3>
          <div className="third-party-grid">
            <FormField label="可用服务器"><Select variant="segmented" value={draft.scope} onChange={e => update({ scope: e.target.value as 'global' | 'server' })} aria-label="可用服务器"><option value="global">全部</option><option value="server">指定</option></Select></FormField>
            {draft.scope === 'server'
              ? <FormField label="服务器" required><Select value={draft.server_id} onChange={e => update({ server_id: Number(e.target.value) })} aria-label="服务器"><option value={0}>选择服务器</option>{servers.map(s => <option value={s.id} key={s.id}>{s.name}</option>)}</Select></FormField>
              : chainOnly ? null : <FormField label="订阅展示" hint={isForm ? undefined : 'HTTP / HTTPS / WireGuard 节点会忽略此项。'}><Select variant="segmented" value={String(draft.expose_to_users)} onChange={e => update({ expose_to_users: e.target.value === 'true' })} aria-label="订阅展示"><option value="false">隐藏</option><option value="true">允许授权</option></Select></FormField>}
          </div>
          {draft.scope === 'server' && !chainOnly && <FormField label="订阅展示" hint={isForm ? undefined : 'HTTP / HTTPS / WireGuard 节点会忽略此项。'}><Select variant="segmented" value={String(draft.expose_to_users)} onChange={e => update({ expose_to_users: e.target.value === 'true' })} aria-label="订阅展示"><option value="false">隐藏</option><option value="true">允许授权</option></Select></FormField>}
          {chainOnly && <p className="third-party-callout"><Info aria-hidden="true" />{choice.label} 节点只能作为代理链路的出口，不会下发到用户订阅。</p>}
        </section>
      </div>
    </div>
    <footer className="dialog-actions third-party-actions">
      {issue && <span className="third-party-issue" aria-live="polite">{issue}</span>}
      <button className="ghost" onClick={onCancel}>取消</button>
      <button onClick={onSubmit} disabled={Boolean(issue) || submitting}>{submitting ? '保存中…' : isForm ? '添加节点' : detected.summary.startsWith('识别到') ? `导入 ${detected.summary.match(/\d+/)?.[0]} 个节点` : '导入节点'}</button>
    </footer>
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

