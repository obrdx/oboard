import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { motionValue } from 'motion/react'
import { DemoAborted, DemoTimeline } from './engine'

describe('DemoTimeline', () => {
  let now = 0
  beforeEach(() => {
    vi.useFakeTimers()
    now = 0
  })
  afterEach(() => vi.useRealTimers())

  const advance = async (ms: number) => {
    now += ms
    await vi.advanceTimersByTimeAsync(ms)
  }

  it('resolves waits after their playback time and freezes while paused', async () => {
    const timeline = new DemoTimeline({ now: () => now })
    const done = vi.fn()
    void timeline.wait(1000).then(done)
    await advance(400)
    timeline.pause()
    await advance(5000)
    expect(done).not.toHaveBeenCalled()
    expect(timeline.elapsed()).toBe(400)
    timeline.resume()
    await advance(599)
    expect(done).not.toHaveBeenCalled()
    await advance(1)
    expect(done).toHaveBeenCalled()
  })

  it('applies a speed change to the remaining wait and to elapsed time', async () => {
    const timeline = new DemoTimeline({ now: () => now })
    const done = vi.fn()
    void timeline.wait(1000).then(done)
    await advance(500)
    timeline.setSpeed(2)
    await advance(249)
    expect(done).not.toHaveBeenCalled()
    await advance(1)
    expect(done).toHaveBeenCalled()
    expect(timeline.elapsed()).toBe(1000)
  })

  it('rejects pending and later work once aborted', async () => {
    const timeline = new DemoTimeline({ now: () => now })
    const pending = timeline.wait(1000)
    timeline.abort()
    await expect(pending).rejects.toBeInstanceOf(DemoAborted)
    await expect(timeline.wait(10)).rejects.toBeInstanceOf(DemoAborted)
    await expect(timeline.tween(motionValue(0), 1, { duration: 100 })).rejects.toBeInstanceOf(DemoAborted)
  })

  it('dry-runs instantly, jumps values to their end and sums durations', async () => {
    const timeline = new DemoTimeline({ dry: true })
    const value = motionValue(0)
    const seen: number[] = []
    await timeline.wait(300)
    await timeline.tween(value, 40, { duration: 200 })
    await timeline.progress({ duration: 500, onUpdate: t => seen.push(t) })
    expect(value.get()).toBe(40)
    expect(seen).toEqual([1])
    expect(timeline.elapsed()).toBe(1000)
  })

  it('jumps tweens immediately in instant mode but keeps waits', async () => {
    const timeline = new DemoTimeline({ instant: true, now: () => now })
    const value = motionValue(0)
    await timeline.tween(value, 10, { duration: 400 })
    expect(value.get()).toBe(10)
    const done = vi.fn()
    void timeline.wait(200).then(done)
    await advance(199)
    expect(done).not.toHaveBeenCalled()
    await advance(1)
    expect(done).toHaveBeenCalled()
  })
})
