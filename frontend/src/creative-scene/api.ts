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
export type ModelGenerator = components['schemas']['CreativeModelGeneratorInput']
export type RevisionItem = components['schemas']['CreativeDocumentRevisionItem']
export type SceneJobSummary = components['schemas']['CreativeSceneJobSummary']
export type RunChanges = components['schemas']['CreativeSceneRunChanges']
export type RunChange = components['schemas']['CreativeSceneRunChange']
export type RestoreBatch = components['schemas']['CreativeSceneRestoreBatch']

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

/**
 * A model revision's generator with its code. The scene read leaves code
 * out, so the code panel and a parameter change read it here.
 */
export async function readModelGenerator(revision: string, signal?: AbortSignal): Promise<ModelGenerator> {
  const r = await read<components['schemas']['CreativeContentRevision']>(
    `/content-revisions/${encodeURIComponent(revision)}`,
    signal,
  )
  const generator = r.kind === 'model' ? (r.payload as components['schemas']['CreativeModelPayload']).model.generator : undefined
  if (!generator) throw new Error('读不到这个模型的代码')
  return generator
}

/** Newest first; `runID` keeps one run's writes, `since` those after a revision. */
export function readRevisions(
  id: string,
  cursor: string | null,
  signal?: AbortSignal,
  filter: { runID?: string; since?: string; limit?: number } = {},
): Promise<RevisionPage> {
  const query = new URLSearchParams({ limit: String(filter.limit ?? 50) })
  if (cursor) query.set('cursor', cursor)
  if (filter.runID) query.set('run_id', filter.runID)
  if (filter.since) query.set('since_revision', filter.since)
  return read<RevisionPage>(`${documentPath(id)}/revisions?${query}`, signal)
}

/** The scene's queued and running client jobs: no code, parameters or claims. */
export async function listSceneJobs(id: string, signal?: AbortSignal): Promise<SceneJobSummary[]> {
  const list = await read<components['schemas']['CreativeSceneJobList']>(`${documentPath(id)}/scene-jobs`, signal)
  return list.items
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

/** What one assistant run changed in the scene, with the commands that put each part back. */
export function readRunChanges(id: string, runID: string, signal?: AbortSignal): Promise<RunChanges> {
  return read<RunChanges>(`${documentPath(id)}/run-changes?${new URLSearchParams({ run_id: runID })}`, signal)
}

/** Places export images beside the scene node; the caller keeps one operation ID and time across retries. */
export function exportScene(
  account: string,
  id: string,
  operationID: string,
  createdAt: string,
  payload: components['schemas']['CreativeSceneExportPayload'],
): Promise<components['schemas']['CreativeSceneExportResult']> {
  if (currentAccount() !== account) return Promise.reject(new Error('账号已变化，请重新登录原账号'))
  return request(`/creative${documentPath(id)}/scene-exports`, {
    method: 'POST',
    headers: { 'Idempotency-Key': operationID },
    body: JSON.stringify({ operation_id: operationID, client_created_at: createdAt, payload }),
  })
}
