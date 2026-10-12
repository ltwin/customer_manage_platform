import type { Asset, GraphAction } from './api.ts'
export const assetGap = 40
export const assetWidth = 300
/** 两个入口共用同一份素材引用；尺寸仍由画布按内容原始比例生成。 */
export function assetActions(
  assets: Asset[],
  ids: string[],
  at: (index: number, asset: Asset) => { x: number; y: number },
): GraphAction[] {
  return assets.map((asset, i) => ({
    type: 'add_node',
    node_id: ids[i]!,
    type_key: `core.${asset.kind}`,
    title: asset.title,
    ...at(i, asset),
    asset: {
      asset_id: asset.id,
      expected_asset_revision: asset.revision,
      content_revision_id: asset.content_revision_id,
    },
  }))
}
