export const STAGE_WIDTH = 1040
export const STAGE_HEIGHT = 585

export type Point = { x: number; y: number }

export type DemoNodeKind = 'client' | 'entry' | 'server' | 'warp' | 'direct' | 'routing'

export type DemoNode = {
  id: string
  kind: DemoNodeKind
  x: number
  y: number
  label: string
  title: string
  detail: string
  region?: string
  status?: 'synced' | 'syncing' | 'pending'
}

export type DemoEdgeKind = 'client' | 'singbox' | 'port_forward' | 'tunnel' | 'local' | 'rule'

export type DemoEdge = {
  id: string
  from: string
  to: string
  kind: DemoEdgeKind
  // fromPort picks a handle on a node with several outputs (routing rules).
  fromPort?: number
  packets?: 'primary' | 'secondary'
}

export type DemoMenu = { x: number; y: number; hover: number }
export type DemoDialog =
  | { kind: 'entry'; protocol: string; port: string; pressed: boolean }
  | { kind: 'transport'; hover: number; selected: number; pressed: boolean }
export type DemoPicker = { x: number; y: number; hover: number }

export type DemoScene = {
  nodes: DemoNode[]
  edges: DemoEdge[]
  menu: DemoMenu | null
  dialog: DemoDialog | null
  picker: DemoPicker | null
  // provisional is the dashed edge that follows the cursor while dragging.
  provisional: { from: string; fromPort?: number } | null
  activeHandle: { node: string; port?: number; side: 'in' | 'out' } | null
  cursorVisible: boolean
  cursorPressed: boolean
  ripples: { id: number; x: number; y: number; kind: 'left' | 'right' }[]
  hint: string
  syncBadge: 'none' | 'syncing' | 'synced'
  emphasis: string | null
}

export const emptyScene = (): DemoScene => ({
  nodes: [],
  edges: [],
  menu: null,
  dialog: null,
  picker: null,
  provisional: null,
  activeHandle: null,
  cursorVisible: false,
  cursorPressed: false,
  ripples: [],
  hint: '',
  syncBadge: 'none',
  emphasis: null,
})

export const NODE_WIDTH = 172
export const CLIENT_WIDTH = 116

export function nodeWidth(node: Pick<DemoNode, 'kind'>) {
  return node.kind === 'client' ? CLIENT_WIDTH : NODE_WIDTH
}

export function nodeHeight(node: Pick<DemoNode, 'kind'>) {
  if (node.kind === 'client') return 64
  if (node.kind === 'routing') return 132
  return 96
}

// Routing cards expose one output per rule; the last one is the fallback.
export const ROUTING_PORT_OFFSETS = [78, 110]

export function inputHandle(node: DemoNode): Point {
  return { x: node.x, y: node.y + nodeHeight(node) / 2 }
}

export function outputHandle(node: DemoNode, port?: number): Point {
  if (node.kind === 'routing' && port !== undefined) return { x: node.x + nodeWidth(node), y: node.y + ROUTING_PORT_OFFSETS[port] }
  return { x: node.x + nodeWidth(node), y: node.y + nodeHeight(node) / 2 }
}

export function nodeCenter(node: DemoNode): Point {
  return { x: node.x + nodeWidth(node) / 2, y: node.y + nodeHeight(node) / 2 }
}

export function edgePath(from: Point, to: Point) {
  const dx = Math.max(36, Math.abs(to.x - from.x) * 0.5)
  return `M ${from.x} ${from.y} C ${from.x + dx} ${from.y}, ${to.x - dx} ${to.y}, ${to.x} ${to.y}`
}

export function sceneEdgePath(scene: Pick<DemoScene, 'nodes'>, edge: DemoEdge) {
  const from = scene.nodes.find(node => node.id === edge.from)
  const to = scene.nodes.find(node => node.id === edge.to)
  if (!from || !to) return ''
  return edgePath(outputHandle(from, edge.fromPort), inputHandle(to))
}

// arcPoint moves along a shallow quadratic arc, which reads as a hand moving
// a mouse rather than a robot sliding in a straight line.
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

export const MENU_ITEMS = [
  { id: 'entry', label: '入口节点' },
  { id: 'imported', label: '导入节点' },
  { id: 'direct', label: '直接出口' },
  { id: 'warp', label: 'WARP 出口' },
  { id: 'vpngate', label: 'VPNGate 出口' },
  { id: 'routing', label: '分流出口' },
  { id: 'server', label: '其他服务器' },
] as const

export type MenuItemID = typeof MENU_ITEMS[number]['id']

export const MENU_WIDTH = 184
export const MENU_TITLE_HEIGHT = 32
export const MENU_ITEM_HEIGHT = 32

export function menuItemPoint(menu: Pick<DemoMenu, 'x' | 'y'>, id: MenuItemID): Point {
  const index = MENU_ITEMS.findIndex(item => item.id === id)
  return { x: menu.x + 64, y: menu.y + 6 + MENU_TITLE_HEIGHT + index * MENU_ITEM_HEIGHT + MENU_ITEM_HEIGHT / 2 }
}

export const PICKER_SERVERS = [
  { id: 'hk', name: '香港-02', region: 'HK' },
  { id: 'sg', name: '新加坡-03', region: 'SG' },
  { id: 'us', name: '洛杉矶-04', region: 'US' },
] as const

export const PICKER_WIDTH = 236

export function pickerTilePoint(picker: Pick<DemoPicker, 'x' | 'y'>, index: number): Point {
  return { x: picker.x + PICKER_WIDTH / 2, y: picker.y + 46 + index * 46 + 20 }
}

export const TRANSPORT_OPTIONS = [
  { kind: 'singbox' as const, title: 'sing-box 出站链', detail: '按协议逐跳加密转发，最通用' },
  { kind: 'port_forward' as const, title: '透明端口转发', detail: '原样转发流量，延迟最低' },
  { kind: 'tunnel' as const, title: 'SSH / WireGuard 隧道', detail: '先建隧道再转发，适合受限网络' },
]

// Stage-space geometry of the centred dialogs, kept here so the cursor lands
// exactly on the controls the stage renders.
export const DIALOG_WIDTH = 380
export const DIALOG_LEFT = (STAGE_WIDTH - DIALOG_WIDTH) / 2

export const ENTRY_DIALOG_HEIGHT = 236
export const ENTRY_DIALOG_TOP = (STAGE_HEIGHT - ENTRY_DIALOG_HEIGHT) / 2
export const entryDialogSubmit: Point = { x: DIALOG_LEFT + DIALOG_WIDTH - 62, y: ENTRY_DIALOG_TOP + ENTRY_DIALOG_HEIGHT - 32 }

export const TRANSPORT_DIALOG_HEIGHT = 318
export const TRANSPORT_DIALOG_TOP = (STAGE_HEIGHT - TRANSPORT_DIALOG_HEIGHT) / 2
export function transportOptionPoint(index: number): Point {
  return { x: DIALOG_LEFT + 150, y: TRANSPORT_DIALOG_TOP + 82 + index * 62 }
}
export const transportDialogConfirm: Point = { x: DIALOG_LEFT + DIALOG_WIDTH - 62, y: TRANSPORT_DIALOG_TOP + TRANSPORT_DIALOG_HEIGHT - 30 }
