import { useState } from 'react'
import { useCoalescedReadRequest } from './request-coalesce'
import { Select } from './components/ui/select'
import { applyAuditChange, prepareAuditChange, type AuditClient } from './audit-changes'

type Diagnostic = { scope: 'user' | 'node'; id: number; until: string }
type Collection = { mode: 'light' | 'standard'; revision: number; diagnostics: Diagnostic[] }
export function AuditCollectionSettings({ client }: { client: AuditClient }) {
  const [config, setConfig] = useState<Collection | null>(null)
  const [revision, refresh] = useState(0)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [scope, setScope] = useState<'user' | 'node'>('user')
  const [target, setTarget] = useState('')
  const [minutes, setMinutes] = useState(15)
  const [prepared, setPrepared] = useState<{ id: string; reason: string } | null>(null)
  useCoalescedReadRequest(`audit-collection:${revision}`, signal => client.request('/audit/collection', { signal }), {
    onSuccess: (value: Collection) => { setConfig(value); setPrepared(null) },
    onError: () => setError('无法读取采集配置，请检查管理员权限或稍后重试'),
  })
  const mutate = (next: Collection) => { setConfig(next); setPrepared(null); setError('') }
  const prepare = async () => {
    if (!config || busy) return
    setBusy(true); setError('')
    try {
      const reason = `调整采集档位为${config.mode === 'light' ? '轻量' : '标准'}，临时诊断${config.diagnostics.length}项；不修改账号权限与风险阈值`
      const result = await prepareAuditChange(client, 'audit.collection.update', config, reason, crypto.randomUUID())
      setPrepared({ id: result.id, reason })
    } catch (e) { setError(e instanceof Error ? e.message : '校验失败') } finally { setBusy(false) }
  }
  const apply = async () => {
    if (!prepared || busy) return
    setBusy(true); setError('')
    try { await applyAuditChange(client, prepared.id, prepared.reason); refresh(n => n + 1) }
    catch (e) { setError(e instanceof Error ? e.message : '保存失败') } finally { setBusy(false) }
  }
  const targetId = Number(target)
  const canAdd = !!config && !busy && config.diagnostics.length < 8 && Number.isSafeInteger(targetId) && targetId > 0 && minutes >= 1 && minutes <= 60
  return <section className="audit-config-section" aria-labelledby="audit-collection-title">
    <header className="audit-config-head"><div><h3 id="audit-collection-title">采集档位</h3><p>采集内容与判定敏感度分别管理。未采集的历史明细无法事后补回。</p></div></header>
    {error && <p role="alert" className="form-error">{error}</p>}
    {!config ? <p role="status" className="audit-config-muted">正在读取采集配置…</p> : <>
      <div className="audit-config-row">
        <div className="audit-config-row-text"><strong>常驻档位</strong><span>{config.mode === 'light' ? '保留基础统计、来源活动和关键事件，不持续保存连接来源明细。' : '在基础统计之外保留短期连接来源明细，并在异常出现时保存相关证据。'}</span></div>
        <Select variant="segmented" aria-label="常驻采集档位" value={config.mode} disabled={busy} onChange={e => mutate({ ...config, mode: e.target.value as Collection['mode'] })}><option value="light">轻量采集</option><option value="standard">标准采集</option></Select>
      </div>
      <div className="audit-config-group">
        <div className="audit-config-group-head"><strong>临时诊断</strong><span>{config.diagnostics.length} / 8</span></div>
        <p className="audit-config-muted">为指定账号或节点临时增加采集内容，到期后恢复原设置。每项最多 60 分钟；不改变授权、配额或自动处置规则。</p>
        {config.diagnostics.length ? <ul className="audit-config-list">{config.diagnostics.map((d, i) => <li key={`${d.scope}:${d.id}`}><span>{d.scope === 'user' ? '账号' : '节点'} #{d.id}</span><time>截止 {d.until}</time><button type="button" className="ghost" disabled={busy} onClick={() => mutate({ ...config, diagnostics: config.diagnostics.filter((_, j) => i !== j) })}>结束</button></li>)}</ul> : <p className="audit-config-empty">当前没有临时诊断</p>}
        <div className="audit-diagnostic-form">
          <label>范围<Select aria-label="诊断范围" value={scope} onChange={e => setScope(e.target.value as 'user' | 'node')}><option value="user">指定账号</option><option value="node">指定节点</option></Select></label>
          <label>对象 ID<input aria-label="诊断对象 ID" type="number" min="1" step="1" value={target} onChange={e => setTarget(e.target.value)} /></label>
          <label>时长（分钟）<input aria-label="诊断分钟" type="number" min="1" max="60" value={minutes} onChange={e => setMinutes(Number(e.target.value))} /></label>
          <button type="button" className="ghost" disabled={!canAdd} onClick={() => { mutate({ ...config, diagnostics: [...config.diagnostics, { scope, id: targetId, until: new Date(Date.now() + minutes * 60000).toISOString() }] }); setTarget('') }}>加入诊断</button>
        </div>
      </div>
      {prepared && <p role="status" className="audit-config-callout">变更已校验：{prepared.reason}。诊断到期时间不会因重试延长。</p>}
      <footer className="audit-config-actions">{prepared ? <button type="button" disabled={busy} onClick={() => void apply()}>{busy ? '正在应用…' : '确认应用采集配置'}</button> : <button type="button" disabled={busy} onClick={() => void prepare()}>{busy ? '正在校验…' : '校验采集配置'}</button>}</footer>
    </>}
  </section>
}
