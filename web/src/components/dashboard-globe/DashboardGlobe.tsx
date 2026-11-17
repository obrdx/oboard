import React, { useEffect, useMemo, useRef, useState } from 'react'
import { regionLabel } from '../ui/RegionFlag'
import { LAND_MASK_BASE64, LAND_POINT_COUNT } from './land-mask'
import { regionAnchor } from './region-coordinates'
import './dashboard-globe.css'

export type DashboardRouteRegion = { code: string; servers: number; online: number; paths: number }
export type DashboardRouteLink = { from: string; to: string; paths: number; degraded_paths: number }
export type DashboardRouteMap = { regions?: DashboardRouteRegion[]; links?: DashboardRouteLink[] }
export type DashboardGlobeStat = { label: string; value: string; detail: string }

type Vec3 = [number, number, number]
type GlobeRegion = DashboardRouteRegion & { v: Vec3; name: string }
type GlobeLink = { pts: Vec3[]; paths: number; down: boolean; offset: number }

const DEG = Math.PI / 180
const ARC_SAMPLES = 64
const FRAME_MS = 1000 / 30
const STAT_REFRESH_MS = 5000

function vec(lat: number, lon: number): Vec3 {
  const la = lat * DEG, lo = lon * DEG
  return [Math.cos(la) * Math.sin(lo), Math.sin(la), Math.cos(la) * Math.cos(lo)]
}

function slerp(a: Vec3, b: Vec3, t: number): Vec3 {
  const d = Math.min(1, Math.max(-1, a[0] * b[0] + a[1] * b[1] + a[2] * b[2]))
  const om = Math.acos(d), so = Math.sin(om)
  if (so < 1e-6) return [a[0], a[1], a[2]]
  const k1 = Math.sin((1 - t) * om) / so, k2 = Math.sin(t * om) / so
  return [a[0] * k1 + b[0] * k2, a[1] * k1 + b[1] * k2, a[2] * k1 + b[2] * k2]
}

let landDots: Float32Array | null = null
export function decodeLandDots(): Float32Array {
  if (landDots) return landDots
  const bin = atob(LAND_MASK_BASE64)
  const out: number[] = []
  const golden = Math.PI * (3 - Math.sqrt(5))
  for (let i = 0; i < LAND_POINT_COUNT; i++) {
    if (!(bin.charCodeAt(i >> 3) & (1 << (i & 7)))) continue
    const y = 1 - (i / (LAND_POINT_COUNT - 1)) * 2, r = Math.sqrt(1 - y * y), th = golden * i
    out.push(Math.cos(th) * r, y, Math.sin(th) * r)
  }
  landDots = new Float32Array(out)
  return landDots
}

export function buildGlobeModel(map?: DashboardRouteMap) {
  const regions: GlobeRegion[] = []
  const byCode = new Map<string, GlobeRegion>()
  for (const region of map?.regions || []) {
    const anchor = regionAnchor(region.code)
    if (!anchor) continue
    const item = { ...region, v: vec(anchor[0], anchor[1]), name: regionLabel(region.code) }
    regions.push(item)
    byCode.set(region.code, item)
  }
  const links: GlobeLink[] = []
  for (const link of map?.links || []) {
    const from = byCode.get(link.from), to = byCode.get(link.to)
    if (!from || !to || link.paths <= 0) continue
    const a = from.v, b = to.v
    const ang = Math.acos(Math.min(1, a[0] * b[0] + a[1] * b[1] + a[2] * b[2]))
    const lift = 0.03 + Math.min(ang, 1.8) * 0.1
    const pts: Vec3[] = []
    for (let s = 0; s <= ARC_SAMPLES; s++) {
      const t = s / ARC_SAMPLES, v = slerp(a, b, t), h = 1 + Math.sin(Math.PI * t) * lift
      pts.push([v[0] * h, v[1] * h, v[2] * h])
    }
    // Stable per-link phase so comets do not jump when telemetry refreshes.
    let hash = 0
    for (const ch of link.from + link.to) hash = (hash * 31 + ch.charCodeAt(0)) >>> 0
    links.push({ pts, paths: link.paths, down: link.degraded_paths >= link.paths, offset: (hash % 1000) / 1000 })
  }
  return { regions, links }
}

