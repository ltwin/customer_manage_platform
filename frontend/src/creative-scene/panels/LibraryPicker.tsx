import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import type { components } from '../../api/schema'
import * as api from '../../creative-canvas/editor/api.ts'
import { MediaFigure } from '../../creative-canvas/editor/MediaView.tsx'
import { fileTitle } from '../../creative-canvas/editor/fileImport.ts'
import { uploadFile, type MediaCapabilities, type UploadProgress } from '../../creative-canvas/editor/media.ts'
import { blank, type LibraryQuery } from '../../creative-canvas/assets/query.ts'
import { useAssetList } from '../../creative-canvas/assets/useLibrary.ts'
import { builtinSlot, filterBuiltin, type BuiltinMaterial } from '../../creative-canvas/assets/builtin.ts'
import { assetMaterial } from '../../creative-canvas/viewer/modelInfo.ts'

type CanvasSnapshot = components['schemas']['CreativeCanvasSnapshot']
type CanvasNode = CanvasSnapshot['nodes'][number]
type LibraryAsset = components['schemas']['CreativeLibraryAsset']
type MaterialSlot = components['schemas']['CreativeSceneMaterialSlot']
type Source = 'library' | 'canvas' | 'upload'

const sources: { id: Source; label: string }[] = [
  { id: 'library', label: '资产库' },
  { id: 'canvas', label: '画布' },
  { id: 'upload', label: '上传' },
]

/**
 * Picks one of the account's images for a texture input (C5c1 proposal
 * 「材质贴图」): from the asset library, from the canvas the scene sits on,
 * or a file uploaded into the library, so it can be used again. Gives the
 * image's content revision; the slot edit is the caller's.
 */
export function ImagePicker({ title, canvasID, onPick, onClose }: { title: string; canvasID: string | undefined; onPick: (revision: string) => void; onClose: () => void }) {
  const [source, setSource] = useState<Source>('library')
  return (
    <PickerDialog title={title} onClose={onClose}>
      <div className="cc-scene-image-tabs" role="tablist" aria-label="图片来源">
        {sources.map((s) => (
          <button key={s.id} type="button" role="tab" aria-selected={source === s.id} className={source === s.id ? 'is-active' : ''} onClick={() => setSource(s.id)}>
            {s.label}
          </button>
        ))}
      </div>
      {source === 'library' && <LibraryAssets kind="image" onPick={(a) => onPick(a.content_revision_id)} />}
      {source === 'canvas' && <CanvasImages canvasID={canvasID} onPick={onPick} />}
      {source === 'upload' && <UploadImage onPick={onPick} />}
    </PickerDialog>
  )
}

/**
 * Picks a material for a slot: one of the account's material assets (C5c1
 * design「材质资产的存入与使用」) or a built-in textured one (C5c2), as the
 * slot it is written as; the slot edit is the caller's.
 */
export function MaterialPicker({ title, onPick, onClose }: { title: string; onPick: (picked: { title: string; slot: MaterialSlot }) => void; onClose: () => void }) {
  const [source, setSource] = useState<'mine' | 'builtin'>('mine')
  return (
    <PickerDialog title={title} onClose={onClose}>
      <div className="cc-scene-image-tabs" role="tablist" aria-label="材质来源">
        {(['mine', 'builtin'] as const).map((s) => (
          <button key={s} type="button" role="tab" aria-selected={source === s} className={source === s ? 'is-active' : ''} onClick={() => setSource(s)}>
            {s === 'mine' ? '我的' : '内置'}
          </button>
        ))}
      </div>
      {source === 'mine' ? (
        <LibraryAssets
          kind="material"
          onPick={(a) => {
            const slot = assetMaterial(a.content)
            if (slot) onPick({ title: a.title, slot })
          }}
        />
      ) : (
        <BuiltinAssets onPick={(m) => onPick({ title: m.label, slot: builtinSlot(m.id) })} />
      )}
    </PickerDialog>
  )
}

