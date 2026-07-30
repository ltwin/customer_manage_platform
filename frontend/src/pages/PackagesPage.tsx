import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  ApiError,
  createPackage,
  deletePackage,
  listPackages,
  updatePackage,
} from '../api/client'
import type {
  CreatePackageBody,
  PackageListResponse,
  PackageListStatus,
  UpdatePackageBody,
} from '../api/client'
import { useShell } from '../components/shellContext'
import type { PriceDraft } from './packagePrice'
import {
  isPackagePriceYuanInputAllowed,
  packagePriceCentsToYuan,
  packagePriceYuanToCents,
  validatePackagePriceYuan,
} from './packagePrice'
import StateNotice from '../components/StateNotice'
import {
  beginPageRead,
  completePageRead,
  failPageRead,
  pageReadPresentation,
  readyPageData,
  type PageReadState,
} from '../components/pageReadState'

type PackageItem = PackageListResponse['items'][number]
type NumberDraft = number | ''

interface PackageDraft {
  id?: string
  name: string
  shootType: CreatePackageBody['shoot_type']
  pricingMode: CreatePackageBody['pricing_mode']
  basePriceYuan: PriceDraft
  durationMinutes: NumberDraft
  shotCountMin: NumberDraft
  shotCountMax: NumberDraft
  rawDeliveryCount: NumberDraft
  retouchCount: NumberDraft
  note: string
}

const shootTypeLabels: Record<CreatePackageBody['shoot_type'], string> = {
  portrait: '写真',
  cosplay: 'Cosplay',
  other: '其他',
}

const pricingModeLabels: Record<CreatePackageBody['pricing_mode'], string> = {
  per_duration: '按时长',
  per_photo: '按张',
  fixed: '一口价',
}

const statusFilters: Array<[PackageListStatus & string, string]> = [
  ['active', '在售'],
  ['archived', '已下架'],
  ['all', '全部'],
]

const shootTypeBadgeClasses: Record<CreatePackageBody['shoot_type'], string> = {
  portrait: 'badge badge-accent',
  cosplay: 'badge badge-warning',
  other: 'badge badge-muted',
}

const packagePageSize = 50

const defaultDraft: PackageDraft = {
  name: '',
  shootType: 'portrait',
  pricingMode: 'per_duration',
  basePriceYuan: '680',
  durationMinutes: 90,
  shotCountMin: 60,
  shotCountMax: 80,
  rawDeliveryCount: '',
  retouchCount: 8,
  note: '',
}

