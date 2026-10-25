import type { MotionValue } from 'motion/react'
import { demoEaseInOut, demoEaseOut, type DemoTimeline } from './engine'
import {
  DRAWER_DONE, ENTRY_SUBMIT, ROUTING_ADD, ROUTING_DONE, TRANSPORT_CONFIRM, WORKBENCH_RESET,
  arcPoint, column, drawerTilePoint, emptyScene, entryProtocolPoint, fitCamera, inputHandle, menuItemPoint,
  menuPosition, outputHandle, routingActionPoint, toStage, transportModePoint, transportTargetPoint, travelDuration,
  type Camera, type DemoEdge, type DemoNode, type DemoScene, type MenuItemID, type Point,
} from './scene'

export type DemoRuntime = {
  timeline: DemoTimeline
  cursorX: MotionValue<number>
  cursorY: MotionValue<number>
  cameraX: MotionValue<number>
  cameraY: MotionValue<number>
  cameraScale: MotionValue<number>
  scene(): DemoScene
  update(change: (scene: DemoScene) => DemoScene): void
  caption(text: string): void
}

export type DemoChapter = {
  id: string
  title: string
  summary: string
  start: () => DemoScene
  cursorStart?: Point
  run: (runtime: DemoRuntime) => Promise<void>
}

// Card factories. Copy, roles and rows follow what the real canvas shows for
// the same object, so a viewer recognises every card on the real page.
const Y = { entry: 80, tokyo: 250, hk: 400, tail: 566, warpAfterRouting: 736 }

const cards = {
  device: (): DemoNode => ({ id: 'device', kind: 'device', x: column('device'), y: 40, title: '用户设备', role: '' }),
  site: (y: number): DemoNode => ({ id: 'site', kind: 'site', x: column('site'), y, title: '目标网站', role: '' }),
  entry: (paths = 0, y = Y.entry): DemoNode => ({
    id: 'entry', kind: 'entry', x: column('entry'), y, title: 'VLESS', role: '代理入口', badge: 'VLESS',
    rows: [{ label: '客户端连接', value: '203.0.113.11:10443', mono: true }],
    chips: ['端口可用', '用户数：3'],
    footer: paths ? `${paths} 条代理路径` : '直接接入',
  }),
  tokyo: (withEntry = true, y = Y.tokyo): DemoNode => ({
    id: 'tokyo', kind: 'server', x: column('server'), y, title: '东京-01', role: '一级服务器', status: 'online', region: 'JP',
    rows: [{ label: '公网地址', value: '203.0.113.11', mono: true }],
    handleLabel: withEntry ? 'VLESS:10443' : undefined,
  }),
  hkLoose: (): DemoNode => ({
    id: 'hk', kind: 'server', x: 668, y: Y.hk, title: '香港-02', role: '可控服务器', status: 'online', region: 'HK',
    rows: [{ label: '公网地址', value: '203.0.113.12', mono: true }],
  }),
  hk: (tail: boolean, x = column('server'), y = Y.hk): DemoNode => ({
    id: 'hk', kind: 'server', x, y, title: '香港-02', role: '链路服务器', status: 'online', region: 'HK',
    rows: tail
      ? [{ label: '出口地区', value: '香港', region: 'HK' }, { label: '公网地址', value: '203.0.113.12', mono: true }]
      : [{ label: '公网地址', value: '203.0.113.12', mono: true }],
  }),
  warp: (connected: boolean, x = column('warp'), y = Y.tail): DemoNode => connected
    ? { id: 'warp', kind: 'warp', x, y, title: 'WARP', role: '链路出口', meta: '由 香港-02 自动申请', connected: true }
    : { id: 'warp', kind: 'warp', x, y, title: 'WARP', role: '可选出口', meta: '未连接' },
  routing: (connected: boolean, x = column('routing'), y = Y.tail): DemoNode => connected
    ? { id: 'routing', kind: 'routing', x, y, title: '分流规则', role: '路径分流', rules: [{ name: '国内网站', action: '直连' }], meta: '1 条规则 · 未命中继续链路', connected: true }
    : { id: 'routing', kind: 'routing', x, y, title: '分流出口', role: '规则出口', meta: '连接服务器后配置规则' },
}

