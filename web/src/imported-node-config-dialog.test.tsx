// @vitest-environment jsdom
import * as React from 'react'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import { ImportedNodeConfigDialog } from './main'

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
const node = { id: 2, name: 'NTT', protocol: 'http', target_address: '180.53.81.127', target_port: 8080, config_json: '{"tls":{"enabled":true}}', enabled: true, expose_to_users: false, scope: 'global', region_mode: 'auto' } as any
const data = { proxy_path_steps: [{ id: 1, path_id: 113, external_outbound_id: 2, node_type: 'imported', position: 1 }, { id: 2, path_id: 114, external_outbound_id: 2, node_type: 'imported', position: 1 }] }
const button = (label: string) => [...document.querySelectorAll<HTMLButtonElement>('button')].find(el => el.textContent === label)!
async function edit(label: string, value: string) {
  const input = document.querySelector<HTMLInputElement | HTMLTextAreaElement>(`[aria-label="${label}"]`)!
  const prototype = input.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype
  await act(async () => {
    Object.getOwnPropertyDescriptor(prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
it('updates the existing shared node, refreshes references and excludes HTTP subscription exposure', async () => {
  const request = vi.fn().mockResolvedValue({})
  const load = vi.fn().mockResolvedValue(undefined)
  const onClose = vi.fn()
  await act(async () => root.render(<ImportedNodeConfigDialog node={node} data={data} client={{ request } as any} load={load} onClose={onClose} />))
  expect(document.body.textContent).toContain('已被 2 条链路引用')
  expect(document.body.textContent).not.toContain('允许订阅授权')
  await edit('目标地址', 'proxy.example.com')
  await edit('协议配置（JSON）', '{"username":"alice","password":"new-secret","tls":{"enabled":false}}')
  await act(async () => button('保存并同步').click())
  expect(request).toHaveBeenCalledTimes(1)
  expect(request.mock.calls[0][0]).toBe('/external-outbounds/2')
  const payload = JSON.parse(request.mock.calls[0][1].body)
  expect(payload).toMatchObject({ target_address: 'proxy.example.com', expose_to_users: false, region_code: '' })
  expect(JSON.parse(payload.config_json)).toMatchObject({ password: 'new-secret', tls: { enabled: false } })
  expect(load).toHaveBeenCalledTimes(1)
  expect(onClose).toHaveBeenCalledTimes(1)
})
it('keeps invalid JSON and rejected saves open with an inline error', async () => {
  const request = vi.fn().mockRejectedValue(new Error('invalid configuration'))
  const onClose = vi.fn()
  await act(async () => root.render(<ImportedNodeConfigDialog node={node} data={data} client={{ request } as any} load={vi.fn()} onClose={onClose} />))
  await edit('协议配置（JSON）', '[]')
  await act(async () => button('保存并同步').click())
  expect(request).not.toHaveBeenCalled()
  expect(document.querySelector('[role="alert"]')?.textContent).toContain('JSON 对象')
  await edit('协议配置（JSON）', '{}')
  await act(async () => button('保存并同步').click())
  expect(request).toHaveBeenCalledTimes(1)
  expect(document.querySelector('[role="alert"]')).not.toBeNull()
  expect(onClose).not.toHaveBeenCalled()
})
