import { describe, expect, it } from 'vitest'
import { motionValue } from 'motion/react'
import { DemoTimeline } from './engine'
import { chapterDuration, proxyPathChapters, type DemoRuntime } from './proxy-path-script'
import { STAGE_HEIGHT, STAGE_WIDTH, nodeHeight, nodeWidth, toStage, type DemoScene } from './scene'

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
    cameraX: motionValue(scene.camera.x),
    cameraY: motionValue(scene.camera.y),
    cameraScale: motionValue(scene.camera.scale),
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
    // Links run top to bottom, like the real canvas.
    const from = scene.nodes.find(node => node.id === edge.from)!
    const to = scene.nodes.find(node => node.id === edge.to)!
    expect(to.y, `${edge.id} runs downward`).toBeGreaterThan(from.y + nodeHeight(from) - 1)
  }
  for (const node of scene.nodes) {
    const topLeft = toStage(scene.camera, { x: node.x, y: node.y })
    const bottomRight = toStage(scene.camera, { x: node.x + nodeWidth(node), y: node.y + nodeHeight(node) })
    expect(topLeft.x, node.id).toBeGreaterThanOrEqual(0)
    expect(topLeft.y, node.id).toBeGreaterThanOrEqual(scene.chrome ? 52 : 0)
    expect(bottomRight.x, node.id).toBeLessThanOrEqual(STAGE_WIDTH)
    expect(bottomRight.y, node.id).toBeLessThanOrEqual(STAGE_HEIGHT)
  }
}

describe('proxy path demo script', () => {
  it.each(proxyPathChapters.map((chapter, index) => [chapter.id, index] as const))('chapter %s ends in a settled, consistent scene', async (_id, index) => {
    const { scene, cursor, captions } = await dryRun(index)
    expectConsistent(scene)
    expect(scene.menu).toBeNull()
    expect(scene.dialog).toBeNull()
    expect(scene.drawer).toBeNull()
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

  it('makes the last server the exit until an exit is attached', async () => {
    const relay = await dryRun(proxyPathChapters.findIndex(chapter => chapter.id === 'connect'))
    expect(relay.scene.edges.map(edge => edge.id).sort()).toEqual(['entry-tokyo', 'tokyo-hk'])
    expect(relay.scene.nodes.find(node => node.id === 'hk')?.rows?.[0].label).toBe('出口地区')
    const exit = await dryRun(proxyPathChapters.findIndex(chapter => chapter.id === 'exit'))
    expect(exit.scene.edges.map(edge => edge.id).sort()).toEqual(['entry-tokyo', 'hk-warp', 'tokyo-hk'])
    expect(exit.scene.nodes.find(node => node.id === 'hk')?.rows?.some(row => row.label === '出口地区')).toBe(false)
  })

  it('inserts routing between the server and the exit it already had', async () => {
    const routed = await dryRun(proxyPathChapters.findIndex(chapter => chapter.id === 'routing'))
    expect(routed.scene.edges.map(edge => edge.id).sort()).toEqual(['entry-tokyo', 'hk-routing', 'routing-warp', 'tokyo-hk'])
    expect(routed.scene.nodes.find(node => node.id === 'routing')?.meta).toBe('1 条规则 · 未命中继续链路')
  })

  it('reports the exact playback length of each chapter', async () => {
    for (const chapter of proxyPathChapters) {
      const duration = await chapterDuration(chapter, new DemoTimeline({ dry: true }), motionValue)
      expect(duration).toBeGreaterThan(3000)
      expect(duration).toBeLessThan(30000)
    }
  })
})
