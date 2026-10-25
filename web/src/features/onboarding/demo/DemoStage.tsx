import React, { useEffect, useLayoutEffect, useRef } from 'react'
import { AnimatePresence, animate, m, motionValue, useTransform, type MotionValue } from 'motion/react'
import {
  ArrowLeftRight, ArrowRightToLine, Ban, Boxes, Check, ChevronDown, Globe, LogOut, Network, Plus, Search, Server, Shield,
  ShieldCheck, Sliders, Smartphone, Split, Workflow, X, Zap,
} from 'lucide-react'
import { RegionFlag } from '../../../components/ui/RegionFlag'
import {
  CHROME_HEIGHT, DRAWER_LEFT, DRAWER_SERVERS, DRAWER_TILE_WIDTH, DRAWER_WIDTH, ENTRY_DIALOG, ENTRY_PROTOCOLS, MENU_ITEMS,
  MENU_ITEM_HEIGHT, MENU_SEPARATOR_HEIGHT, MENU_WIDTH, ROUTING_ACTIONS, ROUTING_DIALOG, STAGE_HEIGHT, STAGE_WIDTH,
  TRANSPORT_DIALOG, TRANSPORT_MODES, TRANSPORT_TARGETS, dragPath, edgePath, menuItemTop, nodeHeight, nodeWidth,
  type DemoEdge, type DemoNode, type DemoScene, type MenuItemID,
} from './scene'

const popSpring = { type: 'spring' as const, stiffness: 520, damping: 36, mass: 0.7 }
const moveSpring = { type: 'spring' as const, stiffness: 200, damping: 30, mass: 0.9 }

type NodeMotion = { x: MotionValue<number>; y: MotionValue<number> }
export type DemoCamera = { x: MotionValue<number>; y: MotionValue<number>; scale: MotionValue<number> }

const menuIcons: Partial<Record<MenuItemID, React.ComponentType<{ size?: number }>>> = {
  entry: ArrowRightToLine, imported: Boxes, direct: LogOut, warp: Zap, vpngate: ShieldCheck, routing: Split, server: Server,
  family: Network, fit: Search, reset: Sliders, details: Workflow,
}

