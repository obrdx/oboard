import { useEffect, useId, useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { Check, Copy } from 'lucide-react'
import { createPopoverPortal as createPortal } from '../../ui/modal-layer'
import type { Server } from '../../proxy-path/types'

export async function copyText(value: string) {
  const text = String(value || '')
  if (!text) return false
  try {
    if (navigator.clipboard?.writeText && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // Plain-HTTP panels and denied clipboard permissions fall back to a textarea.
  }
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.setAttribute('readonly', '')
  textarea.style.position = 'fixed'
  textarea.style.left = '-9999px'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  textarea.select()
  try {
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    document.body.removeChild(textarea)
  }
}

export function serverCustomEntry(server: Pick<Server, 'entry_ip_mode' | 'entry_address' | 'entry_ddns_domain'>) {
  const mode = String(server.entry_ip_mode || 'auto')
  if (mode === 'ddns') return String(server.entry_ddns_domain || '').trim()
  if (mode !== 'custom') return ''
  return String(server.entry_address || '').trim()
}

export function ipv6DisplayParts(value: string) {
  const text = String(value || '').trim()
  const last = text.lastIndexOf(':')
  const prev = last > 0 ? text.lastIndexOf(':', last - 1) : -1
  if (prev <= 0) return { head: text, tail: '' }
  return { head: text.slice(0, prev), tail: text.slice(prev) }
}

type Kind = 'v4' | 'v6' | 'entry'
type Address = { kind: Kind; label: string; value: string }

const PANEL_WIDTH = 280
const HOVER_OPEN_MS = 120
const HOVER_CLOSE_MS = 180

export function serverAddressList(server: Server): Address[] {
  const list: Address[] = []
  const v4 = String(server.public_ipv4 || '').trim()
  const v6 = String(server.public_ipv6 || '').trim()
  const entry = serverCustomEntry(server)
  if (v4) list.push({ kind: 'v4', label: '出口 IPv4', value: v4 })
  if (v6) list.push({ kind: 'v6', label: '出口 IPv6', value: v6 })
  if (entry) list.push({ kind: 'entry', label: '入口', value: entry })
  return list
}

function AddressValue({ address }: { address: Address }) {
  const parts = address.kind === 'v6' ? ipv6DisplayParts(address.value) : null
  return parts?.tail
    ? <span className="fleet-address-value is-middle"><span className="fleet-address-head">{parts.head}</span><span>{parts.tail}</span></span>
    : <span className="fleet-address-value">{address.value}</span>
}

// One fixed-height address button per server: the first address is shown and
// the full list (egress IPv4/IPv6 and a custom entry) opens in a small popover
// on hover or click, where each line copies itself.
export function ServerAddresses({ server }: { server: Server }) {
  const addresses = serverAddressList(server)
  const [open, setOpen] = useState(false)
  const [pinned, setPinned] = useState(false)
  const [copied, setCopied] = useState<Kind | null>(null)
  const [position, setPosition] = useState({ top: 0, left: 0 })
  const buttonRef = useRef<HTMLButtonElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const hoverTimer = useRef(0)
  const copyTimer = useRef(0)
  const panelID = useId()

  useEffect(() => () => {
    window.clearTimeout(hoverTimer.current)
    window.clearTimeout(copyTimer.current)
  }, [])

  const close = () => {
    window.clearTimeout(hoverTimer.current)
    setOpen(false)
    setPinned(false)
  }

  const place = () => {
    const button = buttonRef.current
    if (!button) return
    const rect = button.getBoundingClientRect()
    const width = Math.min(PANEL_WIDTH, window.innerWidth - 16)
    const height = panelRef.current?.offsetHeight || 120
    const below = window.innerHeight - rect.bottom - 8
    const top = below >= height || below >= rect.top ? rect.bottom + 4 : Math.max(8, rect.top - height - 4)
    setPosition({ top, left: Math.max(8, Math.min(window.innerWidth - width - 8, rect.left)) })
  }

  useLayoutEffect(() => {
    if (open) place()
  }, [open])

  useEffect(() => {
    if (!open) return
    const outside = (event: MouseEvent) => {
      const target = event.target as Node | null
      if (target && !buttonRef.current?.contains(target) && !panelRef.current?.contains(target)) close()
    }
    const escape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      event.stopPropagation()
      close()
      buttonRef.current?.focus()
    }
    window.addEventListener('resize', place)
    window.addEventListener('scroll', place, true)
    document.addEventListener('mousedown', outside)
    document.addEventListener('keydown', escape)
    return () => {
      window.removeEventListener('resize', place)
      window.removeEventListener('scroll', place, true)
      document.removeEventListener('mousedown', outside)
      document.removeEventListener('keydown', escape)
    }
  }, [open])

  const hoverIn = (event: ReactPointerEvent) => {
    if (event.pointerType !== 'mouse') return
    window.clearTimeout(hoverTimer.current)
    if (!open) hoverTimer.current = window.setTimeout(() => setOpen(true), HOVER_OPEN_MS)
  }
  const hoverOut = (event: ReactPointerEvent) => {
    if (event.pointerType !== 'mouse' || pinned) return
    window.clearTimeout(hoverTimer.current)
    hoverTimer.current = window.setTimeout(() => setOpen(false), HOVER_CLOSE_MS)
  }

  const copy = async (address: Address) => {
    if (!(await copyText(address.value))) return
    window.clearTimeout(copyTimer.current)
    setCopied(address.kind)
    copyTimer.current = window.setTimeout(() => setCopied(null), 1200)
  }

  if (!addresses.length) return <div className="fleet-addresses"><span className="fleet-address-empty">出口待检测</span></div>

  const [primary] = addresses
  const more = addresses.length - 1
  return (
    <div className="fleet-addresses">
      <button
        ref={buttonRef}
        type="button"
        className={`fleet-address-trigger${open ? ' is-open' : ''}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? panelID : undefined}
        aria-label={`查看并复制地址，共 ${addresses.length} 个`}
        onPointerEnter={hoverIn}
        onPointerLeave={hoverOut}
        onClick={event => {
          event.stopPropagation()
          window.clearTimeout(hoverTimer.current)
          if (open && pinned) close()
          else { setOpen(true); setPinned(true) }
        }}
      >
        <AddressValue address={primary} />
        {more > 0 ? <span className="fleet-address-more">+{more}</span> : null}
      </button>
      {open && createPortal(
        <div
          ref={panelRef}
          id={panelID}
          role="dialog"
          aria-label="服务器地址"
          className="fleet-address-panel"
          style={{ position: 'fixed', top: position.top, left: position.left, width: Math.min(PANEL_WIDTH, window.innerWidth - 16) }}
          onPointerEnter={hoverIn}
          onPointerLeave={hoverOut}
          onClick={event => event.stopPropagation()}
        >
          {addresses.map(address => (
            <button
              key={address.kind}
              type="button"
              className={`fleet-address${copied === address.kind ? ' is-copied' : ''}`}
              data-kind={address.kind}
              aria-label={`复制${address.label} ${address.value}`}
              onClick={event => { event.stopPropagation(); void copy(address) }}
            >
              <span className="fleet-address-kicker">{address.label}</span>
              <span className="fleet-address-full">{address.value}</span>
              {copied === address.kind
                ? <Check size={13} aria-hidden="true" className="fleet-address-icon" />
                : <Copy size={13} aria-hidden="true" className="fleet-address-icon" />}
            </button>
          ))}
        </div>,
        document.body,
      )}
    </div>
  )
}
