import {
  useLayoutEffect,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
} from 'react'
import { createPortal } from 'react-dom'
import { LoaderCircle, X } from 'lucide-react'
import { useFocusTrap } from '../../components/useFocusTrap'
import type { CanvasNode } from './api.ts'
import { MediaFigure } from './MediaView.tsx'
import { imageEditTransport, useEditRun, type CropSession } from './imageEdit.ts'
import {
  compressOutputNote,
  cropOutputNote,
  cropRatios,
  formatBytes,
  formatLabel,
  handles,
  isWholeImage,
  moveBox,
  originalOf,
  outcomeSummary,
  phaseLabel,
  qualityDefault,
  qualityPresets,
  ratioValue,
  reductionPercent,
  resizeBox,
  roundBox,
  type Box,
  type CropRatio,
  type Handle,
  type Outcome,
  type Size,
} from './imageEditModel.ts'
import './imageEdit.css'

// 图片编辑一期（V6-076）：压缩面板、画布内裁切。处理规则在服务端，这里只选参数、
// 等结果、展示真实文件的大小与格式。

// ---- 压缩 ----

export function CompressPanel({
  node,
  account,
  canvasID,
  dataRevision,
  onClose,
  onNotice,
}: {
  node: CanvasNode
  account: string
  canvasID: string
  dataRevision: () => string
  onClose: () => void
  onNotice: (message: string) => void
}) {
  // 打开时固定源图：之后节点怎么变，这次压缩都只针对它。
  const [source] = useState(() => node.content)
  const original = originalOf(source)
  const [quality, setQuality] = useState(qualityDefault)
  const [typed, setTyped] = useState(String(qualityDefault))
  const [outcome, setOutcome] = useState<Outcome | null>(null)
  const open = useRef(true)
  const { phase, run, cancel } = useEditRun(account, (o) => {
    if (open.current) setOutcome(o)
    // 面板已经关了：结果用提示告诉摄影师，不能悄悄吞掉。
    else onNotice(outcomeSummary(o, 'compress'))
  })
  const close = () => {
    if (phase && !cancel()) {
      open.current = false
      onNotice('压缩结果正在确认，完成后会提示')
    }
    onClose()
  }
  const ref = useFocusTrap<HTMLDivElement>(true, close)
  const working = phase !== null
  const applied = outcome?.kind === 'applied'
  const apply = () => {
    if (!source || working) return
    setOutcome(null)
    run({ canvasID, nodeID: node.id, sourceRevisionID: source.id, action: 'compress', parameters: { quality }, dataRevision })
  }
  const discardBusy = (id: string) => {
    void imageEditTransport(account)
      .discard(id)
      .catch(() => undefined)
      .then(apply)
  }
  const setValue = (v: number) => {
    const q = Math.min(100, Math.max(1, Math.round(v)))
    setQuality(q)
    setTyped(String(q))
  }
  const out = applied ? outcome.render.output : undefined
  const src = applied ? (outcome.render.source_size ?? original?.byte_size ?? 0) : 0
  return createPortal(
    <div
      className="overlay open cc-dialog-overlay cc-compress-overlay"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) close()
      }}
    >
      <div
        ref={ref}
        className="dialog cc-editor-dialog cc-surface cc-compress-dialog"
        role="dialog"
        aria-modal="true"
        aria-label="压缩图片"
        tabIndex={-1}
      >
        <header>
          <h2>压缩图片</h2>
          <button className="cc-icon-button" aria-label="关闭压缩" onClick={close}>
            <X size={18} />
          </button>
        </header>
        <div className="cc-dialog-body cc-compress">
          <dl className="cc-compress-source">
            <div>
              <dt>原始大小</dt>
              <dd>{original ? formatBytes(original.byte_size) : '—'}</dd>
            </div>
            <div>
              <dt>格式</dt>
              <dd>{original ? formatLabel(original.mime) : '—'}</dd>
            </div>
            <div>
              <dt>像素尺寸</dt>
              <dd>{original?.width ? `${original.width} × ${original.height}` : '—'}</dd>
            </div>
          </dl>
          <p className="cc-compress-format">
            {original ? compressOutputNote(original.mime) : ''}。输出不保留拍摄信息（EXIF/GPS），原图保留在版本里。
          </p>
          <fieldset className="cc-compress-quality" disabled={working || applied}>
            <legend>输出质量</legend>
            <div className="cc-compress-presets" role="group" aria-label="快捷档位">
              {qualityPresets.map((p) => (
                <button
                  key={p.value}
                  type="button"
                  aria-pressed={quality === p.value}
                  onClick={() => setValue(p.value)}
                >
                  {p.label} {p.value}
                </button>
              ))}
            </div>
            <div className="cc-compress-slider">
              <input
                type="range"
                min={1}
                max={100}
                step={1}
                value={quality}
                aria-label="输出质量"
                onChange={(e) => setValue(Number(e.target.value))}
              />
              <input
                type="number"
                min={1}
                max={100}
                step={1}
                inputMode="numeric"
                aria-label="输出质量数值"
                value={typed}
                onChange={(e) => {
                  setTyped(e.target.value)
                  const n = Number(e.target.value)
                  if (e.target.value !== '' && Number.isFinite(n) && n >= 1 && n <= 100) setQuality(Math.round(n))
                }}
                onBlur={() => setTyped(String(quality))}
                onKeyDown={(e) => {
                  // 输入框里 Enter 只确认数值，不触发应用（产品稿 §3.2）。
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    setTyped(String(quality))
                  }
                }}
              />
            </div>
            <p className="cc-compress-hint">质量越低，通常文件越小，但细节损失更多。像素尺寸保持不变。</p>
          </fieldset>
          {outcome && (
            <div
              className={`cc-compress-result ${applied ? 'is-applied' : 'is-problem'}`}
              role={applied ? 'status' : 'alert'}
            >
              {applied && out ? (
                <>
                  <strong>
                    {formatBytes(src)} → {formatBytes(out.size)}
                  </strong>
                  <span>
                    减少 {reductionPercent(src, out.size)}% · 输出 {formatLabel(out.mime)} · {out.width} × {out.height}
                  </span>
                  <small>已作为新版本应用，可撤销或在版本里切回原图。</small>
                </>
              ) : (
                <span>{outcomeSummary(outcome, 'compress')}</span>
              )}
              {outcome.kind === 'failed' && outcome.busyRenderID && (
                <button type="button" className="btn btn-sm" onClick={() => discardBusy(outcome.busyRenderID!)}>
                  放弃那项编辑并重试
                </button>
              )}
            </div>
          )}
          <footer className="cc-compress-actions">
            {applied ? (
              <button type="button" className="btn btn-primary" onClick={onClose}>
                完成
              </button>
            ) : (
              <>
                <button type="button" className="btn" onClick={close}>
                  取消
                </button>
                <button
                  type="button"
                  className="btn btn-primary"
                  disabled={working || !source || !original}
                  onClick={apply}
                >
                  {working ? (
                    <>
                      <LoaderCircle size={14} className="cc-spin" aria-hidden="true" />
                      {phase === 'encoding' ? '正在压缩' : phaseLabel[phase]}
                    </>
                  ) : (
                    '应用压缩'
                  )}
                </button>
              </>
            )}
          </footer>
        </div>
      </div>
    </div>,
    document.querySelector('.cc-workspace') ?? document.body,
  )
}

