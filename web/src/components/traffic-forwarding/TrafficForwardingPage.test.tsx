// @vitest-environment jsdom

import React, { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Server } from '../proxy-path/types'
import { DialogContext, type DialogApi } from '../ui/dialog-context'
import { TrafficForwardingPage } from './TrafficForwardingPage'
import { REDACTED, type TrafficForward, type TrafficTunnel } from './model'

vi.mock('../ui/motion', () => ({
  MotionDialogPanel: ({ children }: { children: React.ReactNode }) => <section data-testid="dialog">{children}</section>,
}))

const servers = [
  { id: 1, name: '香港入口', status: 'online', public_ipv4: '203.0.113.1', port_range_start: 10000, port_range_end: 10010 },
  { id: 2, name: '东京应用', status: 'online', public_ipv4: '203.0.113.2', port_range_start: 20000, port_range_end: 20010 },
] as unknown as Server[]

const forwards: TrafficForward[] = [{
  id: 11, name: '转发-A', source_server_id: 1, target_server_id: 2, listen_ip: '', listen_port: 10000, target_address: '', target_port: 443,
  protocol: 'tcp', backend: 'realm', probe_mode: 'periodic', probe_interval_seconds: 300, priority: 100, config_json: '{}', enabled: true,
}]

const tunnels: TrafficTunnel[] = [{
  id: 7, name: '隧道-B', source_server_id: 2, target_server_id: 1, type: 'wireguard', local_address: '', peer_address: '', listen_port: 0,
  target_endpoint: '', target_port: 0, priority: 100, config_json: JSON.stringify({ private_key: REDACTED, peer_public_key: REDACTED, allowed_ips: [], persistent_keepalive: 25 }), enabled: true,
}]

function setInput(element: HTMLInputElement | null, value: string) {
  if (!element) throw new Error('input missing')
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(element, value)
  element.dispatchEvent(new Event('input', { bubbles: true }))
}