const edge = (from: string, to: string, kind: DemoEdge['kind'], packets?: DemoEdge['packets']): DemoEdge => ({ id: `${from}-${to}`, from, to, kind, packets })

const withPackets = (edges: DemoEdge[]) => edges.map(item => ({ ...item, packets: 'primary' as const }))

const relayChain = (): DemoScene => ({
  ...emptyScene(),
  entryServer: { title: '东京-01', region: 'JP', entries: 1 },
  sync: 'synced',
  nodes: [cards.entry(1), cards.tokyo(), cards.hk(true)],
  edges: [edge('entry', 'tokyo', 'belongs'), edge('tokyo', 'hk', 'singbox')],
})

const warpChain = (): DemoScene => ({
  ...relayChain(),
  camera: fitCamera(Y.entry, Y.tail + 80),
  nodes: [cards.entry(1), cards.tokyo(), cards.hk(false), cards.warp(true)],
  edges: withPackets([edge('entry', 'tokyo', 'belongs'), edge('tokyo', 'hk', 'singbox'), edge('hk', 'warp', 'warp')]),
})

const routedChain = (): DemoScene => ({
  ...warpChain(),
  camera: fitCamera(Y.entry, Y.warpAfterRouting + 80),
  nodes: [cards.entry(1), cards.tokyo(), cards.hk(false), cards.routing(true), cards.warp(true, column('warp'), Y.warpAfterRouting)],
  edges: withPackets([edge('entry', 'tokyo', 'belongs'), edge('tokyo', 'hk', 'singbox'), edge('hk', 'routing', 'routing'), edge('routing', 'warp', 'warp')]),
  ruleMatching: 'routing',
})

function find(runtime: DemoRuntime, id: string) {
  const node = runtime.scene().nodes.find(item => item.id === id)
  if (!node) throw new Error(`demo node ${id} missing`)
  return node
}

function patch(runtime: DemoRuntime, change: Partial<DemoScene>) {
  runtime.update(scene => ({ ...scene, ...change }))
}

function putNode(runtime: DemoRuntime, node: DemoNode) {
  runtime.update(scene => {
    const index = scene.nodes.findIndex(item => item.id === node.id)
    const nodes = index < 0 ? [...scene.nodes, node] : scene.nodes.map(item => item.id === node.id ? node : item)
    return { ...scene, nodes }
  })
}

function putEdges(runtime: DemoRuntime, add: DemoEdge[], remove: string[] = []) {
  runtime.update(scene => ({ ...scene, edges: [...scene.edges.filter(item => !remove.includes(item.id) && !add.some(next => next.id === item.id)), ...add] }))
}

function camera(runtime: DemoRuntime): Camera {
  return { x: runtime.cameraX.get(), y: runtime.cameraY.get(), scale: runtime.cameraScale.get() }
}

function stagePoint(runtime: DemoRuntime, world: Point) {
  return toStage(camera(runtime), world)
}

async function moveCamera(runtime: DemoRuntime, target: Camera, duration = 700) {
  patch(runtime, { camera: target })
  await Promise.all([
    runtime.timeline.tween(runtime.cameraX, target.x, { duration, ease: demoEaseOut }),
    runtime.timeline.tween(runtime.cameraY, target.y, { duration, ease: demoEaseOut }),
    runtime.timeline.tween(runtime.cameraScale, target.scale, { duration, ease: demoEaseOut }),
  ])
}

async function moveTo(runtime: DemoRuntime, to: Point, duration?: number) {
  const from = { x: runtime.cursorX.get(), y: runtime.cursorY.get() }
  await runtime.timeline.progress({
    duration: duration ?? travelDuration(from, to),
    ease: demoEaseInOut,
    onUpdate: t => {
      const point = arcPoint(from, to, t)
      runtime.cursorX.set(point.x)
      runtime.cursorY.set(point.y)
    },
  })
}

let rippleSeed = 0

async function click(runtime: DemoRuntime, kind: 'left' | 'right' = 'left') {
  const ripple = { id: ++rippleSeed, x: runtime.cursorX.get(), y: runtime.cursorY.get(), kind }
  runtime.update(scene => ({ ...scene, cursorPressed: true, ripples: [...scene.ripples, ripple] }))
  await runtime.timeline.wait(120)
  patch(runtime, { cursorPressed: false })
  await runtime.timeline.wait(110)
}

