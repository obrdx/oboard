// @vitest-environment jsdom
import * as React from 'react'
import { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ServerSettingsDialog } from './main'

let host: HTMLDivElement
let root: Root

beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})

afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  vi.unstubAllGlobals()
})

const baseDraft = { name: 'server-1', region_mode: 'auto', entry_ip_mode: 'auto', entry_address: '', listen_mode: 'auto', listen_ip: '0.0.0.0', ip_stack: 'auto', udp_inbound_mode: 'allow', port_range_start: 10000, port_range_end: 20000, internal_port_range_start: 30000, internal_port_range_end: 59999, auto_renew_enabled: true, renewal_cycle: '', display_tags: [] }

function Harness({ onSubmit, panelBBR, initial = baseDraft }: { onSubmit: (draft: any) => Promise<void>; panelBBR?: { qdisc?: string; cakeMbit?: number }; initial?: any }) {
  const [draft, setDraft] = useState<any>(initial)
  return <ServerSettingsDialog mode="create" draft={draft} update={patch => setDraft((old: any) => ({ ...old, ...patch }))} onCancel={vi.fn()} onSubmit={() => onSubmit(draft)} panelBBR={panelBBR} />
}

const tab = (label: string) => [...document.querySelectorAll<HTMLButtonElement>('[role="tab"]')].find(item => item.textContent?.startsWith(label))!
const button = (label: string) => [...document.querySelectorAll<HTMLButtonElement>('button')].find(item => item.textContent === label)!

it('creates without a renewal cycle while no expiry date is set', async () => {
  const onSubmit = vi.fn().mockResolvedValue(undefined)
  await act(async () => root.render(<Harness onSubmit={onSubmit} />))
  await act(async () => button('创建').click())
  expect(onSubmit).toHaveBeenCalledTimes(1)
  expect(document.querySelector('#server-renewal-cycle-error')).toBeNull()
})

it('navigates sections and sends a missing renewal cycle back to billing before creating', async () => {
  const onSubmit = vi.fn().mockResolvedValue(undefined)
  await act(async () => root.render(<Harness onSubmit={onSubmit} initial={{ ...baseDraft, expires_at: '2027-01-02' }} />))
  expect(tab('基本信息').getAttribute('aria-selected')).toBe('true')
  expect(document.querySelectorAll('[role="tab"]')).toHaveLength(5)

  await act(async () => button('下一项').click())
  expect(tab('计费与流量').getAttribute('aria-selected')).toBe('true')
  await act(async () => tab('主机与安全').click())
  expect(document.querySelector('[role="tabpanel"]')?.textContent).toContain('时间校准')
  expect([...document.querySelectorAll('.ss-group-title')].map(title => title.textContent)).toEqual(['主机', '网络优化'])

  await act(async () => button('创建').click())
  expect(onSubmit).not.toHaveBeenCalled()
  expect(tab('计费与流量').getAttribute('aria-selected')).toBe('true')
  expect(document.querySelector('#server-renewal-cycle-error')?.textContent).toContain('必须选择续期周期')
  expect(tab('计费与流量').querySelector('.ss-nav-alert')).not.toBeNull()
})

it('blocks saving without a server name', async () => {
  const onSubmit = vi.fn().mockResolvedValue(undefined)
  await act(async () => root.render(<ServerSettingsDialog mode="create" draft={{ ...baseDraft, name: '  ', auto_renew_enabled: false }} update={vi.fn()} onCancel={vi.fn()} onSubmit={onSubmit} />))
  expect(button('创建').disabled).toBe(true)
  expect(tab('基本信息').querySelector('.ss-nav-alert')).not.toBeNull()
})