// ---- 裁切 ----

/** 裁切工具条：取消、比例、重置、输出尺寸、应用。替换选区工具条的位置。 */
export function CropControls({
  session,
  onCancel,
  onRatio,
  onReset,
  onApply,
  onDiscardBusy,
}: {
  session: CropSession
  onCancel: () => void
  onRatio: (r: CropRatio) => void
  onReset: () => void
  onApply: () => void
  onDiscardBusy: (id: string) => void
}) {
  const out = roundBox(session.box, session.size)
  const working = session.phase !== null
  const whole = isWholeImage(session.box, session.size)
  return (
    <>
      <button type="button" className="cc-tool" onClick={onCancel}>
        取消
      </button>
      <span className="cc-tool-sep" />
      <label className="cc-crop-ratio">
        <select
          aria-label="裁切比例"
          value={session.ratio}
          disabled={working}
          onChange={(e) => onRatio(e.target.value as CropRatio)}
        >
          {cropRatios.map((r) => (
            <option key={r.key} value={r.key}>
              {r.label}
            </option>
          ))}
        </select>
      </label>
      <button type="button" className="cc-tool" disabled={working} onClick={onReset}>
        重置
      </button>
      <span className="cc-crop-size" aria-live="polite" title={cropOutputNote(originalOf(session.source)?.mime ?? '')}>
        {out.width} × {out.height}
      </span>
      {session.error && (
        <span className="cc-crop-error" role="alert">
          {session.error.message}
          {session.error.busyRenderID && (
            <button type="button" className="cc-tool" onClick={() => onDiscardBusy(session.error!.busyRenderID!)}>
              放弃那项编辑并重试
            </button>
          )}
        </span>
      )}
      <button
        type="button"
        className="cc-tool cc-crop-apply"
        disabled={working || whole}
        title={whole ? '选区与原图相同，无需裁切' : undefined}
        onClick={onApply}
      >
        {working ? (
          <>
            <LoaderCircle size={14} className="cc-spin" aria-hidden="true" />
            {session.phase === 'encoding' ? '正在裁切' : phaseLabel[session.phase!]}
          </>
        ) : (
          '应用裁切'
        )}
      </button>
    </>
  )
}

