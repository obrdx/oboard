import { animate, type MotionValue } from 'motion/react'

export class DemoAborted extends Error {
  constructor() {
    super('demo aborted')
    this.name = 'DemoAborted'
  }
}

export type DemoEase = readonly [number, number, number, number]

export const demoEaseOut: DemoEase = [0.22, 1, 0.36, 1]
export const demoEaseInOut: DemoEase = [0.65, 0, 0.35, 1]

export type DemoTweenOptions = { duration: number; ease?: DemoEase; onUpdate?: (value: number) => void }

type Controls = { pause(): void; play(): void; stop(): void; speed: number; then(resolve: () => void, reject?: (reason: unknown) => void): Promise<void> }

type Waiter = { remaining: number; startedAt: number; timer: ReturnType<typeof setTimeout> | null; resolve: () => void; reject: (reason: unknown) => void }

// DemoTimeline runs one chapter script. Every wait and tween goes through it so
// pause, speed and abort apply to the whole timeline at once. Playback time is
// tracked separately from wall time so a progress bar survives pause and speed
// changes. A dry timeline jumps every value to its end and only sums
// durations, which gives each chapter its exact length without an estimate.
export class DemoTimeline {
  readonly dry: boolean
  private speedValue = 1
  private pausedValue = false
  private abortedValue = false
  private instant: boolean
  private waiters = new Set<Waiter>()
  private controls = new Map<Controls, (reason: unknown) => void>()
  private elapsedMs = 0
  private resumedAt: number
  private now: () => number

  constructor(options: { dry?: boolean; instant?: boolean; speed?: number; now?: () => number } = {}) {
    this.dry = Boolean(options.dry)
    this.instant = Boolean(options.instant)
    this.speedValue = options.speed || 1
    this.now = options.now || (() => performance.now())
    this.resumedAt = this.now()
  }

  get aborted() { return this.abortedValue }
  get paused() { return this.pausedValue }
  get speed() { return this.speedValue }

  // elapsed returns playback milliseconds, scaled by speed and frozen while paused.
  elapsed() {
    if (this.dry) return this.elapsedMs
    if (this.pausedValue || this.abortedValue) return this.elapsedMs
    return this.elapsedMs + (this.now() - this.resumedAt) * this.speedValue
  }

  wait(ms: number): Promise<void> {
    if (this.abortedValue) return Promise.reject(new DemoAborted())
    if (this.dry) {
      this.elapsedMs += ms
      return Promise.resolve()
    }
    if (ms <= 0) return Promise.resolve()
    return new Promise<void>((resolve, reject) => {
      const waiter: Waiter = { remaining: ms, startedAt: 0, timer: null, resolve, reject }
      this.waiters.add(waiter)
      if (!this.pausedValue) this.schedule(waiter)
    })
  }

  tween(value: MotionValue<number>, to: number, options: DemoTweenOptions): Promise<void> {
    if (this.abortedValue) return Promise.reject(new DemoAborted())
    if (this.dry || this.instant || options.duration <= 0) {
      if (this.dry) this.elapsedMs += options.duration
      value.set(to)
      options.onUpdate?.(to)
      return Promise.resolve()
    }
    return this.track(animate(value, to, { duration: options.duration / 1000, ease: options.ease || demoEaseOut, onUpdate: options.onUpdate }) as unknown as Controls)
  }

  // progress animates 0 → 1 and reports each frame, for motion that follows a
  // computed curve rather than a straight line between two values.
  progress(options: DemoTweenOptions & { onUpdate: (value: number) => void }): Promise<void> {
    if (this.abortedValue) return Promise.reject(new DemoAborted())
    if (this.dry || this.instant || options.duration <= 0) {
      if (this.dry) this.elapsedMs += options.duration
      options.onUpdate(1)
      return Promise.resolve()
    }
    return this.track(animate(0, 1, { duration: options.duration / 1000, ease: options.ease || demoEaseOut, onUpdate: options.onUpdate }) as unknown as Controls)
  }

  pause() {
    if (this.pausedValue || this.abortedValue || this.dry) return
    this.elapsedMs = this.elapsed()
    this.pausedValue = true
    for (const waiter of this.waiters) {
      if (waiter.timer) clearTimeout(waiter.timer)
      waiter.timer = null
      waiter.remaining -= (this.now() - waiter.startedAt) * this.speedValue
    }
    for (const control of this.controls.keys()) control.pause()
  }

  resume() {
    if (!this.pausedValue || this.abortedValue) return
    this.pausedValue = false
    this.resumedAt = this.now()
    for (const waiter of this.waiters) this.schedule(waiter)
    for (const control of this.controls.keys()) control.play()
  }

  setSpeed(speed: number) {
    if (speed <= 0 || speed === this.speedValue) return
    if (!this.pausedValue && !this.dry) {
      this.elapsedMs = this.elapsed()
      this.resumedAt = this.now()
      for (const waiter of this.waiters) {
        if (waiter.timer) clearTimeout(waiter.timer)
        waiter.remaining -= (this.now() - waiter.startedAt) * this.speedValue
      }
    }
    this.speedValue = speed
    for (const control of this.controls.keys()) control.speed = speed
    if (!this.pausedValue) for (const waiter of this.waiters) this.schedule(waiter)
  }

  abort() {
    if (this.abortedValue) return
    this.elapsedMs = this.elapsed()
    this.abortedValue = true
    for (const waiter of this.waiters) {
      if (waiter.timer) clearTimeout(waiter.timer)
      waiter.reject(new DemoAborted())
    }
    this.waiters.clear()
    for (const [control, reject] of this.controls) {
      control.stop()
      reject(new DemoAborted())
    }
    this.controls.clear()
  }

  private schedule(waiter: Waiter) {
    waiter.startedAt = this.now()
    waiter.timer = setTimeout(() => {
      this.waiters.delete(waiter)
      waiter.resolve()
    }, Math.max(0, waiter.remaining / this.speedValue))
  }

  private track(control: Controls): Promise<void> {
    control.speed = this.speedValue
    if (this.pausedValue) control.pause()
    return new Promise<void>((resolve, reject) => {
      this.controls.set(control, reject)
      control.then(() => {
        this.controls.delete(control)
        if (this.abortedValue) reject(new DemoAborted())
        else resolve()
      }, reason => {
        this.controls.delete(control)
        reject(this.abortedValue ? new DemoAborted() : reason)
      })
    })
  }
}