async function openMenu(runtime: DemoRuntime, at: Point) {
  await moveTo(runtime, at)
  await runtime.timeline.wait(160)
  await click(runtime, 'right')
  const position = menuPosition(at)
  patch(runtime, { menu: { ...position, hover: null } })
  await runtime.timeline.wait(420)
}

async function chooseMenu(runtime: DemoRuntime, id: MenuItemID) {
  const menu = runtime.scene().menu
  if (!menu) throw new Error('demo menu closed')
  await moveTo(runtime, menuItemPoint(menu, id))
  runtime.update(scene => scene.menu ? { ...scene, menu: { ...scene.menu, hover: id } } : scene)
  await runtime.timeline.wait(260)
  await click(runtime)
  patch(runtime, { menu: null })
  await runtime.timeline.wait(180)
}

// connect drags from a card's bottom handle to the next card's top handle.
async function connect(runtime: DemoRuntime, fromID: string, toID: string) {
  const start = stagePoint(runtime, outputHandle(find(runtime, fromID)))
  const end = stagePoint(runtime, inputHandle(find(runtime, toID)))
  await moveTo(runtime, start)
  patch(runtime, { activeHandle: { node: fromID, side: 'out' } })
  await runtime.timeline.wait(200)
  patch(runtime, { cursorPressed: true, provisional: { from: fromID } })
  await runtime.timeline.wait(140)
  await moveTo(runtime, end, Math.max(640, travelDuration(start, end) + 160))
  patch(runtime, { activeHandle: { node: toID, side: 'in' } })
  await runtime.timeline.wait(220)
  patch(runtime, { cursorPressed: false, provisional: null, activeHandle: null })
}

// saved mirrors the top-bar indicator after any change: 正在保存 → 正在同步 → 配置已同步.
async function saved(runtime: DemoRuntime, servers: number) {
  patch(runtime, { sync: 'saving' })
  await runtime.timeline.wait(380)
  patch(runtime, { sync: 'syncing', syncCount: servers })
  await runtime.timeline.wait(620)
  patch(runtime, { sync: 'synced' })
}

async function updateDialog<T extends NonNullable<DemoScene['dialog']>>(runtime: DemoRuntime, kind: T['kind'], change: Partial<T>) {
  runtime.update(scene => scene.dialog?.kind === kind ? { ...scene, dialog: { ...scene.dialog, ...change } as T } : scene)
}

