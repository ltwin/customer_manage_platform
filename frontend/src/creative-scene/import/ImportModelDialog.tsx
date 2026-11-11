import { useEffect, useMemo, useRef, useState } from 'react'
import type { Group } from 'three'
import { clone } from 'three/examples/jsm/utils/SkeletonUtils.js'
import { currentAccount, send } from '../../creative-canvas/editor/api.ts'
import { fileTitle } from '../../creative-canvas/editor/fileImport.ts'
import { discardCandidate, readableFileName, uploadGenerated } from '../../creative-canvas/editor/media.ts'
import StudioDialog from '../../creative-canvas/editor/StudioDialog.tsx'
import { renderMaterialThumbnail } from '../library/thumbnail.ts'
import { forgetLocalImage, provideLocalImage } from '../materials/images.ts'
import { partsOf } from '../models/cache.ts'
import { parseModelGLB } from '../sandbox/gltf.ts'
import { convert, toBottomCenter, type Conversion, type ReportItem } from './convert.ts'
import { bundledDecoders } from './decoders.ts'
import { browserCodec, prepareImages, type PreparedImage } from './images.ts'
import { checkMapping, fileConstraints, fingerCount, followClone, jointObjects, submittedRig } from './mapping.ts'
import { ImportRejected, parseImport, type ImportRig } from './parse.ts'
import RigMapping from './RigMapping.tsx'
import { ImportRun, type ImportJob, type ImportNetwork, type ImportPayload, type ImportResult, type RunState } from './run.ts'
import { writeGLB, writtenRig, type Written } from './write.ts'
import './import.css'

// 导入模型（GLB）到资产库：读文件、换算、给摄影师看换算报告，确认后上传并提交
// （import/run.ts 管提交边界）。资产库页面和场景编辑器共用；画布页按需加载它，
// three.js 不进画布页的主包。带骨骼的模型另外给出人形映射的结果，不成立时可以
// 手动指定或按非人形骨架导入（import/mapping.ts）。

type Mount = ImportPayload['mount']
type Prepared = {
  conversion: Conversion
  /** The joints tracked: any mapping, found or set by hand, follows them into the GLB. */
  written: Written
  images: PreparedImage[]
  report: ReportItem[]
  copyright: string
  rig: ImportRig | null
}

const vias = { vrm1: 'VRM 1.0', vrm0: 'VRM 0.x', mixamo: 'Mixamo 命名', ue: 'UE 小白人命名' } as const

const mounts: { value: NonNullable<Mount>; label: string }[] = [
  { value: 'floor', label: '放在地面上' },
  { value: 'wall', label: '贴在墙上' },
  { value: 'ceiling', label: '吊在天花板上' },
  { value: 'free', label: '自由放置' },
]

/** 每个阶段之间让出主线程，进度先画出来（design「换算的实现要点」线程）。 */
const breathe = () => new Promise((r) => setTimeout(r, 0))

async function prepare(file: File, onStage: (s: string) => void): Promise<Prepared> {
  onStage('正在读取模型…')
  const parsed = await parseImport(await file.arrayBuffer(), bundledDecoders)
  await breathe()
  onStage('正在换算材质和几何…')
  const conversion = convert(parsed, { originToBottomCenter: false })
  await breathe()
  const written = await writeGLB(conversion.scene, parsed.rig ? jointObjects(parsed.rig) : [])
  await breathe()
  onStage('正在检查贴图…')
  const { images, report } = await prepareImages(conversion.images, parsed.images, browserCodec)
  return { conversion, written, images, report: [...conversion.report, ...report], copyright: parsed.json.asset?.copyright ?? '', rig: parsed.rig }
}

const network: ImportNetwork = {
  upload: (file, declared, signal) => uploadGenerated(file, declared, { kind: 'staged' }, signal),
  discard: (u) => (u.publication?.kind === 'candidate' ? discardCandidate(u.publication.id, u.publication.revision) : Promise.resolve()),
  submit: (body, operationID) => {
    const account = currentAccount()
    if (!account) return Promise.reject(new Error('登录已失效，请重新登录'))
    return send<ImportResult>(account, '/assets/from-glb', body, operationID)
  },
  wait: (ms) => new Promise((r) => setTimeout(r, ms)),
  now: () => Date.now(),
}

