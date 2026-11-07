// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'

import { createAPIClientFactory } from './api-client'

const createClient = createAPIClientFactory(path => path, (data, res) => {
  const error = new Error(data?.error || res.statusText) as Error & { status?: number }
  error.status = res.status
  return error
})

afterEach(() => {
  sessionStorage.clear()
  vi.unstubAllGlobals()
})

it('uses renewed CSRF credentials on every request from an existing cookie client', async () => {
  const fetcher = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response(JSON.stringify({ data: {} }), { status: 200 }))
  vi.stubGlobal('fetch', fetcher)
  sessionStorage.setItem('oboard.csrf', 'before-renewal')
  const client = createClient('cookie')
  await client.request('/controller-update/activity', { method: 'POST' })
  expect(new Headers(fetcher.mock.calls[0][1]?.headers).get('x-oboard-csrf')).toBe('before-renewal')
  sessionStorage.setItem('oboard.csrf', 'after-renewal')
  await client.request('/controller-update/install', { method: 'POST' })
  await client.requestV2('/changesets', { method: 'POST' })
  await client.upload('/upload', new FormData())
  for (const call of fetcher.mock.calls.slice(1)) {
    expect(new Headers(call[1]?.headers).get('x-oboard-csrf')).toBe('after-renewal')
  }
  sessionStorage.removeItem('oboard.csrf')
  await client.request('/controller-update/activity', { method: 'POST' })
  expect(new Headers(fetcher.mock.calls.at(-1)?.[1]?.headers).has('x-oboard-csrf')).toBe(false)
})

it('does not send cookie CSRF credentials with bearer authentication', async () => {
  const fetcher = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response('{}', { status: 200 }))
  vi.stubGlobal('fetch', fetcher)
  sessionStorage.setItem('oboard.csrf', 'cookie-token')
  await createClient('bearer-token').request('/controller-update/install', { method: 'POST' })
  const headers = new Headers(fetcher.mock.calls[0][1]?.headers)
  expect(headers.get('authorization')).toBe('Bearer bearer-token')
  expect(headers.has('x-oboard-csrf')).toBe(false)
})

const csrfRejection = () => new Response(JSON.stringify({ error: 'invalid CSRF token' }), { status: 403 })

it('renews a stale CSRF token once and resends the rejected write', async () => {
  const fetcher = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input).endsWith('/auth/session')) return new Response(JSON.stringify({ csrf_token: 'fresh' }), { status: 200 })
    return new Headers(init?.headers).get('x-oboard-csrf') === 'fresh' ? new Response(JSON.stringify({ ok: true }), { status: 200 }) : csrfRejection()
  })
  vi.stubGlobal('fetch', fetcher)
  sessionStorage.setItem('oboard.csrf', 'stale')
  await expect(createClient('cookie').request('/servers', { method: 'POST', body: '{}' })).resolves.toEqual({ ok: true })
  expect(sessionStorage.getItem('oboard.csrf')).toBe('fresh')
  expect(fetcher).toHaveBeenCalledTimes(3)
})

it('keeps the CSRF error when renewal fails or the retry is rejected again', async () => {
  const fetcher = vi.fn(async (input: RequestInfo | URL) => String(input).endsWith('/auth/session') ? new Response('{}', { status: 401 }) : csrfRejection())
  vi.stubGlobal('fetch', fetcher)
  sessionStorage.setItem('oboard.csrf', 'stale')
  await expect(createClient('cookie').request('/servers', { method: 'POST', body: '{}' })).rejects.toThrow('invalid CSRF token')
  expect(fetcher).toHaveBeenCalledTimes(2)
})

it('does not renew for reads, bearer clients or other 403 answers', async () => {
  const fetcher = vi.fn(async () => new Response(JSON.stringify({ error: 'forbidden' }), { status: 403 }))
  vi.stubGlobal('fetch', fetcher)
  await expect(createClient('cookie').request('/servers', { method: 'POST', body: '{}' })).rejects.toThrow('forbidden')
  const rejecting = vi.fn(async () => csrfRejection())
  vi.stubGlobal('fetch', rejecting)
  await expect(createClient('cookie').request('/servers')).rejects.toThrow('invalid CSRF token')
  await expect(createClient('bearer').request('/servers', { method: 'POST', body: '{}' })).rejects.toThrow('invalid CSRF token')
  expect(fetcher).toHaveBeenCalledTimes(1)
  expect(rejecting).toHaveBeenCalledTimes(2)
})
