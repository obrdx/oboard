import { useEffect, useState } from 'react'
import { AlertTriangle, CheckCircle2, Database, HardDrive, RefreshCw } from 'lucide-react'
import type { APIClient } from '../../shared/api-client'

type Health = {
  role: 'core' | 'history'
  status: 'normal' | 'error' | 'maintenance'
  path: string
  schema_version?: number
  db_bytes: number
  wal_bytes: number
  shm_bytes: number
  page_count?: number
  page_size?: number
  freelist_count?: number
  growth_24h_bytes: number
  last_maintenance_at?: string
  last_maintenance?: Record<string, unknown>
  checkpoint?: Record<string, unknown>
  last_error?: string
  opened_at?: string
}

type TableSpace = {
  database: string
  table: string
  data_bytes: number
  index_bytes: number
  row_count: number
  exact: boolean
  growth_24h_bytes: number
}

type Operation = {
 sample_count?: number
 p50_ms?: number
 window_start?: string
 window_end?: string
  role: string
  kind: string
  count: number
  avg_ms: number
  p95_ms: number
  p99_ms: number
  max_ms: number
}

type LedgerStage = { stage: string; count: number; avg_ms: number; p50_ms: number; p95_ms: number; p99_ms: number; max_ms: number }
type LedgerPerformance = {
 count: number; sample_count: number; window_start: string; window_end: string; stages: LedgerStage[]
 reports: number; streams: number; users: number; periods: number; accepted: number; duplicate: number; covered: number; rejected: number
 queries: number; writes: number; failures: number; timeouts: number; canceled: number; max_reports: number
}
const ledgerStageLabels: Record<string, string> = {
 connection_wait: '获取写连接', begin_duration: 'BEGIN IMMEDIATE', stream_duration: 'Stream 处理', report_duration: '报告入账',
 sql_duration: '事务内 SQL', transaction_duration: '事务持有', finalize_duration: '累计及修订更新', commit_duration: 'COMMIT', rollback_duration: 'ROLLBACK', total_duration: '完整操作',
}
type Performance = {
 traffic_ledger?: LedgerPerformance
  role: string
  wait_count: number
  wait_duration_ms: number
  open_connections: number
  in_use: number
  busy_errors: number
  locked_errors: number
  operations?: Operation[]
}

type Diagnostics = {
  history_available?: boolean
  table_space_available?: boolean
  table_space_pending?: boolean
  core?: Health
  history?: Health
  tables?: TableSpace[]
  performance?: Performance[]
  analyzed_at?: string
}

function bytes(value: number) {
  const n = Number(value || 0)
  if (!Number.isFinite(n) || n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = n
  let unit = 0
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024
    unit++
  }
  return `${size >= 100 || unit === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[unit]}`
}

function growth(value: number) {
  const n = Number(value || 0)
  if (n === 0) return '持平'
  return `${n > 0 ? '+' : ''}${bytes(Math.abs(n))}`
}

function time(value?: string) {
  if (!value) return '—'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? '—' : parsed.toLocaleString()
}

function HealthCard({ health }: { health?: Health }) {
  if (!health) return null
  const ok = health.status === 'normal'
  return <section className="settings-card">
    <div className="settings-card-head">
      <div className="settings-heading">
        <h3>{health.role === 'history' ? '历史数据库' : '核心数据库'}</h3>
        <p>{health.role === 'history' ? '资源采样、延迟聚合与连接审计历史。' : '授权、计费、用户、配置与 Agent 运行状态。'}</p>
      </div>
      <span className={`status-pill ${ok ? 'ok' : health.status === 'maintenance' ? 'warning' : 'danger'}`}>{ok ? '正常' : health.status === 'maintenance' ? '维护中' : '异常'}</span>
    </div>
    <div className="controller-update-meta">
      <span>数据文件<strong>{bytes(health.db_bytes)}</strong></span>
      <span>WAL<strong>{bytes(health.wal_bytes)}</strong></span>
      <span>空闲页<strong>{health.freelist_count || 0} / {health.page_count || 0}</strong></span>
      <span>24h 增长<strong>{growth(health.growth_24h_bytes)}</strong></span>
    </div>
    <div className="backup-settings-summary" style={{ marginTop: 12 }}>
      <span className="backup-settings-summary-icon"><HardDrive size={18} /></span>
      <div>
        <strong>{health.schema_version ? `结构版本 ${health.schema_version}` : 'SQLite 当前结构'} · {health.role === 'history' ? '独立 WAL' : '核心 WAL'}</strong>
        <span>最近维护 {time(health.last_maintenance_at)}{health.opened_at ? ` · 本次打开 ${time(health.opened_at)}` : ''}</span>
        <small>{health.last_error ? `异常：${health.last_error}` : health.path}</small>
      </div>
    </div>
  </section>
}

