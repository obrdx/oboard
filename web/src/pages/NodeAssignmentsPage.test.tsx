// @vitest-environment jsdom
import * as React from 'react'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { NodeAssignmentsPage } from './NodeAssignmentsPage'

const node = (name: string) => ({ key: 'inbound:1', type: 'inbound', id: 1, name, source_name: name, plans: [], status: 'ok', effective_users: 0, allow_exceptions: 0, deny_exceptions: 0 })
const flush = async () => { await act(async () => { await new Promise(resolve => setTimeout(resolve, 0)) }) }

describe('NodeAssignmentsPage reading context', () => {
  let container: HTMLDivElement
  let root: Root
  beforeEach(() => {
    ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div'); document.body.appendChild(container); root = createRoot(container)
  })
  afterEach(() => { act(() => root.unmount()); container.remove() })

  it('warns on both layouts only when a plan contains a disabled node', async () => {
    const plans = [{ plan_id: 1, name: '基础套餐' }]
    const nodes = [
      { ...node('香港'), key: 'proxy_path:10', type: 'proxy_path', id: 10, status: 'disabled', plans },
      { ...node('香港 | 东京'), key: 'proxy_path:11', type: 'proxy_path', id: 11, effective_users: 1, allow_exceptions: 1 },
      { ...node('停用入口'), key: 'inbound:2', id: 2, status: 'disabled' },
      { ...node('离线入口'), key: 'inbound:3', id: 3, status: 'offline', plans },
    ]
    const request = vi.fn(async () => ({ nodes, total: nodes.length, page: 1, page_size: 50 }))
    await act(async () => root.render(<NodeAssignmentsPage data={{}} client={{ request }} load={vi.fn()} />))
    await flush()
    for (const selector of ['.nw-catalog-table tbody tr', '.nw-card-list article']) {
      const rows = container.querySelectorAll(selector)
      expect(rows).toHaveLength(4)
      expect(rows[0].textContent).toContain('基础套餐')
      expect(rows[0].textContent).toContain('套餐仍含此禁用节点')
      expect(rows[1].textContent).toContain('未加入套餐')
      expect(rows[1].textContent).toContain('1 人+1')
      for (const row of Array.from(rows).slice(1)) expect(row.textContent).not.toContain('套餐仍含此禁用节点')
    }
  })

  it('requests the selected page rather than silently showing page one', async () => {
    const request = vi.fn(async (path: string) => {
      const page = new URL(path, 'http://localhost').searchParams.get('page')
      return { nodes: [node(`page-${page}`)], total: 120, page: Number(page), page_size: 50 }
    })
    await act(async () => root.render(<NodeAssignmentsPage data={{ session: { role: 'viewer' } }} client={{ request }} load={vi.fn()} />))
    await flush()
    act(() => Array.from(container.querySelectorAll('button')).find(button => button.textContent === '下一页')?.click())
    await flush()
    expect(container.textContent).toContain('page-2')
    expect(container.textContent).toContain('第 2 / 3 页')
  })

  it('ignores a late search response and keeps current results', async () => {
    let finishOld!: (value: unknown) => void
    const old = new Promise(resolve => { finishOld = resolve })
    const request = vi.fn(async (path: string) => {
      const query = new URL(path, 'http://localhost').searchParams.get('query')
      if (query === 'old') return old
      return { nodes: [node(query || 'initial')], total: 1, page: 1, page_size: 50 }
    })
    await act(async () => root.render(<NodeAssignmentsPage data={{ session: { role: 'viewer' } }} client={{ request }} load={vi.fn()} />))
    await flush()
    const search = container.querySelector<HTMLInputElement>('[aria-label="搜索节点"]')!
    const change = (value: string) => act(() => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(search, value)
      search.dispatchEvent(new Event('input', { bubbles: true }))
    })
    change('old'); await flush(); change('new'); await flush()
    await act(async () => { finishOld({ nodes: [node('stale-result')], total: 1 }); await old })
    expect(container.textContent).toContain('new')
    expect(container.textContent).not.toContain('stale-result')
  })
  it('shows applied search as a removable chip and a batch bar for selected nodes', async () => {
    const request = vi.fn(async (path: string) => {
      const query = new URL(path, 'http://localhost').searchParams.get('query') || ''
      return { nodes: [{ ...node(`东京${query}`), key: 'inbound:1' }, { ...node('香港'), key: 'inbound:2', id: 2 }], total: 2, page: 1, page_size: 50 }
    })
    await act(async () => root.render(<NodeAssignmentsPage data={{ session: { role: 'admin' }, subscription_plans: [] }} client={{ request }} load={vi.fn()} />))
    await flush()
    expect(container.querySelector('.nw-batch-bar')).toBeNull()
    act(() => container.querySelector<HTMLInputElement>('.nw-catalog-table [aria-label="选择 香港"]')!.click())
    expect(container.querySelector('.nw-batch-bar')?.textContent).toContain('已选 1 个节点')

    const search = container.querySelector<HTMLInputElement>('[aria-label="搜索节点"]')!
    act(() => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(search, 'A')
      search.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await flush()
    const chip = container.querySelector<HTMLButtonElement>('[aria-label="移除搜索 A"]')!
    expect(chip.textContent).toContain('搜索：A')
    act(() => chip.click())
    await flush()
    expect(container.querySelector('[aria-label="移除搜索 A"]')).toBeNull()
    expect(new URL(String(request.mock.calls.at(-1)?.[0]), 'http://localhost').searchParams.get('query')).toBeNull()
  })

})
