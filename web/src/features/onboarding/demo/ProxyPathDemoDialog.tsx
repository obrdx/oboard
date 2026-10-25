import React, { useCallback, useEffect, useRef, useState } from 'react'
import { AnimatePresence, m, motionValue, useMotionValue, useReducedMotion } from 'motion/react'
import { Check, ChevronLeft, ChevronRight, Pause, Play, RotateCcw } from 'lucide-react'
import { Dialog } from '../../../components/ui/dialog'
import { DemoAborted, DemoTimeline } from './engine'
import { DemoStage, type DemoCamera } from './DemoStage'
import { chapterDuration, proxyPathChapters, type DemoChapter, type DemoRuntime } from './proxy-path-script'
import type { Camera, DemoScene } from './scene'
import './demo.css'

const CHAPTER_GAP_MS = 1200
const SPEEDS = [1, 1.5] as const

export function ProxyPathDemoDialog({ isOpen = true, onClose, onTry, onFinished, chapters = proxyPathChapters }: {
  isOpen?: boolean
  onClose: () => void
  onTry?: () => void
  onFinished?: () => void
  chapters?: DemoChapter[]
}) {
  return <Dialog isOpen={isOpen} onClose={onClose} title="代理链路演示" size="xl" className="pd-dialog-host">
    <div className="pd-host"><ProxyPathDemo chapters={chapters} onFinished={onFinished} onTry={() => { onClose(); onTry?.() }} /></div>
  </Dialog>
}