function initialView(regions: GlobeRegion[]) {
  const sum: Vec3 = [0, 0, 0]
  for (const region of regions) { sum[0] += region.v[0]; sum[1] += region.v[1]; sum[2] += region.v[2] }
  const len = Math.hypot(sum[0], sum[1], sum[2])
  if (len < 1e-3) return { yaw: -110 * DEG, pitch: 20 * DEG }
  const lat = Math.asin(sum[1] / len), lon = Math.atan2(sum[0], sum[2])
  return { yaw: -lon, pitch: Math.max(-0.9, Math.min(0.9, lat * 0.8)) }
}

type Palette = { dot: string; line: string; flow: string; warn: string; labelBg: string; sphereA: string; sphereB: string; rim: string }
function readPalette(el: HTMLElement): Palette {
  const s = getComputedStyle(el)
  const v = (name: string) => s.getPropertyValue(name).trim()
  return { dot: v('--globe-dot'), line: v('--globe-line'), flow: v('--globe-flow'), warn: v('--globe-warn'), labelBg: v('--globe-label-bg'), sphereA: v('--globe-sphere-a'), sphereB: v('--globe-sphere-b'), rim: v('--globe-rim') }
}

// Throttles a fast-changing value so live counters settle instead of flickering
// on every telemetry push.
function useSettledValue<T>(value: T, intervalMS: number, placeholder?: (shown: T) => boolean): T {
  const [shown, setShown] = useState(value)
  const latest = useRef(value)
  const last = useRef(0)
  latest.current = value
  useEffect(() => {
    const now = Date.now()
    if (now - last.current >= intervalMS || placeholder?.(shown)) {
      last.current = now
      setShown(value)
      return
    }
    const timer = window.setTimeout(() => { last.current = Date.now(); setShown(latest.current) }, intervalMS - (now - last.current))
    return () => window.clearTimeout(timer)
  }, [value, intervalMS])
  return shown
}

