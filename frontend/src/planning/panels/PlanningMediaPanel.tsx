import { useCallback, useEffect, useRef, useState } from 'react'
import type { ChangeEvent } from 'react'
import type { components } from '../../api/schema'
import {
  createPlanAssetBinding,
  fetchPlanAssetDisplay,
  listPlanAssets,
  newPlanningMutationKey,
  uploadPlanAsset,
  type PlanAsset,
} from '../api'
import { planningErrorMessage } from '../presentation'
import {
  generationGrantSelectable,
  matrixRejectionNote,
  mediaPurposeOptions,
  mediaSourceMeta,
  mediaSourceOptions,
  purposeAllowed,
  rightsDeclarationFor,
  type MediaPurpose,
} from '../mediaRights'
import { removedShotLabel, shotPositionLabel } from '../share/shotReference'

type Source = components['schemas']['PlanningMediaSourceClass']

export type MediaShotOption = { id: string; position: number; title: string }

export default function PlanningMediaPanel({ planID, planRevision, readOnly, shots }: { planID: string; planRevision: number; readOnly: boolean; shots: MediaShotOption[] }) {
  const [assets, setAssets] = useState<PlanAsset[]>([])
  const [source, setSource] = useState<Source>('photographer_owned')
  const [purpose, setPurpose] = useState<MediaPurpose>('moodboard_display')
  const [generationGrant, setGenerationGrant] = useState(false)
  const [shotTargets, setShotTargets] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const [notice, setNotice] = useState('')
  const requestVersion = useRef(0)
  const fileRef = useRef<HTMLInputElement>(null)
  const load = useCallback(async () => {
    const version = ++requestVersion.current
    setLoading(true)
    try {
      const page = await listPlanAssets(planID)
      if (version === requestVersion.current) setAssets(page.items)
    } finally {
      if (version === requestVersion.current) setLoading(false)
    }
  }, [planID])
  useEffect(() => { void load().catch((error) => setNotice(planningErrorMessage(error, '参考素材加载失败'))) }, [load])

  function changeSource(next: Source) {
    setSource(next)
    if (!purposeAllowed(next, generationGrant, purpose)) setPurpose('moodboard_display')
  }

  function changeGenerationGrant(next: boolean) {
    setGenerationGrant(next)
    if (!purposeAllowed(source, next, purpose)) setPurpose('moodboard_display')
  }

  async function selectFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    if (!file) return
    setBusy(true); setNotice('')
    ++requestVersion.current
    try {
      const result = await uploadPlanAsset(planID, planRevision, file, rightsDeclarationFor(source, generationGrant), purpose, newPlanningMutationKey('asset-upload'))
      setAssets((current) => [result.asset, ...current.filter((item) => item.id !== result.asset.id)])
      setNotice('素材已安全保存，可继续挂到整案或镜头。')
    } catch (error) { setNotice(planningErrorMessage(error, '素材上传失败')) } finally { setBusy(false); event.target.value = '' }
  }

  async function bind(asset: PlanAsset, holderKind: 'plan' | 'shot', holderID: string, bindPurpose: 'moodboard_display' | 'shot_reference_display') {
    setBusy(true); setNotice('')
    try {
      const result = await createPlanAssetBinding(planID, asset.id, {
        generation: asset.current_generation, holder_kind: holderKind, holder_id: holderID,
        purpose: bindPurpose, expected_plan_revision: planRevision, expected_asset_revision: asset.revision,
      }, newPlanningMutationKey(holderKind === 'plan' ? 'asset-bind' : 'asset-bind-shot'))
      setAssets((current) => current.map((item) => item.id === asset.id ? result.asset : item))
      setNotice(holderKind === 'plan' ? '已挂到整案情绪板。' : '已挂到镜头（镜头参考）。')
    } catch (error) { setNotice(planningErrorMessage(error, '素材挂载失败')) } finally { setBusy(false) }
  }

  const generationSelectable = purposeAllowed(source, generationGrant, 'generation_reference')

  return <section className="planning-media-panel" aria-labelledby="planning-media-title">
    <div className="panel-heading"><div><h2 id="planning-media-title">参考素材</h2><p className="muted">上传时会声明来源与用途；未挂载素材保留 48 小时，方便重试。</p></div>
      {!readOnly && <button className="btn btn-primary" type="button" disabled={busy || loading} onClick={() => fileRef.current?.click()}>上传参考图</button>}
    </div>
    {!readOnly && (
      <div className="planning-media-controls">
        <label>素材来源<select value={source} onChange={(event) => changeSource(event.target.value as Source)}>
          {mediaSourceOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
        </select></label>
        <label>用途<select value={purpose} onChange={(event) => setPurpose(event.target.value as MediaPurpose)}>
          {mediaPurposeOptions.map((option) => (
            <option key={option.value} value={option.value} disabled={option.value === 'generation_reference' && !generationSelectable}>
              {option.label}{option.value === 'generation_reference' && !generationSelectable ? '（本来源禁止）' : ''}
            </option>
          ))}
        </select></label>
        {generationGrantSelectable(source) && (
          <label className="planning-media-grant"><input type="checkbox" checked={generationGrant} onChange={(event) => changeGenerationGrant(event.target.checked)} />已获得生成参考授权（许可在档）</label>
        )}
        <p className="planning-media-matrix-note">{matrixRejectionNote}</p>
        <input ref={fileRef} className="visually-hidden" type="file" accept="image/jpeg,image/png,image/webp" onChange={(event) => void selectFile(event)} />
      </div>
    )}
    <details className="planning-media-matrix">
      <summary>来源 × 权利 × 用途对照</summary>
      <div className="planning-media-matrix-rows">
        <p><span>来源</span><span>权利依据</span><span>可选用途</span></p>
        {mediaSourceOptions.map((option) => (
          <p key={option.value}>
            <span>{option.label}</span>
            <span>{option.basisLabel}</span>
            <span>{purposeAllowed(option.value, true, 'generation_reference') ? '情绪板 · 镜头参考 · 生成参考' : '情绪板 · 镜头参考'}</span>
          </p>
        ))}
      </div>
    </details>
    {notice && <div className="planning-feedback" role="status">{notice}</div>}
    {loading ? <div className="planning-empty"><strong>正在加载参考素材</strong><span>素材列表加载完成后即可继续操作。</span></div> : assets.length === 0 ? <div className="planning-empty"><strong>还没有参考素材</strong><span>可先上传场地、动作、构图或灯光参考。</span></div> : <div className="planning-media-grid">{assets.map((asset) => {
      const bindable = !readOnly && (asset.state === 'staged' || asset.state === 'active')
      const shotTarget = shotTargets[asset.id] ?? ''
      return <article className={`planning-media-card state-${asset.state}`} key={asset.id}>
        <AssetImage planID={planID} asset={asset} />
        <h3>{asset.display_name || '未命名素材'}</h3>
        <p>{stateLabel(asset.state)} · 第 {asset.current_generation} 代</p>
        {asset.rights && (
          <p className="planning-media-tags">
            <span className="planning-tag">{mediaSourceMeta(asset.rights.source_class).label}</span>
            <span className="planning-tag">{mediaSourceMeta(asset.rights.source_class).basisLabel}</span>
            {asset.rights.license_generation_reference_granted && <span className="planning-tag">生成授权</span>}
          </p>
        )}
        {asset.active_bindings && asset.active_bindings.length > 0 && (
          <p className="planning-media-bindings">已挂：{asset.active_bindings.map((binding) => binding.holder_kind === 'plan'
            ? '整案情绪板'
            : bindingLabel(shots, binding.holder_id)).join('、')}</p>
        )}
        {bindable && (
          <div className="planning-media-bind-row">
            <button className="btn" type="button" disabled={busy} onClick={() => void bind(asset, 'plan', planID, 'moodboard_display')}>挂到整案</button>
            <select aria-label={`挂载 ${asset.display_name || '素材'} 到镜头`} value={shotTarget} disabled={busy || shots.length === 0} onChange={(event) => setShotTargets((current) => ({ ...current, [asset.id]: event.target.value }))}>
              <option value="" disabled>{shots.length === 0 ? '本策划还没有镜头' : '挂到镜头…'}</option>
              {shots.map((shot) => <option key={shot.id} value={shot.id}>{shotPositionLabel(shot.position)} · {shot.title}</option>)}
            </select>
            <button className="btn" type="button" disabled={busy || !shotTarget} onClick={() => void bind(asset, 'shot', shotTarget, 'shot_reference_display')}>挂到该镜头</button>
          </div>
        )}
      </article>
    })}</div>}
  </section>
}