it('lets one server override the panel BBR qdisc and cake bandwidth', async () => {
  const onSubmit = vi.fn().mockResolvedValue(undefined)
  await act(async () => root.render(<Harness onSubmit={onSubmit} panelBBR={{ qdisc: 'fq', cakeMbit: 0 }} initial={{ ...baseDraft, renewal_cycle: 'monthly', bbr_qdisc: '', bbr_cake_bandwidth_mbit: 0 }} />))
  await act(async () => tab('主机与安全').click())
  const trigger = document.querySelector<HTMLButtonElement>('[role="combobox"][aria-label="拥塞控制"]')!
  expect(trigger.textContent).toContain('跟随系统设置（BBR + FQ）')
  expect(document.querySelector('[aria-label="CAKE 带宽（Mbit/s）"]')).toBeNull()

  await act(async () => trigger.click())
  const options = [...document.querySelectorAll<HTMLElement>('[role="option"]')]
  expect(options.map(option => option.textContent)).toEqual(['跟随系统设置（BBR + FQ）', 'BBR + FQ', 'BBR + CAKE', 'BBR + FQ-PIE', '不修改'])
  await act(async () => options.find(option => option.textContent === 'BBR + CAKE')!.click())

  const input = document.querySelector<HTMLInputElement>('[aria-label="CAKE 带宽（Mbit/s）"]')!
  expect(input).not.toBeNull()
  const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!
  await act(async () => { setValue.call(input, '950'); input.dispatchEvent(new Event('input', { bubbles: true })) })
  await act(async () => button('创建').click())
  expect(onSubmit).toHaveBeenCalledTimes(1)
  expect(onSubmit.mock.calls[0][0]).toMatchObject({ bbr_qdisc: 'cake', bbr_cake_bandwidth_mbit: 950 })
})

it('lets one server turn off the panel TCP tuning switch', async () => {
  const onSubmit = vi.fn().mockResolvedValue(undefined)
  await act(async () => root.render(<Harness onSubmit={onSubmit} panelBBR={{ qdisc: 'fq', cakeMbit: 0, tcpTuning: true }} initial={{ ...baseDraft, renewal_cycle: 'monthly', bbr_qdisc: '', tcp_tuning: '' }} />))
  await act(async () => tab('主机与安全').click())
  const trigger = document.querySelector<HTMLButtonElement>('[role="combobox"][aria-label="TCP 调优"]')!
  expect(trigger.textContent).toContain('跟随系统设置（开启）')

  await act(async () => trigger.click())
  const options = [...document.querySelectorAll<HTMLElement>('[role="option"]')]
  expect(options.map(option => option.textContent)).toEqual(['跟随系统设置（开启）', '开启', '关闭'])
  await act(async () => options.find(option => option.textContent === '关闭')!.click())
  await act(async () => button('创建').click())
  expect(onSubmit).toHaveBeenCalledTimes(1)
  expect(onSubmit.mock.calls[0][0]).toMatchObject({ tcp_tuning: 'off' })
})

it('explains which inbounds a DDNS default entry reaches and requires a record', async () => {
  const server = { id: 4, name: 'edge', public_ipv4: '203.0.113.10', public_ipv6: '2001:db8::10', entry_ip_mode: 'ddns', entry_ddns_record_id: 0 } as any
  const inbounds = [
    { id: 1, server_id: 4, entry_ip_mode: 'auto' },
    { id: 2, server_id: 4, entry_ip_mode: 'ipv4' },
    { id: 3, server_id: 4, entry_ip_mode: 'auto', dns_sync_enabled: true, dns_domain: 'own.example.com' },
    { id: 4, server_id: 9, entry_ip_mode: 'auto' },
  ] as any[]
  const client = { request: vi.fn(), requestV2: vi.fn(async () => ({ records: [{ id: 7, server_id: 4, name: 'dual', domain: 'edge.example.com', record_types: 'both', enabled: true }] })) }
  const onSubmit = vi.fn().mockResolvedValue(undefined)
  function EditHarness() {
    const [draft, setDraft] = useState<any>({ ...baseDraft, ...server })
    return <ServerSettingsDialog mode="edit" server={server} draft={draft} update={patch => setDraft((old: any) => ({ ...old, ...patch }))} onCancel={vi.fn()} onSubmit={() => onSubmit(draft)} client={client} inbounds={inbounds} role="admin" />
  }
  await act(async () => root.render(<EditHarness />))
  await act(async () => tab('连接与端口').click())
  const panel = () => document.querySelector('[role="tabpanel"]')?.textContent || ''
  expect(panel()).toContain('1 个入口将改用')
  expect(panel()).toContain('1 个入口单独选了')
  expect(panel()).toContain('1 个入口仍在维护自己的解析域名')
  expect(tab('连接与端口').querySelector('.ss-nav-alert')).not.toBeNull()
})
