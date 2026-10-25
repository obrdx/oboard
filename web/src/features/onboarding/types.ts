export type OnboardingStatus = 'pending' | 'in_progress' | 'completed' | 'dismissed'
export type OnboardingItemStatus = 'done' | 'pending' | 'skipped'
export type OnboardingMode = 'fresh' | 'established' | 'tracked'

export type OnboardingItemKey =
  | 'password' | 'controller_url' | 'node_defaults' | 'server' | 'entry' | 'access' | 'subscription'
  | 'two_factor' | 'backup' | 'notifications' | 'dns_credentials' | 'updates' | 'audit' | 'security' | 'retention'

export type OnboardingItem = {
  key: OnboardingItemKey
  level: 'required' | 'recommended'
  status: OnboardingItemStatus
  detail?: Record<string, any>
}

export type OnboardingView = {
  state: {
    status: OnboardingStatus
    password_rotated: boolean
    skipped: string[]
    confirmed: string[]
    started_at?: string
    completed_at?: string
  }
  mode: OnboardingMode
  show_welcome: boolean
  items: OnboardingItem[]
  required_total: number
  required_done: number
  can_complete: boolean
  tours: Record<string, 'started' | 'completed'>
  bootstrap_admin: boolean
}

export type OnboardingAction = 'start' | 'dismiss' | 'skip' | 'unskip' | 'confirm' | 'unconfirm' | 'complete' | 'reopen'

export type OnboardingClient = { request: (path: string, init?: RequestInit) => Promise<any> }
