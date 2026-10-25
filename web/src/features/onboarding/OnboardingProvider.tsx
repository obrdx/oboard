import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { AnimatePresence, m } from 'motion/react'
import { ListChecks, X } from 'lucide-react'
import { writeStoredValue } from '../../browser-storage'
import { actOnboarding, fetchOnboarding, recordTour } from './api'
import { firstOpenRequired, guideTracked, type OnboardingTarget } from './content'
import type { OnboardingAction, OnboardingClient, OnboardingItemKey, OnboardingView } from './types'
import './onboarding.css'

const WelcomeScreen = React.lazy(() => import('./WelcomeScreen').then(module => ({ default: module.WelcomeScreen })))
const SetupGuide = React.lazy(() => import('./SetupGuide').then(module => ({ default: module.SetupGuide })))
const ProxyPathDemoDialog = React.lazy(() => import('./demo/ProxyPathDemoDialog').then(module => ({ default: module.ProxyPathDemoDialog })))

export type GuideSection = 'required' | 'recommended' | 'tutorials'

type OnboardingContextValue = {
  available: boolean
  view: OnboardingView | null
  openGuide: (section?: GuideSection, item?: OnboardingItemKey) => void
  openDemo: () => void
}

const OnboardingContext = createContext<OnboardingContextValue>({ available: false, view: null, openGuide: () => undefined, openDemo: () => undefined })

export function useOnboarding() {
  return useContext(OnboardingContext)
}

const SETTINGS_LOCATION_KEY = 'oboard.settings.location'
const RESUMED_KEY = 'oboard.onboarding.resumed'
const RESUME_HIDDEN_KEY = 'oboard.onboarding.resume-hidden'

function sessionFlag(key: string) {
  try { return sessionStorage.getItem(key) === '1' } catch { return false }
}

function setSessionFlag(key: string) {
  try { sessionStorage.setItem(key, '1') } catch { /* private mode */ }
}