/** The built-in textured materials (C5c2), filtered here by name and category. */
function BuiltinAssets({ onPick }: { onPick: (m: BuiltinMaterial) => void }) {
  const [text, setText] = useState('')
  const items = filterBuiltin(text)
  return (
    <>
      <label className="cc-scene-field">
        <span>搜索</span>
        <input type="search" aria-label="搜索内置材质" placeholder="搜索名称、品类" value={text} onChange={(e) => setText(e.target.value)} />
      </label>
      {!items.length ? (
        <p>没有符合条件的内置材质</p>
      ) : (
        <div className="cc-scene-assets-grid cc-scene-image-grid" role="listbox" aria-label="内置材质">
          {items.map((m) => (
            <BuiltinCard key={m.id} material={m} onClick={() => onPick(m)} />
          ))}
        </div>
      )}
    </>
  )
}

/** A built-in material's card: its rendered ball and name. */
export function BuiltinCard({ material: m, ...rest }: { material: BuiltinMaterial } & Omit<React.ComponentProps<'button'>, 'children'>) {
  return (
    <button type="button" role="option" aria-selected={false} className="cc-scene-asset" aria-label={m.label} title={`${m.label} · ${m.categoryLabel} · ${m.sizeM} 米一块`} data-builtin={m.id} {...rest}>
      <span className="cc-scene-asset-thumb">
        <img src={m.thumbnail} alt="" draggable={false} />
      </span>
      <span className="cc-scene-asset-name">{m.label}</span>
    </button>
  )
}

const focusable = 'button:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'

/**
 * A dialog over the page: Escape or a click beside it closes it. Its keys
 * stay in it: Cmd+Z in it is not the scene's undo (the editor's keys are
 * React's, SceneEditor onKeyDown, so a document-level trap would not see
 * them), and Tab goes round its controls. Focus goes back where it was.
 */
function PickerDialog({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  const dialog = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    dialog.current?.focus()
    return () => opener?.focus({ preventScroll: true })
  }, [])
  return (
    <div className="cc-scene-dialog-backdrop is-fixed" onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div
        ref={dialog}
        className="cc-scene-dialog cc-scene-image-picker"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        onKeyDown={(e) => {
          e.stopPropagation()
          if (e.key === 'Escape') return onClose()
          if (e.key !== 'Tab' || !dialog.current) return
          const controls = Array.from(dialog.current.querySelectorAll<HTMLElement>(focusable))
          const at = controls.indexOf(document.activeElement as HTMLElement)
          const wrap = e.shiftKey ? at <= 0 : at === controls.length - 1 || at < 0
          if (!wrap || !controls.length) return
          e.preventDefault()
          controls[e.shiftKey ? controls.length - 1 : 0].focus()
        }}
      >
        <h3>{title}</h3>
        {children}
        <footer>
          <button type="button" className="btn" onClick={onClose}>
            取消
          </button>
        </footer>
      </div>
    </div>
  )
}

function ImageGrid<T>({ label, kind, items, onPick }: { label: string; kind: string; items: { key: string; value: T; title: string; content: Parameters<typeof MediaFigure>[0]['content'] }[]; onPick: (value: T) => void }) {
  return (
    <div className="cc-scene-assets-grid cc-scene-image-grid" role="listbox" aria-label={label}>
      {items.map((i) => (
        <button key={i.key} type="button" role="option" aria-selected={false} className="cc-scene-asset" aria-label={i.title} title={i.title} onClick={() => onPick(i.value)}>
          <MediaFigure content={i.content} kind={kind} title={i.title} className="cc-scene-asset-thumb" />
          <span className="cc-scene-asset-name">{i.title}</span>
        </button>
      ))}
    </div>
  )
}

const noun = { image: '图片', material: '材质' } as const