const validURL = (s: string) => {
  try {
    const u = new URL(s)
    return (u.protocol === 'http:' || u.protocol === 'https:') && s.length <= 2048
  } catch {
    return false
  }
}

function progressText(s: RunState): string {
  switch (s.phase) {
    case 'uploading':
      return `正在上传（${s.done} / ${s.total}）…`
    case 'thumbnail':
      return '正在生成缩略图…'
    case 'committing':
      return '正在保存到资产库…'
    default:
      return ''
  }
}

export default function ImportModelDialog({
  file,
  groupIDs,
  onImported,
  onClose,
}: {
  file: File
  /** 资产库里导入时所在的分组。 */
  groupIDs?: string[]
  onImported: (result: ImportResult, title: string) => void
  onClose: () => void
}) {
  const [prepared, setPrepared] = useState<Prepared | null>(null)
  const [stage, setStage] = useState('正在读取模型…')
  const [refused, setRefused] = useState<string | null>(null)
  const [title, setTitle] = useState(() => fileTitle(file.name))
  const [attribution, setAttribution] = useState('')
  const [sourceURL, setSourceURL] = useState('')
  const [mount, setMount] = useState<NonNullable<Mount>>('floor')
  const [origin, setOrigin] = useState(false)
  const [humanoid, setHumanoid] = useState<Record<string, number>>({})
  // false：按非人形骨架导入。
  const [asHumanoid, setAsHumanoid] = useState(true)
  const [mapping, setMapping] = useState(false)
  const [manual, setManual] = useState(false)
  const [run, setRun] = useState<{ run: ImportRun; state: RunState } | null>(null)
  const [message, setMessage] = useState('')
  // 点了「导入」、还在按原点重写 GLB：已经锁定，关闭就放弃，没有东西要清理。
  const [starting, setStarting] = useState(false)
  const abandoned = useRef(false)
  const imported = useRef(false)

  useEffect(() => {
    let live = true
    prepare(file, (s) => live && setStage(s)).then(
      (p) => {
        if (!live) return
        setPrepared(p)
        if (p.rig) setHumanoid(p.rig.detection.humanoid)
        setAttribution((a) => a || [...p.copyright].slice(0, 2000).join(''))
      },
      (e: unknown) => live && setRefused(e instanceof ImportRejected ? e.message : `无法读取这个模型：${e instanceof Error ? e.message : String(e)}`),
    )
    return () => {
      live = false
    }
  }, [file])

  const rig = prepared?.rig ?? null
  const constraints = useMemo(() => (rig ? fileConstraints(rig) : null), [rig])
  const check = useMemo(() => (prepared?.rig && asHumanoid ? checkMapping(prepared.rig, prepared.conversion.scene, humanoid) : null), [prepared, asHumanoid, humanoid])
  // 只有成立的映射才能提交；非人形骨架照常导入。
  const rigReady = !check || check.problem === null

  const state = run?.state
  const working = state !== undefined && ['uploading', 'thumbnail', 'committing', 'unknown'].includes(state.phase)
  // 上传和提交中关闭或刷新页面，先让浏览器问一声；结束后撤掉。
  useEffect(() => {
    if (!working) return
    const leave = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', leave)
    return () => window.removeEventListener('beforeunload', leave)
  }, [working])

  useEffect(() => {
    if (state?.phase === 'done' && !imported.current) {
      imported.current = true
      onImported(state.result, title.trim())
    }
    if (state?.phase === 'failed') setMessage(state.message)
    if (state?.phase === 'cancelled') setMessage('已取消，上传的文件已丢弃')
  }, [state, onImported, title])

  // 上传中关闭等于取消；提交中不能取消，结果可能已经写入。
  const close = () => {
    if (working) {
      run?.run.cancel()
      return
    }
    abandoned.current = true
    onClose()
  }

  async function start() {
    if (!prepared) return
    const name = title.trim()
    if (!name) return setMessage('请填写资产名称')
    if (sourceURL.trim() && !validURL(sourceURL.trim())) return setMessage('来源页面须是 http 或 https 开头的网址')
    if (!rigReady) return setMessage('人形映射还不成立：请手动指定，或按非人形骨架导入')
    setMessage('')
    setStarting(true)
    let written = prepared.written
    let objects = rig?.objects
    if (origin) {
      // 换算结果保持原样，下次不勾时还能用。带骨骼的网格要连骨架一起复制。
      const moved = clone(prepared.conversion.scene) as Group
      toBottomCenter(moved)
      objects = rig ? followClone(rig.objects, prepared.conversion.scene, moved) : undefined
      try {
        written = await writeGLB(moved, rig && objects ? jointObjects(rig, objects) : [])
      } catch (e) {
        setStarting(false)
        return setMessage(e instanceof Error ? e.message : String(e))
      }
      if (abandoned.current) return
    }
    const fileRig = rig ? submittedRig(check ? check.humanoid : null, constraints?.constraints ?? []) : undefined
    const job: ImportJob = {
      glb: written.glb,
      fileName: [...readableFileName(file.name)].slice(0, 255).join(''),
      images: prepared.images,
      slots: prepared.conversion.slots,
      async thumbnail(materials, revisions) {
        const provided: string[] = []
        for (const image of prepared.images) {
          const revision = revisions.get(image.images[0]!)
          if (!revision) continue
          provideLocalImage(revision, new Blob([image.bytes as Uint8Array<ArrayBuffer>], { type: image.mime }))
          provided.push(revision)
        }
        try {
          const { scene, identities } = await parseModelGLB(written.glb)
          return await renderMaterialThumbnail(partsOf(scene, identities), written.manifest, materials)
        } finally {
          provided.forEach(forgetLocalImage)
        }
      },
      fields: {
        mount,
        ...(fileRig && objects ? { rig: writtenRig(fileRig, objects, written.nodes) } : {}),
        import: {
          file_name: [...readableFileName(file.name)].slice(0, 255).join('') || 'model.glb',
          ...(attribution.trim() ? { attribution: attribution.trim() } : {}),
          ...(sourceURL.trim() ? { source_url: sourceURL.trim() } : {}),
        },
        target: { title: name, ...(groupIDs?.length ? { group_ids: groupIDs } : {}) },
      },
    }
    const next = new ImportRun(job, network, (s) => setRun((r) => (r ? { ...r, state: s } : r)))
    setRun({ run: next, state: { phase: 'uploading', done: 0, total: 0 } })
    setStarting(false)
    await next.start()
  }

  const summary = prepared && [`${prepared.conversion.slots.size} 个材质槽位`, `${prepared.images.length} 张贴图`, `${prepared.written.manifest.triangles.toLocaleString()} 个三角形`].join(' · ')
  const changed = prepared?.report.filter((r) => r.level === 'changed') ?? []
  const ignored = prepared?.report.filter((r) => r.level === 'ignored') ?? []
  const line = (r: ReportItem, i: number) => <li key={i}>{r.slot ? `${r.slot}：${r.text}` : r.text}</li>
  const editable = !starting && !working && state?.phase !== 'done'
  const rigNotes = [...(check?.dropped.map((d) => `${d.message}，已从映射里去掉`) ?? []), ...(constraints?.notes ?? [])]

  return (
    <StudioDialog title="导入模型" onClose={close}>
      <form
        className="cc-form cc-import-model"
        noValidate
        onSubmit={(e) => {
          e.preventDefault()
          if (editable) void start()
        }}
      >
        <p className="cc-muted">{readableFileName(file.name)}</p>
        {refused ? (
          <p className="cc-import-refused" role="alert">
            {refused}
          </p>
        ) : !prepared ? (
          <p className="cc-muted" role="status">
            {stage}
          </p>
        ) : (
          <>
            <p>{summary}</p>
            {(changed.length > 0 || ignored.length > 0) && (
              <div className="cc-import-report" aria-label="换算报告">
                {changed.length > 0 && (
                  <>
                    <h3>换算过的</h3>
                    <ul>{changed.map(line)}</ul>
                  </>
                )}
                {ignored.length > 0 && (
                  <>
                    <h3>没有导入的</h3>
                    <ul>{ignored.map(line)}</ul>
                  </>
                )}
              </div>
            )}
            {rig && (
              <div className="cc-import-rig" aria-label="骨骼">
                <h3>骨骼</h3>
                <p>
                  {rig.skeleton.joints.length} 根骨骼
                  {constraints && constraints.constraints.length > 0 && ` · ${constraints.constraints.length} 条节点约束`}
                </p>
                {!check ? (
                  <p>按非人形骨架导入：姿态只能在同一套骨骼上复用</p>
                ) : check.problem === null ? (
                  <p>
                    人形映射（{manual || !rig.detection.via ? '手动指定' : vias[rig.detection.via]}）：{Object.keys(check.humanoid).length} 根标准骨骼 ·{' '}
                    {fingerCount(check.humanoid) > 0 ? `手指 ${fingerCount(check.humanoid)} / 30` : '没有手指'}
                  </p>
                ) : (
                  <p className="cc-import-refused" role="alert">
                    人形映射不成立：{check.problem}
                  </p>
                )}
                {rigNotes.length > 0 && <ul>{rigNotes.map((n, i) => <li key={i}>{n}</li>)}</ul>}
                {check && check.nonUniform.length > 0 && <p className="cc-muted">这些骨骼带非均匀缩放，摆姿态时可能变形：{check.nonUniform.join('、')}</p>}
                <div className="cc-import-rig-actions">
                  {asHumanoid ? (
                    <>
                      <button type="button" className="btn" disabled={!editable} onClick={() => setMapping((m) => !m)}>
                        {mapping ? '收起手动指定' : '手动指定'}
                      </button>
                      {check?.problem && (
                        <button
                          type="button"
                          className="btn"
                          disabled={!editable}
                          onClick={() => {
                            setAsHumanoid(false)
                            setMapping(false)
                          }}
                        >
                          按非人形骨架导入
                        </button>
                      )}
                    </>
                  ) : (
                    <button type="button" className="btn" disabled={!editable} onClick={() => setAsHumanoid(true)}>
                      改回人形映射
                    </button>
                  )}
                </div>
                {asHumanoid && mapping && (
                  <RigMapping
                    skeleton={rig.skeleton}
                    humanoid={humanoid}
                    disabled={!editable}
                    onChange={(h) => {
                      setManual(true)
                      setHumanoid(h)
                    }}
                  />
                )}
              </div>
            )}
            <label>
              资产名称
              <input className="input" name="title" value={title} maxLength={200} disabled={!editable} onChange={(e) => setTitle(e.target.value)} />
            </label>
            <label>
              授权说明
              <textarea className="input" name="attribution" rows={2} value={attribution} maxLength={2000} disabled={!editable} placeholder="例如：作者、许可证（CC BY 4.0）" onChange={(e) => setAttribution(e.target.value)} />
            </label>
            <label>
              来源页面
              <input className="input" name="source_url" type="url" value={sourceURL} maxLength={2048} disabled={!editable} placeholder="https://" onChange={(e) => setSourceURL(e.target.value)} />
            </label>
            <label>
              贴附方式
              <select className="input" name="mount" value={mount} disabled={!editable} onChange={(e) => setMount(e.target.value as NonNullable<Mount>)}>
                {mounts.map((m) => (
                  <option key={m.value} value={m.value}>
                    {m.label}
                  </option>
                ))}
              </select>
            </label>
            <label className="cc-import-check">
              <input type="checkbox" name="origin" checked={origin} disabled={!editable} onChange={(e) => setOrigin(e.target.checked)} />
              原点移到底面中心
            </label>
          </>
        )}
        {state && working && (
          <p className="cc-muted" role="status">
            {state.phase === 'unknown' ? '网络一直没有回应，不确定是否已经存入。可以重试（不会重复存入），或关闭后到资产库查看。' : progressText(state)}
          </p>
        )}
        {message && (
          <p className="cc-muted" role="status">
            {message}
          </p>
        )}
        <div className="cc-import-actions">
          {state?.phase === 'unknown' ? (
            <>
              <button type="button" className="btn" onClick={onClose}>
                关闭
              </button>
              <button type="button" className="btn btn-primary" onClick={() => void run?.run.retry()}>
                重试
              </button>
            </>
          ) : (
            <>
              <button type="button" className="btn" disabled={working && !run?.run.cancellable} onClick={close}>
                {starting || working ? '取消导入' : '关闭'}
              </button>
              {!refused && (
                <button className="btn btn-primary" disabled={!prepared || !editable || !rigReady}>
                  {starting || working ? '正在导入…' : '导入'}
                </button>
              )}
            </>
          )}
        </div>
      </form>
    </StudioDialog>
  )
}
