import { useMemo, useState, type ReactNode } from 'react'
import { Search, X } from 'lucide-react'
import { CustomSelect } from '../ui/CustomSelect'
import { RegionFlag, regionLabel, serverRegionCode } from '../ui/RegionFlag'
import { orderServerRegions } from '../../region-order'

type PickerServer = { id: number; name: string; status?: string; region_mode?: string; region_code?: string; detected_region_code?: string }

const isOnline = (server: PickerServer) => String(server.status || '').toLowerCase() === 'online'
const serverName = (server: PickerServer) => server.name || `server-${server.id}`
const regionName = (code: string) => code ? regionLabel(code) : '待检测'

export function ServerPickerSelect<T extends PickerServer>({
  servers,
  value,
  onChange,
  ariaLabel,
  placeholder = '选择服务器',
  required,
  disabled,
  className = '',
  emptyMessage = '没有匹配的服务器',
  meta,
}: {
  servers: T[]
  value: number
  onChange: (serverID: number) => void
  ariaLabel: string
  placeholder?: string
  required?: boolean
  disabled?: boolean
  className?: string
  emptyMessage?: ReactNode
  meta?: (server: T) => ReactNode
}) {
  const [query, setQuery] = useState('')
  const [region, setRegion] = useState('all')
  const regions = useMemo(() => orderServerRegions(servers, regionLabel), [servers])
  const regionRank = useMemo(() => new Map(regions.map((item, index) => [item.code, index])), [regions])
  const normalizedQuery = query.trim().toLocaleLowerCase('zh-CN')

  const grouped = useMemo(() => {
    const filtered = servers.filter(server => {
      const code = serverRegionCode(server)
      if (region !== 'all' && code !== region) return false
      if (!normalizedQuery) return true
      return [server.name, `server-${server.id}`, code, regionLabel(code)]
        .some(item => String(item || '').toLocaleLowerCase('zh-CN').includes(normalizedQuery))
    })
    return filtered
      .map(server => ({ server, code: serverRegionCode(server) }))
      .sort((a, b) => (regionRank.get(a.code) ?? Number.MAX_SAFE_INTEGER) - (regionRank.get(b.code) ?? Number.MAX_SAFE_INTEGER)
        || serverName(a.server).localeCompare(serverName(b.server), 'zh-CN', { numeric: true }))
  }, [servers, region, normalizedQuery, regionRank])

  const groupStats = useMemo(() => {
    const stats = new Map<string, { total: number; online: number }>()
    for (const { server, code } of grouped) {
      const stat = stats.get(code) || { total: 0, online: 0 }
      stat.total += 1
      if (isOnline(server)) stat.online += 1
      stats.set(code, stat)
    }
    return stats
  }, [grouped])

  const label = (server: T) => {
    const online = isOnline(server)
    const extra = meta?.(server)
    return <div className="entry-server-option-label">
      <span className={`entry-server-status ${online ? 'online' : 'offline'}`} aria-label={online ? '在线' : '离线'} />
      <RegionFlag code={serverRegionCode(server)} size={18} />
      <span className="entry-server-name">{serverName(server)}</span>
      {extra != null && <span className="entry-server-count">{extra}</span>}
    </div>
  }

  const options = grouped.map(({ server, code }) => {
    const stat = groupStats.get(code) || { total: 0, online: 0 }
    return {
      value: String(server.id),
      label: label(server),
      group: code || '~pending',
      groupLabel: <span className="server-picker-group">
        <span className="server-picker-group-name">{regionName(code)}</span>
        {code && <span className="server-picker-group-code">{code}</span>}
        <span className="server-picker-group-stat">{stat.online}/{stat.total} 在线</span>
      </span>,
    }
  })
  const selected = servers.find(server => server.id === value)
  const reset = () => {
    setQuery('')
    setRegion('all')
  }

  const header = <div className="server-picker-tools">
    <label className="server-picker-search">
      <Search size={16} aria-hidden="true" />
      <input
        autoFocus
        type="text"
        value={query}
        onChange={event => setQuery(event.target.value)}
        placeholder="搜索名称、地区或代码"
        aria-label={`搜索${ariaLabel}`}
        autoComplete="off"
        spellCheck={false}
      />
      {query && <button type="button" className="server-picker-clear" aria-label="清除搜索" onClick={() => setQuery('')}>
        <X size={14} aria-hidden="true" />
      </button>}
    </label>
    {regions.length > 1 && <div className="server-picker-regions" role="group" aria-label={`按地区筛选${ariaLabel}`}>
      <button type="button" className={region === 'all' ? 'selected' : ''} aria-pressed={region === 'all'} onClick={() => setRegion('all')}>
        <span>全部</span>
        <span className="server-picker-region-count">{servers.length}</span>
      </button>
      {regions.map(({ code, count }) => <button
        key={code || 'pending'}
        type="button"
        className={region === code ? 'selected' : ''}
        aria-pressed={region === code}
        onClick={() => setRegion(region === code ? 'all' : code)}
        title={`${regionName(code)}，${count} 台服务器`}
      >
        <RegionFlag code={code} size={14} />
        <span>{code || '待检测'}</span>
        <span className="server-picker-region-count">{count}</span>
      </button>)}
    </div>}
  </div>

  return <CustomSelect
    className={`server-picker-select${className ? ` ${className}` : ''}`}
    value={value ? String(value) : ''}
    onChange={next => {
      onChange(Number(next))
      reset()
    }}
    options={options}
    selectedLabel={selected ? label(selected) : undefined}
    placeholder={placeholder}
    menuHeader={header}
    menuMinWidth={340}
    emptyMessage={emptyMessage}
    ariaLabel={ariaLabel}
    required={required}
    disabled={disabled}
  />
}
