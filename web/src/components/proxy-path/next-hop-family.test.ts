import { describe, expect, it } from 'vitest'
import { nextHopSteps, sharedStepGroup, summarizeAddressFamily, withAddressFamily } from './next-hop-family'
import type { ProxyPath, ProxyPathStep } from './types'

const path = (id: number, inbound_id = 1, extra: Partial<ProxyPath> = {}) => ({ id, kind: 'chain', inbound_id, enabled: true, ...extra }) as ProxyPath
const step = (id: number, path_id: number, position: number, extra: Partial<ProxyPathStep> = {}): ProxyPathStep => ({ id, path_id, position, node_type: 'server_inbound', transport_mode: 'singbox', server_id: 10 + position, config_json: '{}', ...extra })

// Entry 1 -> Cogent (shared by three paths) -> three different exits.
const paths = [path(1), path(2), path(3), path(4, 1, { kind: 'family_branch' })]
const steps = [
  step(11, 1, 1), step(12, 1, 2, { node_type: 'imported', server_id: undefined, external_outbound_id: 100 }),
  step(21, 2, 1), step(22, 2, 2, { node_type: 'imported', server_id: undefined, external_outbound_id: 200, config_json: '{"address_family":"prefer_ipv6"}' }),
  step(31, 3, 1), step(32, 3, 2, { node_type: 'warp', server_id: undefined }),
  step(41, 4, 1),
]

describe('next hop address family', () => {
  it('collects every segment leaving a shared node', () => {
    expect(sharedStepGroup(paths, steps, 21).map(item => item.id)).toEqual([11, 21, 31, 41])
    expect(nextHopSteps({ type: 'proxy-path-step', id: 21 }, paths, steps, []).map(item => item.id)).toEqual([12, 22])
  })

  it('collects first hops from an entry or its server, skipping family branches', () => {
    expect(nextHopSteps({ type: 'entry', id: 1 }, paths, steps, []).map(item => item.id)).toEqual([11, 21, 31])
    expect(nextHopSteps({ type: 'server', id: 9 }, paths, steps, [{ id: 1, server_id: 9 }]).map(item => item.id)).toEqual([11, 21, 31])
  })

  it('summarizes and rewrites the stored family', () => {
    expect(summarizeAddressFamily([steps[1], steps[3]])).toBe('mixed')
    expect(summarizeAddressFamily([steps[1]])).toBe('auto')
    expect(JSON.parse(withAddressFamily('{"type":"ssh"}', 'ipv6_only'))).toEqual({ type: 'ssh', address_family: 'ipv6_only' })
    expect(JSON.parse(withAddressFamily('{"address_family":"ipv6_only"}', 'auto'))).toEqual({})
  })
})
