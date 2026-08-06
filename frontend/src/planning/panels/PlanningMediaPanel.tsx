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

type Source = components['schemas']['PlanningMediaSourceClass']

export default function PlanningMediaPanel({ planID, planRevision, readOnly }: { planID: string; planRevision: number; readOnly: boolean }) {
  const [assets, setAssets] = useState<PlanAsset[]>([])
  const [source, setSource] = useState<Source>('photographer_owned')
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

  async function selectFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    if (!file) return
    setBusy(true); setNotice('')
    ++requestVersion.current
    try {
      const result = await uploadPlanAsset(planID, planRevision, file, rightsFor(source), 'moodboard_display', newPlanningMutationKey('asset-upload'))
      setAssets((current) => [result.asset, ...current.filter((item) => item.id !== result.asset.id)])
      setNotice('素材已安全保存，可继续挂到整案或镜头。')
    } catch (error) { setNotice(planningErrorMessage(error, '素材上传失败')) } finally { setBusy(false); event.target.value = '' }
  }

  async function bindToPlan(asset: PlanAsset) {
    setBusy(true); setNotice('')
    try {
      const result = await createPlanAssetBinding(planID, asset.id, {
        generation: asset.current_generation, holder_kind: 'plan', holder_id: planID,
        purpose: 'moodboard_display', expected_plan_revision: planRevision, expected_asset_revision: asset.revision,
      }, newPlanningMutationKey('asset-bind'))
      setAssets((current) => current.map((item) => item.id === asset.id ? result.asset : item))
      setNotice('已挂到整案情绪板。')
    } catch (error) { setNotice(planningErrorMessage(error, '素材挂载失败')) } finally { setBusy(false) }
  }

  return <section className="planning-media-panel" aria-labelledby="planning-media-title">
    <div className="panel-heading"><div><h2 id="planning-media-title">参考素材</h2><p className="muted">上传后会保留安全展示版本；未挂载素材保留 48 小时，方便重试。</p></div>
      {!readOnly && <button className="btn btn-primary" type="button" disabled={busy || loading} onClick={() => fileRef.current?.click()}>上传参考图</button>}
    </div>
    {!readOnly && <div className="planning-media-controls"><label>素材来源<select value={source} onChange={(event) => setSource(event.target.value as Source)}><option value="photographer_owned">本人拍摄或创作</option><option value="licensed">已取得许可证</option><option value="official">官方素材</option><option value="fan">同人作品</option><option value="anime_screenshot">动画截图</option><option value="setting_book">设定集</option><option value="unknown_web">网络来源待确认</option><option value="customer_supplied">客户提供</option></select></label><input ref={fileRef} className="visually-hidden" type="file" accept="image/jpeg,image/png,image/webp" onChange={(event) => void selectFile(event)} /></div>}
    {notice && <div className="planning-feedback" role="status">{notice}</div>}
    {loading ? <div className="planning-empty"><strong>正在加载参考素材</strong><span>素材列表加载完成后即可继续操作。</span></div> : assets.length === 0 ? <div className="planning-empty"><strong>还没有参考素材</strong><span>可先上传场地、动作、构图或灯光参考。</span></div> : <div className="planning-media-grid">{assets.map((asset) => <article className={`planning-media-card state-${asset.state}`} key={asset.id}><AssetImage planID={planID} asset={asset} /><h3>{asset.display_name || '未命名素材'}</h3><p>{stateLabel(asset.state)} · 第 {asset.current_generation} 代</p>{asset.state === 'staged' && !readOnly && <button className="btn" type="button" disabled={busy} onClick={() => void bindToPlan(asset)}>挂到整案</button>}</article>)}</div>}
  </section>
}

function rightsFor(source: Source): components['schemas']['PlanningMediaRightsDeclaration'] {
  const basis: Record<Source, components['schemas']['PlanningMediaRightsBasis']> = {
    official: 'citation_or_display', anime_screenshot: 'citation_or_display', setting_book: 'citation_or_display', fan: 'citation_or_display', unknown_web: 'citation_or_display', photographer_owned: 'ownership_attested', licensed: 'license_recorded', customer_supplied: 'display_consent',
  }
  return { source_class: source, rights_basis: basis[source], license_generation_reference_granted: false }
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