// OnboardingProvider owns the initialization guide for administrators: it
// keeps the derived checklist current, opens the welcome screen on a new
// panel, resumes an unfinished guide once per session, and routes the guide's
// shortcuts to the real pages. Everything heavy is loaded on first use.
export function OnboardingProvider({ client, isAdmin, navigate, realtimeRevision, children }: {
  client: OnboardingClient
  isAdmin: boolean
  navigate: (tab: string) => void
  realtimeRevision: number
  children: React.ReactNode
}) {
  const [view, setView] = useState<OnboardingView | null>(null)
  const [welcomeOpen, setWelcomeOpen] = useState(false)
  const [guide, setGuide] = useState<{ section: GuideSection; item?: OnboardingItemKey } | null>(null)
  const [demoOpen, setDemoOpen] = useState(false)
  const [resumeVisible, setResumeVisible] = useState(false)
  const loadedRef = useRef(false)

  const refresh = useCallback(async () => {
    if (!isAdmin) return
    try {
      const next = await fetchOnboarding(client)
      setView(next)
      return next
    } catch {
      return undefined
    }
  }, [client, isAdmin])

  useEffect(() => {
    if (!isAdmin) {
      setView(null)
      loadedRef.current = false
      return
    }
    let active = true
    void refresh().then(next => {
      if (!active || !next || loadedRef.current) return
      loadedRef.current = true
      if (next.show_welcome) {
        setWelcomeOpen(true)
      } else if (next.state.status === 'in_progress' && firstOpenRequired(next) && !sessionFlag(RESUMED_KEY)) {
        setSessionFlag(RESUMED_KEY)
        setGuide({ section: 'required' })
      }
      if (guideTracked(next) && !sessionFlag(RESUME_HIDDEN_KEY)) setResumeVisible(true)
    })
    return () => { active = false }
  }, [isAdmin, refresh])

  // Checklist items follow live data (a server coming online, an entry being
  // created), so re-read after realtime changes while the guide is tracked.
  useEffect(() => {
    if (!isAdmin || !loadedRef.current) return
    if (!guide && !guideTracked(view)) return
    const timer = window.setTimeout(() => { void refresh() }, 1200)
    return () => window.clearTimeout(timer)
  }, [realtimeRevision])

  const act = useCallback(async (action: OnboardingAction, item?: OnboardingItemKey) => {
    const next = await actOnboarding(client, action, item)
    setView(next)
    return next
  }, [client])

  const go = useCallback((target: OnboardingTarget) => {
    if ('category' in target) {
      writeStoredValue(SETTINGS_LOCATION_KEY, JSON.stringify({ category: target.category, sections: { [target.category]: target.section } }))
    }
    setGuide(null)
    if (guideTracked(view)) setResumeVisible(true)
    navigate(target.tab)
  }, [navigate, view])

  const openGuide = useCallback((section: GuideSection = 'required', item?: OnboardingItemKey) => {
    setWelcomeOpen(false)
    setGuide({ section, item })
  }, [])

  const openDemo = useCallback(() => {
    setDemoOpen(true)
    void recordTour(client, 'proxy_path', 'started').then(tours => setView(current => current ? { ...current, tours } : current)).catch(() => undefined)
  }, [client])

  const finishDemo = useCallback(() => {
    void recordTour(client, 'proxy_path', 'completed').then(tours => setView(current => current ? { ...current, tours } : current)).catch(() => undefined)
  }, [client])

  const value = useMemo<OnboardingContextValue>(() => ({ available: isAdmin && Boolean(view), view, openGuide, openDemo }), [isAdmin, view, openGuide, openDemo])

  const resumeShown = resumeVisible && !guide && !welcomeOpen && !demoOpen && guideTracked(view)

  return <OnboardingContext.Provider value={value}>
    {children}
    <React.Suspense fallback={null}>
      {welcomeOpen && view && <WelcomeScreen
        onStart={() => { void act('start'); openGuide('required') }}
        onDemo={() => { setWelcomeOpen(false); void act('start'); openDemo() }}
        onLater={() => { setWelcomeOpen(false); void act('dismiss'); setResumeVisible(true) }}
      />}
      {guide && view && <SetupGuide
        view={view}
        client={client}
        initialSection={guide.section}
        initialItem={guide.item}
        act={act}
        refresh={refresh}
        go={go}
        openDemo={() => { setGuide(null); openDemo() }}
        onClose={() => { setGuide(null); if (guideTracked(view)) setResumeVisible(true) }}
      />}
      {demoOpen && <ProxyPathDemoDialog
        onClose={() => setDemoOpen(false)}
        onFinished={finishDemo}
        onTry={() => navigate('proxy-paths')}
      />}
    </React.Suspense>
    <AnimatePresence>
      {resumeShown && view && <m.div
        key="resume"
        className="onboarding-resume"
        role="complementary"
        aria-label="初始化进度"
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        exit={{ opacity: 0, y: 8 }}
        transition={{ type: 'spring', stiffness: 420, damping: 34 }}
      >
        <button type="button" className="onboarding-resume-main" onClick={() => openGuide('required')}>
          <ListChecks size={16} />
          <span><strong>初始化配置</strong><small>必要配置 {view.required_done} / {view.required_total}</small></span>
          <span className="onboarding-resume-bar" aria-hidden="true"><i style={{ transform: `scaleX(${view.required_total ? view.required_done / view.required_total : 0})` }} /></span>
        </button>
        <button type="button" className="ghost icon-button onboarding-resume-close" aria-label="本次会话不再显示" title="本次会话不再显示" onClick={() => { setResumeVisible(false); setSessionFlag(RESUME_HIDDEN_KEY) }}><X size={14} /></button>
      </m.div>}
    </AnimatePresence>
  </OnboardingContext.Provider>
}

// OnboardingTopbarButton is the permanent entry to the guide and tutorials.
export function OnboardingTopbarButton() {
  const { available, view, openGuide } = useOnboarding()
  if (!available) return null
  const pending = guideTracked(view) && (view?.required_done || 0) < (view?.required_total || 0)
  return <button type="button" className="ghost icon-button onboarding-topbar-button" onClick={() => openGuide(guideTracked(view) ? 'required' : 'tutorials')} aria-label="初始化与教程" title="初始化与教程">
    <ListChecks size={16} />
    {pending && <i className="onboarding-topbar-dot" aria-hidden="true" />}
  </button>
}

// OnboardingDashboardCard summarises progress on the overview page until the
// guide is completed.
export function OnboardingDashboardCard() {
  const { available, view, openGuide, openDemo } = useOnboarding()
  if (!available || !view || !guideTracked(view)) return null
  const ratio = view.required_total ? view.required_done / view.required_total : 0
  return <section className="onboarding-card" aria-label="初始化配置">
    <div className="onboarding-card-copy">
      <strong>完成面板初始化配置</strong>
      <span>已完成 {view.required_done} / {view.required_total} 项必要配置。未完成前，服务器可能无法接入，用户也无法获得可用的订阅。</span>
      <span className="onboarding-card-bar" aria-hidden="true"><i style={{ transform: `scaleX(${ratio})` }} /></span>
    </div>
    <div className="onboarding-card-actions">
      <button type="button" className="ghost" onClick={openDemo}>代理链路演示</button>
      <button type="button" onClick={() => openGuide('required')}>继续配置</button>
    </div>
  </section>
}
