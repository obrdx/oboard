import * as React from 'react'
import { Check, Eye, Pencil, Plus, RefreshCw, Trash2, X } from 'lucide-react'
import { useDialogs } from '../ui/dialog-context'
import { Dialog } from '../ui/dialog'
import { errorText, groupKindLabels, loadSavedNodeLink, sourceStatusLabels, type Client, type ImportPreview, type LibraryNode, type NodeGroup, type Notify, type Workspace } from './types'

const groupKindHints: Record<NodeGroup['kind'], string> = {
  oboard: '管理员授权给你的 OBoard 节点，随套餐自动更新。',
  remote: '定期从 HTTPS 订阅地址同步节点。',
  manual: '手动粘贴的节点链接或配置。',
}

export function NodeGroupsPanel({ workspace, nodes, busy, mutate, subjectQuery, client, refresh, message }: {
  workspace: Workspace
  nodes: LibraryNode[]
  busy: boolean
  mutate: (path: string, init: RequestInit, success: string) => Promise<boolean>
  subjectQuery: string
  client: Client
  refresh: () => Promise<void>
  message: Notify
}) {
  const dialogs = useDialogs()
  const [createOpen, setCreateOpen] = React.useState(false)
  const [previewBusy, setPreviewBusy] = React.useState(false)
  const [formError, setFormError] = React.useState('')
  const [kind, setKind] = React.useState<'manual' | 'remote'>('remote')
  const [name, setName] = React.useState('')
  const [value, setValue] = React.useState('')
  const [importPreview, setImportPreview] = React.useState<ImportPreview | null>(null)
  const [editingID, setEditingID] = React.useState(0)
  const [editName, setEditName] = React.useState('')
  const [editURL, setEditURL] = React.useState('')
  const [editContent, setEditContent] = React.useState('')
  const [refreshingID, setRefreshingID] = React.useState(0)
  const [nodeTargetID, setNodeTargetID] = React.useState('')
  const [nodeMode, setNodeMode] = React.useState<'edit' | 'view' | null>(null)
  const [nodeName, setNodeName] = React.useState('')
  const [nodeContent, setNodeContent] = React.useState('')
  const [loadedNodeContent, setLoadedNodeContent] = React.useState('')
  const [nodeError, setNodeError] = React.useState('')
  const [nodeLoading, setNodeLoading] = React.useState(false)
  const [nodeBusy, setNodeBusy] = React.useState(false)
  const nodeTicket = React.useRef(0)
  const nodeContentTouched = React.useRef(false)
  const latestDraft = React.useRef({ kind, name, value, editingID, editName, editURL, editContent })
  latestDraft.current = { kind, name, value, editingID, editName, editURL, editContent }
  const previewSequence = React.useRef(0)
  const editingGroup = workspace.node_groups.find(group => group.id === editingID) || null
  const editingSource = editingGroup ? workspace.node_sources.find(item => item.group_id === editingGroup.id) : undefined

  const switchKind = (next: 'manual' | 'remote') => { setKind(next); setImportPreview(null); setFormError('') }
  const create = async () => {
    if (!name.trim() || !value.trim()) return
    if (await mutate('/node-groups', { method: 'POST', body: JSON.stringify({ name, kind, ...(kind === 'remote' ? { url: value } : { content: value }) }) }, '节点组已创建')) {
      const current = latestDraft.current
      if (current.name === name && current.kind === kind && current.value === value) { setName(''); setValue(''); setImportPreview(null); setCreateOpen(false) }
    } else setFormError('创建失败，请检查输入后重试')
  }
  const previewImport = async () => {
    const sequence = ++previewSequence.current
    setPreviewBusy(true)
    setFormError('')
    try {
      const result = await client.request<ImportPreview>(`/node-import-preview${subjectQuery}`, { method: 'POST', body: JSON.stringify(kind === 'remote' ? { url: value } : { content: value }) })
      if (sequence === previewSequence.current && latestDraft.current.kind === kind && latestDraft.current.value === value) setImportPreview(result)
    } catch (error) {
      if (sequence !== previewSequence.current || latestDraft.current.kind !== kind || latestDraft.current.value !== value) return
      setImportPreview(null)
      setFormError(errorText(error))
    } finally { if (sequence === previewSequence.current) setPreviewBusy(false) }
  }
  const resetNodeEditor = () => {
    nodeTicket.current++
    nodeContentTouched.current = false
    setNodeTargetID('')
    setNodeMode(null)
    setNodeName('')
    setNodeContent('')
    setLoadedNodeContent('')
    setNodeError('')
    setNodeLoading(false)
  }
  const openEdit = (group: NodeGroup) => { resetNodeEditor(); setFormError(''); setEditingID(group.id); setEditName(group.name); setEditURL(''); setEditContent('') }
  const closeEdit = () => { resetNodeEditor(); setEditingID(0); setEditName(''); setEditURL(''); setEditContent('') }
  const openNode = (node: LibraryNode, mode: 'edit' | 'view') => {
    const ticket = ++nodeTicket.current
    nodeContentTouched.current = false
    setNodeTargetID(node.id)
    setNodeMode(mode)
    setNodeName(node.name)
    setNodeContent('')
    setLoadedNodeContent('')
    setNodeError('')
    if (!node.copyable) { setNodeLoading(false); return }
    setNodeLoading(true)
    void loadSavedNodeLink(client, subjectQuery, node.id).then(url => {
      if (nodeTicket.current !== ticket || nodeContentTouched.current) return
      setLoadedNodeContent(url)
      setNodeContent(url)
    }).catch(error => {
      if (nodeTicket.current === ticket) setNodeError(errorText(error))
    }).finally(() => {
      if (nodeTicket.current === ticket) setNodeLoading(false)
    })
  }
  const saveNode = async (node: LibraryNode) => {
    const nextName = nodeName.trim()
    const content = nodeContent.trim()
    const contentChanged = content !== loadedNodeContent.trim()
    if (nodeBusy || (!nextName && !contentChanged)) return
    setNodeBusy(true)
    setNodeError('')
    try {
      await client.request(`/node-library/${node.id}${subjectQuery}`, { method: 'PATCH', body: JSON.stringify({ name: nextName, content: contentChanged ? content : '' }) })
      message('节点已更新')
      resetNodeEditor()
      await refresh()
    } catch (error) {
      setNodeError(errorText(error))
    } finally {
      setNodeBusy(false)
    }
  }
  const saveEdit = async (group: NodeGroup) => {
    const nextName = editName.trim()
    const url = editURL.trim()
    const content = editContent.trim()
    if (!nextName) return
    const body: Record<string, unknown> = { name: nextName }
    if (group.kind === 'remote' && url) body.url = url
    if (group.kind === 'manual' && content) body.content = content
    if (await mutate(`/node-groups/${group.id}`, { method: 'PATCH', body: JSON.stringify(body) }, '节点组已更新')) {
      const current = latestDraft.current
      if (current.editingID === group.id && current.editName === editName && current.editURL === editURL && current.editContent === editContent) closeEdit()
    } else setFormError('保存失败，请检查输入后重试')
  }
  const deleteGroup = async (group: NodeGroup) => {
    if (!await dialogs.confirm({ title: `删除节点组“${group.name}”？`, message: '该节点组及其中节点都会被删除，引用它的组合订阅将不再输出这些节点。', confirmText: '删除', tone: 'danger' })) return
    await mutate(`/node-groups/${group.id}`, { method: 'DELETE' }, '节点组已删除')
  }
  const refreshSource = async (group: NodeGroup, sourceID: number) => {
    setRefreshingID(group.id)
    try {
      await client.request(`/node-sources/${sourceID}/refresh${subjectQuery}`, { method: 'POST', body: '{}' })
      message('来源已刷新')
      await refresh()
    } catch (error) { message(errorText(error), 'error') } finally { setRefreshingID(0) }
  }
  const editUnchanged = editingGroup ? !editURL.trim() && !editContent.trim() && editName.trim() === editingGroup.name : true

  return <section className="nw-section" aria-label="节点组">
    <div className="nw-toolbar">
      <p className="nw-toolbar-note">节点组决定节点从哪里来。组合订阅按顺序挑选节点组输出。</p>
      <span className="nw-toolbar-meta">{workspace.node_groups.length} 个节点组</span>
      <button type="button" onClick={() => { setFormError(''); setImportPreview(null); setCreateOpen(true) }}><Plus size={16} />添加节点组</button>
    </div>

    <div className="nw-group-grid">{workspace.node_groups.map(group => {
      const source = workspace.node_sources.find(item => item.group_id === group.id)
      return <article className="nw-group-card" key={group.id}>
        <header>
          <div className="nw-group-title"><strong>{group.name}</strong><span className={`nw-kind nw-kind-${group.kind}`}>{groupKindLabels[group.kind]}</span></div>
          <div className="nw-row-actions">
            {source && <button type="button" className="ghost icon-button" title="立即同步" aria-label={`刷新 ${group.name}`} disabled={busy || refreshingID === group.id} onClick={() => void refreshSource(group, source.id)}><RefreshCw size={15} className={refreshingID === group.id ? 'spin' : undefined} /></button>}
            <button type="button" className="ghost icon-button" title="编辑节点组" aria-label={`编辑 ${group.name}`} disabled={busy} onClick={() => openEdit(group)}><Pencil size={15} /></button>
            {group.kind !== 'oboard' && <button type="button" className="ghost icon-button danger-text" title="删除节点组" aria-label={`删除 ${group.name}`} disabled={busy} onClick={() => void deleteGroup(group)}><Trash2 size={15} /></button>}
          </div>
        </header>
        <div className="nw-group-stat"><b>{group.node_count}</b><span>个节点</span></div>
        {source ? <div className="nw-group-source">
          <code title={source.url_display}>{source.url_display}</code>
          <span className={`nw-status nw-status-${source.status}`}>{sourceStatusLabels[source.status] || '等待同步'}{source.last_success_at ? ` · ${new Date(source.last_success_at).toLocaleString()}` : ''}</span>
          {source.last_error && <p className="nw-group-error">{source.last_error}</p>}
        </div> : <p className="nw-group-hint">{groupKindHints[group.kind]}</p>}
      </article>
    })}</div>

    <Dialog isOpen={createOpen} onClose={() => { if (!busy && !previewBusy) setCreateOpen(false) }} title="添加节点组" size="lg" className="nw-dialog">
      <div className="nw-form nw-group-create">
        <div className="nw-segmented" role="group" aria-label="来源类型">
          <button type="button" className={kind === 'remote' ? 'active' : ''} aria-pressed={kind === 'remote'} onClick={() => switchKind('remote')}>远程订阅</button>
          <button type="button" className={kind === 'manual' ? 'active' : ''} aria-pressed={kind === 'manual'} onClick={() => switchKind('manual')}>手动节点</button>
        </div>
        <p className="nw-form-hint">{groupKindHints[kind]}</p>
        <label><span>节点组名称</span><input value={name} onChange={event => setName(event.target.value)} maxLength={80} /></label>
        <label><span>{kind === 'remote' ? 'HTTPS 订阅 URL' : '节点链接或配置'}</span>{kind === 'remote' ? <input type="url" value={value} onChange={event => setValue(event.target.value)} placeholder="https://" /> : <textarea value={value} onChange={event => setValue(event.target.value)} rows={4} placeholder="每行一个节点链接，或粘贴完整配置" />}</label>
        {formError && <p className="nw-form-error" role="alert">{formError}</p>}
        {importPreview && <div className="nw-import-preview" aria-live="polite">
          <strong>可导入 {importPreview.nodes.length} 个节点</strong>
          <div>{importPreview.nodes.slice(0, 8).map(node => <span key={node.fingerprint}>{node.name}<small>{node.protocol}</small></span>)}</div>
          {importPreview.nodes.length > 8 && <small>另有 {importPreview.nodes.length - 8} 个节点</small>}
          {importPreview.issues.length > 0 && <details><summary>{importPreview.issues.length} 项无法导入</summary>{importPreview.issues.map(issue => <span key={`${issue.index}-${issue.message}`}>第 {issue.index} 项：{issue.message}</span>)}</details>}
        </div>}
        <div className="nw-form-actions">
          <button type="button" className="ghost" onClick={() => void previewImport()} disabled={busy || previewBusy || !value.trim()}><Eye size={15} />{previewBusy ? '解析中…' : '预览'}</button>
          <button type="button" onClick={() => void create()} disabled={busy || !name.trim() || !value.trim()}><Plus size={15} />添加</button>
        </div>
      </div>
    </Dialog>

    <Dialog isOpen={editingGroup !== null} onClose={() => { if (!busy && !nodeBusy) closeEdit() }} title={`编辑 ${editingGroup?.name || '节点组'}`} size="lg" className="nw-dialog">
      {editingGroup && <form className="nw-form nw-group-edit" onSubmit={event => { event.preventDefault(); void saveEdit(editingGroup) }}>
        <label><span>节点组名称</span><input value={editName} onChange={event => setEditName(event.target.value)} maxLength={80} /></label>
        <SavedNodes
          kind={editingGroup.kind}
          nodes={nodes.filter(node => node.group_id === editingGroup.id)}
          activeID={nodeTargetID}
          mode={nodeMode}
          nodeName={nodeName}
          nodeContent={nodeContent}
          loadedContent={loadedNodeContent}
          loading={nodeLoading}
          busy={busy || nodeBusy}
          error={nodeError}
          onName={setNodeName}
          onContent={value => { nodeContentTouched.current = true; setNodeContent(value) }}
          onOpen={openNode}
          onClose={resetNodeEditor}
          onSave={node => void saveNode(node)}
        />
        {editingGroup.kind === 'remote' && <label><span>HTTPS 订阅 URL{editingSource && <small>当前 {editingSource.url_display}</small>}</span><input type="url" value={editURL} placeholder={editingSource?.url_display || 'https://'} onChange={event => setEditURL(event.target.value)} /></label>}
        {editingGroup.kind === 'manual' && <label><span>新增节点链接</span><textarea value={editContent} rows={4} placeholder="粘贴新的节点链接或配置。已保存的节点不会被替换" onChange={event => setEditContent(event.target.value)} /></label>}
        {formError && <p className="nw-form-error" role="alert">{formError}</p>}
        <div className="nw-form-actions"><button type="button" className="ghost" disabled={busy || nodeBusy} onClick={closeEdit}><X size={15} />取消</button><button type="submit" disabled={busy || nodeBusy || !editName.trim() || editUnchanged}><Check size={15} />保存</button></div>
      </form>}
    </Dialog>
  </section>
}