describe('TrafficForwardingPage', () => {
  let container: HTMLDivElement
  let root: Root
  let client: { request: ReturnType<typeof vi.fn> }
  let load: ReturnType<typeof vi.fn>

  const dialogs: DialogApi = {
    alert: vi.fn(async () => undefined),
    confirm: vi.fn(async () => true),
    prompt: vi.fn(async () => null),
  } as unknown as DialogApi

  const render = () => act(() => root.render(
    <DialogContext.Provider value={dialogs}>
      <TrafficForwardingPage data={{ servers, port_forwards: forwards, port_forward_probes: [], tunnels }} client={client} load={load} />
    </DialogContext.Provider>,
  ))

  beforeEach(() => {
    ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    client = { request: vi.fn(async () => ({})) }
    load = vi.fn(async () => undefined)
  })

  afterEach(() => {
    act(() => root.unmount())
    container.remove()
    document.body.querySelectorAll('.custom-select-menu').forEach(element => element.remove())
    ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = false
  })

  it('lists forwards and tunnels together, grouped by entry server', () => {
    render()
    const groups = Array.from(container.querySelectorAll('.tf-group'))
    expect(groups.map(group => group.getAttribute('aria-label'))).toEqual(['入口 香港入口', '入口 东京应用'])
    expect(groups[0].textContent).toContain('转发-A')
    expect(groups[1].textContent).toContain('隧道-B')
    expect(container.textContent).toContain('端口转发 1 · 隧道 1')
  })

  it('narrows the list by search and offers to clear filters', () => {
    render()
    act(() => setInput(container.querySelector<HTMLInputElement>('[aria-label="搜索转发"]'), 'wireguard'))
    expect(container.textContent).toContain('隧道-B')
    expect(container.textContent).not.toContain('转发-A')
    act(() => setInput(container.querySelector<HTMLInputElement>('[aria-label="搜索转发"]'), 'nothing-matches'))
    expect(container.textContent).toContain('没有符合条件的转发')
  })

  it('creates a port forward to an arbitrary address with an automatic name', async () => {
    render()
    act(() => Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find(button => button.textContent?.includes('新建转发'))?.click())
    expect(container.querySelector('[data-testid="dialog"]')?.textContent).toContain('新建转发')
    act(() => setInput(container.querySelector<HTMLInputElement>('[aria-label="目标地址"]'), '198.51.100.9'))
    await act(async () => {
      container.querySelector<HTMLButtonElement>('button[type="submit"]')?.click()
      await Promise.resolve()
    })
    expect(client.request).toHaveBeenCalledWith('/port-forwards', expect.objectContaining({ method: 'POST' }))
    const body = JSON.parse(client.request.mock.calls[0][1].body)
    expect(body).toMatchObject({ source_server_id: 1, listen_port: 10001, target_address: '198.51.100.9', name: '香港入口:10001 → 198.51.100.9', enabled: true })
    expect(load).toHaveBeenCalled()
  })

  it('requires keys when the operator chooses manual entry', async () => {
    render()
    act(() => Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find(button => button.textContent?.includes('新建转发'))?.click())
    act(() => container.querySelector<HTMLButtonElement>('.tf-kind-option[aria-checked="false"]')?.click())
    act(() => Array.from(container.querySelectorAll<HTMLButtonElement>('button[role="radio"]')).find(button => button.textContent === '手动填写')?.click())
    expect(container.textContent).toContain('本机私钥')
    await act(async () => {
      container.querySelector<HTMLButtonElement>('button[type="submit"]')?.click()
      await Promise.resolve()
    })
    expect(client.request).not.toHaveBeenCalled()
    expect(container.textContent).toContain('请填写本机私钥')
  })

  it('creates a tunnel with automatic keys and the selected endpoints', async () => {
    render()
    act(() => Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find(button => button.textContent?.includes('新建转发'))?.click())
    act(() => container.querySelector<HTMLButtonElement>('.tf-kind-option[aria-checked="false"]')?.click())
    expect(container.querySelector('[aria-label="WireGuard 密钥方式"] [aria-checked="true"]')?.textContent).toBe('自动生成')
    expect(container.querySelector('[aria-label="本机私钥"]')).toBeNull()
    act(() => container.querySelector<HTMLButtonElement>('button[type="submit"]')?.click())
    expect(client.request).not.toHaveBeenCalled()
    expect(container.textContent).toContain('请填写目标服务器的 UDP 端口')
    act(() => {
      setInput(container.querySelector('[aria-label="本端地址"]'), '10.10.0.1/30')
      setInput(container.querySelector('[aria-label="对端地址"]'), '10.10.0.2/30')
      setInput(container.querySelector('[aria-label="目标 UDP 端口"]'), '20003')
    })
    await act(async () => {
      container.querySelector<HTMLButtonElement>('button[type="submit"]')?.click()
    })
    expect(client.request).toHaveBeenCalledWith('/tunnels', expect.objectContaining({ method: 'POST' }))
    const body = JSON.parse(client.request.mock.calls[0][1].body)
    expect(body).toMatchObject({ generate_keys: true, local_address: '10.10.0.1/30', peer_address: '10.10.0.2/30', target_port: 20003 })
    expect(JSON.parse(body.config_json)).toEqual({ persistent_keepalive: 25 })
  })

  it('toggles a tunnel by sending its redacted keys back unchanged', async () => {
    render()
    await act(async () => {
      container.querySelector<HTMLInputElement>('[aria-label="停用 隧道-B"]')?.click()
      await Promise.resolve()
    })
    expect(client.request).toHaveBeenCalledWith('/tunnels/7', expect.objectContaining({ method: 'PATCH' }))
    const body = JSON.parse(client.request.mock.calls[0][1].body)
    expect(body.enabled).toBe(false)
    expect(JSON.parse(body.config_json)).toMatchObject({ private_key: REDACTED, peer_public_key: REDACTED })
  })
})
