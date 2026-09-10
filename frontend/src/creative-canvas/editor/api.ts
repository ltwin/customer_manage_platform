import type { components } from '../../api/schema'
import { request } from '../../api/transport.ts'
import { getAuthSnapshot } from '../../auth/session.ts'

export type Canvas = components['schemas']['CreativeCanvasSnapshot']
export type CanvasNode = components['schemas']['CreativeCanvasNode']
export type Asset = components['schemas']['CreativeLibraryAsset']
export type Project = components['schemas']['CreativeProject']
export type Command = components['schemas']['CreativeCanvasCommandPayload']
export type ContentDraft = components['schemas']['CreativeContentDraft']
export type Content = components['schemas']['CreativeContentRevision']
export type Capabilities =
  components['schemas']['CreativeFoundationCapabilities']
export type Receipt = components['schemas']['CreativeOperationReceipt']
export type AssetPage = components['schemas']['CreativeAssetPage']
export type ProjectPage = components['schemas']['CreativeProjectPage']
export type Envelope = components['schemas']['CommandCreativeCanvasRequest']

// Queue identity includes the account; never send an old account's draft after login changes.
export function currentAccount(): string | null {
  const s = getAuthSnapshot()
  return s.status === 'authenticated' ? s.account.id : null
}
export function read<T>(path: string, signal?: AbortSignal): Promise<T> {
  const before = getAuthSnapshot()
  return request<T>(`/creative${path}`, {
    signal,
    cache: 'no-store',
  }).then((value) => {
    if (getAuthSnapshot().sessionEpoch !== before.sessionEpoch)
      throw new Error('登录会话已变化，请重新打开创作台')
    return value
  })
}
export function send(
  account: string,
  path: string,
  body: string,
  operation: string,
): Promise<unknown> {
  if (currentAccount() !== account)
    return Promise.reject(new Error('账号已变化，请重新登录原账号恢复草稿'))
  const before = getAuthSnapshot()
  return request(`/creative${path}`, {
    method: 'POST',
    headers: { 'Idempotency-Key': operation },
    body,
  }).then((value) => {
    if (
      currentAccount() !== account ||
      getAuthSnapshot().sessionEpoch !== before.sessionEpoch
    )
      throw new Error('登录会话已变化；原操作仍需确认')
    return value
  })
}
