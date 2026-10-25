import React, { useEffect, useLayoutEffect, useRef } from 'react'
import { AnimatePresence, animate, m, motionValue, useTransform, type MotionValue } from 'motion/react'
import { ArrowRightToLine, Boxes, Check, LogOut, Server, ShieldCheck, Split, Zap } from 'lucide-react'
import { RegionFlag } from '../../../components/ui/RegionFlag'
import {
  CLIENT_WIDTH, DIALOG_LEFT, DIALOG_WIDTH, ENTRY_DIALOG_HEIGHT, ENTRY_DIALOG_TOP, MENU_ITEMS, MENU_ITEM_HEIGHT, MENU_WIDTH,
  NODE_WIDTH, PICKER_SERVERS, PICKER_WIDTH, ROUTING_PORT_OFFSETS, STAGE_HEIGHT, STAGE_WIDTH, TRANSPORT_DIALOG_HEIGHT,
  TRANSPORT_DIALOG_TOP, TRANSPORT_OPTIONS, edgePath, nodeHeight, nodeWidth,
  type DemoEdge, type DemoNode, type DemoScene,
} from './scene'

const popSpring = { type: 'spring' as const, stiffness: 520, damping: 36, mass: 0.7 }
const moveSpring = { type: 'spring' as const, stiffness: 210, damping: 30, mass: 0.9 }

type NodeMotion = { x: MotionValue<number>; y: MotionValue<number> }

const menuIcons = { entry: ArrowRightToLine, imported: Boxes, direct: LogOut, warp: Zap, vpngate: ShieldCheck, routing: Split, server: Server }

const nodeIcons: Record<DemoNode['kind'], React.ComponentType<{ size?: number }>> = {
  client: ({ size }) => <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true"><rect x="6" y="3" width="12" height="18" rx="2.5" /><path d="M11 17.5h2" /></svg>,
  entry: ArrowRightToLine,
  server: Server,
  warp: Zap,
  direct: LogOut,
  routing: Split,
}