// DemoStage draws one frame of the scripted topology page. Node positions,
// the camera and the cursor are motion values, so cards gliding into place
// or the camera zooming out never re-render React; edges follow on their own.
export function DemoStage({ scene, cursorX, cursorY, camera, paused, speed, reducedMotion, onRippleDone }: {
  scene: DemoScene
  cursorX: MotionValue<number>
  cursorY: MotionValue<number>
  camera: DemoCamera
  paused: boolean
  speed: number
  reducedMotion: boolean
  onRippleDone: (id: number) => void
}) {
  const frameRef = useRef<HTMLDivElement>(null)
  const motions = useRef(new Map<string, NodeMotion>())

  const nodeMotion = (node: DemoNode) => {
    let value = motions.current.get(node.id)
    if (!value) {
      value = { x: motionValue(node.x), y: motionValue(node.y) }
      motions.current.set(node.id, value)
    }
    return value
  }

  useEffect(() => {
    const running: Array<{ stop(): void }> = []
    for (const node of scene.nodes) {
      const value = nodeMotion(node)
      if (value.x.get() === node.x && value.y.get() === node.y) continue
      if (reducedMotion) {
        value.x.set(node.x)
        value.y.set(node.y)
        continue
      }
      running.push(animate(value.x, node.x, moveSpring), animate(value.y, node.y, moveSpring))
    }
    return () => running.forEach(item => item.stop())
  }, [scene.nodes, reducedMotion])

  // The stage is laid out at a fixed logical size and scaled as one layer, so
  // every coordinate in the script is the same on any screen.
  useLayoutEffect(() => {
    const frame = frameRef.current
    if (!frame) return
    const apply = () => frame.style.setProperty('--pd-scale', String(frame.clientWidth / STAGE_WIDTH))
    apply()
    if (typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(apply)
    observer.observe(frame)
    return () => observer.disconnect()
  }, [])

  const gridSize = useTransform(camera.scale, value => `${32 * value}px ${32 * value}px`)
  const gridPosition = useTransform([camera.x, camera.y], ([x, y]: number[]) => `${x}px ${y}px`)

  const byID = new Map(scene.nodes.map(node => [node.id, node]))
  const focusNode = scene.emphasis && scene.emphasis !== 'sync' ? scene.emphasis : null
  const flowDepth = edgeDepths(scene.edges)
  const provisionalSource = scene.provisional ? byID.get(scene.provisional.from) : undefined

  return <div ref={frameRef} className={`pd-frame${paused ? ' is-paused' : ''}${reducedMotion ? ' is-reduced' : ''}`} style={{ ['--pd-speed' as string]: String(speed) }} aria-hidden="true">
    <div className="pd-stage" style={{ width: STAGE_WIDTH, height: STAGE_HEIGHT }}>
      <m.div className="pd-grid" style={{ backgroundSize: gridSize, backgroundPosition: gridPosition }} />

      <m.div className="pd-world" style={{ x: camera.x, y: camera.y, scale: camera.scale }}>
        <svg className="pd-edges" width={STAGE_WIDTH} height={STAGE_HEIGHT}>
          {scene.edges.map(item => {
            const from = byID.get(item.from)
            const to = byID.get(item.to)
            if (!from || !to) return null
            return <DemoEdgePath key={item.id} edge={item} from={from} to={to} fromMotion={nodeMotion(from)} toMotion={nodeMotion(to)} dimmed={Boolean(focusNode)} flowDepth={reducedMotion || !item.packets ? null : flowDepth.get(item.id) ?? 0} />
          })}
          {provisionalSource && <ProvisionalEdge node={provisionalSource} motion={nodeMotion(provisionalSource)} cursorX={cursorX} cursorY={cursorY} camera={camera} />}
        </svg>


        {scene.nodes.map(node => <DemoNodeCard
          key={node.id}
          node={node}
          motion={nodeMotion(node)}
          emphasis={focusNode === null ? null : focusNode === node.id}
          activeSide={scene.activeHandle?.node === node.id ? scene.activeHandle.side : null}
          matching={scene.ruleMatching === node.id}
          edges={scene.edges}
          reducedMotion={reducedMotion}
        />)}
      </m.div>

      <AnimatePresence>
        {scene.chrome && <m.div key="chrome" className="pd-chrome" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
          <div className="pd-scope">
            <span>入口服务器</span>
            <div className="pd-select"><RegionFlag code={scene.entryServer.region} size={14} /><strong>{scene.entryServer.title}</strong><small>（{scene.entryServer.entries} 入口）</small><ChevronDown size={14} /></div>
          </div>
          <AnimatePresence>
            {scene.sync !== 'none' && <m.div key="sync" className={`pd-sync is-${scene.sync}${scene.emphasis === 'sync' ? ' is-emphasis' : ''}`} initial={{ opacity: 0, y: -4 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0 }} transition={popSpring}>
              {scene.sync === 'synced' ? <Check size={13} /> : <span className="pd-spinner" />}
              <span>{scene.sync === 'saving' ? '正在保存...' : scene.sync === 'syncing' ? `正在同步 ${scene.syncCount} 台服务器` : '配置已同步'}</span>
            </m.div>}
          </AnimatePresence>
        </m.div>}
      </AnimatePresence>
      {scene.chrome && <div className="pd-workbench">
        <span>右键空白处新建</span>
        <span className="pd-workbench-button"><Search size={13} />查看全图</span>
        <span className={`pd-workbench-button${scene.workbenchPressed === 'reset' ? ' is-pressed' : ''}`}><Sliders size={13} />重置布局</span>
      </div>}

      <AnimatePresence>
        {scene.menu && <m.div
          key="menu"
          className="pd-menu"
          style={{ left: scene.menu.x, top: scene.menu.y, width: MENU_WIDTH, transformOrigin: '0 0' }}
          initial={reducedMotion ? { opacity: 0 } : { opacity: 0, scale: 0.94 }}
          animate={{ opacity: 1, scale: 1 }}
          exit={{ opacity: 0, scale: reducedMotion ? 1 : 0.97, transition: { duration: 0.12 } }}
          transition={popSpring}
        >
          <div className="pd-menu-title">新建</div>
          {scene.menu.hover && <m.div className="pd-menu-hover" initial={false} animate={{ y: menuItemTop(scene.menu.hover) }} transition={popSpring} style={{ height: MENU_ITEM_HEIGHT }} />}
          {MENU_ITEMS.map(item => {
            if (item.id.startsWith('sep')) return <div key={item.id} className="pd-menu-separator" style={{ height: MENU_SEPARATOR_HEIGHT }} />
            const Icon = menuIcons[item.id]
            return <div key={item.id} className="pd-menu-item" style={{ height: MENU_ITEM_HEIGHT }}>{Icon && <Icon size={14} />}{item.label}</div>
          })}
        </m.div>}
      </AnimatePresence>

      <AnimatePresence>
        {(scene.dialog || scene.drawer) && <m.div key="backdrop" className="pd-backdrop" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} transition={{ duration: 0.16 }} />}
        {scene.drawer && <m.div
          key="drawer"
          className="pd-drawer"
          style={{ left: DRAWER_LEFT, width: DRAWER_WIDTH, height: STAGE_HEIGHT }}
          initial={reducedMotion ? { opacity: 0 } : { x: 40, opacity: 0 }}
          animate={{ x: 0, opacity: 1 }}
          exit={{ x: reducedMotion ? 0 : 30, opacity: 0, transition: { duration: 0.14 } }}
          transition={popSpring}
        >
          <div className="pd-sheet-head"><div><strong>添加其他服务器</strong><span>选择已有服务器或导入节点放入画布；同一服务器可以多次添加。</span></div><span className="pd-icon-button"><X size={14} /></span></div>
          <div className="pd-drawer-body">
            <div className="pd-search"><Search size={13} /><span>搜索服务器或节点</span></div>
            <div className="pd-group-head"><span>服务器</span><small>{DRAWER_SERVERS.length}</small></div>
            <div className="pd-tiles">
              {DRAWER_SERVERS.map((server, index) => <div key={server.name} className={`pd-tile${scene.drawer?.hover === index ? ' is-hover' : ''}`} style={{ width: DRAWER_TILE_WIDTH }}>
                <RegionFlag code={server.region} size={18} />
                <span><strong>{server.name}</strong><small>在线</small></span>
                {scene.drawer?.placed && index === 1 ? <Check size={14} className="pd-tile-placed" /> : <Plus size={14} />}
              </div>)}
            </div>
          </div>
          <div className="pd-sheet-foot"><span className={`pd-button primary${scene.drawer.pressed ? ' is-pressed' : ''}`}>完成</span></div>
        </m.div>}
        {scene.dialog?.kind === 'entry' && <DialogShell key="entry" box={ENTRY_DIALOG} reducedMotion={reducedMotion}>
          <div className="pd-sheet-head"><div><strong>添加入口</strong><span>部署到 东京-01</span></div><span className="pd-icon-button"><X size={14} /></span></div>
          <div className="pd-dialog-body">
            <div className="pd-section-title"><strong>协议</strong><span>TCP + Reality + Vision：借用目标站点的 TLS 握手，免域名和证书</span></div>
            <div className="pd-protocols">
              {ENTRY_PROTOCOLS.map(protocol => <div key={protocol.name} className={`pd-protocol${protocol.name === 'VLESS' ? ' is-selected' : ''}${scene.dialog?.kind === 'entry' && scene.dialog.hover === protocol.name ? ' is-hover' : ''}`}><strong>{protocol.name}</strong><small>{protocol.hint}</small></div>)}
            </div>
            <div className="pd-inline-field"><span>类型</span><div className="pd-segmented"><span className="is-active">Reality</span><span>WebSocket</span><span>TCP</span></div></div>
            <div className="pd-inline-field"><span>套用节点预设</span><div className="pd-input">VLESS Reality Vision<ChevronDown size={14} /></div></div>
          </div>
          <div className="pd-sheet-foot"><span className="pd-button ghost">取消</span><span className={`pd-button primary${scene.dialog.pressed ? ' is-pressed' : ''}`}>创建入口</span></div>
        </DialogShell>}
        {scene.dialog?.kind === 'transport' && <DialogShell key="transport" box={TRANSPORT_DIALOG} reducedMotion={reducedMotion}>
          <div className="pd-sheet-head"><div><strong>选择传递方式</strong><span>VLESS → 香港-02</span></div><span className="pd-icon-button"><X size={14} /></span></div>
          <div className="pd-dialog-body">
            <div className="pd-modes">
              {TRANSPORT_MODES.map((mode, index) => {
                const dialog = scene.dialog?.kind === 'transport' ? scene.dialog : null
                const Icon = index === 0 ? Workflow : index === 1 ? ArrowLeftRight : Shield
                return <div key={mode.title} className={`pd-mode${dialog?.mode === index ? ' is-selected' : ''}${dialog?.hover === index ? ' is-hover' : ''}`}>
                  <span className="pd-mode-head"><Icon size={14} /><strong>{mode.title}</strong>{dialog?.mode === index && <Check size={13} className="pd-mode-check" />}</span>
                  <small>{mode.hint}</small>
                </div>
              })}
            </div>
            <div className="pd-group-head"><span>系统生成入口</span></div>
            <div className="pd-targets">
              {TRANSPORT_TARGETS.map((target, index) => <div key={target} className={`pd-target${scene.dialog?.kind === 'transport' && scene.dialog.target === index ? ' is-selected' : ''}`}><strong>{target}</strong><small>系统隐藏 · 复用 0</small></div>)}
            </div>
            <div className="pd-preview"><span>本次变更</span><strong>VLESS → 香港-02 · sing-box 出站链 · {TRANSPORT_TARGETS[scene.dialog.target]} · 生成 1 条路径</strong><small>拓扑检查通过</small></div>
          </div>
          <div className="pd-sheet-foot"><span className="pd-button ghost">取消</span><span className={`pd-button primary${scene.dialog.pressed ? ' is-pressed' : ''}`}>确定</span></div>
        </DialogShell>}
        {scene.dialog?.kind === 'routing' && <DialogShell key="routing" box={ROUTING_DIALOG} reducedMotion={reducedMotion}>
          <div className="pd-sheet-head"><div><strong>分流规则编排</strong><span>香港-02 · 规则写在连上的服务器上，后面连接的节点是未匹配兜底</span></div><span className="pd-icon-button"><X size={14} /></span></div>
          <div className="pd-routing-body">
            <aside className="pd-routing-rules">
              <div className="pd-group-head"><span>B 节点规则（香港-02）</span></div>
              <AnimatePresence initial={false} mode="popLayout">
                {scene.dialog.added
                  ? <m.div key="rule" className="pd-rule-item" initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }} transition={popSpring}><b>1</b><span><strong>国内网站</strong><small>domain_suffix · cn · 直连</small></span></m.div>
                  : <m.div key="empty" className="pd-rule-empty" exit={{ opacity: 0 }}>暂无分流规则</m.div>}
              </AnimatePresence>
              <div className="pd-routing-fallback"><span>↓ 未命中规则时</span><strong>后面连接的节点</strong></div>
            </aside>
            <div className="pd-routing-form">
              <div className="pd-field"><span>规则名称</span><div className="pd-input">国内网站</div></div>
              <div className="pd-field-row">
                <div className="pd-field"><span>匹配类型</span><div className="pd-input">域名后缀 (domain_suffix)<ChevronDown size={14} /></div></div>
                <div className="pd-field"><span>匹配内容</span><div className="pd-input">cn</div></div>
              </div>
              <div className="pd-field"><span>处理动作与目标出口</span></div>
              <div className="pd-actions">
                {ROUTING_ACTIONS.map((action, index) => {
                  const Icon = [Zap, Ban, LogOut, Boxes, Split][index]
                  return <div key={action} className={`pd-action${index === 0 ? ' is-selected' : ''}`}><Icon size={15} /><span>{action}</span></div>
                })}
              </div>
              <div className="pd-note"><strong>直连：</strong>命中此规则的流量将直接从 香港-02 发起直连，不走任何代理出口。</div>
            </div>
          </div>
          <div className="pd-sheet-foot"><span className={`pd-button ghost${scene.dialog.donePressed ? ' is-pressed' : ''}`}>完成</span><span className={`pd-button primary${scene.dialog.addPressed ? ' is-pressed' : ''}`}><Plus size={14} />添加到 B 节点</span></div>
        </DialogShell>}
      </AnimatePresence>

      {scene.ripples.map(ripple => <span key={ripple.id} className={`pd-ripple is-${ripple.kind}`} style={{ left: ripple.x, top: ripple.y }} onAnimationEnd={() => onRippleDone(ripple.id)} />)}

      <AnimatePresence>
        {scene.cursorVisible && !reducedMotion && <m.div key="cursor" className="pd-cursor" style={{ x: cursorX, y: cursorY }} initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} transition={{ duration: 0.2 }}>
          <m.svg width="22" height="24" viewBox="0 0 22 24" animate={{ scale: scene.cursorPressed ? 0.84 : 1 }} transition={{ type: 'spring', stiffness: 700, damping: 30 }} style={{ transformOrigin: '3px 3px' }}>
            <path d="M3 2.5v17.2l4.6-4.3 3 6.6 3.1-1.4-3-6.5 6.3-.5z" />
          </m.svg>
        </m.div>}
      </AnimatePresence>
    </div>
  </div>
}

