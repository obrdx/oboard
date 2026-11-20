import { useEffect, useMemo, useRef, useState } from 'react'
import { buildAreaPath, buildLinePath } from '../../server-unified-chart'
import { formatBytes } from '../../shared/presentation'
import type { DashboardLiveTotals } from '../../dashboard-traffic'
import './dashboard-traffic.css'

export type DashboardTrafficDay = { date: string; upload_bytes: number; download_bytes: number }
export type DashboardUserTraffic = { user_id: number; username: string; nickname?: string; upload_bytes: number; download_bytes: number; traffic_limit_bytes: number }
export type DashboardTrafficData = { timezone?: string; days?: DashboardTrafficDay[]; users?: DashboardUserTraffic[]; user_count?: number }

const LIVE_SAMPLE_MS = 3000
const LIVE_MAX_SAMPLES = 120

function formatRate(bps: number) {
  return `${formatBytes(Math.max(0, bps))}/s`
}

/** Rounds `max` up to a 1/2/2.5/5 × 10^n step so four gridlines read cleanly. */
export function niceAxisMax(max: number, steps = 4): number {
  if (!(max > 0)) return steps
  const rough = max / steps
  const magnitude = 10 ** Math.floor(Math.log10(rough))
  const step = [1, 2, 2.5, 5, 10].map(m => m * magnitude).find(value => value >= rough) || 10 * magnitude
  return step * steps
}

/** Byte axes step in powers of 1024 so labels land on whole units. */
export function niceByteAxisMax(max: number, steps = 4): number {
  if (!(max > 0)) return steps
  let unit = 1
  while (max / unit >= 1024 && unit < 1024 ** 5) unit *= 1024
  return niceAxisMax(max / unit, steps) * unit
}

export function trailingAverage(values: number[], window = 7): number[] {
  return values.map((_, index) => {
    const slice = values.slice(Math.max(0, index - window + 1), index + 1)
    return slice.reduce((sum, value) => sum + value, 0) / slice.length
  })
}

function axisTicks(max: number, steps = 4) {
  return Array.from({ length: steps + 1 }, (_, i) => max * (steps - i) / steps)
}

export function DailyTrafficChart({ days, loading }: { days: DashboardTrafficDay[]; loading: boolean }) {
  const [hover, setHover] = useState<number | null>(null)
  const totals = days.map(day => Math.max(0, Number(day.upload_bytes) || 0) + Math.max(0, Number(day.download_bytes) || 0))
  const average = trailingAverage(totals)
  const max = niceByteAxisMax(Math.max(...totals, ...average, 0))
  const sum = totals.reduce((acc, value) => acc + value, 0)
  const hasData = sum > 0
  const avgPoints = average.map((value, index) => ({ x: ((index + 0.5) / Math.max(1, days.length)) * 100, y: 100 - (value / max) * 100 }))
  const active = hover !== null ? days[hover] : null
  const labelEvery = days.length > 16 ? 5 : 1

  return (
    <div className="dash-panel dash-chart-panel">
      <div className="dash-section-head">
        <div>
          <h2>近 30 天流量</h2>
        </div>
        <span className="dash-total">{hasData ? `合计 ${formatBytes(sum)}` : ''}</span>
      </div>
      {!hasData ? (
        <div className="dash-empty dash-chart-empty">{loading ? '正在加载…' : '近 30 天暂无流量记录'}</div>
      ) : (
        <>
          <div className="dash-chart" onMouseLeave={() => setHover(null)}>
            <div className="dash-chart-axis" aria-hidden="true">
              {axisTicks(max).map(tick => <span key={tick}>{formatBytes(tick)}</span>)}
            </div>
            <div className="dash-chart-plot">
              <div className="dash-chart-grid" aria-hidden="true">{axisTicks(max).map(tick => <i key={tick} />)}</div>
              <div className="dash-bars" role="list" aria-label="每日流量">
                {days.map((day, index) => {
                  const up = Math.max(0, Number(day.upload_bytes) || 0)
                  const down = Math.max(0, Number(day.download_bytes) || 0)
                  return (
                    <div
                      key={day.date}
                      role="listitem"
                      aria-label={`${day.date} ${formatBytes(up + down)}`}
                      className={`dash-bar${hover === index ? ' is-active' : ''}`}
                      onMouseEnter={() => setHover(index)}
                    >
                      <span className="dash-bar-stack" style={{ height: `${((up + down) / max) * 100}%` }}>
                        <i className="is-upload" style={{ flexGrow: up }} />
                        <i className="is-download" style={{ flexGrow: down }} />
                      </span>
                    </div>
                  )
                })}
              </div>
              <svg className="dash-chart-overlay" viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
                <path d={buildLinePath(avgPoints, true)} className="dash-avg-line" vectorEffect="non-scaling-stroke" />
              </svg>
              {active ? (
                <div className="dash-chart-tooltip" style={{ left: `${((hover! + 0.5) / days.length) * 100}%` }}>
                  <strong>{active.date}</strong>
                  <span>合计 {formatBytes(totals[hover!])}</span>
                  <span><em className="is-download" />下载 {formatBytes(active.download_bytes)}</span>
                  <span><em className="is-upload" />上传 {formatBytes(active.upload_bytes)}</span>
                  <span><em className="is-avg" />7 日均值 {formatBytes(average[hover!])}</span>
                </div>
              ) : null}
            </div>
            <div className="dash-chart-xaxis" aria-hidden="true">
              {days.map((day, index) => <span key={day.date}>{(days.length - 1 - index) % labelEvery === 0 ? day.date.slice(5) : ''}</span>)}
            </div>
          </div>
          <div className="dash-legend">
            <span><em className="is-download" />下载</span>
            <span><em className="is-upload" />上传</span>
            <span><em className="is-avg" />7 日均值</span>
          </div>
        </>
      )}
    </div>
  )
}

