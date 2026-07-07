import { useState } from 'react'
import { DICT } from '../crm/prototypeData'
import type { Package, PricingMode, ShootType } from '../crm/prototypeData'
import { formatPrice } from '../crm/model'
import type { PackageInput } from '../crm/prototypeStoreContext'
import { usePrototypeStore } from '../crm/prototypeStoreContext'
import { useShell } from '../components/shellContext'

const defaultDraft: PackageInput = {
  name: '',
  shootType: 'portrait',
  pricingMode: 'per_duration',
  basePriceYuan: 680,
  durationMinutes: 90,
  shotCountMin: 60,
  shotCountMax: 80,
  rawDeliveryCount: null,
  retouchCount: 8,
  note: '',
}

export default function PackagesPage() {
  const store = usePrototypeStore()
  const { notify } = useShell()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [draft, setDraft] = useState<PackageInput>(defaultDraft)
  const [submitted, setSubmitted] = useState(false)
  const activeCount = store.packages.filter((pkg) => pkg.status === 'active').length
  const archivedCount = store.packages.length - activeCount

  function openNew() {
    setDraft(defaultDraft)
    setSubmitted(false)
    setDialogOpen(true)
  }

  function openEdit(pkg: Package) {
    setDraft({
      id: pkg.id,
      name: pkg.name,
      shootType: pkg.shoot_type,
      pricingMode: pkg.pricing_mode,
      basePriceYuan: pkg.base_price / 100,
      durationMinutes: pkg.duration_minutes,
      shotCountMin: pkg.shot_count_min,
      shotCountMax: pkg.shot_count_max,
      rawDeliveryCount: pkg.raw_delivery_count,
      retouchCount: pkg.retouch_count,
      note: pkg.note,
    })
    setSubmitted(false)
    setDialogOpen(true)
  }

  function savePackage() {
    setSubmitted(true)
    if (!draft.name.trim()) return
    store.upsertPackage({ ...draft, name: draft.name.trim() })
    setDialogOpen(false)
    notify(`套系「${draft.name.trim()}」已保存`)
  }

  return (
    <>
      <header className="topbar">
        <div>
          <h1>套系</h1>
          <div className="sub">在售 {activeCount} 个 · 已归档 {archivedCount} 个 · 新约单从这里选套系</div>
        </div>
        <div className="topbar-actions">
          <button className="btn btn-primary" type="button" onClick={openNew}>＋ 新建套系</button>
        </div>
      </header>

      <main className="content">
        <div className="mode-hint">
          <div className="m"><b>按时长</b>基础价对应约定时长，超时另计</div>
          <div className="m"><b>按张计价</b>单张单价 × 起拍张数，精修另计</div>
          <div className="m"><b>一口价</b>打包价含棚租/灯光等固定成本</div>
        </div>

        <div className="pkg-grid">
          {store.packages.map((pkg) => {
            const archived = pkg.status === 'archived'
            const usedCount = store.orders.filter((order) => order.package_id === pkg.id).length
            return (
              <div className={`pkg-card${archived ? ' archived' : ''}`} key={pkg.id}>
                <div className="pkg-head">
                  <h3>{pkg.name}</h3>
                  <span className={pkg.shoot_type === 'cosplay' ? 'badge badge-warning' : pkg.shoot_type === 'portrait' ? 'badge badge-accent' : 'badge badge-muted'}>{DICT.shootType[pkg.shoot_type]}</span>
                  {archived && <span className="badge badge-muted">已下架</span>}
                </div>
                <div className="pkg-price">{formatPrice(pkg.base_price)} <small>{priceUnit(pkg)}</small></div>
                <div className="pkg-specs">
                  <span className="k">定价模式</span><span className="v">{DICT.pricingMode[pkg.pricing_mode]}</span>
                  <span className="k">拍摄时长</span><span className="v">{pkg.duration_minutes} 分钟</span>
                  <span className="k">拍摄张数</span><span className="v">{pkg.shot_count_min}–{pkg.shot_count_max} 张</span>
                  <span className="k">底片交付</span><span className="v">{pkg.raw_delivery_count == null ? '全送' : `${pkg.raw_delivery_count} 张`}</span>
                  <span className="k">精修</span><span className="v">{pkg.retouch_count > 0 ? `含 ${pkg.retouch_count} 张` : '不含'}</span>
                  <span className="k">历史约单</span><span className="v">{usedCount} 单</span>
                </div>
                <div className="pkg-note">{pkg.note}</div>
                <div className="pkg-foot">
                  {archived ? (
                    <button className="btn btn-sm" type="button" onClick={() => { store.setPackageStatus(pkg.id, 'active'); notify('已重新上架') }}>重新上架</button>
                  ) : (
                    <>
                      <button className="btn btn-sm" type="button" onClick={() => openEdit(pkg)}>编辑</button>
                      <button className="btn btn-sm btn-ghost" type="button" onClick={() => { store.setPackageStatus(pkg.id, 'archived'); notify('已归档，历史订单不受影响') }}>归档</button>
                    </>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      </main>

      <div className={`overlay${dialogOpen ? ' open' : ''}`} onClick={(event) => { if (event.target === event.currentTarget) setDialogOpen(false) }}>
        <div className="dialog" role="dialog" aria-modal="true" aria-labelledby="packageDialogTitle">
          <h2 id="packageDialogTitle">{draft.id ? '编辑套系' : '新建套系'}</h2>
          <div className="dialog-sub">定价与拍摄信息将展示给客户确认，请按实际交付填写</div>

          <div className={`field${submitted && !draft.name.trim() ? ' show-err' : ''}`}>
            <label htmlFor="packageName">套系名称 *</label>
            <input id="packageName" className={`input${submitted && !draft.name.trim() ? ' invalid' : ''}`} value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} placeholder="如：日系写真 · 基础" autoFocus />
            <div className="err">名称不能为空</div>
          </div>

          <div className="field-row">
            <div className="field">
              <label htmlFor="packageShootType">拍摄类型</label>
              <select id="packageShootType" className="input" value={draft.shootType} onChange={(event) => setDraft({ ...draft, shootType: event.target.value as ShootType })}>
                {Object.entries(DICT.shootType).map(([key, label]) => <option key={key} value={key}>{label}</option>)}
              </select>
            </div>
            <div className="field">
              <label htmlFor="packagePricingMode">定价模式</label>
              <select id="packagePricingMode" className="input" value={draft.pricingMode} onChange={(event) => setDraft({ ...draft, pricingMode: event.target.value as PricingMode })}>
                {Object.entries(DICT.pricingMode).map(([key, label]) => <option key={key} value={key}>{label}</option>)}
              </select>
            </div>
          </div>

          <div className="field-row">
            <NumberField label="基础价（元）" value={draft.basePriceYuan} onChange={(value) => setDraft({ ...draft, basePriceYuan: value })} />
            <NumberField label="时长（分钟）" value={draft.durationMinutes} onChange={(value) => setDraft({ ...draft, durationMinutes: value })} step={15} />
          </div>

          <div className="field-row">
            <div className="field">
              <label>拍摄张数范围</label>
              <div className="inline-inputs">
                <input className="input" type="number" min="0" value={draft.shotCountMin} onChange={(event) => setDraft({ ...draft, shotCountMin: Number(event.target.value) })} />
                <span>–</span>
                <input className="input" type="number" min="0" value={draft.shotCountMax} onChange={(event) => setDraft({ ...draft, shotCountMax: Number(event.target.value) })} />
              </div>
            </div>
            <NumberField label="精修张数" value={draft.retouchCount} onChange={(value) => setDraft({ ...draft, retouchCount: value })} hint="填 0 表示不含精修" />
          </div>

          <div className="field">
            <label htmlFor="packageRaw">底片交付</label>
            <select id="packageRaw" className="input" value={draft.rawDeliveryCount == null ? 'all' : 'count'} onChange={(event) => setDraft({ ...draft, rawDeliveryCount: event.target.value === 'all' ? null : draft.shotCountMin })}>
              <option value="all">底片全送</option>
              <option value="count">按数量交付（暂按起拍张数）</option>
            </select>
          </div>

          <div className="field">
            <label htmlFor="packageNote">备注</label>
            <textarea id="packageNote" className="input" value={draft.note} onChange={(event) => setDraft({ ...draft, note: event.target.value })} placeholder="服装造型、棚租是否包含、加拍规则…" />
          </div>

          <div className="dialog-actions">
            <button className="btn" type="button" onClick={() => setDialogOpen(false)}>取消</button>
            <button className="btn btn-primary" type="button" onClick={savePackage}>保存套系</button>
          </div>
        </div>
      </div>
    </>
  )
}

function NumberField({ label, value, onChange, step, hint }: { label: string; value: number; onChange(value: number): void; step?: number; hint?: string }) {
  return (
    <div className="field">
      <label>{label}</label>
      <input className="input" type="number" min="0" step={step} value={value} onChange={(event) => onChange(Number(event.target.value))} />
      {hint && <div className="hint">{hint}</div>}
    </div>
  )
}

function priceUnit(pkg: Package): string {
  if (pkg.pricing_mode === 'per_photo') return `/张 起拍 ${pkg.shot_count_min} 张`
  if (pkg.pricing_mode === 'per_duration') return `/${pkg.duration_minutes >= 60 ? `${pkg.duration_minutes / 60} 小时` : `${pkg.duration_minutes} 分钟`}`
  return '一口价'
}
