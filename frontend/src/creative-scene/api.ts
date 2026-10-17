import type { components } from '../api/schema'
import { request } from '../api/transport.ts'
import { getAuthSnapshot } from '../auth/session.ts'
import { currentAccount, read } from '../creative-canvas/editor/api.ts'

export type SceneDocument = components['schemas']['CreativeSceneDocument']
export type Scene = components['schemas']['CreativeScene']
export type SceneObject = components['schemas']['CreativeSceneObject']
export type SceneTransform = components['schemas']['CreativeSceneTransform']
export type SceneEnvironment = components['schemas']['CreativeSceneEnvironment']
export type SceneRender = components['schemas']['CreativeSceneRender']
export type SceneOperation = components['schemas']['CreativeSceneOperation']
export type ObjectUpdate = components['schemas']['CreativeSceneObjectUpdate']
export type CommandReceipt = components['schemas']['CreativeDocumentCommandResult']
export type RevisionPage = components['schemas']['CreativeDocumentRevisionPage']
export type RevisionView = components['schemas']['CreativeDocumentRevisionView']

const documentPath = (id: string) => `/documents/${encodeURIComponent(id)}`

/** Reads the scene; a document of another type is reported as unreadable. */
export async function readScene(
  id: string,
  signal?: AbortSignal,
): Promise<SceneDocument> {
  const doc = await read<components['schemas']['CreativeDocument']>(
    documentPath(id),
    signal,
  )
  if (doc.type_key !== 'scene') throw new Error('这不是 3D 场景')
  return doc
}

/**
 * Sends one command. keepalive lets it outlive a closing page; browsers cap
 * such bodies near 64 KiB, so larger ones go out as ordinary requests.
 */
export function commandScene(
  account: string,
  id: string,
  operationID: string,
  createdAt: string,
  payload: unknown,
  keepalive = false,
): Promise<CommandReceipt> {
  if (currentAccount() !== account)
    return Promise.reject(new Error('账号已变化，请重新登录原账号'))
  const body = JSON.stringify({
    operation_id: operationID,
    client_created_at: createdAt,
    payload,
  })
  const before = getAuthSnapshot()
  return request<CommandReceipt>(`/creative${documentPath(id)}/commands`, {
    method: 'POST',
    headers: { 'Idempotency-Key': operationID },
    body,
    keepalive: keepalive && body.length < 60_000,
  }).then((value) => {
    if (
      currentAccount() !== account ||
      getAuthSnapshot().sessionEpoch !== before.sessionEpoch
    )
      throw new Error('登录会话已变化；修改是否保存需要重新确认')
    return value
  })
}

export function readRevisions(
  id: string,
  cursor: string | null,
  signal?: AbortSignal,
): Promise<RevisionPage> {
  const query = new URLSearchParams({ limit: '50' })
  if (cursor) query.set('cursor', cursor)
  return read<RevisionPage>(`${documentPath(id)}/revisions?${query}`, signal)
}

export function readRevision(
  id: string,
  revision: string,
  signal?: AbortSignal,
): Promise<RevisionView> {
  return read<RevisionView>(
    `${documentPath(id)}/revisions/${encodeURIComponent(revision)}`,
    signal,
  )
}
