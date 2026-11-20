import { useEffect, useMemo, useRef, useState } from 'react'
import { Check, X } from 'lucide-react'
import type { SceneObject } from '../api.ts'
import { defaultPasses, exportCount, type ExportPass, type ExportPlan, type Progress, type ShotImage } from '../host/exportShots.ts'
import { longSideOf, type Engine, type Resolution } from '../render/engines.ts'
import type { DisplayMode } from '../state/sceneStore.ts'

// The 3-stage export dialog:
//   1) configure (shot/camera grid, pass cards, unkeyed prompt, inline error)
//   2) exporting (optical viewfinder + Eurika 33-pearl starburst + cost-weighted progress)
//   3) completed (neutral contact-sheet thumbnails + stay / export more / focus canvas nodes)

/** A shot as the dialog lists it. */
export type ExportShot = { id: string; title: string; bound: boolean; lensText?: string }

/** What to do with changes not keyed yet before exporting. */
export type UnkeyedChoice = 'key' | 'discard'

/** Result held in Stage 3 until dismissed or starting another batch. */
export type ExportCompleted = {
  nodeIDs: readonly string[]
  previews: readonly ShotImage[]
  exportedIDs: readonly string[]
}

const passes: { pass: ExportPass; label: string; short: string; desc: string }[] = [
  { pass: 'clay', label: '白模（clay）', short: '白模', desc: '体积光影' },
  { pass: 'line', label: '线稿', short: '线稿', desc: '结构轮廓' },
  { pass: 'lit', label: '材质光照', short: '光照', desc: '最终渲染' },
]

const passShort: Record<ExportPass, string> = { clay: '白模', line: '线稿', lit: '光照' }

type Pearl = { x: number; y: number; r: number; op: number; delay: number; core?: boolean }

// Exact 33-pearl vector proportions from frontend/public/brand/mark.svg centered at (60, 60).
const eurikaPearls: readonly Pearl[] = (() => {
  const cx = 60
  const cy = 60
  const step = 6.2
  const scaleR = 0.68
  const axial = [
    { r: 3.32, op: 1 },
    { r: 3.1, op: 0.95 },
    { r: 2.86, op: 0.89 },
    { r: 2.6, op: 0.83 },
    { r: 2.29, op: 0.76 },
    { r: 1.92, op: 0.68 },
  ]
  const quads = [
    { ix: 1, iy: 1, r: 2.46, op: 0.79, tier: 1.6 },
    { ix: 1, iy: 2, r: 1.89, op: 0.67, tier: 2.6 },
    { ix: 2, iy: 1, r: 1.89, op: 0.67, tier: 2.6 },
  ]
  const out: Pearl[] = [{ x: cx, y: cy, r: axial[0].r * scaleR, op: 1, delay: 0, core: true }]
  for (let i = 1; i <= 5; i++) {
    const { r, op } = axial[i]
    const d = i * step
    const delay = i * 0.13
    out.push(
      { x: cx - d, y: cy, r: r * scaleR, op, delay },
      { x: cx + d, y: cy, r: r * scaleR, op, delay },
      { x: cx, y: cy - d, r: r * scaleR, op, delay },
      { x: cx, y: cy + d, r: r * scaleR, op, delay },
    )
  }
  for (const q of quads) {
    for (const [sx, sy] of [
      [-1, -1],
      [1, -1],
      [-1, 1],
      [1, 1],
    ] as const) {
      out.push({
        x: cx + sx * q.ix * step,
        y: cy + sy * q.iy * step,
        r: q.r * scaleR,
        op: q.op,
        delay: q.tier * 0.13,
      })
    }
  }
  return out
})()

function cameraLensSummary(c: SceneObject): string {
  const cam = c.camera
  if (!cam) return '默认镜头'
  const parts = [`${Math.round(cam.focal_mm ?? 50)}mm`]
  if (cam.depth_of_field && cam.f_stop) parts.push(`f/${cam.f_stop}`)
  if (cam.aspect) parts.push(cam.aspect)
  return parts.join(' · ')
}

function progressText(p: Progress | null): string {
  if (!p) return '正在准备导出…'
  if (p.stage === 'prepare') return p.currentLabel ?? '正在准备场景…'
  if (p.stage === 'render') return `正在渲染 ${p.done}/${p.total}`
  if (p.stage === 'upload') return `正在上传 ${p.done}/${p.total}`
  return '正在放到画布…'
}

