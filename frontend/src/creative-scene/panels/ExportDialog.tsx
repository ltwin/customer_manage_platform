import { useState } from 'react'
import type { SceneObject } from '../api.ts'
import { defaultPasses, exportCount, type ExportPass, type ExportPlan, type Progress } from '../host/exportShots.ts'
import { longSideOf, type Resolution } from '../render/engines.ts'
import type { DisplayMode } from '../state/sceneStore.ts'

// The export dialog (design「导出对话框」「lit 导出与 render_view」): which
// cameras, which passes, how many images. Every camera is chosen at first;
// clay and line art always, the lit look too when the view shows materials.
// Nothing or more than 48 images cannot be exported. A scene with shots
// lists its shots instead, every one with a camera chosen
// (scene-timeline-shots); changes waiting for a key are asked about first,
// since the export draws the saved scene (AC-8b).

/** A shot as the dialog lists it. */
export type ExportShot = { id: string; title: string; bound: boolean }

/** What to do with changes not keyed yet before exporting. */
export type UnkeyedChoice = 'key' | 'discard'

const passes: { pass: ExportPass; label: string }[] = [
  { pass: 'clay', label: '白模（clay）' },
  { pass: 'line', label: '线稿' },
  { pass: 'lit', label: '材质光照' },
]

const progressText = (p: Progress | null) =>
  !p ? '准备中…' : p.stage === 'render' ? `正在渲染 ${p.done}/${p.total}` : p.stage === 'upload' ? `正在上传 ${p.done}/${p.total}` : '正在放到画布…'

export function ExportDialog({ cameras, shots = [], unkeyed = [], display, resolution, busy, progress, onExport, onClose }: {
  cameras: readonly SceneObject[]
  /** The scene's shots in frame order; none for a scene exported by camera. */
  shots?: readonly ExportShot[]
  /** The changes waiting for a key, as the prompt names them. */
  unkeyed?: readonly string[]
  /** The view's shading when the dialog opened. */
  display: DisplayMode
  /** The scene's output resolution: the images' long side. */
  resolution: Resolution
  busy: boolean
  progress: Progress | null
  onExport: (plan: ExportPlan, unkeyed?: UnkeyedChoice) => void
  onClose: () => void
}) {
  const byShot = shots.length > 0
  const [chosen, setChosen] = useState<ReadonlySet<string>>(() => new Set(byShot ? shots.filter((s) => s.bound).map((s) => s.id) : cameras.map((c) => c.id)))
  const [picked, setPicked] = useState<ReadonlySet<ExportPass>>(() => new Set(defaultPasses(display)))
  const [asking, setAsking] = useState(false)
  const ids = byShot ? shots.filter((s) => s.bound && chosen.has(s.id)).map((s) => s.id) : cameras.filter((c) => chosen.has(c.id)).map((c) => c.id)
  const kinds = passes.filter((p) => picked.has(p.pass)).map((p) => p.pass)
  const { count, problem } = exportCount(ids.length, kinds.length, byShot)
  const plan: ExportPlan = byShot ? { cameraObjectIDs: [], markerIDs: ids, passes: kinds } : { cameraObjectIDs: ids, passes: kinds }
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
  return (
    <div className="cc-scene-dialog-backdrop">
      <div className="cc-scene-dialog" role="dialog" aria-label="导出分镜">
        <h3>导出分镜</h3>
        {cameras.length === 0 ? (
          <p>场景里还没有摄像机：在「添加」菜单里添加摄像机后再导出。</p>
        ) : (
          <>
            {byShot ? (
              <fieldset disabled={busy}>
                <legend>分镜</legend>
                {shots.map((s) => (
                  <label key={s.id}>
                    <input type="checkbox" disabled={!s.bound} checked={s.bound && chosen.has(s.id)} onChange={(e) => setChosen(toggle(chosen, s.id, e.target.checked))} />
                    {s.title}
                    {!s.bound && '（未绑定摄像机）'}
                  </label>
                ))}
              </fieldset>
            ) : (
              <fieldset disabled={busy}>
                <legend>摄像机</legend>
                {cameras.map((c) => (
                  <label key={c.id}>
                    <input type="checkbox" checked={chosen.has(c.id)} onChange={(e) => setChosen(toggle(chosen, c.id, e.target.checked))} />
                    {c.camera?.label || c.name || c.id}
                  </label>
                ))}
              </fieldset>
            )}
            <fieldset disabled={busy}>
              <legend>导出方式</legend>
              {passes.map((p) => (
                <label key={p.pass}>
                  <input type="checkbox" checked={picked.has(p.pass)} onChange={(e) => setPicked(toggle(picked, p.pass, e.target.checked))} />
                  {p.label}
                </label>
              ))}
            </fieldset>
            <p role="status">{busy ? progressText(progress) : (problem ?? `共 ${count} 张，长边 ${longSideOf(resolution)}（${resolution.toUpperCase()}），放在场景节点右侧`)}</p>
          </>
        )}
        {asking ? (
          <div role="alertdialog" aria-label="有改动还没有插入关键帧">
            <p>这些改动还没有插入关键帧，导出用的是已保存的场景：</p>
            <ul>
              {unkeyed.map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
            <footer>
              <button className="btn" onClick={() => setAsking(false)}>
                取消
              </button>
              <button className="btn" onClick={() => go('discard')}>
                放弃这些改动后导出
              </button>
              <button className="btn btn-primary" onClick={() => go('key')}>
                插入关键帧后导出
              </button>
            </footer>
          </div>
        ) : (
          <footer>
            <button className="btn" onClick={onClose} disabled={busy}>
              取消
            </button>
            <button className="btn btn-primary" disabled={busy || !!problem || cameras.length === 0} onClick={() => (unkeyed.length ? setAsking(true) : go())}>
              导出
            </button>
          </footer>
        )}
      </div>
    </div>
  )
}
