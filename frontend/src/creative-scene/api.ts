import type { components } from '../api/schema'
import { read, send } from '../creative-canvas/editor/api.ts'

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

export function commandScene(
  account: string,
  id: string,
  operationID: string,
  createdAt: string,
  payload: unknown,
): Promise<CommandReceipt> {
  const body = JSON.stringify({
    operation_id: operationID,
    client_created_at: createdAt,
    payload,
  })
  return send<CommandReceipt>(
    account,
    `${documentPath(id)}/commands`,
    body,
    operationID,
  )
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