function SavedNodes({ kind, nodes, activeID, mode, nodeName, nodeContent, loadedContent, loading, busy, error, onName, onContent, onOpen, onClose, onSave }: {
  kind: NodeGroup['kind']
  nodes: LibraryNode[]
  activeID: string
  mode: 'edit' | 'view' | null
  nodeName: string
  nodeContent: string
  loadedContent: string
  loading: boolean
  busy: boolean
  error: string
  onName: (value: string) => void
  onContent: (value: string) => void
  onOpen: (node: LibraryNode, mode: 'edit' | 'view') => void
  onClose: () => void
  onSave: (node: LibraryNode) => void
}) {
  const active = nodes.find(node => node.id === activeID) || null
  const hint = kind === 'manual' ? '选择节点即可查看并修改已保存的链接。' : kind === 'remote' ? '这些节点由订阅同步，下次同步会按订阅内容更新，不能单独修改。' : '这些节点随套餐授权更新，不能在这里修改。'
  const unchanged = Boolean(active) && nodeName.trim() === active?.name && nodeContent.trim() === loadedContent.trim()
  return <section className="nw-saved-nodes" aria-label="已保存节点">
    <div className="nw-saved-nodes-head"><span>已保存节点</span><small>{nodes.length} 个</small></div>
    <p className="nw-form-hint">{hint}</p>
    {nodes.length === 0
      ? <p className="nw-form-hint">这个节点组里还没有节点。</p>
      : <ul className="nw-saved-node-list">{nodes.map(node => <li key={node.id} className={node.id === activeID ? 'is-editing' : undefined}>
        <strong>{node.name}</strong>
        <span className="nw-mono-chip">{node.protocol}</span>
        {node.editable
          ? <button type="button" className="ghost" aria-label={`修改 ${node.name}`} disabled={busy} onClick={() => onOpen(node, 'edit')}>修改</button>
          : node.copyable
            ? <button type="button" className="ghost" aria-label={`查看 ${node.name}`} disabled={busy} onClick={() => onOpen(node, 'view')}>查看</button>
            : null}
      </li>)}</ul>}
    {active && mode && <div className="nw-saved-node-edit" aria-label={mode === 'edit' ? `修改 ${active.name}` : `查看 ${active.name}`} onKeyDown={event => { if (event.key === 'Enter' && (event.target as HTMLElement).tagName === 'INPUT') event.preventDefault() }}>
      {mode === 'edit' && <label><span>节点名称</span><input value={nodeName} onChange={event => onName(event.target.value)} maxLength={80} /></label>}
      <label><span>节点链接{loading && <small>正在读取已保存的链接</small>}</span><textarea value={nodeContent} readOnly={mode === 'view' || loading} rows={4} aria-label={mode === 'edit' ? '节点链接' : '已保存的节点链接'} placeholder={loading ? '正在读取已保存的链接…' : mode === 'view' ? '无法读取已保存的链接' : active.copyable ? '修改当前保存的链接' : '当前配置无法显示为链接，粘贴新链接可以替换它'} onChange={event => onContent(event.target.value)} /></label>
      {mode === 'edit' && !loading && loadedContent && <p className="nw-form-hint">这是当前保存的链接，改完后只更新这个节点。</p>}
      {error && <p className="nw-form-error" role="alert">{error}</p>}
      <div className="nw-form-actions">
        <button type="button" className="ghost" onClick={onClose}>{mode === 'edit' ? '取消修改' : '收起'}</button>
        {mode === 'edit' && <button type="button" disabled={busy || loading || unchanged || (!nodeName.trim() && !nodeContent.trim())} onClick={() => onSave(active)}><Check size={15} />保存这个节点</button>}
      </div>
    </div>}
  </section>
}