type LiveSample = { up: number; down: number; host: number }

export function LiveRateChart({ live, unknown }: { live: DashboardLiveTotals; unknown: boolean }) {
  const liveRef = useRef(live)
  liveRef.current = live
  const unknownRef = useRef(unknown)
  unknownRef.current = unknown
  const [samples, setSamples] = useState<LiveSample[]>([])

  useEffect(() => {
    const record = () => {
      if (unknownRef.current) return
      const current = liveRef.current
      setSamples(previous => [...previous.slice(-(LIVE_MAX_SAMPLES - 1)), { up: current.uploadBps, down: current.downloadBps, host: current.hostUploadBps + current.hostDownloadBps }])
    }
    record()
    const timer = window.setInterval(record, LIVE_SAMPLE_MS)
    return () => window.clearInterval(timer)
  }, [])

  const max = niceByteAxisMax(Math.max(0, ...samples.map(sample => Math.max(sample.up, sample.down, sample.host))))
  const paths = useMemo(() => {
    if (samples.length < 2) return null
    const span = LIVE_MAX_SAMPLES - 1
    const offset = span - (samples.length - 1)
    const toPoints = (key: keyof LiveSample) => samples.map((sample, index) => ({ x: ((offset + index) / span) * 100, y: 100 - (sample[key] / max) * 100 }))
    const down = toPoints('down')
    const up = toPoints('up')
    return { downLine: buildLinePath(down, true), downArea: buildAreaPath(down, 100, true), upLine: buildLinePath(up, true), upArea: buildAreaPath(up, 100, true), hostLine: buildLinePath(toPoints('host'), true) }
  }, [samples, max])
  const minutes = Math.round((LIVE_MAX_SAMPLES * LIVE_SAMPLE_MS) / 60000)

  return (
    <div className="dash-panel dash-chart-panel">
      <div className="dash-section-head">
        <div>
          <h2>代理速率</h2>
        </div>
      </div>
      <div className="dash-live-stats">
        <div>
          <small>实时连接</small>
          <strong>{unknown ? '—' : live.connections.toLocaleString('zh-CN')}</strong>
          <span>{live.reporting ? `TCP ${live.tcp.toLocaleString('zh-CN')} · UDP ${live.udp.toLocaleString('zh-CN')}` : '暂无在线服务器'}</span>
        </div>
        <div>
          <small><em className="is-download" />代理下载</small>
          <strong>{unknown ? '—' : formatRate(live.downloadBps)}</strong>
        </div>
        <div>
          <small><em className="is-upload" />代理上传</small>
          <strong>{unknown ? '—' : formatRate(live.uploadBps)}</strong>
        </div>
        <div className="dash-live-host" title="整块网卡的速率，含测速、主控同步等非代理流量">
          <small><em className="is-host" />主机网卡</small>
          <span>{unknown ? '—' : `↓ ${formatRate(live.hostDownloadBps)} · ↑ ${formatRate(live.hostUploadBps)}`}</span>
        </div>
      </div>
      <div className="dash-chart dash-chart-live">
        <div className="dash-chart-axis" aria-hidden="true">
          {axisTicks(max).map(tick => <span key={tick}>{formatRate(tick)}</span>)}
        </div>
        <div className="dash-chart-plot">
          <div className="dash-chart-grid" aria-hidden="true">{axisTicks(max).map(tick => <i key={tick} />)}</div>
          {paths ? (
            <svg className="dash-chart-overlay" viewBox="0 0 100 100" preserveAspectRatio="none" role="img" aria-label={`最近 ${minutes} 分钟代理速率`}>
              <path d={paths.downArea} className="dash-live-area is-download" />
              <path d={paths.upArea} className="dash-live-area is-upload" />
              <path d={paths.hostLine} className="dash-live-line is-host" vectorEffect="non-scaling-stroke" />
              <path d={paths.downLine} className="dash-live-line is-download" vectorEffect="non-scaling-stroke" />
              <path d={paths.upLine} className="dash-live-line is-upload" vectorEffect="non-scaling-stroke" />
            </svg>
          ) : (
            <div className="dash-live-waiting">正在记录…</div>
          )}
        </div>
        <div className="dash-chart-xaxis dash-live-xaxis" aria-hidden="true"><span>{minutes} 分钟前</span><span>现在</span></div>
      </div>
    </div>
  )
}