function DialogShell({ box, reducedMotion, children }: { box: { left: number; top: number; width: number; height: number }; reducedMotion: boolean; children: React.ReactNode }) {
  return <m.div
    className="pd-dialog"
    style={{ left: box.left, top: box.top, width: box.width, height: box.height }}
    initial={reducedMotion ? { opacity: 0 } : { opacity: 0, scale: 0.96, y: 8 }}
    animate={{ opacity: 1, scale: 1, y: 0 }}
    exit={{ opacity: 0, scale: reducedMotion ? 1 : 0.98, transition: { duration: 0.12 } }}
    transition={popSpring}
  >{children}</m.div>
}

function useEdgeD(from: DemoNode, to: DemoNode, fromMotion: NodeMotion, toMotion: NodeMotion) {
  const fromWidth = nodeWidth(from)
  const fromHeight = nodeHeight(from)
  const toWidth = nodeWidth(to)
  return useTransform([fromMotion.x, fromMotion.y, toMotion.x, toMotion.y], ([fx, fy, tx, ty]: number[]) =>
    edgePath({ x: fx + fromWidth / 2, y: fy + fromHeight }, { x: tx + toWidth / 2, y: ty }))
}

// edgeDepths numbers each edge by its hop from the start of the chain, so
// the flow light can be staggered and read as one pulse travelling down.
function edgeDepths(edges: DemoEdge[]) {
  const incoming = new Map(edges.map(item => [item.to, item]))
  const depths = new Map<string, number>()
  for (const item of edges) {
    let depth = 0
    for (let parent = incoming.get(item.from); parent && depth < edges.length; parent = incoming.get(parent.from)) depth++
    depths.set(item.id, depth)
  }
  return depths
}

