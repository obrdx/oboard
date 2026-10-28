// @vitest-environment jsdom
import React, { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { LazyMotion, domAnimation } from 'motion/react'
import type { Connection, Edge, Node, ReactFlowProps } from 'reactflow'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ProxyOverview } from '../../main'
import { DialogContext } from '../ui/dialog-context'
import type { Inbound, Server } from './types'

const flow = vi.hoisted(() => ({ props: {} as ReactFlowProps }))
vi.mock('reactflow', async importOriginal => {
  const actual = await importOriginal<typeof import('reactflow')>()
  return {
    ...actual,
    // Only replace the browser canvas. State, layout, connections and dialogs
    // remain owned by the exported production component.
    default: (props: ReactFlowProps) => { flow.props = props; return <div data-testid="flow" /> },
    BaseEdge: ({ id, path, markerEnd, markerStart }: any) => <path data-testid={id} d={path} markerEnd={markerEnd} markerStart={markerStart} />,
    EdgeLabelRenderer: ({ children }: React.PropsWithChildren) => <>{children}</>,
  }
})

const servers = [1, 2, 3].map(id => ({
  id, name: `Server ${id}`, status: 'online', public_ipv4: `203.0.113.${id}`,
  entry_address: '', public_ipv6: '', interface_ipv6: '', region_code: 'US',
  detected_region_code: 'US', region_mode: 'manual', entry_ip_mode: 'auto',
  listen_ip: '0.0.0.0', listen_mode: 'auto', ip_stack: 'auto', udp_inbound_mode: 'allow',
} as Server))
const inbounds = [1, 2].map(id => ({
  id: id * 10, server_id: id, name: `Entry ${id}`, protocol: 'socks',
  port: 1080, listen_ip: '0.0.0.0', enabled: true, config_json: '{}',
} as Inbound))
const data = {
  session: { role: 'admin' }, servers, inbounds, proxy_paths: [], proxy_path_steps: [],
  external_outbounds: [], routing_rules: [], port_forwards: [], tunnels: [], warp_profiles: [],
}