export default function PackagesPage() {
  const navigate = useNavigate()
  const { notify } = useShell()
  const [status, setStatus] = useState<PackageListStatus & string>('active')
  const [readState, setReadState] = useState<PageReadState<{
    items: PackageItem[]
    total: number
    page: number
  }>>({ kind: 'loading', message: '正在加载套系' })
  const [loadingMore, setLoadingMore] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<PackageItem | null>(null)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const [draft, setDraft] = useState<PackageDraft>(defaultDraft)
  const [submitted, setSubmitted] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [actionId, setActionId] = useState<string | null>(null)
  const [reloadTick, setReloadTick] = useState(0)
  const statusRef = useRef(status)
  const loadedStatusRef = useRef<PackageListStatus & string | null>(null)
  const loadMoreRequestSeq = useRef(0)
  const presentation = pageReadPresentation(readState)
  const readData = readyPageData(readState)
  const items = readData?.items ?? []
  const total = readData?.total ?? 0
  const page = readData?.page ?? 1

  const invalidateLoadMore = useCallback(() => {
    loadMoreRequestSeq.current += 1
    setLoadingMore(false)
  }, [])

  const goLogin = useCallback(() => {
    navigate('/login', { replace: true })
  }, [navigate])

  useEffect(() => {
    let active = true
    invalidateLoadMore()
    const preserveReady = loadedStatusRef.current === status
    loadedStatusRef.current = status
    setReadState((current) => beginPageRead(current, '正在加载套系', preserveReady))
    setActionError(null)
    listPackages({ status, page: 1, pageSize: packagePageSize })
      .then((result) => {
        if (!active) return
        setReadState(completePageRead(
          { items: result.items, total: result.total, page: 1 },
          result.items.length === 0,
          `${statusFilters.find(([key]) => key === status)?.[1] ?? '当前筛选'}下暂无套系`,
        ))
      })
      .catch((err: unknown) => {
        if (!active) return
        if (err instanceof ApiError && err.status === 401) {
          setReadState({ kind: 'unauthorized' })
          goLogin()
          return
        }
        setReadState((current) => failPageRead(
          current,
          err instanceof Error ? err.message : '套系列表加载失败',
          () => setReloadTick((tick) => tick + 1),
        ))
      })
    return () => {
      active = false
    }
  }, [goLogin, invalidateLoadMore, reloadTick, status])

  useEffect(() => {
    statusRef.current = status
  }, [status])

  useEffect(() => {
    if (!dialogOpen && !deleteTarget) return
    function onEscape(event: KeyboardEvent) {
      if (event.key !== 'Escape') return
      setDialogOpen(false)
      setDeleteTarget(null)
      setDeleteError(null)
    }
    window.addEventListener('keydown', onEscape)
    return () => window.removeEventListener('keydown', onEscape)
  }, [deleteTarget, dialogOpen])

  const statusText = useMemo(() => {
    if (status === 'active') return '在售'
    if (status === 'archived') return '已下架'
    return '全部'
  }, [status])

  function changeStatusFilter(next: PackageListStatus & string) {
    if (next === status) return
    statusRef.current = next
    invalidateLoadMore()
    setStatus(next)
  }

  function reloadPackages() {
    invalidateLoadMore()
    setReloadTick((tick) => tick + 1)
  }

  function openNew() {
    setDraft(defaultDraft)
    setSubmitted(false)
    setFormError(null)
    setDialogOpen(true)
  }

  function openEdit(pkg: PackageItem) {
    setDraft({
      id: pkg.id,
      name: pkg.name,
      shootType: pkg.shoot_type,
      pricingMode: pkg.pricing_mode,
      basePriceYuan: packagePriceCentsToYuan(pkg.base_price),
      durationMinutes: numberOrBlank(pkg.duration_minutes),
      shotCountMin: numberOrBlank(pkg.shot_count_min),
      shotCountMax: numberOrBlank(pkg.shot_count_max),
      rawDeliveryCount: numberOrBlank(pkg.raw_delivery_count),
      retouchCount: numberOrBlank(pkg.retouch_count),
      note: pkg.note ?? '',
    })
    setSubmitted(false)
    setFormError(null)
    setDialogOpen(true)
  }

  function openDelete(pkg: PackageItem) {
    setDeleteTarget(pkg)
    setDeleteError(null)
  }

  function closeDelete() {
    setDeleteTarget(null)
    setDeleteError(null)
  }

  async function loadMorePackages() {
    const nextPage = page + 1
    const requestStatus = status
    const requestSeq = loadMoreRequestSeq.current + 1
    loadMoreRequestSeq.current = requestSeq
    const requestStillCurrent = () =>
      loadMoreRequestSeq.current === requestSeq && statusRef.current === requestStatus
    setLoadingMore(true)
    setActionError(null)
    try {
      const result = await listPackages({ status: requestStatus, page: nextPage, pageSize: packagePageSize })
      if (!requestStillCurrent()) return
      setReadState((current) => {
        const data = readyPageData(current)
        if (!data) return current
        return completePageRead({
          items: [...data.items, ...result.items],
          total: result.total,
          page: nextPage,
        }, false, '')
      })
    } catch (err) {
      if (!requestStillCurrent()) return
      if (err instanceof ApiError && err.status === 401) {
        goLogin()
        return
      }
      setReadState((current) => failPageRead(
        current,
        err instanceof Error ? err.message : '加载更多失败，显示上次成功数据',
        () => { void loadMorePackages() },
      ))
    } finally {
      if (requestStillCurrent()) setLoadingMore(false)
    }
  }

  async function savePackage() {
    setSubmitted(true)
    setFormError(null)
    const validation = validateDraft(draft)
    if (validation) {
      setFormError(validation)
      return
    }
    setSaving(true)
    try {
      const name = draft.name.trim()
      if (draft.id) {
        await updatePackage(draft.id, toUpdateBody(draft))
        notify(`套系「${name}」已保存`)
        setDialogOpen(false)
        reloadPackages()
      } else {
        await createPackage(toCreateBody(draft))
        notify(`套系「${name}」已创建`)
        setDialogOpen(false)
        invalidateLoadMore()
        if (status === 'archived') {
          statusRef.current = 'active'
          setStatus('active')
        } else {
          setReloadTick((tick) => tick + 1)
        }
      }
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        goLogin()
        return
      }
      setFormError(err instanceof Error ? err.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  async function changeStatus(pkg: PackageItem, next: 'active' | 'archived') {
    if (!pkg.id) return
    setActionId(pkg.id)
    setActionError(null)
    try {
      await updatePackage(pkg.id, { status: next })
      notify(next === 'active' ? '已重新上架' : '已下架')
      reloadPackages()
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        goLogin()
        return
      }
      setActionError(err instanceof Error ? err.message : '操作失败')
    } finally {
      setActionId(null)
    }
  }

  async function confirmDelete() {
    if (!deleteTarget?.id) return
    setActionId(deleteTarget.id)
    setActionError(null)
    setDeleteError(null)
    try {
      await deletePackage(deleteTarget.id)
      notify('套系已删除')
      closeDelete()
      reloadPackages()
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        goLogin()
        return
      }
      setDeleteError(err instanceof Error ? err.message : '删除失败')
    } finally {
      setActionId(null)
    }
  }

  return (
    <>
      <header className="topbar">
        <div>
          <h1>套系</h1>
          <div className="sub">{statusText} <span className="num">{total}</span> 个 · 新约单从这里选套系</div>
        </div>
        <div className="topbar-actions">
          <button className="btn btn-primary" type="button" onClick={openNew}>＋ 新建套系</button>
        </div>
      </header>

      <main className="content">
        <div className="toolbar">
          <div className="chips">
            {statusFilters.map(([key, label]) => (
              <button
                key={key}
                className={`chip${status === key ? ' active' : ''}`}
                type="button"
                onClick={() => changeStatusFilter(key)}
              >
                {label}
              </button>
            ))}
          </div>
        </div>

        {actionError && <div className="form-error" role="alert">{actionError}</div>}
        {presentation.notice && <StateNotice {...presentation.notice} />}

        <div className="mode-hint">
          <div className="m"><b>按时长</b>基础价对应约定时长，超时另计</div>
          <div className="m"><b>按张计价</b>单张单价 × 起拍张数，精修另计</div>
          <div className="m"><b>一口价</b>打包价含棚租/灯光等固定成本</div>
        </div>

        {presentation.showReadyData && (
          <>
            <div className="pkg-grid">
              {items.map((pkg) => {
                const archived = pkg.status === 'archived'
                const busy = actionId === pkg.id
                return (
                  <div className={`pkg-card${archived ? ' archived' : ''}`} key={pkg.id}>
                    <div className="pkg-head">
                      <h3>{pkg.name}</h3>
                      <span className={shootTypeBadgeClasses[pkg.shoot_type]}>
                        {shootTypeLabels[pkg.shoot_type]}
                      </span>
                      {archived && <span className="badge badge-muted">已下架</span>}
                    </div>
                    <div className="pkg-price">{formatPrice(pkg.base_price)} <small>{priceUnit(pkg)}</small></div>
                    <div className="pkg-specs">
                      <span className="k">定价模式</span><span className="v">{pricingModeLabels[pkg.pricing_mode]}</span>
                      <span className="k">拍摄时长</span><span className="v">{specNumber(pkg.duration_minutes, '分钟')}</span>
                      <span className="k">拍摄张数</span><span className="v">{shotRange(pkg)}</span>
                      <span className="k">底片交付</span><span className="v">{specNumber(pkg.raw_delivery_count, '张')}</span>
                      <span className="k">精修</span><span className="v">{retouchText(pkg.retouch_count)}</span>
                      <span className="k">历史约单</span><span className="v">{pkg.orders_count} 单</span>
                    </div>
                    {pkg.note && <div className="pkg-note">{pkg.note}</div>}
                    <div className="pkg-foot">
                      <button className="btn btn-sm" type="button" disabled={busy} onClick={() => openEdit(pkg)}>编辑</button>
                      {archived ? (
                        <button className="btn btn-sm" type="button" disabled={busy} onClick={() => { void changeStatus(pkg, 'active') }}>重新上架</button>
                      ) : (
                        <button className="btn btn-sm btn-ghost" type="button" disabled={busy} onClick={() => { void changeStatus(pkg, 'archived') }}>下架</button>
                      )}
                      <button className="btn btn-sm btn-danger-ghost" type="button" disabled={busy} onClick={() => openDelete(pkg)}>删除</button>
                    </div>
                  </div>
                )
              })}
            </div>
            {items.length < total && (
              <div className="load-more">
                <button className="btn" type="button" disabled={loadingMore} onClick={() => { void loadMorePackages() }}>
                  {loadingMore ? '加载中' : `加载更多（${items.length}/${total}）`}
                </button>
              </div>
            )}
          </>
        )}
      </main>

      <PackageDialog
        draft={draft}
        open={dialogOpen}
        saving={saving}
        submitted={submitted}
        formError={formError}
        onClose={() => setDialogOpen(false)}
        onDraft={setDraft}
        onSave={() => { void savePackage() }}
      />

      {deleteTarget && (
        <div className="overlay open" onClick={(event) => { if (event.target === event.currentTarget) closeDelete() }}>
          <section className="dialog" role="dialog" aria-modal="true" aria-labelledby="deletePackageTitle">
            <h2 id="deletePackageTitle">删除套系</h2>
            <p className="dialog-sub">「{deleteTarget.name}」删除后不会出现在任何套系列表中。</p>
            {deleteError && <div className="form-error">{deleteError}</div>}
            <div className="dialog-actions">
              <button className="btn" type="button" onClick={closeDelete}>取消</button>
              <button
                className="btn btn-danger"
                type="button"
                disabled={actionId === deleteTarget.id}
                onClick={() => { void confirmDelete() }}
              >
                确认删除
              </button>
            </div>
          </section>
        </div>
      )}
    </>
  )
}

