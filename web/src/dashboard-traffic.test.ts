import { describe, expect, it } from 'vitest'

import { dashboardLiveTotals, dashboardServerTrafficBytes } from './dashboard-traffic'

describe('dashboardServerTrafficBytes', () => {
  it('aggregates the current-period traffic reported by every server', () => {
    expect(dashboardServerTrafficBytes([
      { traffic_upload_bytes: 1_024, traffic_download_bytes: 2_048 },
      { traffic_upload_bytes: 4_096, traffic_download_bytes: 8_192 },
    ])).toBe(15_360)
  })

  it('treats missing or invalid counters as zero', () => {
    expect(dashboardServerTrafficBytes([
      {},
      { traffic_upload_bytes: '512', traffic_download_bytes: Number.NaN },
      { traffic_upload_bytes: -1, traffic_download_bytes: Number.POSITIVE_INFINITY },
    ])).toBe(512)
  })
})

describe('dashboardLiveTotals', () => {
  it('sums live connections and throughput from online servers only', () => {
    expect(dashboardLiveTotals([
      { status: 'online', tcp_connection_count: 120, udp_connection_count: 8, network_upload_bps: 1_000, network_download_bps: 4_000 },
      { status: 'ONLINE', tcp_connection_count: '30', udp_connection_count: 2, network_upload_bps: 500, network_download_bps: 1_500 },
      { status: 'offline', tcp_connection_count: 999, udp_connection_count: 999, network_upload_bps: 9_999, network_download_bps: 9_999 },
    ])).toEqual({ reporting: 2, tcp: 150, udp: 10, connections: 160, uploadBps: 1_500, downloadBps: 5_500 })
  })

  it('ignores missing or invalid live counters', () => {
    expect(dashboardLiveTotals([
      { status: 'online' },
      { status: 'online', tcp_connection_count: -5, udp_connection_count: Number.NaN, network_upload_bps: Number.POSITIVE_INFINITY },
    ])).toEqual({ reporting: 2, tcp: 0, udp: 0, connections: 0, uploadBps: 0, downloadBps: 0 })
  })
})
