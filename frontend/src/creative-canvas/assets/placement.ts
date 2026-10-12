import type { components } from '../../api/schema'
import type { Asset, CanvasNode } from '../editor/api.ts'
import { assetActions, assetGap, assetWidth } from '../editor/assetPlacement.ts'

export class PlacementError extends Error {}
export function validatePlacementAssets(assets: Asset[]) {
  if (
    !assets.length ||
    assets.length > 100 ||
    assets.some((a) => a.unavailable || a.deleted_at)
  )
    throw new PlacementError('请选择 1–100 项可用素材')
}

type Positioned = Pick<CanvasNode, 'id' | 'parent_id'> & {
  metadata: Pick<CanvasNode['metadata'], 'x' | 'y' | 'width' | 'height'>
}
/** 子节点坐标相对父组；按世界坐标计算整体右边界。 */
export function placement(nodes: Positioned[]) {
  if (!nodes.length) return { x: 0, y: 0 }
  const byID = new Map(nodes.map((n) => [n.id, n]))
  const boxes = nodes.map((n) => {
    let x = n.metadata.x,
      y = n.metadata.y
    const seen = new Set([n.id])
    for (let p = n.parent_id; p && !seen.has(p);) {
      seen.add(p)
      const parent = byID.get(p)
      if (!parent) break
      x += parent.metadata.x
      y += parent.metadata.y
      p = parent.parent_id
    }
    return { right: x + n.metadata.width, top: y }
  })
  return {
    x: Math.max(...boxes.map((b) => b.right)) + assetGap,
    y: Math.min(...boxes.map((b) => b.top)),
  }
}
export function placementBatch(
  assets: Asset[],
  nodes: Positioned[],
  revision: string,
  ids: string[],
  group: string,
): components['schemas']['CreativeBatchPayload'] {
  validatePlacementAssets(assets)
  const start = placement(nodes)
  return {
    type: 'batch',
    expected_topology_revision: revision,
    change_group_id: group,
    read_set: [],
    actions: assetActions(assets, ids, (i) => ({
      x: start.x + i * (assetWidth + assetGap),
      y: start.y,
    })),
  }
}
