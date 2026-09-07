import type { components, paths } from '../api/schema'
import { request, ApiError } from '../api/transport'
import { authorizedFetch } from '../auth/session'

export type Workspace = components['schemas']['CreativeWorkspace']
export type Detail = components['schemas']['CreativeWorkspaceDetail']
export type Card = components['schemas']['CreativeCard']
export type ShootItem = components['schemas']['CreativeShootItem']
export type Pilot = components['schemas']['CreativePilotAccount']
export type Preflight = components['schemas']['CreativePreflightResult']
export type Command = components['schemas']['CreativeCommand']
export type Asset = components['schemas']['CreativeAsset']
const base = '/creative-workspaces'
export const pilotState = (signal?: AbortSignal) =>
  request<Pilot>('/creative-pilot', { signal })
export const preflight = () => request<Preflight>('/creative-pilot/preflight')
export const enroll = () =>
  request<Pilot>('/creative-pilot/enroll', { method: 'POST', body: '{}' })
export const stopPilot = () =>
  request<Pilot>('/creative-pilot/stop', { method: 'POST' })
export const listSpaces = (signal?: AbortSignal) =>
  request<Workspace[]>(base + '?archived=true', { signal })
export const linkedSpaces = (
  kind: 'order' | 'customer',
  id: string,
  signal?: AbortSignal,
) =>
  request<Workspace[]>(
    `${base}?link_kind=${kind}&link_id=${encodeURIComponent(id)}`,
    { signal },
  )
export const detail = (id: string, signal?: AbortSignal) =>
  request<Detail>(`${base}/${encodeURIComponent(id)}`, { signal })
export const command = (id: string, body: Command) =>
  request<Detail>(`${base}/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(body),
  })
export const createSpace = (key: string) =>
  request<Workspace>(base, {
    method: 'POST',
    headers: { 'Idempotency-Key': key },
    body: '{}',
  })
export const importCards = (
  id: string,
  body: components['schemas']['CreativeImportInput'],
  key: string,
) =>
  request<components['schemas']['CreativeImportCardsResult']>(
    `${base}/${id}/cards`,
    {
      method: 'POST',
      headers: { 'Idempotency-Key': key },
      body: JSON.stringify(body),
    },
  )
export const createShots = (
  id: string,
  cardIDs: string[],
  merge: boolean,
  key: string,
) =>
  request<ShootItem[]>(`${base}/${id}/shoot-items`, {
    method: 'POST',
    headers: { 'Idempotency-Key': key },
    body: JSON.stringify({ card_ids: cardIDs, merge }),
  })
export const createMemos = (id: string, text: string, key: string) =>
  request<components['schemas']['CreativeMemo'][]>(`${base}/${id}/memos`, {
    method: 'POST',
    headers: { 'Idempotency-Key': key },
    body: JSON.stringify({ text }),
  })
export const displayName = (space: Workspace) =>
  space.kind === 'inbox'
    ? '未归类'
    : space.name || space.link?.name || '未命名空间'
export const assetURL = (id: string, assetID: string, checksum: string) =>
  `/api/v1${base}/${encodeURIComponent(id)}/assets/${encodeURIComponent(assetID)}/content?v=${encodeURIComponent(checksum)}`
export async function upload(id: string, file: File): Promise<Asset> {
  const body = new FormData()
  body.append('file', file)
  const res = await authorizedFetch(`/api/v1${base}/${id}/assets`, {
    method: 'POST',
    body,
  })
  if (!res.ok) {
    const error = (await res.json()) as components['schemas']['ErrorEnvelope']
    throw new ApiError(res.status, error.error.code, error.error.message)
  }
  return (await res.json()) as Asset
}
export type Observation =
  paths['/creative-workspaces/{id}/observations']['post']['requestBody']['content']['application/json']
export const observe = (id: string, body: Observation) =>
  request<void>(`${base}/${id}/observations`, {
    method: 'POST',
    body: JSON.stringify(body),
  })
