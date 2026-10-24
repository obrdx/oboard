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
        performance: [{ role: 'core', wait_count: 0, wait_duration_ms: 0, open_connections: 2, in_use: 0, busy_errors: 0, locked_errors: 0, operations: [] }],
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
  } finally {
    await act(async () => root.unmount())
    vi.unstubAllGlobals()
  }
})
