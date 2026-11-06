// @vitest-environment jsdom
import React, { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { OfflineIsolationCard, OfflineIsolationPrompt, isolationRecovery } from './offline-isolation'

const base = { id: 1, incident_id: 7, server_id: 3, server_name: 'PanDE', inbound_name: '9929-anytls-10477' }

describe('offline isolation', () => {
  let container: HTMLDivElement
  let root: Root
  beforeEach(() => {
    ;(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })
  afterEach(() => { act(() => root.unmount()); container.remove() })

  it('describes each recovery policy with a label and a detail', () => {
    const now = new Date('2026-10-15T10:00:00').getTime()
    expect(isolationRecovery({ ...base, recovery_policy: 'auto' }, now)).toEqual({ label: '服务器恢复在线后', detail: '自动恢复' })
    expect(isolationRecovery({ ...base, recovery_policy: 'manual', restore_at: '2026-10-15T16:30:00' }, now)).toEqual({ label: '10-15 16:30 恢复', detail: '剩余 6 小时 30 分钟' })
    expect(isolationRecovery({ ...base, recovery_policy: 'manual' }, now).label).toBe('需要手动恢复')
  })

  it('summarises isolated inbounds on the dashboard card and restores them from the dialog', async () => {
    const requestV2 = vi.fn(async () => ({}))
    const onChanged = vi.fn()
    await act(async () => root.render(<OfflineIsolationCard isolation={{ active: [{ ...base, recovery_policy: 'auto' }, { ...base, id: 2, server_name: 'Alpha', server_id: 4, inbound_name: 'hy2', recovery_policy: 'auto' }] }} client={{ requestV2 }} onChanged={onChanged} />))
    expect(container.textContent).toContain('2 台服务器的 2 个入口')
    expect(document.querySelector('.offline-isolation-items')).toBeNull()
    await act(async () => (container.querySelector('.offline-isolation-open') as HTMLButtonElement).click())
    const rows = document.querySelectorAll('.offline-isolation-items li')
    expect(rows).toHaveLength(2)
    expect(rows[0].querySelector('.offline-isolation-target strong')?.textContent).toBe('hy2')
    expect(rows[1].querySelector('.offline-isolation-target span')?.textContent).toBe('PanDE')
    await act(async () => (document.querySelector('[aria-label="恢复 PanDE 9929-anytls-10477 的订阅"]') as HTMLButtonElement).click())
    expect(requestV2).toHaveBeenCalledWith('/node-incidents/7/restore', { method: 'POST', body: JSON.stringify({ isolation_id: 1 }) })
    expect(onChanged).toHaveBeenCalledTimes(1)
  })

  it('renders nothing on the dashboard card without isolated inbounds', async () => {
    await act(async () => root.render(<OfflineIsolationCard isolation={{ active: [] }} client={{ requestV2: vi.fn() }} onChanged={vi.fn()} />))
    expect(container.innerHTML).toBe('')
  })

  it('summarises offline servers on the dashboard prompt', async () => {
    const incidents = [
      { id: 1, version: 1, server_id: 1, server_name: 'PanDE', inbounds: [{ id: 1, name: 'a' }, { id: 2, name: 'b' }] },
      { id: 2, version: 1, server_id: 2, server_name: 'Alpha', inbounds: [{ id: 3, name: 'c' }] },
    ]
    await act(async () => root.render(<OfflineIsolationPrompt isolation={{ incidents }} client={{ requestV2: vi.fn() }} onChanged={vi.fn()} />))
    expect(container.querySelector('strong')?.textContent).toBe('2 台服务器已离线')
    expect(container.textContent).toContain('PanDE、Alpha。3 个入口仍在订阅里')
  })
})
