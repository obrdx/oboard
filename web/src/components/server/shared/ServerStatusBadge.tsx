import React from 'react'
import type { Server } from '../../proxy-path/types'

export function ServerStatusBadge({ server }: { server: Server }) {
  const health = server.agent_health
  const enrolled = Boolean(String(server.agent_id||'').trim())
  if(!enrolled) return <span className="server-workspace-status unenrolled"><i className="server-workspace-status-dot"/>未接入</span>
  if (health) {
    const heartbeat = health.heartbeat_online
    return <span className={`server-workspace-status ${heartbeat ? 'online' : 'offline'}`} title={health.auth_reason || ''}>
      <i className="server-workspace-status-dot"/>
      {heartbeat ? '心跳在线' : '心跳离线'}
      {' · '}{health.task_channel_available ? '任务通道可用' : '任务通道不可用'}
      {' · '}{health.config_applied ? '配置已应用' : '配置未应用'}
    </span>
  }
  const isOnline = String(server.status||'').toLowerCase()==='online'
  return <span className={`server-workspace-status ${isOnline?'online':'offline'}`}><i className="server-workspace-status-dot"/>{isOnline?'在线':'离线'}</span>
}
