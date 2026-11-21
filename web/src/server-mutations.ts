import { isIndeterminateMutationFailure } from './mutation-outcome'

type ServerRecord = { id: number; name: string }
type ServerClient = { request: (path: string, init?: RequestInit) => Promise<any> }

export class ServerMutationUncertainError extends Error {
  constructor() {
    super('提交结果未知。请核对服务器列表与操作记录，确认前不要重复提交。')
    this.name = 'ServerMutationUncertainError'
  }
  readonly unknownOutcome = true
}

export async function createServerRecord<T extends ServerRecord>(client: ServerClient, payload: { name: string }, _existing: readonly T[]): Promise<{ server: T; recovered: boolean }> {
  try {
    const result = await client.request('/servers', { method: 'POST', body: JSON.stringify(payload) })
    if (!result.server?.id) throw new Error('创建响应缺少服务器数据')
    return { ...result, server: result.server, recovered: false }
  } catch (error) {
    if (!isIndeterminateMutationFailure(error)) throw error
    // Another administrator can create the same name. Without an operation
    // receipt, a matching list entry cannot confirm this particular creation.
    throw new ServerMutationUncertainError()
  }
}

// The Controller requires the server name (or #id when unnamed) to be typed
// back before it deletes a server registration.
export function serverDeleteConfirmName(server: { id: number; name?: string }) {
  return String(server.name || '').trim() || `#${server.id}`
}

export function serverDeleteRequestInit(confirmName: string): RequestInit {
  return { method: 'DELETE', body: JSON.stringify({ confirm_name: confirmName, source: 'servers' }) }
}

export async function deleteServerRecord(client: ServerClient, serverID: number, confirmName: string): Promise<void> {
  const path = `/servers/${serverID}`
  try {
    await client.request(path, serverDeleteRequestInit(confirmName))
  } catch (error) {
    if (!isIndeterminateMutationFailure(error) && Number((error as { status?: number })?.status) !== 404) throw error
    try {
      const result = await client.request(path)
      if (result.server?.id !== serverID) throw new ServerMutationUncertainError()
    } catch (lookupError) {
      if (Number((lookupError as { status?: number })?.status) === 404) return
      throw new ServerMutationUncertainError()
    }
    throw new ServerMutationUncertainError()
  }
}