export function DatabaseSettingsPanel({ client, notify, data }: { client: APIClient; notify: (message: string) => void; data: any }) {
  const diagnostics: Diagnostics = data?.settings?.storage_diagnostics || {}
  const settings = data?.settings || {}
  const [hourlyDays, setHourlyDays] = useState(Number(settings.server_metric_hourly_retention_days || 90))
  const [dailyDays, setDailyDays] = useState(Number(settings.server_metric_daily_retention_days || 365))
  const [latencyDays, setLatencyDays] = useState(Number(settings.latency_rollup_retention_days || 30))
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    setHourlyDays(Number(settings.server_metric_hourly_retention_days || 90))
    setDailyDays(Number(settings.server_metric_daily_retention_days || 365))
    setLatencyDays(Number(settings.latency_rollup_retention_days || 30))
  }, [settings.server_metric_hourly_retention_days, settings.server_metric_daily_retention_days, settings.latency_rollup_retention_days])

  const save = async () => {
    setSaving(true)
    try {
      await client.request('/settings', {
        method: 'POST',
        body: JSON.stringify({
          server_metric_hourly_retention_days: hourlyDays,
          server_metric_daily_retention_days: dailyDays,
          latency_rollup_retention_days: latencyDays,
        }),
      })
      notify('数据库保留策略已保存')
    } catch (error) {
      notify(error instanceof Error ? error.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  const performance = diagnostics.performance || []
 const ledger = performance.find(item => item.role === 'core')?.traffic_ledger
  const tables = diagnostics.tables || []
  return <div className="database-settings">
    <section className="settings-card">
      <div className="settings-card-head">
        <div className="settings-heading"><h3>数据库概览</h3><p>核心业务与历史数据分别使用独立的 SQLite 文件和 WAL。</p></div>
        <span className="status-pill">{diagnostics.analyzed_at ? `分析于 ${time(diagnostics.analyzed_at)}` : '等待采样'}</span>
      </div>
      <div className="controller-update-meta">
        <span>连接池等待<strong>{performance.reduce((sum, item) => sum + Number(item.wait_count || 0), 0)}</strong></span>
        <span>Busy / Locked<strong>{performance.reduce((sum, item) => sum + Number(item.busy_errors || 0), 0)} / {performance.reduce((sum, item) => sum + Number(item.locked_errors || 0), 0)}</strong></span>
        <span>历史服务<strong>{diagnostics.history_available ? '可用' : '不可用'}</strong></span>
      </div>
    </section>
    <div className="database-health-grid"><HealthCard health={diagnostics.core} /><HealthCard health={diagnostics.history} /></div>
    <section className="settings-card">
      <div className="settings-card-head"><div className="settings-heading"><h3>数据表空间</h3><p>按文件占用排序；行数为 rowid 估算值，不执行全表 COUNT。</p></div>{diagnostics.table_space_available || diagnostics.table_space_pending ? <Database size={18} /> : <AlertTriangle size={18} />}</div>
      {diagnostics.table_space_pending ? <div className="muted" role="status">正在后台统计数据表空间，稍后刷新查看。</div> : !diagnostics.table_space_available ? <div className="backup-inline-error" role="status">当前 SQLite 构建不提供 dbstat，仅显示文件级指标。</div> :
        <div className="table-wrap"><table><thead><tr><th>数据库</th><th>数据表</th><th>数据</th><th>索引</th><th>行数</th><th>24h 增长</th></tr></thead><tbody>
          {tables.map(item => <tr key={`${item.database}-${item.table}`}><td>{item.database === 'history' ? '历史' : '核心'}</td><td><code>{item.table}</code></td><td>{bytes(item.data_bytes)}</td><td>{bytes(item.index_bytes)}</td><td>≈ {item.row_count}</td><td>{growth(item.growth_24h_bytes)}</td></tr>)}
        </tbody></table></div>}
    </section>
    <section className="settings-card">
      <div className="settings-card-head"><div className="settings-heading"><h3>性能诊断</h3><p>各操作耗时统计来自最近最多 512 个样本；累计次数为进程启动至今。</p></div><RefreshCw size={18} /></div>
      <div className="table-wrap"><table><thead><tr><th>数据库</th><th>操作</th><th>累计/样本</th><th>平均</th><th>P50</th><th>P95</th><th>P99</th><th>最大</th></tr></thead><tbody>
        {performance.flatMap(item => (item.operations || []).map(op => <tr key={`${item.role}-${op.kind}`}><td>{item.role === 'history' ? '历史' : '核心'}</td><td>{op.kind}</td><td title={op.window_start && op.window_end ? new Date(op.window_start).toLocaleString() + ' — ' + new Date(op.window_end).toLocaleString() : undefined}>{op.count} / {op.sample_count ?? '—'}</td><td>{op.avg_ms.toFixed(2)} ms</td><td>{(op.p50_ms ?? 0).toFixed(2)} ms</td><td>{op.p95_ms.toFixed(2)} ms</td><td>{op.p99_ms.toFixed(2)} ms</td><td>{op.max_ms.toFixed(2)} ms</td></tr>))}
      </tbody></table></div>
      <small className="muted">Go 连接池等待与 SQLite Busy/Locked 分开统计；写事务持有时间需结合维护结果与锁等待判断。</small>
    </section>
    {ledger && <section className="settings-card">
      <div className="settings-card-head"><div className="settings-heading"><h3>流量账本事务性能</h3><p>进程累计 {ledger.count} 批；以下统计来自最近 {ledger.sample_count} 批（最多 512 批）。</p></div></div>
      {ledger.sample_count > 0 ? <>
        <p className="muted">{new Date(ledger.window_start).toLocaleString()} — {new Date(ledger.window_end).toLocaleString()}</p>
        <div className="table-wrap"><table className="tabular-nums"><thead><tr><th>阶段</th><th>样本</th><th>平均</th><th>P50</th><th>P95</th><th>P99</th><th>最大</th></tr></thead><tbody>
          {ledger.stages.map(stage => <tr key={stage.stage}><td>{ledgerStageLabels[stage.stage] || stage.stage}</td><td>{stage.count}</td><td>{stage.avg_ms.toFixed(2)} ms</td><td>{stage.p50_ms.toFixed(2)} ms</td><td>{stage.p95_ms.toFixed(2)} ms</td><td>{stage.p99_ms.toFixed(2)} ms</td><td>{stage.max_ms.toFixed(2)} ms</td></tr>)}
        </tbody></table></div>
        <p className="muted">报告 {ledger.reports} · Stream {ledger.streams} · 用户/周期累计 {ledger.users}/{ledger.periods} · 最大批量 {ledger.max_reports}</p>
        <p className="muted">接受 {ledger.accepted} · 重复 {ledger.duplicate} · 覆盖 {ledger.covered} · 拒绝 {ledger.rejected} · SQL 查询/写入 {ledger.queries}/{ledger.writes}</p>
        <p className="muted">失败 {ledger.failures} · 超时 {ledger.timeouts} · 取消 {ledger.canceled}。结果计数为处理观测，失败批次全部回滚。获取连接耗时不等于 SQLite 内部锁等待；低样本量的 P99 仅供观察。</p>
      </> : <p className="muted">尚无流量账本事务样本。</p>}
    </section>}
    <section className="settings-card">
      <div className="settings-card-head"><div className="settings-heading"><h3>保留策略</h3><p>原始数据先聚合、校验游标后再清理；聚合失败时保留原始记录。</p></div><CheckCircle2 size={18} /></div>
      <div className="settings-form">
        <div className="form-field"><span>资源原始数据（天）</span><input type="number" min={1} max={30} value={Number(settings.server_monitoring_retention_days || 7)} readOnly /><small>沿用现有监控保留设置。</small></div>
        <div className="form-field"><span>资源小时聚合（天）</span><input type="number" min={7} max={180} value={hourlyDays} onChange={event => setHourlyDays(Number(event.target.value))} /></div>
        <div className="form-field"><span>资源每日聚合（天）</span><input type="number" min={30} max={730} value={dailyDays} onChange={event => setDailyDays(Number(event.target.value))} /></div>
        <div className="form-field"><span>延迟 5 分钟聚合（天）</span><input type="number" min={7} max={365} value={latencyDays} onChange={event => setLatencyDays(Number(event.target.value))} /><small>小时聚合固定保留 365 天。</small></div>
        <div className="form-field"><span>连接审计原始数据（天）</span><input type="number" min={1} max={30} value={Number(settings.connection_audit_retention_days || 2)} readOnly /><small>小时汇总至少保留 30 天。</small></div>
      </div>
      <div className="settings-actions"><button type="button" onClick={() => void save()} disabled={saving}>{saving ? '保存中…' : '保存保留策略'}</button></div>
    </section>
  </div>
}
