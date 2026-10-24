// @vitest-environment jsdom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
import { DatabaseSettingsPanel } from './DatabaseSettings'

it('shows separate core and history health plus table space', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const data = {
    settings: {
      server_metric_hourly_retention_days: 90,
      server_metric_daily_retention_days: 365,
      latency_rollup_retention_days: 30,
      storage_diagnostics: {
        history_available: true,
        table_space_available: true,
        core: { role: 'core', status: 'normal', db_bytes: 1024, wal_bytes: 256, shm_bytes: 0, growth_24h_bytes: 128, page_count: 1, freelist_count: 0 },
        history: { role: 'history', status: 'normal', db_bytes: 2048, wal_bytes: 512, shm_bytes: 0, growth_24h_bytes: 256, page_count: 2, freelist_count: 0 },
        tables: [{ database: 'history', table: 'server_metric_samples', data_bytes: 2048, index_bytes: 512, row_count: 12, exact: false, growth_24h_bytes: 256 }],
        performance: [{ role: 'core', wait_count: 0, wait_duration_ms: 0, open_connections: 2, in_use: 0, busy_errors: 0, locked_errors: 0, operations: [], traffic_ledger: { count: 3, sample_count: 2, window_start: '2026-10-25T00:00:00Z', window_end: '2026-10-25T00:01:00Z', stages: [{ stage: 'connection_wait', count: 2, avg_ms: 1, p50_ms: 1, p95_ms: 2, p99_ms: 2, max_ms: 2 }], reports: 4, streams: 2, users: 2, periods: 2, accepted: 4, duplicate: 0, covered: 0, rejected: 0, queries: 8, writes: 16, failures: 0, timeouts: 0, canceled: 0, max_reports: 2 } }],
      },
    },
  }
  const host = document.createElement('div')
  const root = createRoot(host)
  try {
    await act(async () => root.render(<DatabaseSettingsPanel client={{ request: vi.fn() } as any} notify={vi.fn()} data={data} />))
    expect(host.textContent).toContain('核心数据库')
    expect(host.textContent).toContain('历史数据库')
    expect(host.textContent).toContain('server_metric_samples')
    expect(host.textContent).toContain('保留策略')
    expect(host.textContent).toContain('流量账本事务性能')
    expect(host.textContent).toContain('最近 2 批')
    expect(host.textContent).toContain('获取写连接')
    expect(host.textContent).toContain('低样本量的 P99')
  } finally {
    await act(async () => root.unmount())
    vi.unstubAllGlobals()
  }
})
