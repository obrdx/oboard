// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { Server } from '../../proxy-path/types'
import { ServerAddresses } from './ServerAddresses'
import { ServerRow, ServerTile } from './ServerTile'

let host: HTMLDivElement
let root: Root
let copy: ReturnType<typeof vi.fn>

const server = {
  id: 7,
  name: 'Tokyo',
  status: 'online',
  agent_id: 'agent-7',
  agent_build: '20260101000000',
  cpu: 'AMD EPYC',
  cpu_cores: 2,
  cpu_usage_percent: 33.6,
  memory_used_bytes: 512 * 1024 ** 2,
  memory_total_bytes: 1024 ** 3,
  disk_bytes: 15 * 1024 ** 3,
  disk_total_bytes: 20 * 1024 ** 3,
  traffic_upload_bytes: 600 * 1024 ** 3,
  traffic_download_bytes: 1100 * 1024 ** 3,
  traffic_limit_bytes: 2 * 1024 ** 4,
  public_ipv4: '203.0.113.10',
  public_ipv6: '2001:db8::1:2',
  entry_ip_mode: 'custom',
  entry_address: '198.51.100.20',
  time_check_status: 'skewed',
} as Server

beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  copy = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true })
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: copy } })
})

afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  vi.unstubAllGlobals()
})

it('keeps one address button and copies each address from its popover without opening the surface', async () => {
  const open = vi.fn()
  await act(async () => root.render(<div onClick={open}><ServerAddresses server={server} /></div>))
  const trigger = host.querySelector<HTMLButtonElement>('.fleet-address-trigger')!
  expect(trigger.textContent).toBe('203.0.113.10+2')
  expect(document.querySelector('.fleet-address-panel')).toBeNull()
  await act(async () => trigger.click())
  const buttons = [...document.querySelectorAll<HTMLButtonElement>('.fleet-address-panel .fleet-address')]
  expect(buttons.map(button => button.dataset.kind)).toEqual(['v4', 'v6', 'entry'])
  for (const button of buttons) await act(async () => button.click())
  expect(copy.mock.calls.map(call => call[0])).toEqual(['203.0.113.10', '2001:db8::1:2', '198.51.100.20'])
  expect(open).not.toHaveBeenCalled()
  await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' })))
  expect(document.querySelector('.fleet-address-panel')).toBeNull()
})

it('shows only a status dot next to the card name', async () => {
  await act(async () => root.render(<ServerTile server={server} onAction={() => {}} />))
  const dot = host.querySelector('.fleet-name-line .fleet-status.is-dot')!
  expect(dot.getAttribute('aria-label')).toBe('在线')
  expect(dot.textContent).toBe('')
})

it('shows clear card metrics and routes card, name and chip clicks to separate actions', async () => {
  const onAction = vi.fn()
  await act(async () => root.render(<ServerTile server={server} role="admin" expectedBuild="20260201000000" onAction={onAction} />))
  const tile = host.querySelector('.fleet-tile')!
  expect(tile.textContent).toContain('2 核')
  expect(tile.textContent).toContain('34%')
  expect(tile.textContent).toContain('1.7 TB / 2.0 TB')
  expect(host.querySelector('.fleet-meter[title="AMD EPYC"]')).not.toBeNull()
  const disk = [...host.querySelectorAll('.fleet-meter')].find(item => item.textContent?.startsWith('磁盘'))!
  expect(disk.querySelector('.fleet-meter-fill')?.classList.contains('is-warning')).toBe(true)

  await act(async () => (tile.querySelector('.fleet-meter') as HTMLElement).click())
  expect(onAction).toHaveBeenLastCalledWith('resource-details', server)
  await act(async () => host.querySelector<HTMLButtonElement>('.fleet-name')!.click())
  expect(onAction).toHaveBeenLastCalledWith('about', server)
  const chips = [...host.querySelectorAll<HTMLButtonElement>('button.fleet-chip')]
  expect(chips.map(chip => chip.textContent)).toEqual(['时间偏差过大', 'Agent 待更新'])
  await act(async () => chips[1].click())
  expect(onAction).toHaveBeenLastCalledWith('update-agent', server)
  await act(async () => host.querySelector<HTMLButtonElement>('.fleet-target-button')!.click())
  expect(onAction).toHaveBeenLastCalledWith('monitoring-target', server)
  expect(onAction).toHaveBeenCalledTimes(4)
})