export function DashboardGlobe({ routeMap, stats }: { routeMap?: DashboardRouteMap; stats: DashboardGlobeStat[] }) {
  const stageRef = useRef<HTMLDivElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const tipRef = useRef<HTMLDivElement>(null)
  const routeKey = JSON.stringify(routeMap || {})
  const model = useMemo(() => buildGlobeModel(routeMap), [routeKey])
  const modelRef = useRef(model)
  modelRef.current = model
  const statsKey = JSON.stringify(stats)
  const shownStats: DashboardGlobeStat[] = JSON.parse(useSettledValue(statsKey, STAT_REFRESH_MS, shown => shown.includes('"—"')))
  const empty = model.links.length === 0

  useEffect(() => {
    const stage = stageRef.current, canvas = canvasRef.current, tip = tipRef.current
    if (!stage || !canvas || !tip) return
    const ctx = canvas.getContext('2d')
    if (!ctx) return
    const dots = decodeLandDots()
    const reduceMotion = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false
    let W = 0, H = 0, DPR = 1
    const resize = () => {
      DPR = Math.min(window.devicePixelRatio || 1, 2)
      W = stage.clientWidth; H = stage.clientHeight
      canvas.width = Math.round(W * DPR); canvas.height = Math.round(H * DPR)
    }
    resize()
    const resizeObserver = new ResizeObserver(resize)
    resizeObserver.observe(stage)

    let palette = readPalette(stage)
    let paletteAt = 0
    let { yaw, pitch } = initialView(modelRef.current.regions)
    let centered = modelRef.current.regions.length > 0
    let vYaw = 0, dragging = false, lastX = 0, lastY = 0, idleAt = 0
    let mouse: [number, number] | null = null
    let visible = true, raf = 0, lastFrame = 0

    const onDown = (e: PointerEvent) => { dragging = true; lastX = e.clientX; lastY = e.clientY; stage.setPointerCapture(e.pointerId) }
    const onMove = (e: PointerEvent) => {
      const rect = stage.getBoundingClientRect()
      mouse = [e.clientX - rect.left, e.clientY - rect.top]
      if (!dragging) return
      const dx = e.clientX - lastX, dy = e.clientY - lastY
      lastX = e.clientX; lastY = e.clientY
      vYaw = dx * 0.005; yaw += vYaw
      pitch = Math.max(-1.1, Math.min(1.1, pitch + dy * 0.005))
      idleAt = performance.now()
    }
    const onUp = () => { dragging = false; idleAt = performance.now() }
    const onLeave = () => { mouse = null }
    stage.addEventListener('pointerdown', onDown)
    stage.addEventListener('pointermove', onMove)
    stage.addEventListener('pointerup', onUp)
    stage.addEventListener('pointercancel', onUp)
    stage.addEventListener('pointerleave', onLeave)

    const draw = (now: number) => {
      const { regions, links } = modelRef.current
      const t = reduceMotion ? 0 : now / 1000
      if (now - paletteAt > 1000) { palette = readPalette(stage); paletteAt = now }
      if (!centered && regions.length && !dragging) { ({ yaw, pitch } = initialView(regions)); centered = true }
      if (!dragging) {
        vYaw *= 0.94; yaw += vYaw
        if (!reduceMotion && now - idleAt > 2500) yaw += 0.0024
      }
      const cy = Math.cos(yaw), sy = Math.sin(yaw), cp = Math.cos(pitch), sp = Math.sin(pitch)
      const R = Math.min(W, H) * 0.4, CX = W / 2, CY = H / 2
      const proj = (x: number, y: number, z: number): Vec3 => {
        const x1 = x * cy + z * sy, z1 = -x * sy + z * cy
        const y2 = y * cp - z1 * sp, z2 = y * sp + z1 * cp
        return [CX + x1 * R, CY - y2 * R, z2]
      }
      const shown = (p: Vec3) => p[2] > 0 || (p[0] - CX) ** 2 + (p[1] - CY) ** 2 > R * R

      ctx.setTransform(DPR, 0, 0, DPR, 0, 0)
      ctx.clearRect(0, 0, W, H)
      const body = ctx.createRadialGradient(CX - R * 0.35, CY - R * 0.4, R * 0.1, CX, CY, R)
      body.addColorStop(0, palette.sphereA); body.addColorStop(1, palette.sphereB)
      ctx.fillStyle = body
      ctx.beginPath(); ctx.arc(CX, CY, R, 0, Math.PI * 2); ctx.fill()
      ctx.strokeStyle = palette.rim; ctx.lineWidth = 1; ctx.stroke()

      const ds = Math.max(0.9, R / 260)
      for (let i = 0; i < dots.length; i += 3) {
        const p = proj(dots[i], dots[i + 1], dots[i + 2])
        if (p[2] < -0.05) continue
        ctx.fillStyle = `rgba(${palette.dot},${p[2] < 0 ? 0.04 : 0.16 + p[2] * 0.58})`
        const s = ds * (0.7 + p[2] * 0.5)
        ctx.fillRect(p[0] - s / 2, p[1] - s / 2, s, s)
      }

      ctx.lineCap = 'round'
      for (const link of links) {
        const P = link.pts.map(v => proj(v[0], v[1], v[2]))
        const col = link.down ? palette.warn : palette.line
        ctx.lineWidth = 1
        ctx.setLineDash(link.down ? [3, 4] : [])
        for (let i = 0; i < P.length - 1; i++) {
          if (!shown(P[i]) || !shown(P[i + 1])) continue
          ctx.strokeStyle = `rgba(${col},${(link.down ? 0.55 : 0.26) * Math.max(0.15, (P[i][2] + 1) / 2)})`
          ctx.beginPath(); ctx.moveTo(P[i][0], P[i][1]); ctx.lineTo(P[i + 1][0], P[i + 1][1]); ctx.stroke()
        }
        ctx.setLineDash([])
        if (link.down || reduceMotion) continue
        const comets = Math.min(3, link.paths)
        ctx.lineWidth = 1.4
        for (let c = 0; c < comets; c++) {
          const head = ((t * 0.22 + link.offset + c / comets) % 1) * (P.length - 1)
          for (let k = 0; k < 22; k++) {
            const i = Math.floor(head) - k
            if (i < 0 || i + 1 >= P.length || !shown(P[i]) || !shown(P[i + 1])) continue
            ctx.strokeStyle = `rgba(${palette.flow},${(1 - k / 22) * 0.8})`
            ctx.beginPath(); ctx.moveTo(P[i][0], P[i][1]); ctx.lineTo(P[i + 1][0], P[i + 1][1]); ctx.stroke()
          }
        }
      }

      let hover: { region: GlobeRegion; p: Vec3 } | null = null
      const front: { region: GlobeRegion; p: Vec3 }[] = []
      for (const region of regions) {
        const p = proj(region.v[0], region.v[1], region.v[2])
        if (p[2] < 0) continue
        const down = region.servers > 0 && region.online === 0
        const a = 0.35 + p[2] * 0.65
        if (!down && region.online > 0 && !reduceMotion) {
          const ring = (((t * 0.6 + region.v[0] * 3) % 1) + 1) % 1
          ctx.strokeStyle = `rgba(${palette.flow},${(1 - ring) * 0.45 * p[2]})`; ctx.lineWidth = 1
          ctx.beginPath(); ctx.arc(p[0], p[1], 3 + ring * 9, 0, Math.PI * 2); ctx.stroke()
        }
        ctx.fillStyle = `rgba(${palette.labelBg},${a})`
        ctx.beginPath(); ctx.arc(p[0], p[1], 4, 0, Math.PI * 2); ctx.fill()
        ctx.fillStyle = `rgba(${down ? palette.warn : palette.line},${a})`
        ctx.beginPath(); ctx.arc(p[0], p[1], region.servers > 0 ? 2.6 : 1.6, 0, Math.PI * 2); ctx.fill()
        front.push({ region, p })
        if (mouse && Math.hypot(mouse[0] - p[0], mouse[1] - p[1]) < 10) hover = { region, p }
      }

      // Labels: hovered first, then nearest to the viewer; a label that cannot
      // find a free slot is skipped rather than drawn over another one.
      ctx.font = '500 11px ui-sans-serif, system-ui, -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif'
      ctx.textBaseline = 'middle'
      const placed: number[][] = front.map(({ p }) => [p[0] - 5, p[1] - 5, 10, 10])
      const hit = (r: number[]) => placed.some(q => r[0] < q[0] + q[2] && r[0] + r[2] > q[0] && r[1] < q[1] + q[3] && r[1] + r[3] > q[1])
      const LH = 18, PADX = 6
      const prio = (item: { region: GlobeRegion; p: Vec3 }) => (hover?.region === item.region ? 10 : 0) + item.p[2]
      for (const item of front.slice().sort((x, y) => prio(y) - prio(x))) {
        const { region, p } = item
        const important = hover?.region === region
        if (!important && p[2] < 0.3) continue
        const w = ctx.measureText(region.name).width + PADX * 2
        const slots = [[8, -LH / 2], [-8 - w, -LH / 2], [-w / 2, -LH - 7], [-w / 2, 7], [8, -LH - 2], [8, 2], [-8 - w, -LH - 2], [-8 - w, 2]]
        let rect: number[] | null = null
        for (const [dx, dy] of slots) {
          const c = [p[0] + dx, p[1] + dy, w, LH]
          if (c[0] < 4 || c[1] < 4 || c[0] + w > W - 4 || c[1] + LH > H - 4) continue
          if (!hit(c)) { rect = c; break }
        }
        if (!rect) continue
        placed.push([rect[0] - 2, rect[1] - 2, rect[2] + 4, rect[3] + 4])
        const la = important ? 1 : Math.min(1, (p[2] - 0.3) * 2.5)
        const down = region.servers > 0 && region.online === 0
        ctx.fillStyle = `rgba(${palette.labelBg},${0.82 * la})`
        ctx.strokeStyle = `rgba(${palette.dot},${0.14 * la})`; ctx.lineWidth = 1
        ctx.beginPath(); ctx.roundRect(rect[0] + 0.5, rect[1] + 0.5, rect[2] - 1, rect[3] - 1, 5); ctx.fill(); ctx.stroke()
        ctx.fillStyle = `rgba(${down ? palette.warn : palette.line},${0.9 * la})`
        ctx.fillText(region.name, rect[0] + PADX, rect[1] + LH / 2 + 0.5)
      }
      ctx.textBaseline = 'alphabetic'

      if (hover && !dragging) {
        const { region, p } = hover
        const status = region.servers === 0 ? '出口地区' : region.online === region.servers ? '全部在线' : region.online === 0 ? '全部离线' : `${region.servers - region.online} 台离线`
        const servers = region.servers > 0 ? `${region.servers} 台服务器 · ` : ''
        tip.textContent = ''
        const title = document.createElement('b'); title.textContent = region.name
        const state = document.createElement('span'); state.textContent = ` ${status}`
        const detail = document.createElement('small'); detail.textContent = `${servers}${region.paths} 条链路经过`
        tip.append(title, state, document.createElement('br'), detail)
        tip.style.transform = `translate(${Math.min(W - 180, p[0] + 14)}px, ${p[1] + 12}px)`
        tip.style.opacity = '1'
        stage.style.cursor = 'pointer'
      } else {
        tip.style.opacity = '0'
        stage.style.cursor = ''
      }
    }

    const loop = (now: number) => {
      raf = 0
      if (!visible || document.visibilityState !== 'visible') return
      if (now - lastFrame >= FRAME_MS) { lastFrame = now; draw(now) }
      raf = requestAnimationFrame(loop)
    }
    const start = () => { if (!raf && visible && document.visibilityState === 'visible') raf = requestAnimationFrame(loop) }
    const intersection = new IntersectionObserver(entries => {
      visible = entries.some(entry => entry.isIntersecting)
      start()
    })
    intersection.observe(stage)
    document.addEventListener('visibilitychange', start)
    start()

    return () => {
      if (raf) cancelAnimationFrame(raf)
      resizeObserver.disconnect()
      intersection.disconnect()
      document.removeEventListener('visibilitychange', start)
      stage.removeEventListener('pointerdown', onDown)
      stage.removeEventListener('pointermove', onMove)
      stage.removeEventListener('pointerup', onUp)
      stage.removeEventListener('pointercancel', onUp)
      stage.removeEventListener('pointerleave', onLeave)
    }
  }, [])

  const regionCount = model.regions.length
  return (
    <section className="dash-globe" aria-label="链路分布">
      <div ref={stageRef} className="dash-globe-stage">
        <canvas ref={canvasRef} role="img" aria-label={`链路地球：${regionCount} 个地区，${model.links.length} 段跨地区链路`} />
        <div className="dash-globe-head">
          <h2>链路分布</h2>
          <ul className="dash-globe-legend" aria-hidden="true">
            <li><i className="is-line" />链路</li>
            <li><i className="is-flow" />流量</li>
            <li><i className="is-down" />中断</li>
          </ul>
        </div>
        <div className="dash-globe-stats">
          {shownStats.map(stat => (
            <div key={stat.label} className="dash-globe-stat">
              <span>{stat.label}</span>
              <strong>{stat.value}</strong>
              <small>{stat.detail}</small>
            </div>
          ))}
        </div>
        {empty && <p className="dash-globe-empty">还没有跨地区的链路，配置代理链路后会显示在这里</p>}
        <div ref={tipRef} className="dash-globe-tip" role="presentation" />
      </div>
    </section>
  )
}
