import { describe, expect, it } from 'vitest'
import { motionValue } from 'motion/react'
import { DemoTimeline } from './engine'
import { chapterDuration, proxyPathChapters, type DemoRuntime } from './proxy-path-script'
import { STAGE_HEIGHT, STAGE_WIDTH, nodeHeight, nodeWidth, type DemoScene } from './scene'

async function dryRun(index: number) {
  const chapter = proxyPathChapters[index]
  let scene = chapter.start()
  const cursor: Array<{ x: number; y: number }> = []
  const captions: string[] = []
  const cursorX = motionValue(chapter.cursorStart?.x ?? 0)
  const cursorY = motionValue(chapter.cursorStart?.y ?? 0)
  cursorX.on('change', () => cursor.push({ x: cursorX.get(), y: cursorY.get() }))
  const runtime: DemoRuntime = {
    timeline: new DemoTimeline({ dry: true }),
    cursorX,
    cursorY,
    scene: () => scene,
    update: change => { scene = change(scene) },
    caption: text => captions.push(text),
  }
  await chapter.run(runtime)
  return { scene, cursor, captions }
}

function expectConsistent(scene: DemoScene) {
  const ids = new Set(scene.nodes.map(node => node.id))
  expect(ids.size).toBe(scene.nodes.length)
  for (const edge of scene.edges) {
    expect(ids.has(edge.from), `${edge.id} source`).toBe(true)
    expect(ids.has(edge.to), `${edge.id} target`).toBe(true)
  }
  for (const node of scene.nodes) {
    expect(node.x).toBeGreaterThanOrEqual(0)
    expect(node.y).toBeGreaterThanOrEqual(0)
    expect(node.x + nodeWidth(node)).toBeLessThanOrEqual(STAGE_WIDTH)
    expect(node.y + nodeHeight(node)).toBeLessThanOrEqual(STAGE_HEIGHT)
  }
}

describe('proxy path demo script', () => {
  it.each(proxyPathChapters.map((chapter, index) => [chapter.id, index] as const))('chapter %s ends in a settled, consistent scene', async (_id, index) => {
    const { scene, cursor, captions } = await dryRun(index)
    expectConsistent(scene)
    expect(scene.menu).toBeNull()
    expect(scene.dialog).toBeNull()
    expect(scene.picker).toBeNull()
    expect(scene.provisional).toBeNull()
    expect(scene.cursorPressed).toBe(false)
    expect(captions.length).toBeGreaterThan(0)
    for (const point of cursor) {
      expect(point.x).toBeGreaterThanOrEqual(0)
      expect(point.x).toBeLessThanOrEqual(STAGE_WIDTH)
      expect(point.y).toBeGreaterThanOrEqual(0)
      expect(point.y).toBeLessThanOrEqual(STAGE_HEIGHT)
    }
  })

  it('starts every chapter from a consistent snapshot', () => {
    for (const chapter of proxyPathChapters) expectConsistent(chapter.start())
  })

  it('builds the chain that the concept chapter introduces', async () => {
    const built = await dryRun(proxyPathChapters.findIndex(chapter => chapter.id === 'exit'))
    expect(built.scene.edges.map(edge => edge.id).sort()).toEqual(['client-entry', 'entry-hk', 'hk-warp'])
    expect(built.scene.edges.every(edge => edge.packets)).toBe(true)
  })

  it('reports the exact playback length of each chapter', async () => {
    for (const chapter of proxyPathChapters) {
      const duration = await chapterDuration(chapter, new DemoTimeline({ dry: true }), motionValue)
      expect(duration).toBeGreaterThan(3000)
      expect(duration).toBeLessThan(25000)
    }
  })
})