function LibraryAssets({ kind, onPick }: { kind: keyof typeof noun; onPick: (asset: LibraryAsset) => void }) {
  const [text, setText] = useState('')
  const [q, setQ] = useState('')
  useEffect(() => {
    const t = window.setTimeout(() => setQ(text.trim().slice(0, 200)), 250)
    return () => window.clearTimeout(t)
  }, [text])
  const query = useMemo<LibraryQuery>(() => ({ ...blank(), kind, q }), [kind, q])
  const list = useAssetList(query, 0)
  const items = (list.page?.items ?? []).filter((a) => a.kind === kind && !a.deleted_at && !a.unavailable)
  return (
    <>
      <label className="cc-scene-field">
        <span>搜索</span>
        <input type="search" aria-label={`搜索${noun[kind]}`} placeholder={`搜索${noun[kind]}`} value={text} onChange={(e) => setText(e.target.value)} />
      </label>
      {list.error && !list.page ? (
        <p role="alert">{list.error}</p>
      ) : list.loading ? (
        <p role="status">正在读取资产库…</p>
      ) : !items.length ? (
        <p>{q ? `没有符合条件的${noun[kind]}` : `资产库里还没有${noun[kind]}`}</p>
      ) : (
        <ImageGrid label={`资产库的${noun[kind]}`} kind={kind} items={items.map((a) => ({ key: a.id, value: a, title: a.title, content: a.content }))} onPick={onPick} />
      )}
      {list.more !== 'loading' && !!list.page?.next_cursor && (
        <button type="button" className="btn" onClick={list.loadMore}>
          加载更多
        </button>
      )}
    </>
  )
}

function CanvasImages({ canvasID, onPick }: { canvasID: string | undefined; onPick: (revision: string) => void }) {
  const [nodes, setNodes] = useState<CanvasNode[] | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    if (!canvasID) return
    const controller = new AbortController()
    api.read<CanvasSnapshot>(`/canvases/${encodeURIComponent(canvasID)}`, controller.signal).then(
      (snapshot) => setNodes(snapshot.nodes.filter((n) => n.content?.kind === 'image')),
      (e: unknown) => !controller.signal.aborted && setError(e instanceof Error ? e.message : '画布读取失败'),
    )
    return () => controller.abort()
  }, [canvasID])
  if (!canvasID) return <p>这个场景不在画布上打开，没有画布图片可选</p>
  if (error) return <p role="alert">{error}</p>
  if (!nodes) return <p role="status">正在读取画布…</p>
  if (!nodes.length) return <p>画布上还没有图片</p>
  return <ImageGrid label="画布上的图片" kind="image" items={nodes.map((n) => ({ key: n.id, value: n.content!.id, title: n.metadata.title || '图片', content: n.content }))} onPick={onPick} />
}

/**
 * Uploads a file and picks it. Closing the picker, or the panel going with
 * the selection, stops the upload: a picker no longer open picks nothing.
 * The pick is the latest onPick's, which edits the slot as it is now.
 */
function UploadImage({ onPick }: { onPick: (revision: string) => void }) {
  const [progress, setProgress] = useState<UploadProgress | null>(null)
  const [error, setError] = useState('')
  const pick = useRef(onPick)
  useLayoutEffect(() => {
    pick.current = onPick
  })
  const running = useRef<AbortController | null>(null)
  useEffect(() => () => running.current?.abort(), [])
  const busy = !!progress && progress.stage !== 'done' && progress.stage !== 'failed'
  const upload = async (file: File) => {
    setError('')
    running.current?.abort()
    const controller = new AbortController()
    running.current = controller
    try {
      const capabilities = await api.read<MediaCapabilities>('/media-capabilities', controller.signal)
      // Into the asset library, so the image is there to use again.
      const done = await uploadFile(file, { kind: 'asset', asset: { title: fileTitle(file.name) } }, capabilities, setProgress, controller.signal)
      if (controller.signal.aborted) return
      if (!done.content_revision_id) throw new Error('上传完成了，但还没有可用的图片，请到资产库查看')
      pick.current(done.content_revision_id)
    } catch (e) {
      if (controller.signal.aborted) return
      setProgress(null)
      setError(e instanceof Error ? e.message : '上传失败')
    }
  }
  return (
    <>
      <label className="cc-scene-field">
        <span>上传到资产库后用作贴图</span>
        <input
          type="file"
          accept="image/png,image/jpeg,image/webp"
          aria-label="选择图片文件"
          disabled={busy}
          onChange={(e) => {
            const file = e.target.files?.[0]
            if (file) void upload(file)
          }}
        />
      </label>
      {busy && (
        <p role="status">
          {progress.stage === 'uploading' ? `正在上传 ${Math.round((progress.sent / Math.max(progress.total, 1)) * 100)}%` : progress.stage === 'verifying' ? '正在校验…' : '正在准备…'}
        </p>
      )}
      {error && <p role="alert">{error}</p>}
    </>
  )
}
