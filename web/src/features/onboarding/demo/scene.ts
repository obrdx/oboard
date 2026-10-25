// Stage geometry for the proxy path demo. The stage mirrors the real topology
// page: a chrome row with the entry-server selector and sync indicator, then a
// vertical canvas where every link runs from a card's bottom to the next
// card's top. Cards live in world coordinates under a camera; menus, dialogs
// and the cursor live in stage coordinates on top.

export const STAGE_WIDTH = 1040
export const STAGE_HEIGHT = 650
export const CHROME_HEIGHT = 52

export type Point = { x: number; y: number }
export type Camera = { x: number; y: number; scale: number }

export type DemoNodeKind = 'device' | 'site' | 'entry' | 'server' | 'warp' | 'routing'

export type DemoRow = { label: string; value: string; mono?: boolean; region?: string }
export type DemoRule = { name: string; action: string }

export type DemoNode = {
  id: string
  kind: DemoNodeKind
  x: number
  y: number
  title: string
  role: string
  status?: 'online'
  badge?: string
  region?: string
  rows?: DemoRow[]
  chips?: string[]
  footer?: string
  meta?: string
  rules?: DemoRule[]
  // handleLabel is the per-entry output label a gateway server shows under
  // its bottom edge, the same "VLESS:10443" tag the real card draws.
  handleLabel?: string
  connected?: boolean
}

export type DemoEdgeKind = 'belongs' | 'device' | 'singbox' | 'warp' | 'routing' | 'direct'

export type DemoEdge = {
  id: string
  from: string
  to: string
  kind: DemoEdgeKind
  packets?: 'primary' | 'secondary'
}

export type DemoSync = 'none' | 'saving' | 'syncing' | 'synced'

export type DemoScene = {
  nodes: DemoNode[]
  edges: DemoEdge[]
  camera: Camera
  menu: { x: number; y: number; hover: MenuItemID | null } | null
  dialog: EntryDialogState | TransportDialogState | RoutingDialogState | null
  drawer: { hover: number; placed: boolean; pressed: boolean } | null
  provisional: { from: string } | null
  activeHandle: { node: string; side: 'in' | 'out' } | null
  cursorVisible: boolean
  cursorPressed: boolean
  ripples: { id: number; x: number; y: number; kind: 'left' | 'right' }[]
  chrome: boolean
  entryServer: { title: string; region: string; entries: number }
  sync: DemoSync
  syncCount: number
  emphasis: string | null
  focus: string | null
  ruleStub: string | null
  workbenchPressed: 'reset' | null
}

export type EntryDialogState = { kind: 'entry'; hover: string | null; pressed: boolean }
export type TransportDialogState = { kind: 'transport'; mode: number; hover: number; target: number; pressed: boolean }
export type RoutingDialogState = { kind: 'routing'; added: boolean; addPressed: boolean; donePressed: boolean }

export const emptyScene = (): DemoScene => ({
  nodes: [],
  edges: [],
  camera: { x: 0, y: 0, scale: 1 },
  menu: null,
  dialog: null,
  drawer: null,
  provisional: null,
  activeHandle: null,
  cursorVisible: false,
  cursorPressed: false,
  ripples: [],
  chrome: true,
  entryServer: { title: '东京-01', region: 'JP', entries: 0 },
  sync: 'none',
  syncCount: 0,
  emphasis: null,
  focus: null,
  ruleStub: null,
  workbenchPressed: null,
})

export const COLUMN_X = 520

const HEADER = 46
const ROW = 24
const RULE = 30

export function nodeWidth(node: Pick<DemoNode, 'kind'>) {
  if (node.kind === 'device' || node.kind === 'site') return 156
  if (node.kind === 'entry') return 248
  if (node.kind === 'warp') return 220
  return 232
}

export function nodeHeight(node: Pick<DemoNode, 'kind' | 'rows' | 'rules'>) {
  if (node.kind === 'device' || node.kind === 'site') return 40
  if (node.kind === 'entry') return HEADER + 6 + ROW + 26 + 28
  if (node.kind === 'server') return HEADER + 6 + (node.rows?.length || 1) * ROW + 6
  if (node.kind === 'warp') return HEADER + ROW + 4
  const rules = node.rules?.length || 0
  return HEADER + (rules ? rules * RULE + 6 : 0) + ROW + 4
}

export function ruleOffset(index: number) {
  return HEADER + 3 + index * RULE + RULE / 2
}

// column places a card so its centre sits on the canvas column.
export function column(kind: DemoNodeKind) {
  return COLUMN_X - nodeWidth({ kind }) / 2
}

export function inputHandle(node: DemoNode): Point {
  return { x: node.x + nodeWidth(node) / 2, y: node.y }
}

export function outputHandle(node: DemoNode): Point {
  return { x: node.x + nodeWidth(node) / 2, y: node.y + nodeHeight(node) }
}

export function ruleHandle(node: DemoNode, index = 0): Point {
  return { x: node.x + nodeWidth(node), y: node.y + ruleOffset(index) }
}