describe('ProxyOverview canvas behavior', () => {
  let container: HTMLDivElement
  let root: Root
  const request = vi.fn(async () => ({}))
  const alert = vi.fn(async () => undefined)
  const nodes = () => flow.props.nodes as Node[]
  const edges = () => flow.props.edges as Edge[]
  const staged = () => nodes().filter(node => node.id.startsWith('canvas-server-'))
  const provisional = () => edges().filter(edge => edge.className === 'proxy-edge-provisional')
  const positions = () => Object.fromEntries(nodes().map(node => [node.id, { ...node.position }]))
  const button = (text: string) => {
    const found = Array.from(document.querySelectorAll('button')).find(item => item.textContent?.trim() === text || item.getAttribute('aria-label') === text)
    expect(found, `button ${text}`).toBeTruthy()
    return found!
  }
  const render = async (selectedServer = 1, pageData = data) => {
    await act(async () => root.render(<LazyMotion features={domAnimation}>
      <DialogContext.Provider value={{ confirm: async () => true, alert, prompt: async () => null }}>
        <ProxyOverview data={pageData} client={{ request }} selectedServer={selectedServer}
          setSelectedServer={() => undefined} load={async () => undefined} onServerSnapshot={() => undefined} />
      </DialogContext.Provider>
    </LazyMotion>))
  }
  // The node picker is reached from the canvas menu (right click / long press).
  const stage = async (id = 3) => {
    const paneMenu = { clientX: 200, clientY: 160, preventDefault: () => undefined, stopPropagation: () => undefined } as unknown as React.MouseEvent
    await act(async () => flow.props.onPaneContextMenu!(paneMenu))
    await act(async () => Array.from(document.querySelectorAll<HTMLButtonElement>('[aria-label="画布操作"] button')).find(item => item.textContent?.trim() === '其他服务器')!.click())
    await act(async () => button(`将 Server ${id} 放入画布`).click())
    return staged().at(-1)!
  }
  const connect = (target: string): Connection => ({ source: 'entry-10', target, sourceHandle: null, targetHandle: null })
  const startConnection = async () => {
    const target = await stage()
    const connection = connect(target.id)
    await act(async () => flow.props.onConnect!(connection))
    return connection
  }
  const cancel = async () => { await act(async () => button('取消').click()) }

  beforeEach(() => {
    ;(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true
    vi.stubGlobal('matchMedia', (query: string) => ({ matches: false, media: query, addEventListener: () => undefined, removeEventListener: () => undefined }))
    localStorage.clear()
    request.mockClear()
    alert.mockClear()
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })
  afterEach(() => {
    act(() => root.unmount())
    container.remove()
    localStorage.clear()
    document.body.style.overflow = ''
  })

  it('stages a server without moving any existing canvas node or writing topology', async () => {
    await render()
    const before = positions()
    expect(Object.keys(before)).toContain('entry-10')
    const added = await stage()
    expect(added).toBeDefined()
    expect(nodes()).toHaveLength(Object.keys(before).length + 1)
    for (const [id, position] of Object.entries(before)) expect(positions()[id]).toEqual(position)
    expect(request).not.toHaveBeenCalled()
  })

  it('saves Snell shared listener parameters without mode-switching actions', async () => {
    const inbound = { ...inbounds[0], protocol: 'snell', port: 6160, config_json: '{"version":4,"listener_mode":"shared_port"}' } as Inbound
    request.mockResolvedValueOnce({ inbound } as any)
    await render(1, { ...data, inbounds: [inbound] })
    await act(async () => flow.props.onNodeDoubleClick!({} as React.MouseEvent, nodes().find(node => node.id === 'entry-10')!))
    expect(document.body.textContent).not.toContain('独立端口')
    expect(document.body.textContent).not.toContain('预览切换')
    expect(document.querySelector('[aria-describedby="snell-mode-status"]')).toBeNull()
    await act(async () => button('保存').click())
    expect(request.mock.calls.filter(call => (call as any[])[1]?.method === 'PATCH').map(call => (call as any[])[0])).toEqual(['/inbounds/10'])
    expect(alert).not.toHaveBeenCalled()
  })

  it.each([
    { name: 'old public port', mode: 'controller_external', endpoint: '203.0.113.1:32001', version: 12, status: '等待端口探测' },
    { name: 'old local port', mode: 'agent_listener', endpoint: '127.0.0.1:32001', version: 12, status: '等待端口探测' },
    { name: 'old public address', mode: 'controller_external', endpoint: '203.0.113.9:443', version: 12, status: '等待端口探测' },
    { name: 'old configuration', mode: 'controller_external', endpoint: '203.0.113.1:443', version: 11, status: '等待端口探测' },
    { name: 'current public failure', mode: 'controller_external', endpoint: '203.0.113.1:443', version: 12, status: '公网端口异常' },
    { name: 'current local failure', mode: 'agent_listener', endpoint: '127.0.0.1:6160', version: 12, status: '本机监听异常' },
  ])('shows Snell endpoint and checks $name', async ({ mode, endpoint, version, status }) => {
    const inbound = { ...inbounds[0], protocol: 'snell', port: 6160, advertise_port: 443, snell_config_version: 12 } as Inbound
    await render(1, { ...data, inbounds: [inbound], inbound_probes: [{
      inbound_id: inbound.id, config_version: version, mode, endpoint, available: false, confirmed: true,
      sample_count: 5, success_count: 0, transport: 'tcp', created_at: '2026-10-28T00:00:00Z',
    }] } as any)
    const details = nodes().find(node => node.id === 'entry-10')!.data.label.props.entryDetails
    expect(details.address).toBe('203.0.113.1:443（监听 6160）')
    expect(details.probe).toBe(status)
  })

  it('does not let retired Snell ports override a successful current probe', async () => {
    const inbound = { ...inbounds[0], protocol: 'snell', port: 6160 } as Inbound
    const probe = { inbound_id: inbound.id, config_version: 12, mode: 'controller_external', confirmed: true,
      sample_count: 5, success_count: 5, transport: 'tcp', created_at: '2026-10-28T00:00:00Z' }
    await render(1, { ...data, inbounds: [inbound], inbound_probes: [
      { ...probe, endpoint: '203.0.113.1:32001', available: false, success_count: 0 },
      { ...probe, endpoint: '203.0.113.1:6160', available: true },
    ] } as any)
    const details = nodes().find(node => node.id === 'entry-10')!.data.label.props.entryDetails
    expect(details.address).toBe('203.0.113.1:6160')
    expect(details.probe).toBe('入口端口正常')
  })

  it('offers create actions from a right click on the empty canvas and on a server node', async () => {
    vi.stubGlobal('matchMedia', (query: string) => ({ matches: false, media: query, addEventListener: () => undefined, removeEventListener: () => undefined }))
    await render()
    const contextMenu = (target: Record<string, unknown> = {}) => ({ clientX: 200, clientY: 160, preventDefault: () => undefined, stopPropagation: () => undefined, ...target }) as unknown as React.MouseEvent
    await act(async () => flow.props.onPaneContextMenu!(contextMenu()))
    expect(document.querySelector('[aria-label="画布操作"]')).toBeTruthy()
    const before = nodes().length
    const menuItem = (text: string) => Array.from(document.querySelectorAll<HTMLButtonElement>('[aria-label="画布操作"] button')).find(item => item.textContent?.trim() === text)!
    await act(async () => menuItem('直接出口').click())
    expect(document.querySelector('[aria-label="画布操作"]')).toBeNull()
    expect(nodes().length).toBe(before + 1)
    await act(async () => flow.props.onPaneContextMenu!(contextMenu()))
    await act(async () => menuItem('其他服务器').click())
    await act(async () => button('将 Server 3 放入画布').click())
    expect(staged()).toHaveLength(1)
    const server = nodes().find(node => node.id === 'server-1')!
    await act(async () => flow.props.onNodeContextMenu!(contextMenu(), server))
    expect(button('创建入口')).toBeTruthy()
    expect(request).not.toHaveBeenCalled()
  })

    it('keeps staged servers isolated and restores each A/B canvas on switching', async () => {
    await render(1)
    const a = await stage(3)
    const aPosition = { ...a.position }
    await render(2)
    expect(staged()).toHaveLength(0)
    expect(nodes().some(node => node.id === 'entry-20')).toBe(true)
    const b = await stage(1)
    await render(1)
    expect(staged().map(node => node.id)).toEqual([a.id])
    expect(staged()[0].position).toEqual(aPosition)
    await render(2)
    expect(staged().map(node => node.id)).toEqual([b.id])
    expect(request).not.toHaveBeenCalled()
  })

  it('blocks repeated connect events while the first connection awaits confirmation', async () => {
    await render()
    const connection = await startConnection()
    expect(provisional()).toHaveLength(1)
    expect(flow.props.isValidConnection!(connection)).toBe(false)
    await act(async () => {
      flow.props.onConnect!(connection)
      flow.props.onConnect!(connection)
    })
    expect(provisional()).toHaveLength(1)
    expect(container.textContent).toContain('上一条连接仍在处理中')
    expect(request).not.toHaveBeenCalled()
    expect(alert).not.toHaveBeenCalled()
    await cancel()
  })

  it('cancels the provisional connection, clears busy nodes and allows a new attempt', async () => {
    await render()
    const connection = await startConnection()
    expect(provisional()).toHaveLength(1)
    expect(nodes().filter(node => node.className?.includes('graph-node-pending'))).toHaveLength(2)
    await cancel()
    expect(provisional()).toHaveLength(0)
    expect(nodes().some(node => node.className?.includes('graph-node-pending'))).toBe(false)
    expect(flow.props.isValidConnection!(connection)).toBe(true)
    expect(request).not.toHaveBeenCalled()
    await act(async () => flow.props.onConnect!(connection))
    expect(provisional()).toHaveLength(1)
    await cancel()
  })

  it('replaces a placed third-party card with the path node once an entry connects to it', async () => {
    const external = { id: 5, name: 'NTT HTTPS', protocol: 'http', scope: 'global', target_address: '180.53.81.127', target_port: 8080, config_json: '{"tls":{"enabled":true}}', region_mode: 'auto', region_code: '', expose_to_users: false, enabled: true }
    const step = { id: 77, path_id: 9, position: 1, node_type: 'imported', external_outbound_id: 5, transport_mode: 'singbox', config_json: '{}' }
    request.mockImplementation((async (path: string) => path === '/proxy-paths'
      ? { proxy_path: { id: 9, inbound_id: 10, name: 'Entry 1 / NTT', enabled: true, kind: 'chain' }, proxy_path_steps: [step] }
      : {}) as any)
    await render(1, { ...data, external_outbounds: [external] } as any)
    const paneMenu = { clientX: 200, clientY: 160, preventDefault: () => undefined, stopPropagation: () => undefined } as unknown as React.MouseEvent
    await act(async () => flow.props.onPaneContextMenu!(paneMenu))
    await act(async () => Array.from(document.querySelectorAll<HTMLButtonElement>('[aria-label="画布操作"] button')).find(item => item.textContent?.trim() === '第三方节点')!.click())
    await act(async () => button('将 NTT HTTPS 添加到拓扑').click())
    expect(nodes().map(node => node.id)).toContain('imported-5')
    await act(async () => flow.props.onConnect!(connect('imported-5')))
    await act(async () => button('确定').click())
    expect(request).toHaveBeenCalledWith('/proxy-paths', expect.objectContaining({ method: 'POST' }))
    expect(nodes().map(node => node.id)).not.toContain('imported-5')
    request.mockImplementation((async () => ({})) as any)
  })

  it('shows the empty canvas without a stuck loading indicator after the last server is removed', async () => {
    await render()
    await render(0, { ...data, servers: [], inbounds: [] })
    expect(container.querySelector('.graph-canvas-loading')).toBeNull()
    expect(container.textContent).toContain('还没有服务器')
  })

  it('renders the production transport edge without arrow markers even if supplied', async () => {
    await render()
    const Renderer = flow.props.edgeTypes!.proxyTransport
    expect(Renderer).toBeDefined()
    await act(async () => root.render(<svg><Renderer {...{
      id: 'edge-under-test', source: 'a', target: 'b', sourceX: 0, sourceY: 0, targetX: 100, targetY: 100,
      markerStart: 'url(#start)', markerEnd: 'url(#end)',
      data: { route: { points: [{ x: 0, y: 0 }, { x: 100, y: 0 }, { x: 100, y: 100 }] } },
    } as any} /></svg>))
    expect(container.querySelector('[data-testid="edge-under-test"]')).not.toBeNull()
    expect(container.querySelector('[marker-end], [marker-start], marker, .proxy-edge-arrow')).toBeNull()
  })
})