it('dims offline metrics and hides admin-only controls from viewers', async () => {
  const offline = { ...server, status: 'offline', last_seen_at: new Date(Date.now() - 3 * 3600_000).toISOString() } as Server
  await act(async () => root.render(<ServerRow server={offline} role="viewer" onAction={vi.fn()} />))
  expect(host.querySelector('.fleet-status')?.textContent).toBe('离线')
  expect(host.querySelector('.fleet-status')?.getAttribute('title')).toBe('3 小时前离线')
  expect(host.querySelector('.fleet-cell-cpu .fleet-compact-meter')?.classList.contains('is-muted')).toBe(true)
  expect(host.querySelector('.fleet-target-button')).toBeNull()
})

it('does not open details when a menu item inside a portal is clicked', async () => {
  const onAction = vi.fn()
  await act(async () => root.render(<ServerTile server={server} role="admin" onAction={onAction} />))
  await act(async () => host.querySelector<HTMLButtonElement>('.fleet-menu-trigger')!.click())
  const settings = [...document.querySelectorAll<HTMLButtonElement>('[role="menuitem"]')].find(item => item.textContent === '服务器设置')!
  await act(async () => settings.click())
  expect(onAction).toHaveBeenCalledExactlyOnceWith('edit', server)
})

it('draws one latency row per target with its own scale and keeps the bar style selectable', async () => {
  const at = (minute: number) => `2026-11-04T00:${String(minute).padStart(2, '0')}:00Z`
  const grade = { excellent_ms: 165, fair_ms: 270, basis: 'route' as const }
  const monitored = {
    ...server,
    monitoring_display: {
      name: '广东 · 中国移动',
      enabled: true,
      series: [
        { probe_id: 't1-gd-cm-v4', label: '移动', family: 'ipv4' as const, grade, samples: [0, 1, 2].map(minute => ({ available: true, latency_ms: 160, sample_count: 10, success_count: 4, checked_at: at(minute) })) },
        { probe_id: 't1-gd-cm-v6', label: '移动v6', family: 'ipv6' as const, grade, samples: [0, 1, 2].map(minute => ({ available: true, latency_ms: 300, sample_count: 5, success_count: 5, checked_at: at(minute) })) },
      ],
    },
  } as Server
  await act(async () => root.render(<ServerTile server={monitored} onAction={() => {}} />))
  const rows = Array.from(host.querySelectorAll('.fleet-latency-row'))
  expect(rows.map(row => row.querySelector('.fleet-latency-name')?.textContent)).toEqual(['移动', '移动v6'])
  expect(rows[0].querySelector('.fleet-latency-value')?.textContent).toBe('160 ms')
  expect(rows[0].querySelector('.fleet-latency-dot')?.classList.contains('is-ok')).toBe(true)
  expect(rows[1].querySelector('.fleet-latency-dot')?.classList.contains('is-poor')).toBe(true)
  expect(rows[0].querySelector('.fleet-latency-loss strong')?.textContent).toBe('60%')
  expect(rows[0].querySelector('svg.fleet-sparkline path.fleet-sparkline-line')).toBeTruthy()
  expect(rows[0].getAttribute('title')).toContain('≤ 165 ms 优秀')

  await act(async () => root.render(<ServerTile server={monitored} latencyChart="bars" onAction={() => {}} />))
  expect(host.querySelector('.fleet-latency-row svg.fleet-sparkline')).toBeNull()
  expect(host.querySelectorAll('.fleet-latency-row .fleet-quality-bar')).toHaveLength(2)

  await act(async () => root.render(<ServerRow server={monitored} onAction={() => {}} />))
  expect(host.querySelectorAll('.fleet-cell-quality .fleet-quality-inline')).toHaveLength(2)
})