// DemoStage draws one frame of the scripted canvas. Node positions, the
// cursor and every edge endpoint are motion values, so moving a card or the
// mouse never re-renders React: edges and packets follow on the compositor.
export function DemoStage({ scene, cursorX, cursorY, paused, speed, reducedMotion, onRippleDone }: {
  scene: DemoScene
  cursorX: MotionValue<number>
  cursorY: MotionValue<number>
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

  const byID = new Map(scene.nodes.map(node => [node.id, node]))
  const dim = scene.emphasis !== null

  return <div ref={frameRef} className={`pd-frame${paused ? ' is-paused' : ''}${reducedMotion ? ' is-reduced' : ''}`} style={{ ['--pd-speed' as string]: String(speed) }} aria-hidden="true">
    <div className="pd-stage" style={{ width: STAGE_WIDTH, height: STAGE_HEIGHT }}>
      <div className="pd-grid" />
      <div className="pd-scope"><RegionFlag code="JP" size={14} /><span>当前服务器</span><strong>东京-01</strong></div>
      <AnimatePresence>
        {scene.syncBadge !== 'none' && <m.div key="sync" className={`pd-sync is-${scene.syncBadge}`} initial={{ opacity: 0, y: -6 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0 }} transition={popSpring}>
          {scene.syncBadge === 'synced' ? <Check size={13} /> : <span className="pd-spinner" />}
          <span>{scene.syncBadge === 'synced' ? '已同步' : '正在同步…'}</span>
        </m.div>}
      </AnimatePresence>

      <svg className="pd-edges" width={STAGE_WIDTH} height={STAGE_HEIGHT} viewBox={`0 0 ${STAGE_WIDTH} ${STAGE_HEIGHT}`}>
        {scene.edges.map(item => {
          const from = byID.get(item.from)
          const to = byID.get(item.to)
          if (!from || !to) return null
          return <DemoEdgePath key={item.id} edge={item} from={from} to={to} fromMotion={nodeMotion(from)} toMotion={nodeMotion(to)} dimmed={dim} />
        })}
        {scene.provisional && byID.get(scene.provisional.from) && <ProvisionalEdge node={byID.get(scene.provisional.from)!} port={scene.provisional.fromPort} motion={nodeMotion(byID.get(scene.provisional.from)!)} cursorX={cursorX} cursorY={cursorY} />}
      </svg>

      {!reducedMotion && scene.edges.filter(item => item.packets).map(item => {
        const from = byID.get(item.from)
        const to = byID.get(item.to)
        if (!from || !to) return null
        return <EdgePackets key={`packets-${item.id}`} edge={item} from={from} to={to} fromMotion={nodeMotion(from)} toMotion={nodeMotion(to)} />
      })}

      {scene.nodes.map(node => <DemoNodeCard
        key={node.id}
        node={node}
        motion={nodeMotion(node)}
        emphasis={scene.emphasis === null ? null : scene.emphasis === node.id}
        activeHandle={scene.activeHandle?.node === node.id ? scene.activeHandle : null}
        connected={scene.edges}
        reducedMotion={reducedMotion}
      />)}

      <AnimatePresence>
        {scene.hint && <m.div key="hint" className="pd-hint" initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: 4 }} transition={{ duration: 0.18 }}>{scene.hint}</m.div>}
      </AnimatePresence>

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
          <div className="pd-menu-items">
            {scene.menu.hover >= 0 && <m.div className="pd-menu-hover" initial={false} animate={{ y: scene.menu.hover * MENU_ITEM_HEIGHT }} transition={popSpring} style={{ height: MENU_ITEM_HEIGHT }} />}
            {MENU_ITEMS.map((item, index) => {
              const Icon = menuIcons[item.id]
              return <div key={item.id} className={`pd-menu-item${scene.menu?.hover === index ? ' is-hover' : ''}`} style={{ height: MENU_ITEM_HEIGHT }}><Icon size={14} />{item.label}</div>
            })}
          </div>
        </m.div>}
      </AnimatePresence>

      <AnimatePresence>
        {scene.picker && <m.div
          key="picker"
          className="pd-picker"
          style={{ left: scene.picker.x, top: scene.picker.y, width: PICKER_WIDTH, transformOrigin: '0 0' }}
          initial={reducedMotion ? { opacity: 0 } : { opacity: 0, scale: 0.94 }}
          animate={{ opacity: 1, scale: 1 }}
          exit={{ opacity: 0, transition: { duration: 0.12 } }}
          transition={popSpring}
        >
          <div className="pd-picker-title">选择服务器</div>
          {PICKER_SERVERS.map((server, index) => <div key={server.id} className={`pd-picker-tile${scene.picker?.hover === index ? ' is-hover' : ''}`}>
            <RegionFlag code={server.region} size={18} />
            <strong>{server.name}</strong>
            <small>在线</small>
          </div>)}
        </m.div>}
      </AnimatePresence>

      <AnimatePresence>
        {scene.dialog && <m.div key="backdrop" className="pd-backdrop" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} transition={{ duration: 0.16 }} />}
        {scene.dialog?.kind === 'entry' && <m.div
          key="entry-dialog"
          className="pd-dialog"
          style={{ left: DIALOG_LEFT, top: ENTRY_DIALOG_TOP, width: DIALOG_WIDTH, height: ENTRY_DIALOG_HEIGHT }}
          initial={reducedMotion ? { opacity: 0 } : { opacity: 0, scale: 0.96, y: 8 }}
          animate={{ opacity: 1, scale: 1, y: 0 }}
          exit={{ opacity: 0, scale: reducedMotion ? 1 : 0.98, transition: { duration: 0.12 } }}
          transition={popSpring}
        >
          <div className="pd-dialog-head"><strong>新建入口</strong><span>东京-01</span></div>
          <div className="pd-dialog-body pd-fields">
            <div className="pd-field"><span>协议</span><div className="pd-input">{scene.dialog.protocol}</div></div>
            <div className="pd-field"><span>端口</span><div className="pd-input">{scene.dialog.port}</div></div>
          </div>
          <div className="pd-dialog-foot"><span className="pd-button ghost">取消</span><span className={`pd-button primary${scene.dialog.pressed ? ' is-pressed' : ''}`}>创建</span></div>
        </m.div>}
        {scene.dialog?.kind === 'transport' && <m.div
          key="transport-dialog"
          className="pd-dialog"
          style={{ left: DIALOG_LEFT, top: TRANSPORT_DIALOG_TOP, width: DIALOG_WIDTH, height: TRANSPORT_DIALOG_HEIGHT }}
          initial={reducedMotion ? { opacity: 0 } : { opacity: 0, scale: 0.96, y: 8 }}
          animate={{ opacity: 1, scale: 1, y: 0 }}
          exit={{ opacity: 0, scale: reducedMotion ? 1 : 0.98, transition: { duration: 0.12 } }}
          transition={popSpring}
        >
          <div className="pd-dialog-head"><strong>选择传输方式</strong><span>东京-01 → 香港-02</span></div>
          <div className="pd-options">
            {TRANSPORT_OPTIONS.map((option, index) => {
              const dialog = scene.dialog?.kind === 'transport' ? scene.dialog : null
              return <div key={option.kind} className={`pd-option is-${option.kind}${dialog?.hover === index ? ' is-hover' : ''}${dialog?.selected === index ? ' is-selected' : ''}`}>
                <span className="pd-radio" />
                <span className="pd-option-copy"><strong>{option.title}</strong><small>{option.detail}</small></span>
                <TransportGlyph kind={option.kind} active={dialog?.hover === index} />
              </div>
            })}
          </div>
          <div className="pd-dialog-foot"><span className="pd-button ghost">取消</span><span className={`pd-button primary${scene.dialog.pressed ? ' is-pressed' : ''}`}>确认</span></div>
        </m.div>}
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