function DemoEdgePath({ edge, from, to, fromMotion, toMotion, dimmed, flowDepth }: { edge: DemoEdge; from: DemoNode; to: DemoNode; fromMotion: NodeMotion; toMotion: NodeMotion; dimmed: boolean; flowDepth: number | null }) {
  const d = useEdgeD(from, to, fromMotion, toMotion)
  // Dashed links fade in; a dash pattern cannot be drawn on with pathLength.
  const dashed = edge.kind === 'belongs' || edge.kind === 'device'
  // Traffic is a short light streak sliding along the line itself rather
  // than separate dots, which crowded the handles on short hops.
  const flowStyle = flowDepth === null ? undefined : { animationDelay: `calc(${flowDepth * 0.32}s / var(--pd-speed, 1))` }
  return <g className={`pd-edge is-${edge.kind}${dimmed ? ' is-dimmed' : ''}`}>
    {dashed
      ? <m.path className="pd-edge-line" d={d} initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ duration: 0.4 }} />
      : <m.path className="pd-edge-line" d={d} initial={{ pathLength: 0 }} animate={{ pathLength: 1 }} transition={{ duration: 0.55, ease: [0.22, 1, 0.36, 1] }} />}
    {flowStyle && <>
      <m.path className="pd-edge-flow is-halo" d={d} pathLength={100} style={flowStyle} />
      <m.path className="pd-edge-flow" d={d} pathLength={100} style={flowStyle} />
    </>}
  </g>
}