function PassGlyph({ pass }: { pass: ExportPass }) {
  if (pass === 'clay') {
    return (
      <svg className="cc-export-pass-glyph" viewBox="0 0 16 16" fill="none" aria-hidden="true">
        <circle cx="8" cy="8" r="5.5" fill="currentColor" fillOpacity="0.28" stroke="currentColor" strokeWidth="1.2" />
      </svg>
    )
  }
  if (pass === 'line') {
    return (
      <svg className="cc-export-pass-glyph" viewBox="0 0 16 16" fill="none" aria-hidden="true">
        <circle cx="8" cy="8" r="5.5" stroke="currentColor" strokeWidth="1.2" strokeDasharray="2.5 1.5" />
      </svg>
    )
  }
  return (
    <svg className="cc-export-pass-glyph" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <circle cx="8" cy="8" r="5.5" fill="currentColor" fillOpacity="0.82" />
      <circle cx="6.2" cy="6.2" r="1.5" fill="var(--on-primary, #0b0b0c)" />
    </svg>
  )
}

export function ExportDialog({
  cameras,
  shots = [],
  unkeyed = [],
  display,
  resolution,
  transparentBackground = false,
  engine = 'workbench',
  busy,
  progress,
  error = '',
  completed = null,
  onExport,
  onCancelExport,
  onContinueExport,
  onFocusNodes,
  onClose,
}: {
  cameras: readonly SceneObject[]
  /** The scene's shots in frame order; none for a scene exported by camera. */
  shots?: readonly ExportShot[]
  /** The changes waiting for a key, as the prompt names them. */
  unkeyed?: readonly string[]
  /** The view's shading when the dialog opened. */
  display: DisplayMode
  /** The scene's output resolution: the images' long side. */
  resolution: Resolution
  /** Whether the scene enables transparent background for clay and lit passes. */
  transparentBackground?: boolean
  /** The scene's renderer engine, shown in the dialog spec badge. */
  engine?: Engine
  busy: boolean
  progress: Progress | null
  error?: string
  completed?: ExportCompleted | null
  onExport: (plan: ExportPlan, unkeyed?: UnkeyedChoice) => void
  onCancelExport?: () => void
  onContinueExport?: () => void
  onFocusNodes?: (nodeIDs: readonly string[]) => void
  onClose: () => void
}) {
  const byShot = shots.length > 0
  const [chosen, setChosen] = useState<ReadonlySet<string>>(() => new Set(byShot ? shots.filter((s) => s.bound).map((s) => s.id) : cameras.map((c) => c.id)))
  const [picked, setPicked] = useState<ReadonlySet<ExportPass>>(() => new Set(defaultPasses(display)))
  const [asking, setAsking] = useState(false)
  const [exportedIDs, setExportedIDs] = useState<ReadonlySet<string>>(() => new Set())
  const [previewURLs, setPreviewURLs] = useState<{ key: string; caption: string; pass: ExportPass; url: string }[]>([])
  const dialogRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    dialogRef.current?.focus({ preventScroll: true })
  }, [])

  useEffect(() => {
    if (!completed || completed.previews.length === 0) {
      setPreviewURLs([])
      return
    }
    const items = completed.previews.map((img, idx) => ({
      key: `${img.marker ?? img.camera}-${img.pass}-${idx}`,
      caption: img.label ? `${img.label} · ${passShort[img.pass]}` : passShort[img.pass],
      pass: img.pass,
      url: URL.createObjectURL(img.blob),
    }))
    setPreviewURLs(items)
    return () => {
      for (const item of items) URL.revokeObjectURL(item.url)
    }
  }, [completed])

  const ids = byShot ? shots.filter((s) => s.bound && chosen.has(s.id)).map((s) => s.id) : cameras.filter((c) => chosen.has(c.id)).map((c) => c.id)
  const kinds = passes.filter((p) => picked.has(p.pass)).map((p) => p.pass)
  const usesTransparent = transparentBackground === true && kinds.some((k) => k === 'clay' || k === 'lit')
  const { count, problem } = exportCount(ids.length, kinds.length, byShot)
  const plan: ExportPlan = byShot ? { cameraObjectIDs: [], markerIDs: ids, passes: kinds } : { cameraObjectIDs: ids, passes: kinds }

  const allSelectable = useMemo(() => (byShot ? shots.filter((s) => s.bound).map((s) => s.id) : cameras.map((c) => c.id)), [byShot, shots, cameras])
  const selectedCount = ids.length

  const go = (choice?: UnkeyedChoice) => {
    setAsking(false)
    onExport(plan, choice)
  }
  const toggle = <T,>(set: ReadonlySet<T>, v: T, on: boolean) => {
    const next = new Set(set)
    if (on) next.add(v)
    else next.delete(v)
    return next
  }

  const handleContinue = () => {
    const nextExported = new Set([...exportedIDs, ...(completed?.exportedIDs ?? [])])
    setExportedIDs(nextExported)
    const remaining = allSelectable.filter((id) => !nextExported.has(id))
    if (remaining.length > 0) setChosen(new Set(remaining))
    onContinueExport?.()
  }

  const stage: 'configure' | 'exporting' | 'completed' = busy ? 'exporting' : completed ? 'completed' : 'configure'
  const headerTitle = stage === 'exporting' ? '正在导出分镜' : stage === 'completed' ? '导出完成' : '导出分镜'
  const pct = Math.max(0, Math.min(100, Math.round((progress?.fraction ?? 0) * 100)))
  const activeStep = !progress || progress.stage === 'prepare' || progress.stage === 'render' ? 'render' : progress.stage

  return (
    <div
      className="cc-scene-dialog-backdrop"
      onMouseDown={(e) => {
        if (!busy && e.target === e.currentTarget) onClose()
      }}
    >
      <div
        ref={dialogRef}
        className="cc-scene-dialog cc-surface cc-scene-export-dialog"
        role="dialog"
        aria-label="导出分镜"
        aria-modal="true"
        tabIndex={-1}
        onKeyDown={(e) => {
          if (e.key === 'Escape') {
            e.preventDefault()
            e.stopPropagation()
            if (asking) setAsking(false)
            else if (!busy) onClose()
          }
        }}
      >
        <header className="cc-export-header">
          <div className="cc-export-title-wrap">
            <h3>{headerTitle}</h3>
            <span className="cc-export-spec">
              {resolution.toUpperCase()} · {longSideOf(resolution)}px · {engine === 'realtime_gi' ? '实时 GI' : '工作台'}
            </span>
          </div>
          <button type="button" className="cc-icon-button" aria-label="关闭" disabled={busy} onClick={onClose}>
            <X size={16} />
          </button>
        </header>

        {stage === 'exporting' ? (
          <div className="cc-export-body cc-export-body--loading">
            <div className="cc-export-stage" aria-busy="true">
              <div className="cc-export-aura" aria-hidden="true" />
              <div className="cc-export-corners" aria-hidden="true">
                <span />
                <span />
                <span />
                <span />
                <small className="cc-export-rec">REC · {resolution.toUpperCase()}</small>
              </div>
              <svg className="cc-export-reticle" viewBox="0 0 120 120" fill="none" aria-hidden="true">
                <circle cx="60" cy="60" r="53" stroke="rgba(255,255,255,0.12)" strokeWidth="1" strokeDasharray="1.5 4.5" className="cc-export-cw-slow" />
                <circle cx="60" cy="60" r="45" stroke="rgba(255,255,255,0.09)" strokeWidth="1" />
                <circle
                  cx="60"
                  cy="60"
                  r="45"
                  stroke="var(--run, #e2a453)"
                  strokeOpacity="0.82"
                  strokeWidth="1.4"
                  strokeLinecap="round"
                  strokeDasharray="28 255"
                  className="cc-export-cw-orbit"
                />
                <circle cx="60" cy="60" r="39" stroke="rgba(255,255,255,0.14)" strokeWidth="0.8" strokeDasharray="8 12" className="cc-export-ccw-med" />
                <g>
                  {eurikaPearls.map((p, idx) => (
                    <circle
                      key={idx}
                      className="cc-export-pearl"
                      cx={p.x}
                      cy={p.y}
                      r={p.r}
                      fill={p.core ? 'var(--run, #e2a453)' : 'var(--primary, #f4f4f5)'}
                      fillOpacity={p.op}
                      style={{ animationDelay: `${p.delay.toFixed(2)}s` }}
                    />
                  ))}
                </g>
              </svg>
              <div className="cc-export-scan" aria-hidden="true" />
              <div className="cc-export-pill" role="status">
                <i aria-hidden="true" />
                <span>{progressText(progress)}</span>
              </div>
              <span className="cc-export-shot-tag">
                {progress?.currentLabel ? `${progress.currentLabel} · ${pct}%` : `${pct}%`}
              </span>
            </div>

            <div className="cc-export-pipeline">
              <div className="cc-export-steps">
                <div className={`cc-export-step${activeStep === 'render' ? ' is-active' : ' is-done'}`}>
                  <span>1. 离屏渲染</span>
                  <small>
                    {activeStep === 'render'
                      ? progress?.stage === 'render'
                        ? `${progress.done}/${progress.total}`
                        : '准备中'
                      : '✓'}
                  </small>
                </div>
                <div className={`cc-export-step${activeStep === 'upload' ? ' is-active' : activeStep === 'place' ? ' is-done' : ''}`}>
                  <span>2. 上传原图</span>
                  <small>
                    {activeStep === 'upload'
                      ? `${progress?.done ?? 0}/${progress?.total ?? 0}`
                      : activeStep === 'place'
                        ? '✓'
                        : '待命'}
                  </small>
                </div>
                <div className={`cc-export-step${activeStep === 'place' ? ' is-active' : ''}`}>
                  <span>3. 落位画布</span>
                  <small>{activeStep === 'place' ? '进行中' : '待命'}</small>
                </div>
              </div>
              <div className="cc-export-progress-track" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct} aria-label="导出总进度">
                <div className="cc-export-progress-fill" style={{ width: `${pct}%` }} />
              </div>
            </div>

            <footer className="cc-export-footer">
              <span className="cc-export-footnote">导出完成前请勿关闭当前标签页</span>
              <button type="button" className="btn" onClick={() => onCancelExport?.()} disabled={!onCancelExport}>
                取消导出
              </button>
            </footer>
          </div>
        ) : stage === 'completed' && completed ? (
          <div className="cc-export-body cc-export-body--done">
            <div className="cc-export-done-banner" role="status">
              <div className="cc-export-ok-icon" aria-hidden="true">
                <Check size={16} strokeWidth={2.5} />
              </div>
              <div className="cc-export-done-copy">
                <strong>已成功导出 {completed.nodeIDs.length} 张分镜图到画布</strong>
                <small>
                  长边 {longSideOf(resolution)}（{resolution.toUpperCase()}）· 已排布在场景节点右侧
                </small>
              </div>
            </div>

            {previewURLs.length > 0 && (
              <div className="cc-export-section">
                <div className="cc-export-section-head">
                  <span>生成节点预览（{previewURLs.length}）</span>
                  <span className="cc-export-mono">{resolution.toUpperCase()} · {longSideOf(resolution)}px</span>
                </div>
                <div className="cc-export-contact-grid">
                  {previewURLs.map((item) => (
                    <figure key={item.key} className="cc-export-thumb">
                      <img src={item.url} alt={item.caption} />
                      <figcaption>{item.caption}</figcaption>
                    </figure>
                  ))}
                </div>
              </div>
            )}

            <footer className="cc-export-footer cc-export-footer--done">
              <button type="button" className="btn cc-export-btn-ghost" onClick={onClose}>
                留在编辑器
              </button>
              <div className="cc-export-footer-actions">
                <button type="button" className="btn" onClick={handleContinue}>
                  继续导出其他分镜
                </button>
                {onFocusNodes && completed.nodeIDs.length > 0 && (
                  <button type="button" className="btn btn-primary" onClick={() => onFocusNodes(completed.nodeIDs)}>
                    前往画布定位节点
                  </button>
                )}
              </div>
            </footer>
          </div>
        ) : (
          <div className="cc-export-body">
            {error && (
              <div className="cc-export-error" role="alert">
                {error}
              </div>
            )}
            {cameras.length === 0 ? (
              <p className="cc-export-empty">场景里还没有摄像机：在「添加」菜单里添加摄像机后再导出。</p>
            ) : (
              <>
                <div className="cc-export-section">
                  <div className="cc-export-section-head">
                    <span>
                      {byShot ? '分镜' : '摄像机'}{' '}
                      <small className="cc-export-mono">
                        ({selectedCount}/{allSelectable.length})
                      </small>
                    </span>
                    <div className="cc-export-quick-pick">
                      <button type="button" onClick={() => setChosen(new Set(allSelectable))} disabled={allSelectable.length === 0}>
                        全选
                      </button>
                      <span aria-hidden="true">·</span>
                      <button type="button" onClick={() => setChosen(new Set())} disabled={selectedCount === 0}>
                        清空
                      </button>
                    </div>
                  </div>
                  <div className="cc-export-shots-grid">
                    {byShot
                      ? shots.map((s) => {
                          const on = s.bound && chosen.has(s.id)
                          return (
                            <label key={s.id} className={`cc-export-card${on ? ' is-picked' : ''}${!s.bound ? ' is-disabled' : ''}`}>
                              <input
                                type="checkbox"
                                className="cc-export-checkbox"
                                aria-label={s.title}
                                disabled={!s.bound}
                                checked={on}
                                onChange={(e) => setChosen(toggle(chosen, s.id, e.target.checked))}
                              />
                              <span className="cc-export-card-body">
                                <span className="cc-export-card-title">
                                  <span>{s.title}</span>
                                  {exportedIDs.has(s.id) && <span className="cc-export-exported-tag">已导</span>}
                                </span>
                                <small className="cc-export-card-meta">{s.bound ? (s.lensText ?? '已绑定摄像机') : '未绑定摄像机'}</small>
                              </span>
                            </label>
                          )
                        })
                      : cameras.map((c) => {
                          const on = chosen.has(c.id)
                          const title = c.camera?.label || c.name || c.id
                          return (
                            <label key={c.id} className={`cc-export-card${on ? ' is-picked' : ''}`}>
                              <input
                                type="checkbox"
                                className="cc-export-checkbox"
                                aria-label={title}
                                checked={on}
                                onChange={(e) => setChosen(toggle(chosen, c.id, e.target.checked))}
                              />
                              <span className="cc-export-card-body">
                                <span className="cc-export-card-title">
                                  <span>{title}</span>
                                  {exportedIDs.has(c.id) && <span className="cc-export-exported-tag">已导</span>}
                                </span>
                                <small className="cc-export-card-meta">{cameraLensSummary(c)}</small>
                              </span>
                            </label>
                          )
                        })}
                  </div>
                </div>

                <div className="cc-export-section">
                  <div className="cc-export-section-head">
                    <span>导出方式</span>
                  </div>
                  <div className="cc-export-passes-grid">
                    {passes.map((p) => {
                      const on = picked.has(p.pass)
                      return (
                        <label key={p.pass} className={`cc-export-pass-card${on ? ' is-picked' : ''}`}>
                          <span className="cc-export-pass-top">
                            <PassGlyph pass={p.pass} />
                            <input
                              type="checkbox"
                              className="cc-export-checkbox"
                              aria-label={p.label}
                              checked={on}
                              onChange={(e) => setPicked(toggle(picked, p.pass, e.target.checked))}
                            />
                          </span>
                          <strong>{p.label}</strong>
                          <small>{p.desc}</small>
                        </label>
                      )
                    })}
                  </div>
                </div>
              </>
            )}

            {asking ? (
              <div className="cc-export-unkeyed" role="alertdialog" aria-label="有改动还没有插入关键帧">
                <p>这些改动还没有插入关键帧，导出用的是已保存的场景：</p>
                <ul>
                  {unkeyed.map((line) => (
                    <li key={line}>{line}</li>
                  ))}
                </ul>
                <footer>
                  <button type="button" className="btn" onClick={() => setAsking(false)}>
                    取消
                  </button>
                  <button type="button" className="btn" onClick={() => go('discard')}>
                    放弃这些改动后导出
                  </button>
                  <button type="button" className="btn btn-primary" onClick={() => go('key')}>
                    插入关键帧后导出
                  </button>
                </footer>
              </div>
            ) : (
              <footer className="cc-export-footer">
                <p role="status" className={`cc-export-summary${problem ? ' is-problem' : ''}`}>
                  {cameras.length === 0
                    ? '请先添加摄像机'
                    : (problem ?? `共 ${count} 张，长边 ${longSideOf(resolution)}（${resolution.toUpperCase()}）${usesTransparent ? ' · 白模、材质光照使用透明背景' : ''}，放在场景节点右侧`)}
                </p>
                <div className="cc-export-footer-actions">
                  <button type="button" className="btn" onClick={onClose}>
                    取消
                  </button>
                  <button
                    type="button"
                    className="btn btn-primary"
                    disabled={!!problem || cameras.length === 0}
                    onClick={() => (unkeyed.length ? setAsking(true) : go())}
                  >
                    导出
                  </button>
                </div>
              </footer>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