// edgePath draws the real canvas's orthogonal route: down, across, down, with
// rounded corners; aligned cards get a straight line.
export function edgePath(from: Point, to: Point) {
  const dx = to.x - from.x
  if (Math.abs(dx) < 1) return `M ${from.x} ${from.y} L ${to.x} ${to.y}`
  const midY = from.y + Math.max(18, (to.y - from.y) / 2)
  const radius = Math.min(16, Math.abs(dx) / 2, Math.abs(midY - from.y), Math.abs(to.y - midY))
  const sign = dx > 0 ? 1 : -1
  return [
    `M ${from.x} ${from.y}`,
    `L ${from.x} ${midY - radius}`,
    `Q ${from.x} ${midY} ${from.x + sign * radius} ${midY}`,
    `L ${to.x - sign * radius} ${midY}`,
    `Q ${to.x} ${midY} ${to.x} ${midY + radius}`,
    `L ${to.x} ${to.y}`,
  ].join(' ')
}

// dragPath is the loose curve the canvas shows while a connection is drawn.
export function dragPath(from: Point, to: Point) {
  const bend = Math.max(40, Math.abs(to.y - from.y) * 0.5)
  return `M ${from.x} ${from.y} C ${from.x} ${from.y + bend}, ${to.x} ${to.y - bend}, ${to.x} ${to.y}`
}

export function ruleStubPath(node: DemoNode) {
  const start = ruleHandle(node, 0)
  return `M ${start.x} ${start.y} L ${start.x + 120} ${start.y}`
}

export function sceneEdgePath(nodes: DemoNode[], edge: DemoEdge) {
  const from = nodes.find(node => node.id === edge.from)
  const to = nodes.find(node => node.id === edge.to)
  if (!from || !to) return ''
  return edgePath(outputHandle(from), inputHandle(to))
}

export function toStage(camera: Camera, point: Point): Point {
  return { x: point.x * camera.scale + camera.x, y: point.y * camera.scale + camera.y }
}

export function toWorld(camera: Camera, point: Point): Point {
  return { x: (point.x - camera.x) / camera.scale, y: (point.y - camera.y) / camera.scale }
}

// fitCamera centres a world rectangle in the canvas area below the chrome,
// the way 查看全图 does.
export function fitCamera(top: number, bottom: number, margin = 28): Camera {
  const available = STAGE_HEIGHT - CHROME_HEIGHT - margin * 2
  const scale = Math.min(1, available / (bottom - top))
  return { scale, x: COLUMN_X - COLUMN_X * scale, y: CHROME_HEIGHT + margin - top * scale }
}

export function arcPoint(from: Point, to: Point, t: number): Point {
  const dx = to.x - from.x
  const dy = to.y - from.y
  const distance = Math.hypot(dx, dy)
  const bend = Math.min(60, distance * 0.12)
  const control = { x: from.x + dx / 2 - (dy / (distance || 1)) * bend, y: from.y + dy / 2 + (dx / (distance || 1)) * bend * 0.6 }
  const u = 1 - t
  return { x: u * u * from.x + 2 * u * t * control.x + t * t * to.x, y: u * u * from.y + 2 * u * t * control.y + t * t * to.y }
}

export function travelDuration(from: Point, to: Point) {
  const distance = Math.hypot(to.x - from.x, to.y - from.y)
  return Math.round(Math.min(760, Math.max(300, 240 + distance * 0.9)))
}

// The canvas menu, in the real order. Separators sit between groups.
export const MENU_ITEMS = [
  { id: 'entry', label: '入口节点' },
  { id: 'imported', label: '导入节点' },
  { id: 'direct', label: '直接出口' },
  { id: 'warp', label: 'WARP 出口' },
  { id: 'vpngate', label: 'VPNGate 出口' },
  { id: 'routing', label: '分流出口' },
  { id: 'server', label: '其他服务器' },
  { id: 'sep-1', label: '' },
  { id: 'family', label: '双栈模板' },
  { id: 'sep-2', label: '' },
  { id: 'fit', label: '查看全图' },
  { id: 'reset', label: '重置布局' },
  { id: 'details', label: '链路详情' },
] as const

export type MenuItemID = typeof MENU_ITEMS[number]['id']

export const MENU_WIDTH = 196
export const MENU_PADDING = 6
export const MENU_TITLE_HEIGHT = 32
export const MENU_ITEM_HEIGHT = 30
export const MENU_SEPARATOR_HEIGHT = 9

export function menuItemTop(id: MenuItemID) {
  let top = MENU_PADDING + MENU_TITLE_HEIGHT
  for (const item of MENU_ITEMS) {
    if (item.id === id) return top
    top += item.id.startsWith('sep') ? MENU_SEPARATOR_HEIGHT : MENU_ITEM_HEIGHT
  }
  return top
}

export const MENU_HEIGHT = menuItemTop('details') + MENU_ITEM_HEIGHT + MENU_PADDING

