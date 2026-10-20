import { useState } from 'react'
import type { SceneObject } from '../api.ts'
import { defaultPasses, exportCount, type ExportPass, type ExportPlan, type Progress } from '../host/exportShots.ts'
import type { DisplayMode } from '../state/sceneStore.ts'

// The export dialog (design「导出对话框」「lit 导出与 render_view」): which
// cameras, which passes, how many images. Every camera is chosen at first;
// clay and line art always, the lit look too when the view shows materials.
// Nothing or more than 48 images cannot be exported.

const passes: { pass: ExportPass; label: string }[] = [
  { pass: 'clay', label: '白模（clay）' },
  { pass: 'line', label: '线稿' },
  { pass: 'lit', label: '材质光照' },
]

const progressText = (p: Progress | null) =>
  !p ? '准备中…' : p.stage === 'render' ? `正在渲染 ${p.done}/${p.total}` : p.stage === 'upload' ? `正在上传 ${p.done}/${p.total}` : '正在放到画布…'

export function ExportDialog({ cameras, display, busy, progress, onExport, onClose }: {
  cameras: readonly SceneObject[]
  /** The view's shading when the dialog opened. */
  display: DisplayMode
  busy: boolean
  progress: Progress | null
  onExport: (plan: ExportPlan) => void
  onClose: () => void
}) {
  const [chosen, setChosen] = useState<ReadonlySet<string>>(() => new Set(cameras.map((c) => c.id)))
  const [picked, setPicked] = useState<ReadonlySet<ExportPass>>(() => new Set(defaultPasses(display)))
  const ids = cameras.filter((c) => chosen.has(c.id)).map((c) => c.id)
  const kinds = passes.filter((p) => picked.has(p.pass)).map((p) => p.pass)
  const { count, problem } = exportCount(ids.length, kinds.length)
  const toggle = <T,>(set: ReadonlySet<T>, v: T, on: boolean) => {
    const next = new Set(set)
    if (on) next.add(v)
    else next.delete(v)
    return next
  }
  return (
    <div className="cc-scene-dialog-backdrop">
      <div className="cc-scene-dialog" role="dialog" aria-label="导出分镜">
        <h3>导出分镜</h3>
        {cameras.length === 0 ? (
          <p>场景里还没有摄像机：在「添加」菜单里添加摄像机后再导出。</p>
        ) : (
          <>
            <fieldset disabled={busy}>
              <legend>摄像机</legend>
              {cameras.map((c) => (
                <label key={c.id}>
                  <input type="checkbox" checked={chosen.has(c.id)} onChange={(e) => setChosen(toggle(chosen, c.id, e.target.checked))} />
                  {c.camera?.label || c.name || c.id}
                </label>
              ))}
            </fieldset>
            <fieldset disabled={busy}>
              <legend>导出方式</legend>
              {passes.map((p) => (
                <label key={p.pass}>
                  <input type="checkbox" checked={picked.has(p.pass)} onChange={(e) => setPicked(toggle(picked, p.pass, e.target.checked))} />
                  {p.label}
                </label>
              ))}
            </fieldset>
            <p role="status">{busy ? progressText(progress) : (problem ?? `共 ${count} 张，长边 2048，放在场景节点右侧`)}</p>
          </>
        )}
        <footer>
          <button className="btn" onClick={onClose} disabled={busy}>
            取消
          </button>
          <button className="btn btn-primary" disabled={busy || !!problem || cameras.length === 0} onClick={() => onExport({ cameraObjectIDs: ids, passes: kinds })}>
            导出
          </button>
        </footer>
      </div>
    </div>
  )
}
