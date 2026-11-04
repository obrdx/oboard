import { describe, expect, it } from 'vitest'
import { grantMissingScopes } from './GrantEditor'
import { lanEndpoint, sideEffectTargets } from './domain'
import type { Manifest, PermissionLine } from './types'

const manifest = {
  lan: { targets: [
    { id: 'router_status', scheme: 'http', address: '10.10.18.1', port: 5000, methods: ['GET'], path_prefix: '/', reason: '读取' },
    { id: 'router_change', scheme: 'http', address: 'fd00::1', port: 5000, methods: ['POST'], path_prefix: '/change', side_effect: true, reason: '切换' },
  ] },
} as unknown as Manifest

const lanLine = { capability: 'lan.request', resource: 'server', group: 'network', label: '内网', description: '', risk: 'high' } as PermissionLine

describe('LAN targets in the plugin UI', () => {
  it('formats endpoints and lists only granted side-effect targets', () => {
    expect(lanEndpoint(manifest.lan!.targets[1])).toBe('http://[fd00::1]:5000/change')
    expect(sideEffectTargets(manifest, { capabilities: { 'lan.request': { servers: [2], targets: ['router_status'] } } })).toEqual([])
    expect(sideEffectTargets(manifest, { capabilities: { 'lan.request': { servers: [2], targets: ['router_change'] } } }).map(t => t.id)).toEqual(['router_change'])
  })

  it('treats a lan.request grant without targets as missing scope', () => {
    expect(grantMissingScopes([lanLine], { capabilities: { 'lan.request': { servers: [2], targets: [] } } })).toHaveLength(1)
    expect(grantMissingScopes([lanLine], { capabilities: { 'lan.request': { servers: [2], targets: ['router_status'] } } })).toHaveLength(0)
  })
})
