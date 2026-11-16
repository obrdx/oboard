export type DashboardTrafficServer = {
  traffic_upload_bytes?: unknown
  traffic_download_bytes?: unknown
}

export type DashboardLiveServer = {
  status?: unknown
  tcp_connection_count?: unknown
  udp_connection_count?: unknown
  network_upload_bps?: unknown
  network_download_bps?: unknown
}

export type DashboardLiveTotals = {
  reporting: number
  tcp: number
  udp: number
  connections: number
  uploadBps: number
  downloadBps: number
}

function trafficBytes(value: unknown): number {
  const bytes = Number(value)
  return Number.isFinite(bytes) && bytes > 0 ? bytes : 0
}

export function dashboardServerTrafficBytes(servers: DashboardTrafficServer[]): number {
  return servers.reduce((total, server) => (
    total
      + trafficBytes(server.traffic_upload_bytes)
      + trafficBytes(server.traffic_download_bytes)
  ), 0)
}

export function dashboardLiveTotals(servers: DashboardLiveServer[]): DashboardLiveTotals {
  const totals: DashboardLiveTotals = { reporting: 0, tcp: 0, udp: 0, connections: 0, uploadBps: 0, downloadBps: 0 }
  for (const server of servers) {
    if (String(server.status || '').toLowerCase() !== 'online') continue
    totals.reporting++
    totals.tcp += Math.round(trafficBytes(server.tcp_connection_count))
    totals.udp += Math.round(trafficBytes(server.udp_connection_count))
    totals.uploadBps += trafficBytes(server.network_upload_bps)
    totals.downloadBps += trafficBytes(server.network_download_bps)
  }
  totals.connections = totals.tcp + totals.udp
  return totals
}
