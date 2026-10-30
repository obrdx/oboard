import React, { useEffect, useRef, useState } from 'react'
import { Select } from './ui/select'

export type SnellDensityBand = { mode: 'inside' | 'outside'; min: number; max: number }
export type SnellShapingPolicy = { padding_style?: string; max_padding?: number; first_record_density?: SnellDensityBand }

const paddingStyles = [
  { value: 'bit_biased', label: '位偏置二进制' },
  { value: 'utf8_like', label: '类 UTF-8 文本' },
  { value: 'bcd_like', label: '类 BCD 数字' },
  { value: 'motif', label: '重复图样' },
]

// Clears the band measured on a carrier bit-density DPI (drop zone 0.425-0.573) with margin.
export const snellDensityAvoidBand: SnellDensityBand = { mode: 'outside', min: 0.36, max: 0.65 }

export function normalizeSnellShaping(policy?: SnellShapingPolicy | null): SnellShapingPolicy | undefined {
  if (!policy) return undefined
  const out: SnellShapingPolicy = {}
  if (policy.padding_style) out.padding_style = policy.padding_style
  if (policy.max_padding) out.max_padding = policy.max_padding
  if (policy.first_record_density?.mode) out.first_record_density = policy.first_record_density
  return Object.keys(out).length ? out : undefined
}

export function snellShapingSummary(policy?: SnellShapingPolicy | null): string {
  const normalized = normalizeSnellShaping(policy)
  if (!normalized) return ''
  const parts: string[] = []
  const band = normalized.first_record_density
  if (band) parts.push(`首包密度${band.mode === 'outside' ? '避开' : '限定'} ${band.min}–${band.max}`)
  if (normalized.padding_style) parts.push(paddingStyles.find(style => style.value === normalized.padding_style)?.label || normalized.padding_style)
  if (normalized.max_padding) parts.push(`填充 ≤${normalized.max_padding}B`)
  return parts.join(' · ')
}

export type SnellShapingCheck = { feasible: boolean; acceptance: number; minimum_acceptance: number; message?: string; recommended?: SnellShapingPolicy; changes?: string[] }
export type SnellShapingChecker = (policy: SnellShapingPolicy) => Promise<SnellShapingCheck>

// The Controller owns the feasibility rule and the recommendation; the form only asks.
export function snellShapingChecker(request: (path: string, init?: any) => Promise<any>): SnellShapingChecker {
  return async policy => (await request('/snell-shaping/check', { method: 'POST', body: JSON.stringify({ shaping: policy }) })).check
}

// Called as a function so the caller's layout never remounts the inputs.
type Row = (props: { label: string; hint: string; children: React.ReactNode }) => React.ReactElement

function numberOrZero(value: string): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : 0
}

// SnellShapingFields edits a v6 default-mode shaping policy. The traffic
// profile is derived from the PSK, so the Controller enforces the policy by
// choosing each identity's PSK; the form only describes the constraint.
export function SnellShapingFields({ value, onChange, row, check }: { value?: SnellShapingPolicy; onChange: (policy: SnellShapingPolicy | undefined) => void; row: Row; check?: SnellShapingChecker }) {
  const policy = value || {}
  const band = policy.first_record_density
  const set = (patch: Partial<SnellShapingPolicy>) => onChange(normalizeSnellShaping({ ...policy, ...patch }))
  const setBand = (patch: Partial<SnellDensityBand>) => set({ first_record_density: { ...(band || snellDensityAvoidBand), ...patch } })
  const normalized = normalizeSnellShaping(value)
  const key = JSON.stringify(normalized || {})
  const checkRef = useRef(check)
  checkRef.current = check
  const [verdict, setVerdict] = useState<{ key: string; result: SnellShapingCheck } | null>(null)
  useEffect(() => {
    const checker = checkRef.current
    if (!checker || !normalized) return
    let cancelled = false
    const timer = setTimeout(() => {
      checker(normalized).then(result => { if (!cancelled) setVerdict({ key, result }) }).catch(() => { if (!cancelled) setVerdict(null) })
    }, 350)
    return () => { cancelled = true; clearTimeout(timer) }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])
  const current = normalized && verdict?.key === key ? verdict.result : null
  const applyAvoidBand = () => set({
    first_record_density: snellDensityAvoidBand,
    padding_style: policy.padding_style && policy.padding_style !== 'bit_biased' ? 'bit_biased' : policy.padding_style,
    max_padding: policy.max_padding && policy.max_padding < 400 ? undefined : policy.max_padding,
  })
  return <>
    {row({ label: '首包位密度', hint: '按所有请求大小计算首个记录中 1 位的期望占比。避开区间可绕过按位密度识别的 DPI；修改后不满足的身份会更换 PSK，客户端需更新订阅。', children: <>
      <div className="inline-field-action snell-density-field">
        <Select value={band?.mode || ''} onChange={event => event.target.value ? setBand({ mode: event.target.value as SnellDensityBand['mode'] }) : set({ first_record_density: undefined })} aria-label="首包位密度约束">
          <option value="">不限制</option>
          <option value="outside">避开区间</option>
          <option value="inside">限定在区间内</option>
        </Select>
        {band && <span className="snell-density-range">
          <input type="number" min={0} max={1} step={0.01} value={band.min} onChange={event => setBand({ min: numberOrZero(event.target.value) })} aria-label="密度区间下限" />
          <span aria-hidden="true">–</span>
          <input type="number" min={0} max={1} step={0.01} value={band.max} onChange={event => setBand({ max: numberOrZero(event.target.value) })} aria-label="密度区间上限" />
        </span>}
        {(!band || band.mode !== snellDensityAvoidBand.mode || band.min !== snellDensityAvoidBand.min || band.max !== snellDensityAvoidBand.max) && <button type="button" className="ghost" onClick={applyAvoidBand}>DPI 避让推荐</button>}
      </div>
    </> })}
    {row({ label: '填充风格', hint: '填充字节的外观。与密度约束组合过严时保存会被拒绝。', children: <>
      <Select value={policy.padding_style || ''} onChange={event => set({ padding_style: event.target.value || undefined })} aria-label="填充风格">
        <option value="">不限制</option>
        {paddingStyles.map(style => <option key={style.value} value={style.value}>{style.label}</option>)}
      </Select>
    </> })}
    {row({ label: '单记录填充上限', hint: '每条记录随机填充的最大字节数，用于控制带宽开销；留空不限制，可填 184–730。', children: <>
      <input type="number" min={184} max={730} step={1} value={policy.max_padding || ''} onChange={event => set({ max_padding: event.target.value ? numberOrZero(event.target.value) : undefined })} placeholder="不限制" aria-label="单记录填充上限" />
    </> })}
    {current && !current.feasible && <div className="access-note warning snell-shaping-recommendation" role="status">
      <strong>这组设置挑不出可用的 PSK</strong>
      <span>{current.changes?.length ? '推荐这样修改：' : current.message}</span>
      {current.changes?.length ? <ul>{current.changes.map(change => <li key={change}>{change}</li>)}</ul> : null}
      {current.recommended && <button type="button" onClick={() => onChange(normalizeSnellShaping(current.recommended))}>应用推荐</button>}
    </div>}
    {current?.feasible && <p className="field-hint" role="status">可用：约 {(current.acceptance * 100).toFixed(2)}% 的随机 PSK 满足，主控会为每个身份自动挑选。</p>}
  </>
}