function PackageDialog({
  draft,
  open,
  saving,
  submitted,
  formError,
  onClose,
  onDraft,
  onSave,
}: {
  draft: PackageDraft
  open: boolean
  saving: boolean
  submitted: boolean
  formError: string | null
  onClose(): void
  onDraft(next: PackageDraft): void
  onSave(): void
}) {
  return (
    <div className={`overlay${open ? ' open' : ''}`} onClick={(event) => { if (event.target === event.currentTarget) onClose() }}>
      <div className="dialog" role="dialog" aria-modal="true" aria-labelledby="packageDialogTitle">
        <h2 id="packageDialogTitle">{draft.id ? '编辑套系' : '新建套系'}</h2>
        <div className="dialog-sub">定价与拍摄信息将展示给客户确认，请按实际交付填写</div>

        <div className={`field${submitted && !draft.name.trim() ? ' show-err' : ''}`}>
          <label htmlFor="packageName">套系名称 *</label>
          <input
            id="packageName"
            className={`input${submitted && !draft.name.trim() ? ' invalid' : ''}`}
            value={draft.name}
            onChange={(event) => onDraft({ ...draft, name: event.target.value })}
            placeholder="如：日系写真 · 基础"
            autoFocus
          />
          <div className="err">名称不能为空</div>
        </div>

        <div className="field-row">
          <div className="field">
            <label htmlFor="packageShootType">拍摄类型</label>
            <select id="packageShootType" className="input" value={draft.shootType} onChange={(event) => onDraft({ ...draft, shootType: event.target.value as CreatePackageBody['shoot_type'] })}>
              {Object.entries(shootTypeLabels).map(([key, label]) => <option key={key} value={key}>{label}</option>)}
            </select>
          </div>
          <div className="field">
            <label htmlFor="packagePricingMode">定价模式</label>
            <select id="packagePricingMode" className="input" value={draft.pricingMode} onChange={(event) => onDraft({ ...draft, pricingMode: event.target.value as CreatePackageBody['pricing_mode'] })}>
              {Object.entries(pricingModeLabels).map(([key, label]) => <option key={key} value={key}>{label}</option>)}
            </select>
          </div>
        </div>

        <div className="field-row">
          <PriceField
            label="基础价（元）"
            required
            value={draft.basePriceYuan}
            onChange={(value) => onDraft({ ...draft, basePriceYuan: value })}
            step={0.01}
          />
          <NumberField label="时长（分钟）" value={draft.durationMinutes} onChange={(value) => onDraft({ ...draft, durationMinutes: value })} step={15} />
        </div>

        <div className="field-row">
          <div className="field">
            <label>拍摄张数范围</label>
            <div className="inline-inputs">
              <input className="input" type="number" min="0" value={draft.shotCountMin} onChange={(event) => onDraft({ ...draft, shotCountMin: parseNumberDraft(event.target.value) })} />
              <span>–</span>
              <input className="input" type="number" min="0" value={draft.shotCountMax} onChange={(event) => onDraft({ ...draft, shotCountMax: parseNumberDraft(event.target.value) })} />
            </div>
          </div>
          <NumberField label="精修张数" value={draft.retouchCount} onChange={(value) => onDraft({ ...draft, retouchCount: value })} hint="填 0 表示不含精修" />
        </div>

        <div className="field-row">
          <NumberField label="底片交付（张）" value={draft.rawDeliveryCount} onChange={(value) => onDraft({ ...draft, rawDeliveryCount: value })} />
          <div className="field">
            <label htmlFor="packageNote">备注</label>
            <textarea id="packageNote" className="input" value={draft.note} onChange={(event) => onDraft({ ...draft, note: event.target.value })} placeholder="服装造型、棚租是否包含、加拍规则…" />
          </div>
        </div>

        {formError && <div className="form-error">{formError}</div>}

        <div className="dialog-actions">
          <button className="btn" type="button" onClick={onClose}>取消</button>
          <button className="btn btn-primary" type="button" disabled={saving} onClick={onSave}>保存套系</button>
        </div>
      </div>
    </div>
  )
}

