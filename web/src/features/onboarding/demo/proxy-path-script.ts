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
    title: '代理链路的组成',
    summary: '入口、服务器与出口之间的关系。',
    start: () => ({ ...emptyScene(), chrome: false, camera: fitCamera(40, 620, 20), nodes: [cards.device()] }),
    async run(runtime) {
      runtime.caption('代理链路描述流量的转发路径：客户端连接入口，流量依次经过链路中的服务器，最终访问目标网站。')
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
      runtime.caption('入口部署在服务器上，用户订阅中提供的即为入口的连接信息。')
      patch(runtime, { emphasis: 'entry' })
      await runtime.timeline.wait(2200)
      runtime.caption('未配置其他出口时，链路中的最后一台服务器直接访问目标网站，其公网 IP 即为出口 IP。')
      patch(runtime, { emphasis: 'hk' })
      await runtime.timeline.wait(2600)
      patch(runtime, { emphasis: null })
      await runtime.timeline.wait(600)
    },
  },
  {
    id: 'entry',
    title: '创建入口',
    summary: '在画布空白处右键，选择「入口节点」并选择协议。',
    start: () => ({ ...emptyScene(), cursorVisible: true, nodes: [cards.tokyo(false)] }),
    cursorStart: { x: 860, y: 560 },
    async run(runtime) {
      runtime.caption('画布显示上方「入口服务器」所选服务器的拓扑。在画布空白处右键（触屏设备为长按）打开新建菜单。')
      await runtime.timeline.wait(900)
      await openMenu(runtime, { x: 590, y: 120 })
      runtime.caption('选择「入口节点」，入口将创建在当前服务器上。')
      await runtime.timeline.wait(400)
      await chooseMenu(runtime, 'entry')
      patch(runtime, { dialog: { kind: 'entry', hover: null, pressed: false } })
      runtime.caption('选择协议后创建。VLESS Reality 借用目标站点的 TLS 握手，无需域名与证书。')
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
      runtime.caption('入口卡片显示在服务器上方，虚线表示其归属关系；服务器底部同时出现该入口对应的连接点。')
      await moveTo(runtime, { x: 840, y: 470 })
      await saved(runtime, 1)
      await runtime.timeline.wait(1600)
    },
  },
  {
    id: 'relay',
    title: '添加中转服务器',
    summary: '通过「其他服务器」将服务器放入画布。',
    start: () => ({ ...emptyScene(), cursorVisible: true, sync: 'synced', entryServer: { title: '东京-01', region: 'JP', entries: 1 }, nodes: [cards.entry(0), cards.tokyo()], edges: [edge('entry', 'tokyo', 'belongs')] }),
    cursorStart: { x: 840, y: 470 },
    async run(runtime) {
      runtime.caption('如需中转，在画布空白处右键并选择「其他服务器」。')
      await runtime.timeline.wait(400)
      await openMenu(runtime, { x: 780, y: 230 })
      await chooseMenu(runtime, 'server')
      patch(runtime, { drawer: { hover: -1, placed: false, pressed: false } })
      runtime.caption('在列表中选择服务器后点击「完成」。同一台服务器可多次放入画布。')
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
      runtime.caption('尚未连线的服务器显示为「可控服务器」，此时不属于任何链路。')
      await moveTo(runtime, { x: 300, y: 520 })
      await runtime.timeline.wait(1800)
    },
  },
  {
    id: 'connect',
    title: '连接与传递方式',
    summary: '从上一节点底部连接至下一节点顶部，并选择传递方式。',
    start: () => ({ ...emptyScene(), cursorVisible: true, sync: 'synced', entryServer: { title: '东京-01', region: 'JP', entries: 1 }, nodes: [cards.entry(0), cards.tokyo(), cards.hkLoose()], edges: [edge('entry', 'tokyo', 'belongs')] }),
    cursorStart: { x: 300, y: 520 },
    async run(runtime) {
      runtime.caption('按住东京-01 底部的连接点，拖动至香港-02 顶部后释放。')
      await runtime.timeline.wait(500)
      await connect(runtime, 'tokyo', 'hk')
      patch(runtime, { dialog: { kind: 'transport', mode: 0, hover: -1, target: 0, pressed: false } })
      runtime.caption('释放后需为该跳选择传递方式。')
      await runtime.timeline.wait(900)
      const notes = [
        'sing-box 出站链：由东京-01 通过代理协议连接香港-02，适用于大多数场景。',
        '透明端口转发：原样传递客户端密文，仅可用于链路开头。',
        'SSH / WireGuard 隧道：通过服务器之间的专用隧道传输。',
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
      runtime.caption('在下方选择香港-02 上用于接收流量的入口。系统生成的入口为隐藏入口，无需另行创建。')
      await moveTo(runtime, transportTargetPoint(0))
      await click(runtime)
      await updateDialog(runtime, 'transport', { target: 0 })
      await runtime.timeline.wait(1400)
      runtime.caption('底部显示本次变更及拓扑检查结果，确认无误后点击「确定」。')
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
      runtime.caption('连接完成后，可点击「重置布局」按链路自动排列节点。')
      await moveTo(runtime, WORKBENCH_RESET)
      patch(runtime, { workbenchPressed: 'reset' })
      await click(runtime)
      patch(runtime, { workbenchPressed: null })
      putNode(runtime, cards.hk(true))
      await runtime.timeline.wait(500)
      runtime.caption('香港-02 现为链路中的最后一台服务器，默认由其直接访问目标网站。')
      patch(runtime, { emphasis: 'hk' })
      await moveTo(runtime, { x: 820, y: 560 })
      await runtime.timeline.wait(2200)
      patch(runtime, { emphasis: null })
    },
  },
  {
    id: 'exit',
    title: '更换出口',
    summary: '在链路末端连接 WARP 等出口。',
    start: () => ({ ...relayChain(), cursorVisible: true }),
    cursorStart: { x: 820, y: 560 },
    async run(runtime) {
      runtime.caption('如需更换出口 IP，可在链路末端连接 WARP、VPNGate 或导入节点。')
      await moveCamera(runtime, fitCamera(Y.entry, Y.tail + 80))
      await openMenu(runtime, stagePoint(runtime, { x: 540, y: Y.tail + 30 }))
      await chooseMenu(runtime, 'warp')
      putNode(runtime, cards.warp(false))
      runtime.caption('WARP 由前一台服务器自动申请，连接后即可使用，无需选择传递方式。')
      await runtime.timeline.wait(1000)
      await connect(runtime, 'hk', 'warp')
      putNode(runtime, cards.warp(true))
      putNode(runtime, cards.hk(false))
      putEdges(runtime, [edge('hk', 'warp', 'warp')])
      await saved(runtime, 2)
      await moveTo(runtime, stagePoint(runtime, { x: 820, y: Y.tail + 40 }))
      patch(runtime, { cursorVisible: false })
      runtime.update(scene => ({ ...scene, edges: withPackets(scene.edges) }))
      runtime.caption('流量经东京-01 的入口进入，由香港-02 中转，最终通过 WARP 访问目标网站。')
      await runtime.timeline.wait(3400)
    },
  },
  {
    id: 'routing',
    title: '按规则分流',
    summary: '规则配置在服务器上，未命中的流量继续沿链路转发。',
    start: () => ({ ...warpChain(), cursorVisible: true }),
    cursorStart: { x: 860, y: 600 },
    async run(runtime) {
      runtime.caption('如需对部分流量采用不同的处理方式，可使用「分流出口」。')
      await runtime.timeline.wait(400)
      await openMenu(runtime, stagePoint(runtime, { x: 760, y: 360 }))
      await chooseMenu(runtime, 'routing')
      putNode(runtime, cards.routing(false, 700, 420))
      runtime.caption('从香港-02 底部连接至分流出口，打开规则编排。')
      await runtime.timeline.wait(900)
      await connect(runtime, 'hk', 'routing')
      patch(runtime, { dialog: { kind: 'routing', added: false, addPressed: false, donePressed: false } })
      runtime.caption('规则配置在香港-02 上：命中「国内网站」规则的流量由香港-02 直接访问。')
      await runtime.timeline.wait(1200)
      await moveTo(runtime, routingActionPoint(0))
      await runtime.timeline.wait(1400)
      await moveTo(runtime, ROUTING_ADD)
      await updateDialog(runtime, 'routing', { addPressed: true })
      await click(runtime)
      await updateDialog(runtime, 'routing', { added: true, addPressed: false })
      runtime.caption('未命中任何规则的流量，继续转发至其后连接的节点，即原有的 WARP。')
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
      runtime.caption('分流出口位于香港-02 与 WARP 之间：命中规则的流量由香港-02 直接访问，其余流量经 WARP 转发。')
      await runtime.timeline.wait(3800)
    },
  },
  {
    id: 'sync',
    title: '配置下发',
    summary: '保存后自动下发，以「配置已同步」为准。',
    start: () => ({ ...routedChain(), sync: 'none' }),
    async run(runtime) {
      runtime.caption('每次保存后，面板会自动将配置下发至相关服务器，无需手动操作。同步状态显示在页面右上角。')
      patch(runtime, { emphasis: 'sync', sync: 'saving' })
      await runtime.timeline.wait(1100)
      patch(runtime, { sync: 'syncing', syncCount: 2 })
      await runtime.timeline.wait(1900)
      patch(runtime, { sync: 'synced' })
      runtime.caption('显示「配置已同步」即表示配置已生效。如显示「需要处理」，可点击查看原因与处理方法。')
      await runtime.timeline.wait(3000)
      patch(runtime, { emphasis: 'entry' })
      runtime.caption('已获授权的用户更新订阅后，即可使用该入口。')
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
