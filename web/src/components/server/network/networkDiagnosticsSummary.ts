export type DiagnosticState = 'ok' | 'fail' | 'warn' | 'unknown'

export type DiagnosticCheck = {
  key: string
  label: string
  state: DiagnosticState
  detail: string
}

type CommandResult = { available?: boolean; ok?: boolean; output?: string; error?: string }

const obj = (value: unknown): Record<string, any> =>
  value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, any> : {}

const text = (value: unknown) => (typeof value === 'string' ? value : '')

function command(result: Record<string, any>, name: string): CommandResult | null {
  const item = obj(result.commands)[name]
  return item && typeof item === 'object' ? item as CommandResult : null
}

const ran = (item: CommandResult | null) => !!item && item.available !== false
const succeeded = (item: CommandResult | null) => ran(item) && item!.ok === true

function looksLikeResolveFailure(item: CommandResult | null) {
  const message = `${text(item?.output)} ${text(item?.error)}`.toLowerCase()
  return message.includes('could not resolve') || message.includes('no such host') || message.includes('name resolution')
}

function controllerCheck(result: Record<string, any>): DiagnosticCheck {
  const controller = obj(result.controller)
  const tcp = obj(controller.tcp)
  const base = { key: 'controller', label: 'Controller 连接' }
  if (tcp.ok === true) {
    const ms = Number(tcp.duration_ms)
    return { ...base, state: 'ok', detail: Number.isFinite(ms) ? `TCP 可达 · ${ms} ms` : 'TCP 可达' }
  }
  if (text(controller.error)) return { ...base, state: 'fail', detail: text(controller.error) }
  if (tcp.ok === false) return { ...base, state: 'fail', detail: text(tcp.error) || 'TCP 不可达' }
  return { ...base, state: 'unknown', detail: '未返回结果' }
}

function publicAddressCheck(result: Record<string, any>, key: 'ipv4' | 'ipv6'): DiagnosticCheck {
  const item = command(result, key === 'ipv4' ? 'public_ipv4' : 'public_ipv6')
  const base = { key, label: key === 'ipv4' ? 'IPv4' : 'IPv6' }
  if (!ran(item)) return { ...base, state: 'unknown', detail: '缺少 curl，无法检测' }
  const address = text(item!.output).trim()
  if (item!.ok === true && address) return { ...base, state: 'ok', detail: `公网出口 ${address}` }
  return { ...base, state: 'fail', detail: key === 'ipv4' ? '未获取到公网 IPv4 出口' : '未获取到公网 IPv6 出口' }
}

function hasDefaultRoute(item: CommandResult | null) {
  return succeeded(item) && /^default\s/m.test(text(item!.output))
}

function defaultRouteCheck(result: Record<string, any>): DiagnosticCheck {
  const v4 = command(result, 'ip_route_v4')
  const v6 = command(result, 'ip_route_v6')
  const base = { key: 'default_route', label: '默认路由' }
  if (!ran(v4) && !ran(v6)) return { ...base, state: 'unknown', detail: '缺少 ip 命令，无法检测' }
  const families = [hasDefaultRoute(v4) ? 'IPv4' : '', hasDefaultRoute(v6) ? 'IPv6' : ''].filter(Boolean)
  if (families.length) return { ...base, state: 'ok', detail: `${families.join(' / ')} 已配置` }
  return { ...base, state: 'fail', detail: '未找到默认路由' }
}

function configuredInboundPorts(result: Record<string, any>) {
  let config: Record<string, any> = {}
  try { config = obj(JSON.parse(text(obj(obj(result.files).sing_box_config).content) || '{}')) } catch { return null }
  const inbounds = Array.isArray(config.inbounds) ? config.inbounds : []
  const ports = new Set<number>()
  for (const inbound of inbounds) {
    const port = Number(obj(inbound).listen_port)
    if (Number.isInteger(port) && port > 0) ports.add(port)
  }
  return [...ports].sort((a, b) => a - b)
}

