import { describe, expect, it } from 'vitest'
import { summarizeNetworkDiagnostics } from './networkDiagnosticsSummary'

const ok = (output: string) => ({ available: true, ok: true, output })

function healthyResult(overrides: Record<string, unknown> = {}) {
  return {
    controller: { transport: 'agentlink', tcp: { ok: true, duration_ms: 46 } },
    entry_targets: [],
    commands: {
      public_ipv4: ok('45.143.128.5\n'),
      public_ipv6: ok('2604:a840:101:4::a\n'),
      ip_route_v4: ok('default via 45.143.128.254 dev eth0 proto static \n'),
      ip_route_v6: ok('default via 2604:a840:101::abcd:1 dev eth0 proto static metric 1024 onlink pref medium\n'),
      ss_tcp_listen: ok('tcp   LISTEN 0 128 0.0.0.0:22 0.0.0.0:*\ntcp   LISTEN 0 4096 [::]:443 [::]:*\n'),
      ss_udp_listen: ok(''),
      iptables_save: ok(''),
      ip6tables_save: ok(''),
      nft_ruleset: { available: false, error: 'not found' },
      ufw_status: { available: false, error: 'not found' },
    },
    files: {
      sing_box_config: { ok: true, content: JSON.stringify({ inbounds: [{ listen_port: 443 }] }) },
      traffic_sync: { consecutive_failures: 0, recovery_required: true },
    },
    ...overrides,
  }
}

const states = (result: unknown) => Object.fromEntries(summarizeNetworkDiagnostics(result).map((c) => [c.key, c.state]))

describe('summarizeNetworkDiagnostics', () => {
  it('reads real Agent fields instead of matching words like "failures"', () => {
    expect(states(healthyResult())).toEqual({
      controller: 'ok', ipv4: 'ok', ipv6: 'ok', default_route: 'ok',
      listeners: 'ok', dns: 'ok', firewall: 'ok', entry_probe: 'unknown',
    })
  })

  it('reports concrete failures', () => {
    const result = healthyResult({
      controller: { tcp: { ok: false, error: 'connection refused' } },
      entry_targets: [{ name: 'vless', host: '1.2.3.4', port: 443, tcp_self_probe: { ok: false, error: 'timeout' } }],
    })
    ;(result.commands as any).public_ipv6 = { available: true, ok: false, output: 'curl: (7) Failed to connect' }
    ;(result.commands as any).ss_tcp_listen = ok('tcp LISTEN 0 128 0.0.0.0:22 0.0.0.0:*\n')
    const s = states(result)
    expect(s.controller).toBe('fail')
    expect(s.ipv6).toBe('fail')
    expect(s.listeners).toBe('fail')
    expect(s.entry_probe).toBe('fail')
  })

  it('treats an empty kernel inbound list as nothing to check', () => {
    const result = healthyResult()
    ;(result.files as any).sing_box_config.content = JSON.stringify({ inbounds: [] })
    expect(states(result).listeners).toBe('unknown')
  })
})