function PriceField({
  label,
  value,
  onChange,
  required,
  step,
}: {
  label: string
  value: PriceDraft
  onChange(value: PriceDraft): void
  required?: boolean
  step?: number
}) {
  return (
    <div className="field">
      <label>{label}{required ? ' *' : ''}</label>
      <input
        className="input"
        type="number"
        min="0"
        step={step}
        value={value}
        onChange={(event) => {
          if (!isPackagePriceYuanInputAllowed(event.target.value)) return
          onChange(event.target.value)
        }}
      />
    </div>
  )
}

function NumberField({
  label,
  value,
  onChange,
  required,
  step,
  hint,
}: {
  label: string
  value: NumberDraft
  onChange(value: NumberDraft): void
  required?: boolean
  step?: number
  hint?: string
}) {
  return (
    <div className="field">
      <label>{label}{required ? ' *' : ''}</label>
      <input
        className="input"
        type="number"
        min="0"
        step={step}
        value={value}
        onChange={(event) => {
          onChange(parseNumberDraft(event.target.value))
        }}
      />
      {hint && <div className="hint">{hint}</div>}
    </div>
  )
}

function validateDraft(draft: PackageDraft): string | null {
  if (!draft.name.trim()) return '名称不能为空'
  const priceError = validatePackagePriceYuan(draft.basePriceYuan)
  if (priceError) return priceError
  const numericFields: Array<[string, NumberDraft]> = [
    ['时长', draft.durationMinutes],
    ['拍摄张数下限', draft.shotCountMin],
    ['拍摄张数上限', draft.shotCountMax],
    ['底片交付', draft.rawDeliveryCount],
    ['精修张数', draft.retouchCount],
  ]
  for (const [label, value] of numericFields) {
    if (value !== '' && !Number.isFinite(value)) return `${label}必须是有效数字`
    if (value !== '' && value < 0) return `${label}不能为负`
  }
  const integerFields: Array<[string, NumberDraft]> = [
    ['时长', draft.durationMinutes],
    ['拍摄张数下限', draft.shotCountMin],
    ['拍摄张数上限', draft.shotCountMax],
    ['底片交付', draft.rawDeliveryCount],
    ['精修张数', draft.retouchCount],
  ]
  for (const [label, value] of integerFields) {
    if (value !== '' && !Number.isInteger(value)) return `${label}必须是整数`
  }
  if (draft.shotCountMin !== '' && draft.shotCountMax !== '' && draft.shotCountMin > draft.shotCountMax) {
    return '拍摄张数下限不能大于上限'
  }
  if (draft.note.length > 500) return '备注不能超过 500 字'
  return null
}