// menuPosition keeps the whole menu on the stage, like the real canvas menu.
export function menuPosition(at: Point): Point {
  return {
    x: Math.min(at.x, STAGE_WIDTH - MENU_WIDTH - 8),
    y: Math.max(CHROME_HEIGHT + 4, Math.min(at.y, STAGE_HEIGHT - MENU_HEIGHT - 8)),
  }
}

export function menuItemPoint(menu: Point, id: MenuItemID): Point {
  return { x: menu.x + 70, y: menu.y + menuItemTop(id) + MENU_ITEM_HEIGHT / 2 }
}

// Workbench bar in the canvas's top-right corner.
export const WORKBENCH_RESET: Point = { x: STAGE_WIDTH - 62, y: CHROME_HEIGHT + 26 }

// Server drawer: the real 添加其他服务器 sheet slides in from the right.
export const DRAWER_WIDTH = 400
export const DRAWER_LEFT = STAGE_WIDTH - DRAWER_WIDTH
export const DRAWER_SERVERS = [
  { name: '洛杉矶-03', region: 'US' },
  { name: '香港-02', region: 'HK' },
] as const
export const DRAWER_TILE_WIDTH = 176
export function drawerTilePoint(index: number): Point {
  return { x: DRAWER_LEFT + 16 + index * (DRAWER_TILE_WIDTH + 16) + DRAWER_TILE_WIDTH / 2, y: 214 }
}
export const DRAWER_DONE: Point = { x: STAGE_WIDTH - 46, y: STAGE_HEIGHT - 30 }

// Centred dialogs. Each exports the points the cursor lands on.
export function dialogBox(width: number, height: number) {
  return { left: (STAGE_WIDTH - width) / 2, top: (STAGE_HEIGHT - height) / 2, width, height }
}

export const ENTRY_DIALOG = dialogBox(600, 392)
export const ENTRY_PROTOCOLS = [
  { name: 'VLESS', hint: 'Reality · WebSocket · TCP' },
  { name: 'AnyTLS', hint: 'TLS + Padding · 需证书' },
  { name: 'Hysteria2', hint: 'QUIC / UDP · 需证书' },
  { name: 'Shadowsocks', hint: 'AEAD 加密 · 免证书' },
  { name: 'Mieru', hint: '免证书 · 支持多端口' },
  { name: 'Snell', hint: 'UOT · PSK' },
  { name: 'SOCKS5', hint: '用户名密码 · TCP/UDP' },
  { name: 'SSH', hint: '密码认证 · 仅端口转发' },
] as const
export function entryProtocolPoint(index: number): Point {
  const column = index % 4
  const row = Math.floor(index / 4)
  return { x: ENTRY_DIALOG.left + 20 + column * 141 + 66, y: ENTRY_DIALOG.top + 128 + row * 56 + 24 }
}
export const ENTRY_SUBMIT: Point = { x: ENTRY_DIALOG.left + ENTRY_DIALOG.width - 64, y: ENTRY_DIALOG.top + ENTRY_DIALOG.height - 30 }

export const TRANSPORT_DIALOG = dialogBox(600, 440)
export const TRANSPORT_MODES = [
  { title: 'sing-box 出站链', hint: '由当前服务器建立协议出站，连接所选系统服务或已有入口。' },
  { title: '透明端口转发', hint: '原样传递客户端密文，只能位于链路开头。' },
  { title: 'SSH / WireGuard 隧道', hint: '通过服务器间专用隧道连接所选入口。' },
] as const
export const TRANSPORT_TARGETS = ['SS 2022-128', 'SS 2022-256', 'SS 2022-ChaCha20', 'VLESS Reality', 'Mieru TCP', 'SOCKS5'] as const
export function transportModePoint(index: number): Point {
  return { x: TRANSPORT_DIALOG.left + 20 + index * 190 + 90, y: TRANSPORT_DIALOG.top + 64 + 16 + 44 }
}
export function transportTargetPoint(index: number): Point {
  return { x: TRANSPORT_DIALOG.left + 20 + (index % 3) * 190 + 90, y: TRANSPORT_DIALOG.top + 214 + Math.floor(index / 3) * 52 + 22 }
}
export const TRANSPORT_CONFIRM: Point = { x: TRANSPORT_DIALOG.left + TRANSPORT_DIALOG.width - 52, y: TRANSPORT_DIALOG.top + TRANSPORT_DIALOG.height - 30 }

export const ROUTING_DIALOG = dialogBox(760, 470)
export const ROUTING_ACTIONS = ['直连', '阻断', '出口', '导入节点', '代理链路'] as const
export const ROUTING_ADD: Point = { x: ROUTING_DIALOG.left + ROUTING_DIALOG.width - 82, y: ROUTING_DIALOG.top + ROUTING_DIALOG.height - 30 }
export const ROUTING_DONE: Point = { x: ROUTING_DIALOG.left + ROUTING_DIALOG.width - 190, y: ROUTING_DIALOG.top + ROUTING_DIALOG.height - 30 }
export function routingActionPoint(index: number): Point {
  return { x: ROUTING_DIALOG.left + 262 + index * 92 + 42, y: ROUTING_DIALOG.top + 330 }
}
