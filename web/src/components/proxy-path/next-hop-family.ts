import { buildSharedProxyPathTopology } from './graph-topology'
import type { Inbound, ProxyPath, ProxyPathStep } from './types'
import type { AddressFamily } from './TransportDialog'

// A node's outgoing address family is stored on each segment that leaves it:
// step.config_json.address_family describes how the previous node dials that
// step. These helpers let the canvas edit it from the source node so every
// path sharing the node changes together and the shared trunk stays merged.

export type NextHopNode = { type: 'proxy-path-step' | 'entry' | 'server'; id: number }

const FAMILIES: AddressFamily[] = ['auto', 'prefer_ipv4', 'prefer_ipv6', 'ipv4_only', 'ipv6_only']

export function stepAddressFamily(step: Pick<ProxyPathStep, 'config_json'>): AddressFamily {
  try {
    const value = JSON.parse(step.config_json || '{}')?.address_family
    return FAMILIES.includes(value) ? value : 'auto'
  } catch {
    return 'auto'
  }
}

export function withAddressFamily(configJSON: string | undefined, family: AddressFamily): string {
  let config: Record<string, unknown> = {}
  try {
    const parsed = JSON.parse(configJSON || '{}')
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) config = parsed
  } catch {
    config = {}
  }
  if (family === 'auto') delete config.address_family
  else config.address_family = family
  return JSON.stringify(config)
}

function familyCapable(step: ProxyPathStep) {
  return step.node_type === 'server_inbound' || step.node_type === 'imported'
}

/** Steps whose rows belong to the same canvas node as stepID (one per path). */
export function sharedStepGroup(paths: ProxyPath[], steps: ProxyPathStep[], stepID: number): ProxyPathStep[] {
  const step = steps.find(item => item.id === stepID)
  if (!step) return []
  const topology = buildSharedProxyPathTopology(paths, steps)
  const canonical = topology.canonicalStepByID.get(step.id)
  return (canonical && topology.stepsByCanonicalID.get(canonical.id)) || [step]
}

/** The segments that leave a canvas node, across every enabled path through it. */
export function nextHopSteps(node: NextHopNode, paths: ProxyPath[], steps: ProxyPathStep[], inbounds: Pick<Inbound, 'id' | 'server_id'>[]): ProxyPathStep[] {
  const eligiblePaths = paths.filter(path => path.enabled !== false && path.kind !== 'family_branch')
  const eligiblePathIDs = new Set(eligiblePaths.map(path => path.id))
  const stepAt = (pathID: number, position: number) => steps.find(step => step.path_id === pathID && step.position === position)
  const sources: Array<{ pathID: number; position: number }> = []
  if (node.type === 'proxy-path-step') {
    sharedStepGroup(eligiblePaths, steps, node.id).forEach(step => sources.push({ pathID: step.path_id, position: step.position }))
  } else {
    const inboundIDs = new Set(node.type === 'entry' ? [node.id] : inbounds.filter(inbound => inbound.server_id === node.id).map(inbound => inbound.id))
    eligiblePaths.filter(path => inboundIDs.has(path.inbound_id)).forEach(path => sources.push({ pathID: path.id, position: 0 }))
  }
  const seen = new Set<number>()
  const out: ProxyPathStep[] = []
  sources.forEach(source => {
    if (!eligiblePathIDs.has(source.pathID)) return
    const next = stepAt(source.pathID, source.position + 1)
    if (!next || seen.has(next.id) || !familyCapable(next)) return
    seen.add(next.id)
    out.push(next)
  })
  return out
}

/** One family when every outgoing segment agrees, otherwise 'mixed'. */
export function summarizeAddressFamily(steps: ProxyPathStep[]): AddressFamily | 'mixed' {
  const families = new Set(steps.map(stepAddressFamily))
  if (families.size === 0) return 'auto'
  return families.size === 1 ? [...families][0] : 'mixed'
}