function toCreateBody(draft: PackageDraft): CreatePackageBody {
  return {
    name: draft.name.trim(),
    shoot_type: draft.shootType,
    pricing_mode: draft.pricingMode,
    base_price: packagePriceYuanToCents(draft.basePriceYuan),
    duration_minutes: optionalNumber(draft.durationMinutes),
    shot_count_min: optionalNumber(draft.shotCountMin),
    shot_count_max: optionalNumber(draft.shotCountMax),
    raw_delivery_count: optionalNumber(draft.rawDeliveryCount),
    retouch_count: optionalNumber(draft.retouchCount),
    note: optionalText(draft.note),
  }
}

function toUpdateBody(draft: PackageDraft): UpdatePackageBody {
  return {
    name: draft.name.trim(),
    shoot_type: draft.shootType,
    pricing_mode: draft.pricingMode,
    base_price: packagePriceYuanToCents(draft.basePriceYuan),
    duration_minutes: nullableNumber(draft.durationMinutes),
    shot_count_min: nullableNumber(draft.shotCountMin),
    shot_count_max: nullableNumber(draft.shotCountMax),
    raw_delivery_count: nullableNumber(draft.rawDeliveryCount),
    retouch_count: nullableNumber(draft.retouchCount),
    note: optionalText(draft.note) ?? '',
  }
}