function useEdgeD(edge: DemoEdge, from: DemoNode, to: DemoNode, fromMotion: NodeMotion, toMotion: NodeMotion) {
  const fromWidth = nodeWidth(from)
  const fromOffset = from.kind === 'routing' && edge.fromPort !== undefined ? ROUTING_PORT_OFFSETS[edge.fromPort] : nodeHeight(from) / 2
  const toOffset = nodeHeight(to) / 2
  return useTransform([fromMotion.x, fromMotion.y, toMotion.x, toMotion.y], ([fx, fy, tx, ty]: number[]) =>
    edgePath({ x: fx + fromWidth, y: fy + fromOffset }, { x: tx, y: ty + toOffset }))
}

function DemoEdgePath({ edge, from, to, fromMotion, toMotion, dimmed }: { edge: DemoEdge; from: DemoNode; to: DemoNode; fromMotion: NodeMotion; toMotion: NodeMotion; dimmed: boolean }) {
  const d = useEdgeD(edge, from, to, fromMotion, toMotion)
  return <g className={`pd-edge is-${edge.kind}${edge.fromPort === 1 ? ' is-fallback' : ''}${dimmed ? ' is-dimmed' : ''}`}>
    <m.path className="pd-edge-line" d={d} initial={{ pathLength: 0 }} animate={{ pathLength: 1 }} transition={{ duration: 0.55, ease: [0.22, 1, 0.36, 1] }} />
  </g>
}

function ProvisionalEdge({ node, port, motion, cursorX, cursorY }: { node: DemoNode; port?: number; motion: NodeMotion; cursorX: MotionValue<number>; cursorY: MotionValue<number> }) {
  const width = nodeWidth(node)
  const offset = node.kind === 'routing' && port !== undefined ? ROUTING_PORT_OFFSETS[port] : nodeHeight(node) / 2
  const d = useTransform([motion.x, motion.y, cursorX, cursorY], ([x, y, cx, cy]: number[]) => edgePath({ x: x + width, y: y + offset }, { x: cx, y: cy }))
  return <m.path className="pd-edge-provisional" d={d} />
}

