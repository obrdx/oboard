// @vitest-environment jsdom
import * as React from 'react'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { DeleteServerDialog } from './main'
import type { Server } from './components/proxy-path/types'

let host: HTMLDivElement
let root: Root
const server = { id: 1, name: '测试服务器' } as Server

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

it('uses a compact confirmation and blocks dismissal and repeat submission while deleting', async () => {
  const onCancel = vi.fn()
  const onSubmit = vi.fn()
  await act(async () => root.render(<DeleteServerDialog server={server} busy onCancel={onCancel} onSubmit={onSubmit} />))
  expect(document.querySelector('.dialog-layer')?.getAttribute('data-surface-kind')).toBe('compact')
  expect(document.querySelector('.server-delete-dialog')?.getAttribute('aria-labelledby')).toBe('server-delete-title')
  const submit = document.querySelector<HTMLButtonElement>('.danger-button')!
  expect(submit.textContent).toBe('删除中…')
  expect(submit.querySelector('.spin')).not.toBeNull()
  expect(submit.disabled).toBe(true)
  expect(document.querySelector<HTMLInputElement>('.server-delete-confirm input')?.disabled).toBe(true)
  await act(async () => {
    submit.click()
    document.querySelector<HTMLButtonElement>('.dialog-close')!.click()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    document.querySelector<HTMLElement>('.dialog-backdrop')?.click()
  })
  expect(onCancel).not.toHaveBeenCalled()
  expect(onSubmit).not.toHaveBeenCalled()
})

it('keeps the failure in the dialog and allows retry with the selected uninstall option', async () => {
  const onSubmit = vi.fn()
  await act(async () => root.render(<DeleteServerDialog server={server} busy={false} error="删除失败，请重试" onCancel={vi.fn()} onSubmit={onSubmit} />))
  expect(document.querySelector('[role="alert"]')?.textContent).toBe('删除失败，请重试')
  await act(async () => document.querySelector<HTMLInputElement>('.server-delete-option input')!.click())
  const submit = document.querySelector<HTMLButtonElement>('.danger-button')!
  expect(submit.disabled).toBe(true)
  await typeConfirmName(server.name)
  await act(async () => submit.click())
  expect(onSubmit).toHaveBeenCalledExactlyOnceWith(true)
})

it('requires the exact server name before deletion is enabled', async () => {
  await act(async () => root.render(<DeleteServerDialog server={server} busy={false} onCancel={vi.fn()} onSubmit={vi.fn()} />))
  const submit = document.querySelector<HTMLButtonElement>('.danger-button')!
  await typeConfirmName('测试')
  expect(submit.disabled).toBe(true)
  await typeConfirmName(` ${server.name} `)
  expect(submit.disabled).toBe(false)
})

it('shows the Controller-computed impact, including paths of other entry servers', async () => {
  const request = vi.fn().mockResolvedValue({ impact: {
    paths: [{ path_id: 3, name: '香港-东京', root_server_id: 9, root_server_name: '香港', enabled: false, effect: 'truncated', reason: 'hop', from_position: 1, removed_steps: 2, remaining_steps: 0, disabled: true }],
    deleted_path_count: 0, truncated_path_count: 1, kept_direct_count: 0, other_root_path_count: 1, removed_step_count: 2,
    routing_rule_count: 0, plans: [{ id: 4, name: '基础套餐' }], exception_count: 0, inbounds: [], port_forwards: [], tunnels: [], forwards_deleted: true,
  } })
  await act(async () => root.render(<DeleteServerDialog server={server} client={{ request }} busy={false} onCancel={vi.fn()} onSubmit={vi.fn()} />))
  expect(request).toHaveBeenCalledWith('/servers/1/deletion-preview')
  const text = document.querySelector('.server-delete-impact')?.textContent || ''
  expect(text).toContain('香港-东京')
  expect(text).toContain('已停用')
  expect(text).toContain('其他入口服务器')
  expect(text).toContain('基础套餐')
})

async function typeConfirmName(value: string) {
  const input = document.querySelector<HTMLInputElement>('.server-delete-confirm input')!
  await act(async () => {
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!
    setter.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