export function ProxyPathDemo({ chapters, onTry, onFinished }: { chapters: DemoChapter[]; onTry: () => void; onFinished?: () => void }) {
  const reducedMotion = Boolean(useReducedMotion())
  const [chapter, setChapter] = useState(0)
  const [run, setRun] = useState(0)
  const [scene, setScene] = useState<DemoScene>(() => chapters[0].start())
  const [caption, setCaption] = useState('')
  const [playing, setPlaying] = useState(true)
  const [speed, setSpeed] = useState<number>(1)
  const [finished, setFinished] = useState(false)
  const [done, setDone] = useState<Set<number>>(() => new Set())
  const [durations, setDurations] = useState<number[]>([])
  const cursorX = useMotionValue(0)
  const cursorY = useMotionValue(0)
  // Each chapter run gets its own camera, so the layer fading out keeps its
  // framing while the next chapter sets up a different one.
  const [camera, setCamera] = useState<DemoCamera>(() => makeCamera(chapters[0].start().camera))
  const progress = useMotionValue(0)
  const timelineRef = useRef<DemoTimeline | null>(null)
  const playingRef = useRef(playing)
  const speedRef = useRef(speed)
  const hiddenPausedRef = useRef(false)
  const onFinishedRef = useRef(onFinished)
  onFinishedRef.current = onFinished
  playingRef.current = playing
  speedRef.current = speed

  useEffect(() => {
    let cancelled = false
    void Promise.all(chapters.map(item => chapterDuration(item, new DemoTimeline({ dry: true }), value => motionValue(value)))).then(values => {
      if (!cancelled) setDurations(values)
    })
    return () => { cancelled = true }
  }, [chapters])

  useEffect(() => {
    const current = chapters[chapter]
    const timeline = new DemoTimeline({ instant: reducedMotion, speed: speedRef.current })
    timelineRef.current = timeline
    let sceneValue = current.start()
    const runCamera = makeCamera(sceneValue.camera)
    setCamera(runCamera)
    setScene(sceneValue)
    setCaption('')
    progress.set(0)
    cursorX.set(current.cursorStart?.x ?? 520)
    cursorY.set(current.cursorStart?.y ?? 480)
    if (!playingRef.current) timeline.pause()
    const runtime: DemoRuntime = {
      timeline,
      cursorX,
      cursorY,
      cameraX: runCamera.x,
      cameraY: runCamera.y,
      cameraScale: runCamera.scale,
      scene: () => sceneValue,
      update: change => {
        sceneValue = change(sceneValue)
        setScene(sceneValue)
      },
      caption: setCaption,
    }
    current.run(runtime)
      .then(async () => {
        progress.set(1)
        setDone(previous => new Set(previous).add(chapter))
        await timeline.wait(CHAPTER_GAP_MS)
        if (chapter < chapters.length - 1) setChapter(chapter + 1)
        else {
          setFinished(true)
          onFinishedRef.current?.()
        }
      })
      .catch(error => {
        if (!(error instanceof DemoAborted)) throw error
      })
    return () => timeline.abort()
  }, [chapter, run, chapters, reducedMotion, cursorX, cursorY, progress])

  // Progress is read from the timeline each frame and written to a motion
  // value, so the bar moves without re-rendering the dialog.
  useEffect(() => {
    const total = durations[chapter]
    if (!total) return
    let frame = 0
    const tick = () => {
      const timeline = timelineRef.current
      if (timeline) progress.set(Math.min(1, timeline.elapsed() / total))
      frame = requestAnimationFrame(tick)
    }
    frame = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(frame)
  }, [chapter, run, durations, progress])

  useEffect(() => {
    if (playing) timelineRef.current?.resume()
    else timelineRef.current?.pause()
  }, [playing])

  useEffect(() => { timelineRef.current?.setSpeed(speed) }, [speed])

  useEffect(() => {
    const onVisibility = () => {
      if (document.hidden && playingRef.current) {
        hiddenPausedRef.current = true
        setPlaying(false)
      } else if (!document.hidden && hiddenPausedRef.current) {
        hiddenPausedRef.current = false
        setPlaying(true)
      }
    }
    document.addEventListener('visibilitychange', onVisibility)
    return () => document.removeEventListener('visibilitychange', onVisibility)
  }, [])

  const goTo = useCallback((index: number) => {
    const next = Math.max(0, Math.min(chapters.length - 1, index))
    setFinished(false)
    setPlaying(true)
    if (next === chapter) setRun(value => value + 1)
    else setChapter(next)
  }, [chapter, chapters.length])

  const restart = () => {
    setDone(new Set())
    goTo(0)
  }

  const onKeyDown = (event: React.KeyboardEvent) => {
    const target = event.target as HTMLElement
    if (event.key === ' ' && target.tagName !== 'BUTTON') {
      event.preventDefault()
      setPlaying(value => !value)
    } else if (event.key === 'ArrowRight') {
      event.preventDefault()
      goTo(chapter + 1)
    } else if (event.key === 'ArrowLeft') {
      event.preventDefault()
      goTo(chapter - 1)
    }
  }

  const removeRipple = useCallback((id: number) => {
    setScene(current => ({ ...current, ripples: current.ripples.filter(item => item.id !== id) }))
  }, [])

  const current = chapters[chapter]
  return <div className="pd-player" tabIndex={-1} onKeyDown={onKeyDown}>
    <ol className="pd-chapters" aria-label="章节">
      {chapters.map((item, index) => <li key={item.id}>
        <button type="button" className={`pd-chapter${index === chapter ? ' is-current' : ''}${done.has(index) ? ' is-done' : ''}`} aria-current={index === chapter ? 'step' : undefined} onClick={() => goTo(index)}>
          <span className="pd-chapter-index">{done.has(index) && index !== chapter ? <Check size={12} /> : index + 1}</span>
          <span className="pd-chapter-copy"><strong>{item.title}</strong><small>{item.summary}</small></span>
          {index === chapter && <m.span className="pd-chapter-progress" style={{ scaleX: progress }} />}
        </button>
      </li>)}
    </ol>

    <div className="pd-main">
      <div className="pd-viewport">
        <AnimatePresence initial={false}>
          <m.div key={`${chapter}-${run}`} className="pd-layer" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} transition={{ duration: 0.22 }}>
            <DemoStage scene={scene} cursorX={cursorX} cursorY={cursorY} camera={camera} paused={!playing} speed={speed} reducedMotion={reducedMotion} onRippleDone={removeRipple} />
          </m.div>
        </AnimatePresence>
        <AnimatePresence>
          {finished && <m.div key="finished" className="pd-finished" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} transition={{ duration: 0.2 }}>
            <m.div className="pd-finished-card" initial={reducedMotion ? false : { scale: 0.94, y: 8 }} animate={{ scale: 1, y: 0 }} transition={{ type: 'spring', stiffness: 420, damping: 30 }}>
              <span className="pd-finished-mark"><svg viewBox="0 0 24 24" aria-hidden="true"><m.path d="M5 12.5l4.5 4.5L19 7.5" initial={reducedMotion ? false : { pathLength: 0 }} animate={{ pathLength: 1 }} transition={{ duration: 0.45, delay: 0.12 }} /></svg></span>
              <strong>演示结束</strong>
              <p>入口、中转、出口与分流规则的配置方法已演示完毕，可前往「代理拓扑」进行实际配置。</p>
              <div className="pd-finished-actions">
                <button type="button" className="ghost" onClick={restart}><RotateCcw size={15} />重新播放</button>
                <button type="button" onClick={onTry}>前往代理拓扑</button>
              </div>
            </m.div>
          </m.div>}
        </AnimatePresence>
      </div>

      <div className="pd-caption" aria-live="polite">
        <AnimatePresence mode="wait" initial={false}>
          <m.p key={caption || current.title} initial={{ opacity: 0, y: 4 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -3 }} transition={{ duration: 0.16 }}>
            {caption || current.summary}
          </m.p>
        </AnimatePresence>
      </div>

      <div className="pd-controls" role="toolbar" aria-label="播放控制">
        <button type="button" className="ghost icon-button" onClick={() => goTo(chapter - 1)} disabled={chapter === 0} aria-label="上一章" title="上一章 (←)"><ChevronLeft size={16} /></button>
        <button type="button" className="pd-play icon-button" onClick={() => finished ? restart() : setPlaying(value => !value)} aria-label={playing && !finished ? '暂停' : '播放'} title="播放 / 暂停 (空格)">
          {playing && !finished ? <Pause size={16} /> : <Play size={16} />}
        </button>
        <button type="button" className="ghost icon-button" onClick={() => goTo(chapter + 1)} disabled={chapter === chapters.length - 1} aria-label="下一章" title="下一章 (→)"><ChevronRight size={16} /></button>
        <button type="button" className="ghost icon-button" onClick={() => goTo(chapter)} aria-label="重播本章" title="重播本章"><RotateCcw size={15} /></button>
        <span className="pd-controls-spacer" />
        <span className="pd-chapter-label">{chapter + 1} / {chapters.length}</span>
        <div className="pd-speed" role="group" aria-label="播放速度">
          {SPEEDS.map(value => <button key={value} type="button" className={speed === value ? 'is-active' : ''} aria-pressed={speed === value} onClick={() => setSpeed(value)}>{value}×</button>)}
        </div>
        {onTry && <button type="button" className="ghost pd-try" onClick={onTry}>前往代理拓扑</button>}
      </div>
    </div>
  </div>
}

function makeCamera(value: Camera): DemoCamera {
  return { x: motionValue(value.x), y: motionValue(value.y), scale: motionValue(value.scale) }
}