function EdgePackets({ edge, from, to, fromMotion, toMotion }: { edge: DemoEdge; from: DemoNode; to: DemoNode; fromMotion: NodeMotion; toMotion: NodeMotion }) {
  const d = useEdgeD(edge, from, to, fromMotion, toMotion)
  const offsetPath = useTransform(d, value => `path("${value}")`)
  return <>{[0, 1, 2].map(index => <m.span
    key={index}
    className={`pd-packet is-${edge.packets}`}
    style={{ offsetPath, animationDelay: `calc(${index * -0.6}s / var(--pd-speed))` } as any}
  />)}</>
}

function DemoNodeCard({ node, motion, emphasis, activeHandle, connected, reducedMotion }: {
  node: DemoNode
  motion: NodeMotion
  emphasis: boolean | null
  activeHandle: DemoScene['activeHandle']
  connected: DemoEdge[]
  reducedMotion: boolean
}) {
  const Icon = nodeIcons[node.kind]
  const width = node.kind === 'client' ? CLIENT_WIDTH : NODE_WIDTH
  const hasOutput = node.kind === 'client' || node.kind === 'entry' || node.kind === 'server'
  const hasInput = node.kind !== 'client'
  const handleClass = (side: 'in' | 'out', port?: number) => {
    const active = activeHandle?.side === side && (port === undefined || activeHandle.port === port)
    const linked = side === 'out'
      ? connected.some(item => item.from === node.id && (port === undefined || item.fromPort === port))
      : connected.some(item => item.to === node.id)
    return `pd-handle is-${side}${active ? ' is-active' : ''}${linked ? ' is-linked' : ''}`
  }
  return <m.div className="pd-node-anchor" style={{ x: motion.x, y: motion.y, width }}>
    <m.div
      className={`pd-node is-${node.kind}${emphasis === true ? ' is-emphasis' : ''}${emphasis === false ? ' is-dimmed' : ''}`}
      style={{ height: nodeHeight(node) }}
      initial={reducedMotion ? { opacity: 0 } : { opacity: 0, scale: 0.9, y: 6 }}
      animate={{ opacity: 1, scale: 1, y: 0 }}
      transition={popSpring}
    >
      <div className="pd-node-head">
        <span className="pd-node-icon"><Icon size={15} /></span>
        <span className="pd-node-heading"><small>{node.label}</small><strong>{node.region && <RegionFlag code={node.region} size={13} />}{node.title}</strong></span>
      </div>
      {node.kind === 'routing'
        ? <div className="pd-rules">
          <div className="pd-rule" style={{ top: ROUTING_PORT_OFFSETS[0] - 13 }}><span>流媒体</span><small>规则</small><i className={handleClass('out', 0)} /></div>
          <div className="pd-rule is-fallback" style={{ top: ROUTING_PORT_OFFSETS[1] - 13 }}><span>未匹配</span><small>兜底</small><i className={handleClass('out', 1)} /></div>
        </div>
        : node.kind !== 'client' && node.detail && <div className="pd-node-detail">
          <span>{node.detail}</span>
          {node.status && <span className={`pd-node-status is-${node.status}`}>{node.status === 'synced' ? <Check size={11} /> : <span className="pd-spinner" />}{node.status === 'synced' ? '已同步' : '同步中'}</span>}
        </div>}
      {hasInput && <i className={handleClass('in')} />}
      {hasOutput && <i className={handleClass('out')} />}
    </m.div>
  </m.div>
}

function TransportGlyph({ kind, active }: { kind: typeof TRANSPORT_OPTIONS[number]['kind']; active: boolean }) {
  return <span className={`pd-glyph is-${kind}${active ? ' is-active' : ''}`} aria-hidden="true">
    <i className="pd-glyph-node" />
    <i className="pd-glyph-track"><b /></i>
    <i className="pd-glyph-node" />
  </span>
}
