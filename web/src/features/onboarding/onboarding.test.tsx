// @vitest-environment jsdom
import React, { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { LazyMotion, domAnimation } from 'motion/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { OnboardingDashboardCard, OnboardingProvider, OnboardingTopbarButton } from './OnboardingProvider'
import { onboardingContent } from './content'
import type { OnboardingItemKey, OnboardingView } from './types'

const requiredKeys: OnboardingItemKey[] = ['password', 'controller_url', 'node_defaults', 'server', 'entry', 'access', 'subscription']
const recommendedKeys: OnboardingItemKey[] = ['two_factor', 'backup', 'notifications', 'dns_credentials', 'updates', 'audit', 'security', 'retention']

function makeView(overrides: Partial<OnboardingView> = {}): OnboardingView {
  return {
    state: { status: 'pending', password_rotated: false, skipped: [], confirmed: [] },
    mode: 'fresh',
    show_welcome: true,
    items: [
      ...requiredKeys.map(key => ({ key, level: 'required' as const, status: 'pending' as const })),
      ...recommendedKeys.map(key => ({ key, level: 'recommended' as const, status: 'pending' as const })),
    ],
    required_total: requiredKeys.length,
    required_done: 0,
    can_complete: false,
    tours: {},
    bootstrap_admin: true,
    ...overrides,
  }
}

let container: HTMLDivElement
let root: Root

beforeEach(() => {
  ;(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  sessionStorage.clear()
})

afterEach(() => {
  act(() => root.unmount())
  container.remove()
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

async function settle() {
  for (let index = 0; index < 8; index++) {
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 0)) })
  }
}

async function waitFor(check: () => boolean) {
  for (let index = 0; index < 40; index++) {
    if (check()) return
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 10)) })
  }
  throw new Error('condition not met')
}

function render(view: OnboardingView) {
  const calls: Array<{ path: string; body?: any }> = []
  const client = {
    request: vi.fn(async (path: string, init?: RequestInit) => {
      const body = init?.body ? JSON.parse(String(init.body)) : undefined
      calls.push({ path, body })
      if (path === '/onboarding' && body?.action === 'start') return { ...view, show_welcome: false, mode: 'tracked', state: { ...view.state, status: 'in_progress' } }
      if (path === '/onboarding/tours') return { tours: { proxy_path: body.status } }
      return view
    }),
  }
  const navigate = vi.fn()
  act(() => root.render(<LazyMotion features={domAnimation}>
    <OnboardingProvider client={client} isAdmin navigate={navigate} realtimeRevision={0}>
      <OnboardingTopbarButton />
      <OnboardingDashboardCard />
    </OnboardingProvider>
  </LazyMotion>))
  return { calls, navigate }
}

const text = () => document.body.textContent || ''

it('opens the setup guide from the welcome screen on a new panel', async () => {
  const { calls } = render(makeView())
  await settle()
  await waitFor(() => text().includes('欢迎使用 OBoard'))
  expect(document.querySelector('.onboarding-card')).not.toBeNull()

  const start = [...document.querySelectorAll('button')].find(button => button.textContent === '开始配置')!
  await act(async () => start.click())
  await settle()
  await waitFor(() => Boolean(document.querySelector('.onboarding-guide')))
  expect(calls.some(call => call.body?.action === 'start')).toBe(true)
  expect(text()).not.toContain('欢迎使用 OBoard')
  expect(document.querySelector('.onboarding-step.is-active')?.textContent).toContain('修改初始密码')
  expect(document.querySelectorAll('.onboarding-step')).toHaveLength(requiredKeys.length)
})

it('keeps an established panel quiet but reachable from the top bar', async () => {
  render(makeView({ mode: 'established', show_welcome: false }))
  await settle()
  expect(text()).not.toContain('欢迎使用 OBoard')
  expect(document.querySelector('.onboarding-card')).toBeNull()
  expect(document.querySelector('.onboarding-resume')).toBeNull()

  const entry = document.querySelector<HTMLButtonElement>('.onboarding-topbar-button')!
  expect(entry.getAttribute('aria-label')).toBe('初始化与教程')
  await act(async () => entry.click())
  await settle()
  await waitFor(() => Boolean(document.querySelector('.onboarding-guide')))
  expect(document.querySelector('.onboarding-tabs [aria-selected="true"]')?.textContent).toBe('使用教程')
})

it('describes every checklist item in the guide copy', () => {
  for (const key of [...requiredKeys, ...recommendedKeys]) {
    const content = onboardingContent[key]
    expect(content.title, key).toBeTruthy()
    expect(content.description, key).toBeTruthy()
  }
})
