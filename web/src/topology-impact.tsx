import * as React from 'react'

export type TopologyNamedRef = { id: number; name: string }

export type TopologyPathImpact = {
  path_id: number
  name: string
  root_inbound_id?: number
  root_server_id?: number
  root_server_name?: string
  enabled: boolean
  effect: 'deleted' | 'truncated' | 'kept_direct'
  reason: 'root' | 'hop' | 'branch'
  from_position?: number
  removed_steps: number
  remaining_steps: number
  disabled?: boolean
  plans?: TopologyNamedRef[]
}

export type TopologyRemovalImpact = {
  paths: TopologyPathImpact[]
  deleted_path_count: number
  truncated_path_count: number
  kept_direct_count: number
  other_root_path_count: number
  removed_step_count: number
  routing_rule_count: number
  plans: TopologyNamedRef[]
  exception_count: number
  inbounds?: TopologyNamedRef[]
  port_forwards: TopologyNamedRef[]
  tunnels: TopologyNamedRef[]
  forwards_deleted: boolean
}

const listLimit = 12

function pathEffectLabel(path: TopologyPathImpact) {
  if (path.effect === 'deleted') return path.reason === 'branch' ? '分支随之删除' : '整条删除'
  if (path.effect === 'kept_direct') return `移除全部 ${path.removed_steps} 个节点，保留为直连`
  const cut = `从第 ${path.from_position} 跳起移除 ${path.removed_steps} 个节点，保留 ${path.remaining_steps} 个`
  return path.disabled ? `${cut}，并停用` : cut
}

function namesLine(items: readonly TopologyNamedRef[]) {
  const shown = items.slice(0, listLimit).map(item => item.name).join('、')
  return items.length > listLimit ? `${shown} 等 ${items.length} 项` : shown
}

export function topologyImpactSummary(impact: TopologyRemovalImpact) {
  const parts: string[] = []
  if (impact.deleted_path_count) parts.push(`删除 ${impact.deleted_path_count} 条链路`)
  if (impact.truncated_path_count) parts.push(`截断 ${impact.truncated_path_count} 条链路`)
  if (impact.kept_direct_count) parts.push(`${impact.kept_direct_count} 条链路改为直连`)
  return parts.length ? parts.join('，') : '不会删除或截断任何链路'
}

// Renders the impact the Controller computed for a removal. It lists disabled
// paths, branches and paths rooted at other entry servers, because those are
// exactly the effects an operator cannot see on the current canvas.
export function TopologyImpactDetail({ impact, currentRootServerID }: { impact: TopologyRemovalImpact; currentRootServerID?: number }) {
  const paths = impact.paths || []
  const shown = paths.slice(0, listLimit)
  return <div className="topology-impact">
    <p><strong>{topologyImpactSummary(impact)}</strong>{impact.removed_step_count ? `，共移除 ${impact.removed_step_count} 个节点。` : '。'}</p>
    {shown.length > 0 && <ul className="topology-impact-paths">
      {shown.map(path => <li key={path.path_id}>
        <span>{path.name}</span>
        {path.root_server_id && currentRootServerID !== path.root_server_id && path.root_server_name ? <span className="muted">（入口服务器 {path.root_server_name}）</span> : null}
        {!path.enabled && <span className="muted">（已停用）</span>}
        <span className="muted"> — {pathEffectLabel(path)}</span>
      </li>)}
      {paths.length > shown.length && <li className="muted">另有 {paths.length - shown.length} 条链路</li>}
    </ul>}
    {impact.other_root_path_count > 0 && <p>其中 {impact.other_root_path_count} 条属于其他入口服务器，不在当前画布上。</p>}
    {impact.inbounds && impact.inbounds.length > 0 && <p>删除入口：{namesLine(impact.inbounds)}</p>}
    {impact.plans.length > 0 && <p>离开订阅套餐：{namesLine(impact.plans)}</p>}
    {impact.routing_rule_count > 0 && <p>同时移除 {impact.routing_rule_count} 条分流规则。</p>}
    {impact.exception_count > 0 && <p>清理 {impact.exception_count} 条用户访问例外。</p>}
    {(impact.port_forwards.length > 0 || impact.tunnels.length > 0) && (impact.forwards_deleted
      ? <p>删除端口转发 / 隧道：{namesLine([...impact.port_forwards, ...impact.tunnels])}</p>
      : <p className="muted">相关端口转发 / 隧道保留，如需删除请到对应页面：{namesLine([...impact.port_forwards, ...impact.tunnels])}</p>)}
  </div>
}