const RANK_SHADES = 6

export function UserTrafficRank({ users, userCount, totalBytes, loading, onOpen }: { users: DashboardUserTraffic[]; userCount: number; totalBytes: number; loading: boolean; onOpen: () => void }) {
  const rows = users.map(user => ({ ...user, total: Math.max(0, Number(user.upload_bytes) || 0) + Math.max(0, Number(user.download_bytes) || 0) }))
  const max = rows[0]?.total || 1
  const shown = rows.reduce((sum, row) => sum + row.total, 0)
  const total = Math.max(totalBytes, shown)
  const share = total > 0 ? Math.round((shown / total) * 100) : 0

  return (
    <div className="dash-panel">
      <div className="dash-section-head">
        <div>
          <h2>用户用量排行</h2>
        </div>
        {userCount > 0 && <span className="dash-count">{userCount}</span>}
      </div>
      {rows.length === 0 ? (
        <div className="dash-empty">{loading ? '正在加载…' : '当前账期暂无用户流量'}</div>
      ) : (
        <>
          <ul className="dash-list dash-user-rank">
            {rows.map(row => (
              <li key={row.user_id}>
                <button type="button" className="dash-list-row dash-user-row" onClick={onOpen}>
                  <span className="dash-user-name">
                    <strong>{row.nickname || row.username}</strong>
                    <small>↑ {formatBytes(row.upload_bytes)} · ↓ {formatBytes(row.download_bytes)}</small>
                  </span>
                  <span className="dash-user-bar"><i style={{ width: `${Math.max(2, (row.total / max) * 100)}%` }} /></span>
                  <span className="dash-rank-value">
                    {formatBytes(row.total)}
                    {row.traffic_limit_bytes > 0 ? <small> / {formatBytes(row.traffic_limit_bytes)}</small> : null}
                  </span>
                </button>
              </li>
            ))}
          </ul>
          <div className="dash-share">
            <div className="dash-share-bar" aria-hidden="true">
              {rows.map((row, index) => <i key={row.user_id} className={`shade-${Math.min(index, RANK_SHADES - 1)}`} style={{ width: `${(row.total / Math.max(1, total)) * 100}%` }} />)}
            </div>
            <div className="dash-share-meta">
              <span>前 {rows.length} 名占全部 {share}%</span>
              <span>全部合计 {formatBytes(total)}</span>
            </div>
          </div>
        </>
      )}
    </div>
  )
}