/**
 * 节点内的裁切画面。坐标始终是源图像素；屏幕位移按图片实际显示宽度换算，
 * 所以画布缩放、高 DPI 都不改变裁切范围。
 */
export function CropStage({
  session,
  title,
  onChange,
}: {
  session: CropSession
  title: string
  onChange: (box: Box) => void
}) {
  const stage = useRef<HTMLDivElement>(null)
  const frame = useRef<HTMLDivElement>(null)
  const [fit, setFit] = useState<Size | null>(null)
  const [dragging, setDragging] = useState(false)
  const drag = useRef<{ kind: 'move' | Handle; x: number; y: number; start: Box; scale: number } | null>(null)
  const { size, box } = session
  const ratio = ratioValue(session.ratio, size)
  const locked = session.phase !== null
  useLayoutEffect(() => {
    const el = stage.current
    if (!el) return
    // 图片在节点里按 contain 摆放；裁切层只覆盖图片本身。
    const measure = () => {
      const s = Math.min(el.clientWidth / size.width, el.clientHeight / size.height)
      setFit({ width: size.width * s, height: size.height * s })
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(el)
    return () => observer.disconnect()
  }, [size.width, size.height])
  const begin = (kind: 'move' | Handle) => (e: ReactPointerEvent) => {
    if (locked || e.button !== 0) return
    e.stopPropagation()
    e.preventDefault()
    const rect = frame.current?.getBoundingClientRect()
    if (!rect?.width) return
    ;(e.currentTarget as Element).setPointerCapture(e.pointerId)
    drag.current = { kind, x: e.clientX, y: e.clientY, start: box, scale: size.width / rect.width }
    setDragging(true)
  }
  const move = (e: ReactPointerEvent) => {
    const d = drag.current
    if (!d) return
    e.stopPropagation()
    const dx = (e.clientX - d.x) * d.scale
    const dy = (e.clientY - d.y) * d.scale
    onChange(d.kind === 'move' ? moveBox(d.start, dx, dy, size) : resizeBox(d.start, d.kind, dx, dy, ratio, size))
  }
  const end = (e: ReactPointerEvent) => {
    if (!drag.current) return
    e.stopPropagation()
    drag.current = null
    setDragging(false)
  }
  const pct = (v: number, of: number) => `${(v / of) * 100}%`
  const out = roundBox(box, size)
  return (
    <div ref={stage} className="cc-crop-stage nodrag nopan nowheel" onDoubleClick={(e) => e.stopPropagation()}>
      {fit && (
        <div ref={frame} className="cc-crop-frame" style={{ width: fit.width, height: fit.height }}>
          <MediaFigure content={session.source} kind="image" title={title} />
          <div
            className={`cc-crop-box ${dragging ? 'is-dragging' : ''}`}
            style={{
              left: pct(box.left, size.width),
              top: pct(box.top, size.height),
              width: pct(box.width, size.width),
              height: pct(box.height, size.height),
            }}
            role="group"
            aria-label={`裁切范围 ${out.width} × ${out.height}`}
            tabIndex={0}
            onPointerDown={begin('move')}
            onPointerMove={move}
            onPointerUp={end}
            onPointerCancel={end}
            onKeyDown={(e) => {
              // 方向键移动选区：每次 1 个屏幕像素对应的源像素，Shift 十倍。
              const step: Record<string, [number, number]> = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }
              const s = step[e.key]
              const rect = frame.current?.getBoundingClientRect()
              if (!s || locked || !rect?.width) return
              e.preventDefault()
              e.stopPropagation()
              const k = (e.shiftKey ? 10 : 1) * (size.width / rect.width)
              onChange(moveBox(box, s[0] * k, s[1] * k, size))
            }}
          >
            <span className="cc-crop-thirds" aria-hidden="true" />
            {handles.map((h) => (
              <span
                key={h}
                className={`cc-crop-handle is-${h}`}
                aria-hidden="true"
                onPointerDown={begin(h)}
                onPointerMove={move}
                onPointerUp={end}
                onPointerCancel={end}
              />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
