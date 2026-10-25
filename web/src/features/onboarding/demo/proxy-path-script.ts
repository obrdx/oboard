import type { MotionValue } from 'motion/react'
import { demoEaseInOut, type DemoTimeline } from './engine'
import {
  arcPoint, emptyScene, entryDialogSubmit, inputHandle, menuItemPoint, outputHandle, pickerTilePoint,
  transportDialogConfirm, transportOptionPoint, travelDuration,
  type DemoEdge, type DemoNode, type DemoScene, type MenuItemID, type Point,
} from './scene'

export type DemoRuntime = {
  timeline: DemoTimeline
  cursorX: MotionValue<number>
  cursorY: MotionValue<number>
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

// Two layouts: a straight line while the chain is built, and a wider spread
// once a routing card needs room for two branches.
const lineY = 244
const nodes = {
  client: (x = 48, y = 260): DemoNode => ({ id: 'client', kind: 'client', x, y, label: '用户', title: '客户端', detail: '手机 / 电脑' }),
  entry: (x = 236, y = lineY): DemoNode => ({ id: 'entry', kind: 'entry', x, y, label: '入口', title: '东京-01', detail: 'VLESS Reality · 443', region: 'JP' }),
  hk: (x = 488, y = lineY): DemoNode => ({ id: 'hk', kind: 'server', x, y, label: '中转服务器', title: '香港-02', detail: '在线', region: 'HK' }),
  warp: (x = 764, y = lineY): DemoNode => ({ id: 'warp', kind: 'warp', x, y, label: '出口', title: 'WARP 出口', detail: '由末端服务器自动申请' }),
  direct: (x = 828, y = 344): DemoNode => ({ id: 'direct', kind: 'direct', x, y, label: '出口', title: '直接出口', detail: '使用服务器本机 IP' }),
  routing: (x = 604, y = 226): DemoNode => ({ id: 'routing', kind: 'routing', x, y, label: '分流', title: '分流出口', detail: '' }),
}

const spread = { client: { x: 24, y: 260 }, entry: { x: 160, y: lineY }, hk: { x: 384, y: lineY }, warp: { x: 828, y: 140 } }

const edge = (from: string, to: string, kind: DemoEdge['kind'], extra: Partial<DemoEdge> = {}): DemoEdge => ({ id: `${from}-${to}${extra.fromPort !== undefined ? `-${extra.fromPort}` : ''}`, from, to, kind, ...extra })

const fullChain = (): DemoScene => ({
  ...emptyScene(),
  nodes: [nodes.client(), nodes.entry(), nodes.hk(), nodes.warp()],
  edges: [edge('client', 'entry', 'client', { packets: 'primary' }), edge('entry', 'hk', 'singbox', { packets: 'primary' }), edge('hk', 'warp', 'local', { packets: 'primary' })],
})

const routedChain = (): DemoScene => ({
  ...emptyScene(),
  nodes: [nodes.client(spread.client.x, spread.client.y), nodes.entry(spread.entry.x), nodes.hk(spread.hk.x), nodes.routing(), nodes.warp(spread.warp.x, spread.warp.y), nodes.direct()],
  edges: [
    edge('client', 'entry', 'client', { packets: 'primary' }),
    edge('entry', 'hk', 'singbox', { packets: 'primary' }),
    edge('hk', 'routing', 'local', { packets: 'primary' }),
    edge('routing', 'warp', 'rule', { fromPort: 0, packets: 'primary' }),
    edge('routing', 'direct', 'rule', { fromPort: 1, packets: 'secondary' }),
  ],
})

function find(runtime: DemoRuntime, id: string) {
  const node = runtime.scene().nodes.find(item => item.id === id)
  if (!node) throw new Error(`demo node ${id} missing`)
  return node
}

function patch(runtime: DemoRuntime, change: Partial<DemoScene>) {
  runtime.update(scene => ({ ...scene, ...change }))
}

function addNode(runtime: DemoRuntime, node: DemoNode) {
  runtime.update(scene => ({ ...scene, nodes: [...scene.nodes.filter(item => item.id !== node.id), node] }))
}

function addEdge(runtime: DemoRuntime, value: DemoEdge) {
  runtime.update(scene => ({ ...scene, edges: [...scene.edges.filter(item => item.id !== value.id), value] }))
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
  patch(runtime, { menu: { x: at.x, y: at.y, hover: -1 }, hint: '' })
  await runtime.timeline.wait(420)
}

async function chooseMenu(runtime: DemoRuntime, id: MenuItemID) {
  const menu = runtime.scene().menu
  if (!menu) throw new Error('demo menu closed')
  await moveTo(runtime, menuItemPoint(menu, id))
  runtime.update(scene => scene.menu ? { ...scene, menu: { ...scene.menu, hover: ['entry', 'imported', 'direct', 'warp', 'vpngate', 'routing', 'server'].indexOf(id) } } : scene)
  await runtime.timeline.wait(260)
  await click(runtime)
  patch(runtime, { menu: null })
  await runtime.timeline.wait(180)
}

// connect drags from an output handle to a node's input handle. The stage
// draws the provisional edge from the cursor's live position.
async function connect(runtime: DemoRuntime, fromID: string, toID: string, fromPort?: number) {
  const source = find(runtime, fromID)
  const target = find(runtime, toID)
  await moveTo(runtime, outputHandle(source, fromPort))
  patch(runtime, { activeHandle: { node: fromID, port: fromPort, side: 'out' } })
  await runtime.timeline.wait(200)
  patch(runtime, { cursorPressed: true, provisional: { from: fromID, fromPort } })
  await runtime.timeline.wait(140)
  const end = inputHandle(target)
  await moveTo(runtime, end, Math.max(620, travelDuration(outputHandle(source, fromPort), end) + 160))
  patch(runtime, { activeHandle: { node: toID, side: 'in' } })
  await runtime.timeline.wait(220)
  patch(runtime, { cursorPressed: false, provisional: null, activeHandle: null })
}

function moveNodes(runtime: DemoRuntime, positions: Record<string, Point>) {
  runtime.update(scene => ({ ...scene, nodes: scene.nodes.map(node => positions[node.id] ? { ...node, ...positions[node.id] } : node) }))
}

export const proxyPathChapters: DemoChapter[] = [
  {
    id: 'concept',
    title: '什么是代理链路',
    summary: '流量从客户端出发，经过服务器，从出口访问网站。',
    start: () => ({ ...emptyScene(), nodes: [nodes.client()] }),
    async run(runtime) {
      runtime.caption('代理链路就是流量走的路线：从客户端出发，经过服务器，最后从出口访问网站。')
      await runtime.timeline.wait(700)
      addNode(runtime, nodes.entry())
      await runtime.timeline.wait(260)
      addEdge(runtime, edge('client', 'entry', 'client', { packets: 'primary' }))
      await runtime.timeline.wait(700)
      addNode(runtime, nodes.hk())
      await runtime.timeline.wait(260)
      addEdge(runtime, edge('entry', 'hk', 'singbox', { packets: 'primary' }))
      await runtime.timeline.wait(700)
      addNode(runtime, nodes.warp())
      await runtime.timeline.wait(260)
      addEdge(runtime, edge('hk', 'warp', 'local', { packets: 'primary' }))
      await runtime.timeline.wait(2400)
      runtime.caption('用户只连接入口。中转和出口对用户透明，随时可以调整。')
      patch(runtime, { emphasis: 'entry' })
      await runtime.timeline.wait(1500)
      patch(runtime, { emphasis: 'hk' })
      await runtime.timeline.wait(1100)
      patch(runtime, { emphasis: 'warp' })
      await runtime.timeline.wait(1100)
      patch(runtime, { emphasis: null })
      await runtime.timeline.wait(900)
    },
  },
  {
    id: 'entry',
    title: '新建入口',
    summary: '右键空白处，选择「入口节点」。',
    start: () => ({ ...emptyScene(), cursorVisible: true, hint: '右键空白处新建' }),
    cursorStart: { x: 860, y: 500 },
    async run(runtime) {
      runtime.caption('在画布空白处点右键（触屏长按），打开新建菜单。')
      await runtime.timeline.wait(500)
      await openMenu(runtime, { x: 300, y: 196 })
      runtime.caption('选择「入口节点」。入口建在当前选中的服务器上。')
      await runtime.timeline.wait(500)
      await chooseMenu(runtime, 'entry')
      patch(runtime, { dialog: { kind: 'entry', protocol: 'VLESS · Reality', port: '443', pressed: false } })
      runtime.caption('选好协议和端口。VLESS Reality 不需要证书，最适合第一个入口。')
      await runtime.timeline.wait(1500)
      await moveTo(runtime, entryDialogSubmit)
      await runtime.timeline.wait(160)
      runtime.update(scene => scene.dialog?.kind === 'entry' ? { ...scene, dialog: { ...scene.dialog, pressed: true } } : scene)
      await click(runtime)
      patch(runtime, { dialog: null })
      await runtime.timeline.wait(160)
      addNode(runtime, nodes.entry())
      runtime.caption('入口创建好了，它就是用户订阅里能看到的节点。')
      await moveTo(runtime, { x: 360, y: 420 })
      await runtime.timeline.wait(1800)
    },
  },
  {
    id: 'relay',
    title: '放入中转服务器',
    summary: '右键选择「其他服务器」，把中转放进画布。',
    start: () => ({ ...emptyScene(), nodes: [nodes.entry()], cursorVisible: true }),
    cursorStart: { x: 360, y: 420 },
    async run(runtime) {
      runtime.caption('需要中转时，右键选择「其他服务器」。')
      await runtime.timeline.wait(400)
      await openMenu(runtime, { x: 548, y: 120 })
      await chooseMenu(runtime, 'server')
      patch(runtime, { picker: { x: 556, y: 150, hover: -1 } })
      runtime.caption('从服务器列表里挑一台，它会出现在画布上。')
      await runtime.timeline.wait(600)
      const picker = runtime.scene().picker
      if (picker) {
        await moveTo(runtime, pickerTilePoint(picker, 0))
        patch(runtime, { picker: { ...picker, hover: 0 } })
      }
      await runtime.timeline.wait(320)
      await click(runtime)
      patch(runtime, { picker: null })
      await runtime.timeline.wait(140)
      addNode(runtime, nodes.hk())
      await moveTo(runtime, { x: 600, y: 440 })
      await runtime.timeline.wait(1600)
    },
  },
  {
    id: 'connect',
    title: '连接与传输方式',
    summary: '拖动连接点串联，再选择这一跳怎么转发。',
    start: () => ({ ...emptyScene(), nodes: [nodes.entry(), nodes.hk()], cursorVisible: true }),
    cursorStart: { x: 600, y: 440 },
    async run(runtime) {
      runtime.caption('按住卡片右侧的连接点，拖到下一台服务器上松开。')
      await runtime.timeline.wait(400)
      await connect(runtime, 'entry', 'hk')
      patch(runtime, { dialog: { kind: 'transport', hover: -1, selected: 0, pressed: false } })
      runtime.caption('松开后选择这一跳的传输方式。')
      await runtime.timeline.wait(900)
      const notes = [
        'sing-box 出站链：按协议逐跳加密转发，最通用。',
        '透明端口转发：原样转发流量，延迟最低。',
        'SSH / WireGuard 隧道：先建隧道再转发，适合受限网络。',
      ]
      for (let index = 0; index < notes.length; index++) {
        await moveTo(runtime, transportOptionPoint(index))
        runtime.update(scene => scene.dialog?.kind === 'transport' ? { ...scene, dialog: { ...scene.dialog, hover: index } } : scene)
        runtime.caption(notes[index])
        await runtime.timeline.wait(1900)
      }
      await moveTo(runtime, transportOptionPoint(0))
      runtime.update(scene => scene.dialog?.kind === 'transport' ? { ...scene, dialog: { ...scene.dialog, hover: 0 } } : scene)
      await click(runtime)
      runtime.update(scene => scene.dialog?.kind === 'transport' ? { ...scene, dialog: { ...scene.dialog, selected: 0 } } : scene)
      runtime.caption('大多数情况选 sing-box 出站链就可以。')
      await runtime.timeline.wait(900)
      await moveTo(runtime, transportDialogConfirm)
      runtime.update(scene => scene.dialog?.kind === 'transport' ? { ...scene, dialog: { ...scene.dialog, pressed: true } } : scene)
      await click(runtime)
      patch(runtime, { dialog: null })
      await runtime.timeline.wait(120)
      addEdge(runtime, edge('entry', 'hk', 'singbox'))
      await moveTo(runtime, { x: 520, y: 450 })
      await runtime.timeline.wait(1500)
    },
  },
  {
    id: 'exit',
    title: '添加出口',
    summary: '出口决定访问网站时使用哪个 IP。',
    start: () => ({ ...emptyScene(), nodes: [nodes.entry(), nodes.hk()], edges: [edge('entry', 'hk', 'singbox')], cursorVisible: true }),
    cursorStart: { x: 520, y: 450 },
    async run(runtime) {
      runtime.caption('最后添加出口，它决定访问网站时用的是哪个 IP。')
      await runtime.timeline.wait(400)
      await openMenu(runtime, { x: 760, y: 60 })
      await chooseMenu(runtime, 'warp')
      addNode(runtime, nodes.warp())
      runtime.caption('WARP 和直接出口属于服务器本机出口，连上就行，不用选传输方式。')
      await runtime.timeline.wait(1300)
      await connect(runtime, 'hk', 'warp')
      addEdge(runtime, edge('hk', 'warp', 'local'))
      await moveTo(runtime, { x: 700, y: 470 })
      patch(runtime, { cursorVisible: false })
      await runtime.timeline.wait(300)
      addNode(runtime, nodes.client())
      runtime.update(scene => ({ ...scene, edges: [edge('client', 'entry', 'client', { packets: 'primary' }), ...scene.edges.map(item => ({ ...item, packets: 'primary' as const }))] }))
      runtime.caption('链路串好了：流量从东京入口进来，经香港中转，从 WARP 出去。')
      await runtime.timeline.wait(3200)
    },
  },
  {
    id: 'routing',
    title: '按规则分流',
    summary: '命中规则的流量走一个出口，其余走兜底。',
    start: fullChain,
    cursorStart: { x: 700, y: 500 },
    async run(runtime) {
      runtime.caption('想让不同流量走不同出口，就用「分流出口」。')
      runtime.update(scene => ({ ...scene, edges: scene.edges.filter(item => item.id !== 'hk-warp') }))
      moveNodes(runtime, spread)
      await runtime.timeline.wait(500)
      addNode(runtime, nodes.direct())
      patch(runtime, { cursorVisible: true })
      await runtime.timeline.wait(500)
      await openMenu(runtime, { x: 600, y: 300 })
      await chooseMenu(runtime, 'routing')
      addNode(runtime, nodes.routing())
      await runtime.timeline.wait(260)
      addEdge(runtime, edge('hk', 'routing', 'local', { packets: 'primary' }))
      runtime.caption('把规则连到出口：命中「流媒体」的流量走 WARP。')
      await runtime.timeline.wait(1000)
      await connect(runtime, 'routing', 'warp', 0)
      addEdge(runtime, edge('routing', 'warp', 'rule', { fromPort: 0, packets: 'primary' }))
      runtime.caption('「未匹配」连到兜底出口，其余流量从香港本机直接出去。')
      await runtime.timeline.wait(800)
      await connect(runtime, 'routing', 'direct', 1)
      addEdge(runtime, edge('routing', 'direct', 'rule', { fromPort: 1, packets: 'secondary' }))
      await moveTo(runtime, { x: 520, y: 500 })
      patch(runtime, { cursorVisible: false })
      runtime.caption('两种颜色的流量各走各的出口。')
      await runtime.timeline.wait(3200)
    },
  },
  {
    id: 'sync',
    title: '自动生效',
    summary: '保存即下发，显示「已同步」就生效了。',
    start: routedChain,
    async run(runtime) {
      runtime.caption('每一步保存后都不用手动下发，面板会把配置自动同步到相关服务器。')
      runtime.update(scene => ({ ...scene, syncBadge: 'syncing', nodes: scene.nodes.map(node => node.kind === 'entry' || node.kind === 'server' ? { ...node, status: 'syncing' as const } : node) }))
      await runtime.timeline.wait(2000)
      runtime.update(scene => ({ ...scene, nodes: scene.nodes.map(node => node.id === 'hk' ? { ...node, status: 'synced' as const } : node) }))
      await runtime.timeline.wait(700)
      runtime.update(scene => ({ ...scene, syncBadge: 'synced', nodes: scene.nodes.map(node => node.id === 'entry' ? { ...node, status: 'synced' as const } : node) }))
      runtime.caption('全部显示「已同步」就生效了，用户的订阅里会自动出现这个入口。')
      await runtime.timeline.wait(3400)
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
    scene: () => scene,
    update: change => { scene = change(scene) },
    caption: () => undefined,
  }
  await chapter.run(runtime)
  return timeline.elapsed()
}