export const proxyPathChapters: DemoChapter[] = [
  {
    id: 'concept',
    title: '什么是代理链路',
    summary: '设备连上入口，流量依次经过服务器，从最后一台出去。',
    start: () => ({ ...emptyScene(), chrome: false, camera: fitCamera(40, 620, 20), nodes: [cards.device()] }),
    async run(runtime) {
      runtime.caption('代理链路就是流量走的路线：设备连上入口，流量依次经过服务器，最后访问网站。')
      await runtime.timeline.wait(600)
      putNode(runtime, cards.entry(1, 120))
      await runtime.timeline.wait(240)
      putEdges(runtime, [edge('device', 'entry', 'device', 'primary')])
      await runtime.timeline.wait(600)
      putNode(runtime, cards.tokyo(true, 296))
      await runtime.timeline.wait(240)
      putEdges(runtime, [edge('entry', 'tokyo', 'belongs', 'primary')])
      await runtime.timeline.wait(600)
      putNode(runtime, cards.hk(true, column('server'), 426))
      await runtime.timeline.wait(240)
      putEdges(runtime, [edge('tokyo', 'hk', 'singbox', 'primary')])
      await runtime.timeline.wait(600)
      putNode(runtime, cards.site(580))
      await runtime.timeline.wait(240)
      putEdges(runtime, [edge('hk', 'site', 'direct', 'primary')])
      await runtime.timeline.wait(2000)
      runtime.caption('入口建在服务器上，用户订阅里看到的就是这个入口。')
      patch(runtime, { emphasis: 'entry' })
      await runtime.timeline.wait(2200)
      runtime.caption('链路最后一台服务器默认直接访问网站，它的 IP 就是用户看到的出口 IP。')
      patch(runtime, { emphasis: 'hk' })
      await runtime.timeline.wait(2600)
      patch(runtime, { emphasis: null })
      await runtime.timeline.wait(600)
    },
  },
  {
    id: 'entry',
    title: '新建入口',
    summary: '右键空白处，选择「入口节点」，选协议后创建。',
    start: () => ({ ...emptyScene(), cursorVisible: true, nodes: [cards.tokyo(false)] }),
    cursorStart: { x: 860, y: 560 },
    async run(runtime) {
      runtime.caption('画布显示上方「入口服务器」选中的服务器。在空白处点右键（触屏长按）新建。')
      await runtime.timeline.wait(900)
      await openMenu(runtime, { x: 590, y: 120 })
      runtime.caption('选择「入口节点」，入口会建在这台服务器上。')
      await runtime.timeline.wait(400)
      await chooseMenu(runtime, 'entry')
      patch(runtime, { dialog: { kind: 'entry', hover: null, pressed: false } })
      runtime.caption('选择协议。VLESS Reality 借用目标站点的 TLS 握手，不需要域名和证书，适合第一个入口。')
      await runtime.timeline.wait(700)
      await moveTo(runtime, entryProtocolPoint(0))
      await updateDialog(runtime, 'entry', { hover: 'VLESS' })
      await runtime.timeline.wait(1600)
      await moveTo(runtime, ENTRY_SUBMIT)
      await updateDialog(runtime, 'entry', { pressed: true })
      await click(runtime)
      patch(runtime, { dialog: null })
      await runtime.timeline.wait(140)
      putNode(runtime, cards.entry(0))
      putNode(runtime, cards.tokyo(true))
      putEdges(runtime, [edge('entry', 'tokyo', 'belongs')])
      runtime.update(scene => ({ ...scene, entryServer: { ...scene.entryServer, entries: 1 } }))
      runtime.caption('入口出现在服务器上方，虚线表示它属于东京-01。服务器底部多了这个入口的连接点。')
      await moveTo(runtime, { x: 840, y: 470 })
      await saved(runtime, 1)
      await runtime.timeline.wait(1600)
    },
  },
  {
    id: 'relay',
    title: '放入中转服务器',
    summary: '右键选择「其他服务器」，把中转放进画布。',
    start: () => ({ ...emptyScene(), cursorVisible: true, sync: 'synced', entryServer: { title: '东京-01', region: 'JP', entries: 1 }, nodes: [cards.entry(0), cards.tokyo()], edges: [edge('entry', 'tokyo', 'belongs')] }),
    cursorStart: { x: 840, y: 470 },
    async run(runtime) {
      runtime.caption('需要中转时，右键空白处，选择「其他服务器」。')
      await runtime.timeline.wait(400)
      await openMenu(runtime, { x: 780, y: 230 })
      await chooseMenu(runtime, 'server')
      patch(runtime, { drawer: { hover: -1, placed: false, pressed: false } })
      runtime.caption('从服务器列表里点一台，它就会放进画布。同一台服务器可以多次放入。')
      await runtime.timeline.wait(700)
      await moveTo(runtime, drawerTilePoint(1))
      runtime.update(scene => scene.drawer ? { ...scene, drawer: { ...scene.drawer, hover: 1 } } : scene)
      await runtime.timeline.wait(300)
      await click(runtime)
      runtime.update(scene => scene.drawer ? { ...scene, drawer: { ...scene.drawer, placed: true } } : scene)
      await runtime.timeline.wait(600)
      await moveTo(runtime, DRAWER_DONE)
      runtime.update(scene => scene.drawer ? { ...scene, drawer: { ...scene.drawer, pressed: true } } : scene)
      await click(runtime)
      patch(runtime, { drawer: null })
      await runtime.timeline.wait(160)
      putNode(runtime, cards.hkLoose())
      runtime.caption('香港-02 还没有连线，现在只是「可控服务器」，不属于任何链路。')
      await moveTo(runtime, { x: 300, y: 520 })
      await runtime.timeline.wait(1800)
    },
  },
  {
    id: 'connect',
    title: '连接与传递方式',
    summary: '从上一台的底部拖到下一台的顶部，再选传递方式。',
    start: () => ({ ...emptyScene(), cursorVisible: true, sync: 'synced', entryServer: { title: '东京-01', region: 'JP', entries: 1 }, nodes: [cards.entry(0), cards.tokyo(), cards.hkLoose()], edges: [edge('entry', 'tokyo', 'belongs')] }),
    cursorStart: { x: 300, y: 520 },
    async run(runtime) {
      runtime.caption('按住东京-01 底部的连接点，拖到香港-02 顶部松开。')
      await runtime.timeline.wait(500)
      await connect(runtime, 'tokyo', 'hk')
      patch(runtime, { dialog: { kind: 'transport', mode: 0, hover: -1, target: 0, pressed: false } })
      runtime.caption('松开后选择这一跳的传递方式。')
      await runtime.timeline.wait(900)
      const notes = [
        'sing-box 出站链：东京-01 用协议连到香港-02，最通用。',
        '透明端口转发：原样传递客户端密文，只能用在链路开头。',
        'SSH / WireGuard 隧道：通过服务器之间的专用隧道连接。',
      ]
      for (let index = 0; index < notes.length; index++) {
        await moveTo(runtime, transportModePoint(index))
        await updateDialog(runtime, 'transport', { hover: index })
        runtime.caption(notes[index])
        await runtime.timeline.wait(1900)
      }
      await moveTo(runtime, transportModePoint(0))
      await click(runtime)
      await updateDialog(runtime, 'transport', { mode: 0, hover: -1 })
      runtime.caption('下面选择香港-02 上用来接收的入口。系统会自动生成隐藏入口，不用另外创建。')
      await moveTo(runtime, transportTargetPoint(0))
      await click(runtime)
      await updateDialog(runtime, 'transport', { target: 0 })
      await runtime.timeline.wait(1400)
      runtime.caption('底部是本次变更和拓扑检查结果，确认无误后点「确定」。')
      await runtime.timeline.wait(900)
      await moveTo(runtime, TRANSPORT_CONFIRM)
      await updateDialog(runtime, 'transport', { pressed: true })
      await click(runtime)
      patch(runtime, { dialog: null })
      await runtime.timeline.wait(120)
      putNode(runtime, cards.hk(true, 668))
      putNode(runtime, cards.entry(1))
      putEdges(runtime, [edge('tokyo', 'hk', 'singbox')])
      await saved(runtime, 2)
      runtime.caption('连好后点「重置布局」，画布会按链路自动排好。')
      await moveTo(runtime, WORKBENCH_RESET)
      patch(runtime, { workbenchPressed: 'reset' })
      await click(runtime)
      patch(runtime, { workbenchPressed: null })
      putNode(runtime, cards.hk(true))
      await runtime.timeline.wait(500)
      runtime.caption('香港-02 现在是链路的最后一台，默认就从它直接访问网站。')
      patch(runtime, { emphasis: 'hk' })
      await moveTo(runtime, { x: 820, y: 560 })
      await runtime.timeline.wait(2200)
      patch(runtime, { emphasis: null })
    },
  },
  {
    id: 'exit',
    title: '更换出口',
    summary: '在末端接上 WARP 等出口，换一个出口 IP。',
    start: () => ({ ...relayChain(), cursorVisible: true }),
    cursorStart: { x: 820, y: 560 },
    async run(runtime) {
      runtime.caption('想换一个出口 IP，就在链路末端接上 WARP、VPNGate 或导入节点。')
      await moveCamera(runtime, fitCamera(Y.entry, Y.tail + 80))
      await openMenu(runtime, stagePoint(runtime, { x: 540, y: Y.tail + 30 }))
      await chooseMenu(runtime, 'warp')
      putNode(runtime, cards.warp(false))
      runtime.caption('WARP 由前一台服务器自动申请，连上即可，不用选择传递方式。')
      await runtime.timeline.wait(1000)
      await connect(runtime, 'hk', 'warp')
      putNode(runtime, cards.warp(true))
      putNode(runtime, cards.hk(false))
      putEdges(runtime, [edge('hk', 'warp', 'warp')])
      await saved(runtime, 2)
      await moveTo(runtime, stagePoint(runtime, { x: 820, y: Y.tail + 40 }))
      patch(runtime, { cursorVisible: false })
      runtime.update(scene => ({ ...scene, edges: withPackets(scene.edges) }))
      runtime.caption('现在流量从东京入口进来，经香港中转，最后从 WARP 出去。')
      await runtime.timeline.wait(3400)
    },
  },
  {
    id: 'routing',
    title: '按规则分流',
    summary: '规则写在服务器上，没命中的继续走后面的节点。',
    start: () => ({ ...warpChain(), cursorVisible: true }),
    cursorStart: { x: 860, y: 600 },
    async run(runtime) {
      runtime.caption('想让一部分流量走别的路，就用「分流出口」。')
      await runtime.timeline.wait(400)
      await openMenu(runtime, stagePoint(runtime, { x: 760, y: 360 }))
      await chooseMenu(runtime, 'routing')
      putNode(runtime, cards.routing(false, 700, 420))
      runtime.caption('从香港-02 底部拖到分流出口，开始编排规则。')
      await runtime.timeline.wait(900)
      await connect(runtime, 'hk', 'routing')
      patch(runtime, { dialog: { kind: 'routing', added: false, addPressed: false, donePressed: false } })
      runtime.caption('规则写在香港-02 上：命中「国内网站」的流量直接从香港-02 出去。')
      await runtime.timeline.wait(1200)
      await moveTo(runtime, routingActionPoint(0))
      await runtime.timeline.wait(1400)
      await moveTo(runtime, ROUTING_ADD)
      await updateDialog(runtime, 'routing', { addPressed: true })
      await click(runtime)
      await updateDialog(runtime, 'routing', { added: true, addPressed: false })
      runtime.caption('没命中规则的流量，继续走后面连接的节点，也就是原来的 WARP。')
      await runtime.timeline.wait(2000)
      await moveTo(runtime, ROUTING_DONE)
      await updateDialog(runtime, 'routing', { donePressed: true })
      await click(runtime)
      patch(runtime, { dialog: null, cursorVisible: false })
      await runtime.timeline.wait(120)
      putNode(runtime, cards.routing(true))
      putNode(runtime, cards.warp(true, column('warp'), Y.warpAfterRouting))
      putEdges(runtime, withPackets([edge('hk', 'routing', 'routing'), edge('routing', 'warp', 'warp')]), ['hk-warp'])
      await moveCamera(runtime, fitCamera(Y.entry, Y.warpAfterRouting + 80))
      await saved(runtime, 2)
      patch(runtime, { ruleMatching: 'routing' })
      runtime.caption('分流插在香港-02 和 WARP 之间：命中规则的流量直接从香港-02 出去，其余继续去 WARP。')
      await runtime.timeline.wait(3800)
    },
  },
  {
    id: 'sync',
    title: '自动生效',
    summary: '保存即下发，看右上角「配置已同步」。',
    start: () => ({ ...routedChain(), sync: 'none' }),
    async run(runtime) {
      runtime.caption('每次保存都会自动下发到相关服务器，不需要手动操作。留意右上角的状态。')
      patch(runtime, { emphasis: 'sync', sync: 'saving' })
      await runtime.timeline.wait(1100)
      patch(runtime, { sync: 'syncing', syncCount: 2 })
      await runtime.timeline.wait(1900)
      patch(runtime, { sync: 'synced' })
      runtime.caption('显示「配置已同步」就生效了。如果显示「需要处理」，点开能看到原因和处理方法。')
      await runtime.timeline.wait(3000)
      patch(runtime, { emphasis: 'entry' })
      runtime.caption('已授权的用户更新订阅后，就能用上这个入口。')
      await runtime.timeline.wait(2800)
      patch(runtime, { emphasis: null })
      await runtime.timeline.wait(500)
    },
  },
]

// chapterDuration dry-runs a chapter: every wait and tween resolves at once
// and only adds its duration, so the progress bar matches the real script.
export async function chapterDuration(chapter: DemoChapter, timeline: DemoTimeline, makeValue: (value: number) => MotionValue<number>) {
  let scene = chapter.start()
  const runtime: DemoRuntime = {
    timeline,
    cursorX: makeValue(chapter.cursorStart?.x ?? 0),
    cursorY: makeValue(chapter.cursorStart?.y ?? 0),
    cameraX: makeValue(scene.camera.x),
    cameraY: makeValue(scene.camera.y),
    cameraScale: makeValue(scene.camera.scale),
    scene: () => scene,
    update: change => { scene = change(scene) },
    caption: () => undefined,
  }
  await chapter.run(runtime)
  return timeline.elapsed()
}
