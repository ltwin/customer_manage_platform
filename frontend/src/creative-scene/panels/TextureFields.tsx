import { useEffect, useState } from 'react'
import { MathUtils } from 'three'
import { mediaTicket } from '../../creative-canvas/editor/media.ts'
import { defaultAlphaCutoff } from '../materials/resolve.ts'
import {
  isChannelInput,
  mapInputLabel,
  mapInputs,
  withAlpha,
  withChannel,
  withMap,
  withMapping,
  type AlphaMode,
  type Channel,
  type MapInput,
  type MaterialSlot,
} from '../materials/slotEdits.ts'
import { ImagePicker } from './LibraryPicker.tsx'
import { NumberField } from './Properties.tsx'

const channels: Channel[] = ['r', 'g', 'b', 'a']
const alphaLabel: Record<AlphaMode, string> = { opaque: '不透明', clip: '透明裁切', blend: '半透明混合' }

/** An image's small picture, by its content revision alone; nothing while it comes or when it cannot. */
function Thumb({ revision, label }: { revision: string; label: string }) {
  const [url, setUrl] = useState<string | null>(null)
  useEffect(() => {
    let live = true
    setUrl(null)
    mediaTicket(revision, 'thumbnail', 'display').then(
      (t) => live && setUrl(t.url),
      () => undefined,
    )
    return () => {
      live = false
    }
  }, [revision])
  return <span className="cc-scene-map-thumb">{url && <img src={url} alt={label} />}</span>
}

/**
 * A principled slot's textures (C5c1 proposal「编辑器」): five image inputs,
 * the mapping they share, and how the base colour's alpha is used. m is the
 * slot as it draws, made explicit; each edit gives the whole slot back.
 * hasUV: whether the slot's geometry has UVs (null while unknown).
 * unusable: why an image cannot be drawn, or null.
 */
export function TextureFields({
  slot,
  m,
  hasUV,
  unusable,
  canvasID,
  disabled,
  onEdit,
}: {
  slot: string
  m: MaterialSlot
  hasUV: boolean | null
  unusable: (revision: string) => string | null
  canvasID: string | undefined
  disabled: boolean
  onEdit: (label: string, change: (m: MaterialSlot) => MaterialSlot, merge?: string) => void
}) {
  const [picking, setPicking] = useState<MapInput | null>(null)
  const maps = m.maps ?? {}
  const mapping = m.mapping
  const alpha = m.alpha_mode ?? 'opaque'
  return (
    <div className="cc-scene-textures" role="group" aria-label={`${slot} 的贴图`}>
      {mapInputs.map((input) => {
        const map = maps[input]
        const label = mapInputLabel[input]
        const why = map ? unusable(map.content_revision_id) : null
        return (
          <div key={input} className="cc-scene-map" role="group" aria-label={`${label}贴图`}>
            <span className="cc-scene-map-label">{label}</span>
            {map ? <Thumb revision={map.content_revision_id} label={label} /> : <span className="cc-scene-map-thumb is-empty">无</span>}
            <span className="cc-scene-map-tools">
              <button type="button" className="btn" disabled={disabled} onClick={() => setPicking(input)}>
                {map ? '替换' : '选择'}
              </button>
              {map && (
                <button type="button" className="btn" disabled={disabled} onClick={() => onEdit(`清除${label}贴图`, (x) => withMap(x, input, null))}>
                  清除
                </button>
              )}
              {map && isChannelInput(input) && (
                <select
                  aria-label={`${label}通道`}
                  value={(map as { channel?: Channel }).channel ?? 'r'}
                  disabled={disabled}
                  onChange={(e) => onEdit(`设置${label}通道`, (x) => withChannel(x, input, e.target.value as Channel))}
                >
                  {channels.map((c) => (
                    <option key={c} value={c}>
                      {c.toUpperCase()}
                    </option>
                  ))}
                </select>
              )}
            </span>
            {why && (
              <small className="cc-scene-map-note" role="status" title={why}>
                图片不可用
              </small>
            )}
          </div>
        )
      })}
      {mapping && (
        <div className="cc-scene-slot-params" role="group" aria-label="映射">
          <label className="cc-scene-field">
            <span>映射方式</span>
            <select value={mapping.kind ?? 'triplanar'} disabled={disabled} onChange={(e) => onEdit('设置贴图映射', (x) => withMapping(x, { kind: e.target.value as 'triplanar' | 'uv' }))}>
              <option value="triplanar">三平面投影</option>
              <option value="uv">模型 UV</option>
            </select>
          </label>
          {mapping.kind === 'uv' ? (
            <>
              {([0, 1] as const).map((k) => (
                <NumberField
                  key={k}
                  label={k === 0 ? '重复 U' : '重复 V'}
                  value={mapping.repeat?.[k] ?? 1}
                  step={0.1}
                  min={0.01}
                  max={1000}
                  disabled={disabled}
                  onCommit={(v) => onEdit('调整贴图重复', (x) => {
                    const repeat: [number, number] = [...((x.mapping?.repeat as [number, number] | undefined) ?? [1, 1])]
                    repeat[k] = v
                    return withMapping(x, { repeat })
                  }, `${slot}.mapping.repeat`)}
                />
              ))}
            </>
          ) : (
            <NumberField label="尺寸（米）" value={mapping.size_m ?? 1} step={0.1} min={0.01} max={100} disabled={disabled} onCommit={(v) => onEdit('调整贴图尺寸', (x) => withMapping(x, { size_m: v }), `${slot}.mapping.size_m`)} />
          )}
          <NumberField label="旋转（°）" value={mapping.rotation_deg ?? 0} step={1} min={0} max={360} disabled={disabled} onCommit={(v) => onEdit('调整贴图旋转', (x) => withMapping(x, { rotation_deg: v }), `${slot}.mapping.rotation_deg`)} />
          <NumberField label="法线强度" value={mapping.normal_strength ?? 1} step={0.1} min={0} max={4} disabled={disabled} onCommit={(v) => onEdit('调整法线强度', (x) => withMapping(x, { normal_strength: MathUtils.clamp(v, 0, 4) }), `${slot}.mapping.normal_strength`)} />
          {mapping.kind === 'uv' && hasUV === false && <small role="status">这个模型没有 UV，按三平面显示</small>}
        </div>
      )}
      <div className="cc-scene-slot-params" role="group" aria-label="透明">
        <label className="cc-scene-field">
          <span>透明方式</span>
          <select value={alpha} disabled={disabled} onChange={(e) => onEdit('设置透明方式', (x) => withAlpha(x, e.target.value as AlphaMode))}>
            {(['opaque', 'clip', 'blend'] as const).map((a) => (
              <option key={a} value={a}>
                {alphaLabel[a]}
              </option>
            ))}
          </select>
        </label>
        {alpha === 'clip' && (
          <NumberField label="裁切阈值" value={m.alpha_cutoff ?? defaultAlphaCutoff} step={0.05} min={0} max={1} disabled={disabled} onCommit={(v) => onEdit('调整裁切阈值', (x) => withAlpha(x, 'clip', v), `${slot}.alpha_cutoff`)} />
        )}
        {alpha === 'clip' && !maps.base_color && <small role="status">没有透明贴图，裁切只看不透明度</small>}
      </div>
      {picking && (
        <ImagePicker
          title={`选择${mapInputLabel[picking]}贴图`}
          canvasID={canvasID}
          onClose={() => setPicking(null)}
          onPick={(revision) => {
            const input = picking
            setPicking(null)
            onEdit(`设置${mapInputLabel[input]}贴图`, (x) => withMap(x, input, revision))
          }}
        />
      )}
    </div>
  )
}