function ProvisionalEdge({ node, motion, cursorX, cursorY, camera }: { node: DemoNode; motion: NodeMotion; cursorX: MotionValue<number>; cursorY: MotionValue<number>; camera: DemoCamera }) {
  const width = nodeWidth(node)
  const height = nodeHeight(node)
  const d = useTransform([motion.x, motion.y, cursorX, cursorY, camera.x, camera.y, camera.scale], ([x, y, cx, cy, kx, ky, ks]: number[]) =>
    dragPath({ x: x + width / 2, y: y + height }, { x: (cx - kx) / ks, y: (cy - ky) / ks }))
  return <m.path className="pd-edge-provisional" d={d} />
}

function DemoNodeCard({ node, motion, emphasis, activeSide, matching, edges, reducedMotion }: {
  node: DemoNode
  motion: NodeMotion
  emphasis: boolean | null
  activeSide: 'in' | 'out' | null
  matching: boolean
  edges: DemoEdge[]
  reducedMotion: boolean
}) {
  const width = nodeWidth(node)
  const pill = node.kind === 'device' || node.kind === 'site'
  const hasInput = node.kind !== 'device' && node.kind !== 'entry'
  const hasOutput = node.kind === 'entry' || node.kind === 'server' || node.kind === 'device' || (node.kind === 'routing' && Boolean(node.connected))
  const linkedIn = edges.some(item => item.to === node.id)
  const linkedOut = edges.some(item => item.from === node.id)
  const handle = (side: 'in' | 'out') => `pd-handle is-${side}${activeSide === side ? ' is-active' : ''}${(side === 'in' ? linkedIn : linkedOut) ? ' is-linked' : ''}`
  const Icon = node.kind === 'entry' || node.kind === 'site' ? Globe
    : node.kind === 'server' ? (node.role === '一级服务器' ? Server : Workflow)
      : node.kind === 'warp' ? Zap
        : node.kind === 'routing' ? Workflow
          : Smartphone
  const relay = node.kind === 'server' && node.role === '链路服务器'
  return <m.div className="pd-node-anchor" style={{ x: motion.x, y: motion.y, width }}>
    <m.div
      className={`pd-node is-${node.kind}${relay ? ' is-relay' : ''}${node.connected ? ' is-connected' : ''}${matching ? ' is-matching' : ''}${emphasis === true ? ' is-emphasis' : ''}${emphasis === false ? ' is-dimmed' : ''}`}
      style={{ height: nodeHeight(node) }}
      initial={reducedMotion ? { opacity: 0 } : { opacity: 0, scale: 0.92, y: 6 }}
      animate={{ opacity: 1, scale: 1, y: 0 }}
      transition={popSpring}
    >
      {pill
        ? <div className="pd-pill"><Icon size={15} /><strong>{node.title}</strong></div>
        : <>
          <div className="pd-node-head">
            <span className="pd-node-icon"><Icon size={15} /></span>
            {node.kind === 'warp' || node.kind === 'routing'
              ? <span className="pd-node-heading"><small>{node.role}</small><strong>{node.title}</strong></span>
              : <span className="pd-node-heading"><strong>{node.region && <RegionFlag code={node.region} size={13} />}{node.title}</strong><small>{node.role}</small></span>}
            {node.status && <span className="pd-node-status"><i />在线</span>}
            {node.badge && <span className="pd-node-badge">{node.badge}</span>}
          </div>
          {node.rows?.map(row => <div key={row.label} className="pd-node-row"><span>{row.label}</span><strong className={row.mono ? 'is-mono' : ''}>{row.region && <RegionFlag code={row.region} size={12} />}{row.value}</strong></div>)}
          {node.chips && <div className="pd-node-chips">{node.chips.map(chip => <span key={chip}>{chip}</span>)}</div>}
          {node.rules && <div className="pd-node-rules">{node.rules.map(rule => <div key={rule.name} className="pd-node-rule"><span>{rule.name}</span><small>{rule.action}</small><i className="pd-handle is-rule" /></div>)}</div>}
          {node.footer && <div className="pd-node-footer">{node.footer}</div>}
          {node.meta && <div className="pd-node-meta">{node.meta}</div>}
        </>}
      {hasInput && <i className={handle('in')} />}
      {hasOutput && <i className={handle('out')} />}
      {node.handleLabel && <span className="pd-handle-label">{node.handleLabel}</span>}
    </m.div>
  </m.div>
}