function parseNumberDraft(value: string): NumberDraft {
  if (value === '') return ''
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : ''
}

function optionalNumber(value: NumberDraft): number | undefined {
  return value === '' ? undefined : value
}

function nullableNumber(value: NumberDraft): number | null {
  return value === '' ? null : value
}

function optionalText(value: string): string | undefined {
  const trimmed = value.trim()
  return trimmed ? trimmed : undefined
}

function numberOrBlank(value: number | null | undefined): NumberDraft {
  return isNil(value) ? '' : value
}

function formatPrice(cents: number): string {
  return `¥${(cents / 100).toLocaleString('zh-CN', { maximumFractionDigits: 2 })}`
}

function priceUnit(pkg: PackageItem): string {
  if (pkg.pricing_mode === 'per_photo') return isNil(pkg.shot_count_min) ? '/张' : `/张 起拍 ${pkg.shot_count_min} 张`
  if (pkg.pricing_mode === 'per_duration') {
    if (isNil(pkg.duration_minutes)) return '/时长'
    return `/${pkg.duration_minutes >= 60 ? `${pkg.duration_minutes / 60} 小时` : `${pkg.duration_minutes} 分钟`}`
  }
  return '一口价'
}

function specNumber(value: number | null | undefined, unit: string): string {
  return isNil(value) ? '未设置' : `${value} ${unit}`
}

function retouchText(value: number | null | undefined): string {
  if (isNil(value)) return '未设置'
  if (value > 0) return `含 ${value} 张`
  return '不含'
}

function shotRange(pkg: PackageItem): string {
  const hasMin = !isNil(pkg.shot_count_min)
  const hasMax = !isNil(pkg.shot_count_max)
  if (!hasMin && !hasMax) return '未设置'
  if (hasMin && hasMax) return `${pkg.shot_count_min}–${pkg.shot_count_max} 张`
  if (hasMin) return `至少 ${pkg.shot_count_min} 张`
  return `至多 ${pkg.shot_count_max} 张`
}

function isNil(value: unknown): value is null | undefined {
  return value === null || value === undefined
}