function bindingLabel(shots: MediaShotOption[], holderID: string): string {
  const shot = shots.find((option) => option.id === holderID)
  return shot ? `${shotPositionLabel(shot.position)} · ${shot.title}` : removedShotLabel
}

function stateLabel(state: PlanAsset['state']): string { return ({ staged: '待挂载', active: '使用中', gc_pending: '清理中', deleted: '已删除', corrupt: '暂不可用' } as const)[state] }

function AssetImage({ planID, asset }: { planID: string; asset: PlanAsset }) {
  const [src, setSrc] = useState<string | null>(null)
  useEffect(() => {
    if (!asset.display_checksum) return
    let active = true
    let url = ''
    fetchPlanAssetDisplay(planID, asset.id, asset.display_checksum).then((blob) => {
      if (!active) return
      url = URL.createObjectURL(blob); setSrc(url)
    }).catch(() => { if (active) setSrc(null) })
    return () => { active = false; if (url) URL.revokeObjectURL(url) }
  }, [asset.display_checksum, asset.id, planID])
  return src ? <img className="planning-media-image" src={src} alt={asset.display_name || '参考素材'} /> : <div className="planning-media-placeholder" aria-hidden="true">{asset.state === 'corrupt' ? '素材暂不可用' : '参考图'}</div>
}
