import { ApiError } from '../../api/transport.ts'
import * as api from '../editor/api.ts'
import type { components } from '../../api/schema'
import {
  placementBatch,
  PlacementError,
  validatePlacementAssets,
} from './placement.ts'

/** 重试复用原操作身份和原请求体，不能用新身份重发结果未知的写入。 */
export function operation<T>(account: string, path: string, payload: unknown) {
  const id = crypto.randomUUID()
  const body = JSON.stringify({
    operation_id: id,
    client_created_at: new Date().toISOString(),
    payload,
  })
  return () => api.send<T>(account, path, body, id)
}
export const rejected = (e: unknown) =>
  e instanceof PlacementError ||
  (e instanceof ApiError &&
    e.status >= 400 &&
    e.status < 500 &&
    e.status !== 408 &&
    e.status !== 429)
export const conflict = (e: unknown) =>
  e instanceof ApiError && e.code === 'creative_revision_conflict'

/** 一个放入流程拥有一个创建请求和一个批量请求；重试始终接着同一项目完成。 */
export function projectPlacement(
  account: string,
  assets: api.Asset[],
  target: { canvas: string } | { name: string },
) {
  let canvasID = 'canvas' in target ? target.canvas : ''
  const create =
    'name' in target
      ? operation<components['schemas']['CreativeProjectCreateResult']>(
          account,
          '/projects',
          { name: target.name },
        )
      : null
  const ids = assets.map(() => `cwnode_${crypto.randomUUID()}`)
  const group = `cwgroup_${crypto.randomUUID()}`
  let send: (() => Promise<api.ChangeResult>) | null = null
  let replanned = false
  let completed = false
  return async () => {
    validatePlacementAssets(assets)
    if (!canvasID && create) canvasID = (await create()).default_canvas_id
    if (completed) return canvasID
    const prepare = async () => {
      const canvas = await api.read<api.Canvas>(
        `/canvases/${encodeURIComponent(canvasID)}`,
      )
      send = operation<api.ChangeResult>(
        account,
        `/canvases/${encodeURIComponent(canvasID)}/commands`,
        placementBatch(
          assets,
          canvas.nodes,
          canvas.topology_revision,
          ids,
          group,
        ),
      )
    }
    if (!send) await prepare()
    try {
      await send!()
    } catch (e) {
      // 只有结构版本冲突才重排；响应丢失绝不换操作身份。
      if (
        e instanceof ApiError &&
        e.code === 'creative_revision_conflict' &&
        !replanned
      ) {
        replanned = true
        await prepare()
        try {
          await send!()
        } catch (retryError) {
          if (rejected(retryError)) {
            send = null
            replanned = false
          }
          throw retryError
        }
      } else {
        if (rejected(e)) {
          send = null
          replanned = false
        }
        throw e
      }
    }
    completed = true
    return canvasID
  }
}
