import React, { useState } from 'react'
import type { Server } from '../../proxy-path/types'
import { summarizeNetworkDiagnostics, type DiagnosticState } from './networkDiagnosticsSummary'

export function NetworkDiagnosticsTab({ server, client, notify, disabled, disabledReason }: { server: Server; client:any; notify?:(m:string,t?:string)=>void; disabled?:boolean; disabledReason?:string }) {
  const [running, setRunning]=useState(false)
  const [task, setTask]=useState<any>(null)
  const [result, setResult]=useState<any>(null)
  const [error, setError]=useState('')

  const start=async()=>{
    if(running|| disabled) return
    setRunning(true); setTask(null); setResult(null); setError('')
    try{
      const created = await client.request(`/servers/${server.id}/diagnose`, { method:'POST', body:'{}' })
      const taskID = created.task?.id
      setTask(created.task)
      if(!taskID){ setRunning(false); return }
      for(let i=0;i<60;i++){
        await new Promise(r=> setTimeout(r,1000))
        const res = await client.request(`/servers/${server.id}/tasks?limit=30`)
        const next = (res.tasks||[]).find((x:any)=> Number(x.id)===Number(taskID))
        if(next){
          setTask(next)
          if(['succeeded','failed','rollback_failed'].includes(String(next.status))){
            let parsed: any=null
            try{ parsed=JSON.parse(next.result_json||'{}')}catch{}
            setResult(parsed)
            if(String(next.status)!=='succeeded'){
              setError(parsed?.error||'诊断未完全成功')
            } else {
              notify?.('网络诊断已完成','success')
            }
            break
          }
        }
      }
    } catch(e:any){
      setError(e?.message||String(e))
      notify?.(e?.message||String(e),'error')
    } finally{ setRunning(false) }
  }

  const checks = summarizeNetworkDiagnostics(result)
  const statusText=(state:DiagnosticState)=>{
    if(!result) return running ? '检查中' : '—'
    return state==='ok' ? '✓' : state==='fail' ? '✗' : state==='warn' ? '!' : '—'
  }
  const entryTargets: any[] = Array.isArray(result?.entry_targets) ? result.entry_targets : []

  return (
    <div className="server-diagnostics-tab">
      <section className="server-detail-section">
        <h3>网络诊断</h3>
        <p className="muted">对目标服务器执行完整的网络与入口检查，结果通过 Agent 任务返回。</p>
        <button type="button" onClick={()=>void start()} disabled={running|| disabled} title={disabled? disabledReason: '开始完整诊断'}>
          {running ? '正在诊断…' : '开始完整诊断'}
        </button>
        {disabled && <small className="muted" style={{display:'block', marginTop:8}}>{disabledReason}</small>}
        {task && <p className="muted" style={{marginTop:10}}>任务状态：{String(task.status)} {task.completed_at ? `· 完成于 ${new Date(task.completed_at).toLocaleString()}` : '· 执行中'}</p>}
        {error && <div className="access-note warning"><strong>提示</strong><span>{error}</span></div>}
      </section>

      <section className="server-detail-section">
        <h3>诊断项目</h3>
        <div className="server-diagnostics-grid">
          {checks.map(item=> (
            <div key={item.key} className="server-diagnostics-card" title={result ? item.detail : undefined}>
              <span>{item.label}</span>
              <span className={`server-diagnostics-status ${result ? item.state : ''}`}>{statusText(item.state)}</span>
              {result && item.detail ? <small className="muted">{item.detail}</small> : null}
            </div>
          ))}
        </div>
        {result ? (
          <details className="task-details" style={{marginTop:12}} open>
            <summary>详细输出</summary>
            <pre style={{whiteSpace:'pre-wrap', wordBreak:'break-all', maxHeight:420, overflow:'auto', background:'var(--surface-2)', padding:12, borderRadius:'var(--radius-sm)'}}>{JSON.stringify(result, null, 2)}</pre>
          </details>
        ) : <small className="muted">点击“开始完整诊断”后，这里会显示各项检查的详细输出</small>}
      </section>

      {entryTargets.length > 0 && (
        <section className="server-detail-section">
          <h3>入口探测</h3>
          <div className="server-entry-probe-list">
            {entryTargets.map((target:any, idx:number)=>{
              const probe = target?.tcp_self_probe || {}
              const available = !target?.error && probe.ok === true
              const label = target?.name || `${target?.host || ''}:${target?.port || ''}`
              return <div key={idx} className="server-entry-probe-row"><span>{label}</span><span className={available? 'ok':'fail'}>{available? '正常':'异常'}</span><small>{available && probe.duration_ms !== undefined ? `${probe.duration_ms} ms` : (target?.error || probe.error || '')}</small></div>
            })}
          </div>
        </section>
      )}
    </div>
  )
}
