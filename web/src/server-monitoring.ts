export type MonitoringSample = {
  available: boolean
  latency_ms: number | null
  sample_count: number
  success_count: number
  checked_at: string
}

// The latency scale a series is judged on. Controller derives it from the
// server region and the target province ("route"); targets without a known
// location use the region-independent scale ("fixed").
export type LatencyGrade = {
  excellent_ms: number
  fair_ms: number
  basis: 'route' | 'fixed'
}

export type MonitoringSeries = {
  probe_id: string
  label: string
  family?: 'ipv4' | 'ipv6'
  grade: LatencyGrade
  samples: MonitoringSample[]
}

export type MonitoringDisplay = {
  name: string
  enabled: boolean
  series: MonitoringSeries[]
}

export type LatencyChartStyle = 'sparkline' | 'bars'
export type QualityTone = 'ok' | 'fair' | 'poor' | ''

const fixedGrade: LatencyGrade = { excellent_ms: 80, fair_ms: 180, basis: 'fixed' }
const ROUNDS = 20

export function connectivityLatencyLabel(status: string, latency: number | null | undefined) {
  if (status !== 'available' || latency == null || !Number.isFinite(latency) || latency < 0) return '—'
  return latency < 1 ? '<1 ms' : `${Math.round(latency)} ms`
}

export function monitoringLoss(samples: MonitoringSample[]) {
  let sent = 0
  let received = 0
  for (const sample of samples) {
    if (!Number.isFinite(sample.sample_count) || sample.sample_count <= 0) continue
    sent += sample.sample_count
    received += Math.min(sample.sample_count, Math.max(0, sample.success_count))
  }
  return sent ? (sent - received) / sent * 100 : null
}

function validGrade(grade: LatencyGrade | undefined): LatencyGrade {
  if (!grade || !(grade.excellent_ms > 0) || !(grade.fair_ms >= grade.excellent_ms)) return fixedGrade
  return grade
}

export function latencyTone(latency: number | null | undefined, grade?: LatencyGrade): QualityTone {
  if (latency == null || !Number.isFinite(latency) || latency < 0) return ''
  const scale = validGrade(grade)
  return latency <= scale.excellent_ms ? 'ok' : latency <= scale.fair_ms ? 'fair' : 'poor'
}

export function lossTone(loss: number | null): QualityTone {
  return loss == null ? '' : loss === 0 ? 'ok' : loss < 10 ? 'fair' : 'poor'
}

export function latencyGradeTitle(grade: LatencyGrade | undefined) {
  const scale = validGrade(grade)
  const basis = scale.basis === 'route' ? '按服务器地区与目标省份' : '通用标准'
  return `${basis}：≤ ${scale.excellent_ms} ms 优秀，≤ ${scale.fair_ms} ms 一般，更高为较差`
}

export type MonitoringSeriesView = {
  key: string
  label: string
  status: 'offline' | 'disabled' | 'pending' | 'available' | 'unavailable'
  latency: string
  latencyTone: QualityTone
  loss: number | null
  lossTone: QualityTone
  // Latest ROUNDS readings, oldest first, padded at the front with null. A
  // failed round is also null so the chart breaks instead of dropping to zero.
  points: (number | null)[]
  qualitySegments: QualityTone[]
  lossSegments: QualityTone[]
  grade: LatencyGrade
  gradeTitle: string
}

function pad<T>(values: T[], fill: T) {
  return [...Array.from({ length: Math.max(0, ROUNDS - values.length) }, () => fill), ...values]
}

function seriesView(series: MonitoringSeries | undefined, label: string, online: boolean, enabled: boolean): MonitoringSeriesView {
  const samples = enabled && series ? series.samples.slice(-ROUNDS) : []
  const latest = samples[samples.length - 1]
  const grade = validGrade(series?.grade)
  const status = !online ? 'offline' : !enabled ? 'disabled' : !latest ? 'pending' : latest.available ? 'available' : 'unavailable'
  const loss = online && enabled ? monitoringLoss(samples) : null
  const usable = (sample: MonitoringSample) => sample.available && sample.latency_ms != null && Number.isFinite(sample.latency_ms) && sample.latency_ms >= 0
  return {
    key: series?.probe_id || label,
    label: series?.label || label,
    status,
    latency: connectivityLatencyLabel(status, latest?.latency_ms),
    latencyTone: status === 'available' ? latencyTone(latest?.latency_ms, grade) : status === 'unavailable' ? 'poor' : '',
    loss,
    lossTone: lossTone(loss),
    points: pad(samples.map(sample => usable(sample) ? sample.latency_ms : null), null),
    qualitySegments: pad(samples.map(sample => sample.available ? latencyTone(sample.latency_ms, grade) : 'poor'), ''),
    lossSegments: pad(samples.map(sample => lossTone(monitoringLoss([sample]))), ''),
    grade,
    gradeTitle: latencyGradeTitle(grade),
  }
}

export function serverMonitoring(display: MonitoringDisplay | undefined, online: boolean) {
  const name = display?.name || '公网探测'
  const enabled = Boolean(display?.enabled)
  const series = enabled ? (display?.series || []) : []
  const views = series.length ? series.map(item => seriesView(item, name, online, enabled)) : [seriesView(undefined, name, online, enabled)]
  return { name, status: views[0].status, series: views }
}

// sparklinePaths draws the latest rounds into a width × height box. The scale
// starts at zero and reaches at least the excellent ceiling, so a flat line
// near the top means "close to the limit", not merely "the highest reading".
export function sparklinePaths(points: (number | null)[], grade: LatencyGrade, width = 100, height = 24) {
  const values = points.filter((value): value is number => value != null)
  if (!values.length) return { line: '', area: '' }
  const top = Math.max(Math.max(...values) * 1.15, validGrade(grade).excellent_ms * 0.5, 1)
  const step = points.length > 1 ? width / (points.length - 1) : width
  const y = (value: number) => (height - 1) - (Math.min(value, top) / top) * (height - 3)
  let line = ''
  let area = ''
  let run: [number, number][] = []
  const flush = () => {
    if (!run.length) return
    // A lone reading still gets a short visible stroke.
    const segment = run.length === 1 ? [[Math.max(0, run[0][0] - step / 2), run[0][1]], [Math.min(width, run[0][0] + step / 2), run[0][1]]] as [number, number][] : run
    const d = segment.map(([px, py], index) => `${index ? 'L' : 'M'}${px.toFixed(2)} ${py.toFixed(2)}`).join(' ')
    line += `${d} `
    area += `${d} L${segment[segment.length - 1][0].toFixed(2)} ${height} L${segment[0][0].toFixed(2)} ${height} Z `
    run = []
  }
  points.forEach((value, index) => {
    if (value == null) flush()
    else run.push([index * step, y(value)])
  })
  flush()
  return { line: line.trim(), area: area.trim() }
}