function listenersCheck(result: Record<string, any>): DiagnosticCheck {
  const base = { key: 'listeners', label: '入口监听' }
  const ports = configuredInboundPorts(result)
  if (ports === null) return { ...base, state: 'unknown', detail: '无法读取内核配置' }
  if (!ports.length) return { ...base, state: 'unknown', detail: '内核未配置入口' }
  const tcp = command(result, 'ss_tcp_listen')
  const udp = command(result, 'ss_udp_listen')
  if (!succeeded(tcp) && !succeeded(udp)) return { ...base, state: 'unknown', detail: '缺少 ss 命令，无法检测' }
  const listening = `${text(tcp?.output)}\n${text(udp?.output)}`
  const missing = ports.filter((port) => !new RegExp(`[:\\]*]${port}\\s`).test(listening))
  if (!missing.length) return { ...base, state: 'ok', detail: `${ports.length} 个端口均在监听` }
  return { ...base, state: 'fail', detail: `未监听端口：${missing.join(', ')}` }
}

function dnsCheck(result: Record<string, any>): DiagnosticCheck {
  const base = { key: 'dns', label: 'DNS' }
  const lookups = [command(result, 'public_ipv4'), command(result, 'public_ipv6')].filter(ran)
  if (lookups.some(succeeded)) return { ...base, state: 'ok', detail: '系统解析正常' }
  if (lookups.some(looksLikeResolveFailure)) return { ...base, state: 'fail', detail: '系统无法解析域名' }
  return { ...base, state: 'unknown', detail: '无可用的解析检测结果' }
}

function countRules(output: string) {
  return output.split('\n').filter((line) => line.startsWith('-A ')).length
}

function firewallCheck(result: Record<string, any>): DiagnosticCheck {
  const base = { key: 'firewall', label: '防火墙' }
  const nft = command(result, 'nft_ruleset')
  const v4 = command(result, 'iptables_save')
  const v6 = command(result, 'ip6tables_save')
  const ufw = command(result, 'ufw_status')
  if (![nft, v4, v6, ufw].some(succeeded)) return { ...base, state: 'unknown', detail: '无法读取防火墙规则' }
  const parts: string[] = []
  if (succeeded(ufw)) parts.push(/status:\s*active/i.test(text(ufw!.output)) ? 'ufw 已启用' : 'ufw 未启用')
  if (succeeded(nft)) parts.push(text(nft!.output).trim() ? 'nftables 有规则' : 'nftables 无规则')
  const iptablesRules = (succeeded(v4) ? countRules(text(v4!.output)) : 0) + (succeeded(v6) ? countRules(text(v6!.output)) : 0)
  if (succeeded(v4) || succeeded(v6)) parts.push(iptablesRules ? `iptables ${iptablesRules} 条规则` : 'iptables 无规则')
  const hasRules = parts.some((part) => part === 'ufw 已启用' || part === 'nftables 有规则' || /iptables \d+ 条/.test(part))
  return { ...base, state: hasRules ? 'warn' : 'ok', detail: hasRules ? `${parts.join(' · ')}，请确认已放行入口端口` : parts.join(' · ') }
}

function entryProbeCheck(result: Record<string, any>): DiagnosticCheck {
  const base = { key: 'entry_probe', label: '入口公网探测' }
  const targets = Array.isArray(result.entry_targets) ? result.entry_targets.map(obj) : []
  if (!targets.length) return { ...base, state: 'unknown', detail: '无入口探测目标' }
  const failed = targets.filter((target) => text(target.error) || obj(target.tcp_self_probe).ok !== true)
  if (!failed.length) return { ...base, state: 'ok', detail: `${targets.length} 个入口均可达` }
  return { ...base, state: 'fail', detail: `${failed.length}/${targets.length} 个入口不可达` }
}

export function summarizeNetworkDiagnostics(raw: unknown): DiagnosticCheck[] {
  const result = obj(raw)
  return [
    controllerCheck(result),
    publicAddressCheck(result, 'ipv4'),
    publicAddressCheck(result, 'ipv6'),
    defaultRouteCheck(result),
    listenersCheck(result),
    dnsCheck(result),
    firewallCheck(result),
    entryProbeCheck(result),
  ]
}

