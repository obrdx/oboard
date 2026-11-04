import { describe, expect, it } from 'vitest'
import { connectivityLatencyLabel, latencyTone, monitoringLoss, serverMonitoring, sparklinePaths, type LatencyGrade, type MonitoringSample, type MonitoringSeries } from './server-monitoring'

const sample = (overrides: Partial<MonitoringSample> = {}): MonitoringSample => ({ available: true, latency_ms: 0, sample_count: 3, success_count: 3, checked_at: '2026-09-08T00:00:00Z', ...overrides })
const fixed: LatencyGrade = { excellent_ms: 80, fair_ms: 180, basis: 'fixed' }
const series = (samples: MonitoringSample[], overrides: Partial<MonitoringSeries> = {}): MonitoringSeries => ({ probe_id: 'public-cloudflare', label: '公网探测', grade: fixed, samples, ...overrides })

describe('server monitoring targets', () => {
  it('shows sub-millisecond successes as 1 ms without mistaking absent or failed readings for zero', () => {
    expect(connectivityLatencyLabel('available', 0)).toBe('1 ms')
    expect(connectivityLatencyLabel('available', 0.3)).toBe('1 ms')
    expect(connectivityLatencyLabel('available', 0.6)).toBe('1 ms')
    expect(connectivityLatencyLabel('available', 1)).toBe('1 ms')
    for (const value of [null, undefined, NaN, -1]) expect(connectivityLatencyLabel('available', value)).toBe('—')
    expect(connectivityLatencyLabel('unavailable', 0)).toBe('—')
    const card = serverMonitoring({ name: '公网探测', enabled: true, series: [series([sample()])] }, true)
    expect(card.series[0].latency).toBe('1 ms')
    expect(card.series[0].qualitySegments.at(-1)).toBe('ok')
  })

  it('weights partial packet loss by sent packets, not by successful rounds', () => {
    const samples = [sample({ sample_count: 10, success_count: 8 }), sample({ sample_count: 2, success_count: 1 })]
    expect(monitoringLoss(samples)).toBe(25)
    const card = serverMonitoring({ name: '网站', enabled: true, series: [series(samples)] }, true)
    expect(card.name).toBe('网站')
    expect(card.series[0].loss).toBe(25)
    expect(card.series[0].lossTone).toBe('poor')
    expect(card.series[0].lossSegments.slice(-2)).toEqual(['poor', 'poor'])
  })

  it('uses only the selected target latest 20 rounds and never falls back for offline or disabled targets', () => {
    const display = { name: '任务 A', enabled: true, series: [series([sample({ available: false, success_count: 0 }), ...Array.from({ length: 20 }, () => sample({ latency_ms: 47 }))])] }
    expect(serverMonitoring(display, true).series[0].loss).toBe(0)
    expect(serverMonitoring(display, true).series[0].latency).toBe('47 ms')
    expect(serverMonitoring(display, true).series[0].qualitySegments).toHaveLength(20)
    expect(serverMonitoring(display, true).series[0].points).toHaveLength(20)
    for (const card of [serverMonitoring(display, false), serverMonitoring({ ...display, enabled: false }, true), serverMonitoring(undefined, true)]) {
      expect(card.series).toHaveLength(1)
      expect(card.series[0].latency).toBe('—')
      expect(card.series[0].loss).toBeNull()
    }
    expect(serverMonitoring({ ...display, series: [] }, true).status).toBe('pending')
  })

  it('keeps one row per target, such as the IPv4 and IPv6 address of a carrier preset', () => {
    const grade: LatencyGrade = { excellent_ms: 165, fair_ms: 270, basis: 'route' }
    const card = serverMonitoring({ name: '广东 · 中国移动', enabled: true, series: [
      series([sample({ latency_ms: 50, sample_count: 10, success_count: 4 })], { probe_id: 't1-gd-cm-v4', label: '移动', family: 'ipv4', grade }),
      series([sample({ latency_ms: 51 })], { probe_id: 't1-gd-cm-v6', label: '移动v6', family: 'ipv6', grade }),
    ] }, true)
    expect(card.series.map(item => [item.label, item.latency, item.loss])).toEqual([['移动', '50 ms', 60], ['移动v6', '51 ms', 0]])
    expect(card.series[0].gradeTitle).toContain('按服务器地区与目标省份')
  })

  it('grades latency on the scale of the series, not one global threshold', () => {
    const fromUS: LatencyGrade = { excellent_ms: 165, fair_ms: 270, basis: 'route' }
    const fromHK: LatencyGrade = { excellent_ms: 35, fair_ms: 75, basis: 'route' }
    expect(latencyTone(160, fromUS)).toBe('ok')
    expect(latencyTone(160, fromHK)).toBe('poor')
    expect(latencyTone(60, fromHK)).toBe('fair')
    expect(latencyTone(60, undefined)).toBe('ok')
    expect(latencyTone(null, fromUS)).toBe('')
  })

  it('breaks the sparkline at failed rounds instead of drawing them as zero', () => {
    const { line, area } = sparklinePaths([null, 40, 42, null, 41, 43], fixed)
    expect(line.match(/M/g)).toHaveLength(2)
    expect(area.match(/Z/g)).toHaveLength(2)
    expect(sparklinePaths([null, null], fixed)).toEqual({ line: '', area: '' })
  })
})
