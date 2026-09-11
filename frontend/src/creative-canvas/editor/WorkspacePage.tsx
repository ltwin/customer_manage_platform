import {
  Aperture,
  ArrowLeft,
  Library,
  Plus,
  Type,
  Link2,
  LockKeyhole,
  ChevronDown,
  MousePointer2,
  Hand,
  CircleHelp,
  Image as ImageIcon,
  Film,
  AudioLines,
} from 'lucide-react'
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from 'react'
import { Link, useParams } from 'react-router-dom'
import { ReactFlowProvider } from '@xyflow/react'
import ConfirmDialog from '../../components/ConfirmDialog'
import { useOnline } from './useOnline.ts'
import { useJournal } from './useJournal.ts'
import { errorMessage, isWithdrawn } from './queue.ts'
import type { Draft } from './journal.ts'

import * as api from './api.ts'
import { newerCanvas, matchesCanvas } from './snapshot.ts'
import {
  historyOf,
  projectCanvas,
  projected,
  type Intent,
} from './outbox.ts'
import DocumentView from './DocumentView.tsx'
import CanvasView from './CanvasView.tsx'
import ContentForm from './ContentForm.tsx'
import StudioDialog from './StudioDialog.tsx'
import { blankDraft, contentText } from './content.ts'
import '@xyflow/react/dist/style.css'
import './workspace.css'
import LibraryPanel from './LibraryPanel.tsx'
import { emptyCatalog, emptyOrganization } from './libraryState.ts'
import OrganizationPicker from './OrganizationPicker.tsx'
import { useUploads } from './uploads.ts'
import UploadTray from './UploadTray.tsx'
import MediaPreview from './MediaPreview.tsx'
import { downloadMedia } from './MediaView.tsx'
import {
  isMediaKind,
  readableFileName,
  type MediaKind,
} from './media.ts'

export default function WorkspacePage() {
  const account = api.currentAccount()
  return account ? (
    <Workspace key={account} account={account} />
  ) : (
    <main>请先登录</main>
  )
}
function Workspace({ account }: { account: string }) {
  const { queue, error: storageError } = useJournal(account)
  const online = useOnline()
  const [libraryWidth, setLibraryWidth] = useState(318)
  const [libraryOpen, setLibraryOpen] = useState(
    () => !window.matchMedia('(max-width: 760px)').matches,
  )
  const [maximized, setMaximized] = useState<{
    id: string
    libraryOpen: boolean
  } | null>(null)
  const [editing, setEditing] = useState(false)
  const [addMenu, setAddMenu] = useState(false)
  const [help, setHelp] = useState(false)
  const [panMode, setPanMode] = useState(false)
  const { canvasID = '' } = useParams<{ canvasID: string }>()
  const [assets, setAssets] = useState<api.AssetPage>({
    items: [],
    next_cursor: '',
    total_count: 0,
    library_revision: '1',
  })
  const [catalog, setCatalog] = useState(emptyCatalog)
  const [libraryLoading, setLibraryLoading] = useState(true)
  const [canvas, setCanvas] = useState<api.Canvas | null>(null)
  const [selected, setSelected] = useState<string | null>(null)
  const [selection, setSelection] = useState<string[]>([])
  const [groupEditing, setGroupEditing] = useState<api.CanvasNode | null>(null)
  const onSelection = useCallback((ids: string[]) => {
    setSelection((old) =>
      old.length === ids.length && old.every((v, i) => v === ids[i])
        ? old
        : ids,
    )
    setSelected((old) =>
      old && ids.includes(old) ? old : (ids.at(-1) ?? null),
    )
  }, [])
  const [tick, setTick] = useState(0)
  // Edits undone before they left this machine; redo replays them locally.
  const [redoStack, setRedoStack] = useState<Intent[]>([])
  const [full, setFull] = useState<Draft | null>(null)
  const [error, setError] = useState('')
  const [copyNotice, setCopyNotice] = useState('')
  const [localError, setLocalError] = useState('')
  const [loading, setLoading] = useState(true)
  const [discard, setDiscard] = useState<string | null>(null)
  const [form, setForm] = useState<'asset' | null>(null)
  const [preview, setPreview] = useState<string | null>(null)
  const [savingNode, setSavingNode] = useState<api.CanvasNode | null>(null)
  const nodeFileInput = useRef<HTMLInputElement>(null)
  const nodeUploadTarget = useRef<{ id: string; kind: MediaKind } | null>(null)
  const [compare, setCompare] = useState<{
    content: api.ContentDraft
    node: api.CanvasNode
  } | null>(null)
  const [rename, setRename] = useState<{
    id: string
    name: string
    revision: string
  } | null>(null)
  const readSequence = useRef(0)
  const canvasReadSequence = useRef(0)
  const outbox = useMemo(() => queue?.value.outbox ?? [], [queue?.value.outbox])
  // The canvas the photographer sees: snapshot plus local edits on their way.
  const view = useMemo(
    () =>
      canvas && canvas.id === canvasID ? projectCanvas(canvas, outbox) : canvas,
    [canvas, canvasID, outbox],
  )
  useEffect(() => {
    if (!view) return
    const live = new Set(view.nodes.map((n) => n.id))
    setSelection((old) =>
      old.every((id) => live.has(id)) ? old : old.filter((id) => live.has(id)),
    )
    setSelected((old) => (old && live.has(old) ? old : null))
  }, [view])
  // Nodes created by an effect the projection cannot paint (duplicate, group)
  // are selected once the snapshot that contains them has arrived.
  const deferredSelection = useRef<{ ids: string[]; revision: string } | null>(
    null,
  )
  useEffect(() => {
    const deferred = deferredSelection.current
    if (
      !canvas ||
      !deferred ||
      BigInt(canvas.revision) < BigInt(deferred.revision)
    )
      return
    deferredSelection.current = null
    const live = new Set(canvas.nodes.map((n) => n.id))
    onSelection(deferred.ids.filter((id) => live.has(id)))
  }, [canvas, onSelection])
  const node = view?.nodes.find((n) => n.id === selected)
  const nodeKey = `node:${canvasID}:${selected ?? ''}`
  const draft = queue?.value.drafts[nodeKey] ?? full
  // Non-canvas requests (library, projects) still wait for the single slot;
  // canvas edits are queued locally and never block the editor.
  const blocked =
    !queue ||
    queue.busy ||
    !!queue.value.job ||
    !!storageError ||
    !!localError ||
    libraryLoading ||
    loading
  const canEditLocal = matchesCanvas(canvas, canvasID) && !canvas?.archived
  const canWrite = canEditLocal && !error && !!queue && !storageError && !localError
  // Local edits stay enabled while a rejected request awaits a decision only
  // if it is not a canvas command; a refused canvas edit must be resolved first.
  const editable =
    canWrite &&
    !(queue.value.job?.state === 'rejected' && queue.value.job.intent)
  const refresh = useCallback(() => {
    setLibraryLoading(true)
    setTick((v) => v + 1)
  }, [])
  const canMove = !!queue && canEditLocal && !storageError && !localError
  const uploads = useUploads((item) => {
    if (item.upload?.binding?.status === 'needs_review')
      setCopyNotice('文件已上传，但目标已变化；请在待处理项中选择放入位置。')
    refresh()
  })
  useEffect(() => {
    if (!queue || !canvas || canvas.id !== canvasID) return
    void queue
      .pruneOutbox(canvas)
      .catch((e: unknown) => setLocalError(errorMessage(e)))
  }, [queue, canvas, canvasID, outbox])
  // Queued canvas edits leave in order, after a short pause so a burst of
  // moves or a delete-then-undo settles locally before anything is sent.
  useEffect(() => {
    if (
      !queue ||
      !canvas ||
      canvas.id !== canvasID ||
      !canEditLocal ||
      !online ||
      storageError ||
      localError ||
      queue.busy ||
      queue.value.job ||
      loading ||
      // A 'sent' intent whose request never reached the journal is resent.
      !outbox.some((i) => i.canvasID === canvasID && i.state !== 'confirmed')
    )
      return
    const timer = window.setTimeout(() => {
      void queue
        .sendNextIntent(canvasID, canvas)
        .catch((e: unknown) => setLocalError(errorMessage(e)))
    }, 250)
    return () => window.clearTimeout(timer)
  }, [
    queue,
    queue?.value,
    queue?.busy,
    canvas,
    outbox,
    canvasID,
    canEditLocal,
    online,
    storageError,
    localError,
    loading,
  ])
  useEffect(() => {
    const controller = new AbortController()
    const seq = ++readSequence.current
    setLoading(true)
    void (async () => {
      const canvasSeq = ++canvasReadSequence.current
      const c = canvasID
        ? await api.read<api.Canvas>(
            `/canvases/${encodeURIComponent(canvasID)}`,
            controller.signal,
          )
        : null
      if (controller.signal.aborted || seq !== readSequence.current) return
      if (canvasSeq === canvasReadSequence.current)
        setCanvas((old) => newerCanvas(old, c))
      setError('')
    })()
      .catch((e: unknown) => {
        if (!controller.signal.aborted) setError(errorMessage(e))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [canvasID, tick])
  const pollCanvas = useRef<() => Promise<void>>(() => Promise.resolve())
  useEffect(() => {
    const controller = new AbortController()
    let fetching = false
    const poll = async () => {
      if (document.hidden || fetching || !canvasID) return
      fetching = true
      const canvasSeq = ++canvasReadSequence.current
      try {
        const c = await api.read<api.Canvas>(
          `/canvases/${encodeURIComponent(canvasID)}`,
          controller.signal,
        )
        if (
          !controller.signal.aborted &&
          canvasSeq === canvasReadSequence.current
        )
          setCanvas((old) => newerCanvas(old, c))
      } catch (e) {
        if (!controller.signal.aborted) setError(errorMessage(e))
      } finally {
        fetching = false
      }
    }
    pollCanvas.current = poll
    const timer = window.setInterval(() => void poll(), 5000)
    const visible = () => {
      if (!document.hidden) refresh()
    }
    window.addEventListener('online', visible)
    document.addEventListener('visibilitychange', visible)
    return () => {
      controller.abort()
      pollCanvas.current = () => Promise.resolve()
      clearInterval(timer)
      window.removeEventListener('online', visible)
      document.removeEventListener('visibilitychange', visible)
    }
  }, [refresh, canvasID])
  useEffect(() => {
    setCanvas(null)
    setSelected(null)
    setSelection([])
    setGroupEditing(null)
    setMaximized(null)
    setEditing(false)
    setFull(null)
    setCompare(null)
    setRename(null)
    setDiscard(null)
    setRedoStack([])
  }, [canvasID])
  useEffect(() => {
    const controller = new AbortController()
    setFull(null)
    if (!node) return
    const base: Draft = {
      ...blankDraft(
        (node.metadata.type_key.startsWith('core.')
          ? node.metadata.type_key.slice(5)
          : 'text') as Draft['kind'],
      ),
      title: node.metadata.title,
      dataRevision: node.data_revision,
      contentRevision: node.data.content_revision_id,
    }
    if (!node.data.content_revision_id) {
      setFull(base)
      return
    }
    void api
      .read<api.Content>(
        `/content-revisions/${encodeURIComponent(node.data.content_revision_id)}`,
        controller.signal,
      )
      .then((r) => {
        if (!controller.signal.aborted)
          setFull({
            ...base,
            value: contentText(r.payload),
          })
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted) setError(errorMessage(e))
      })
    return () => controller.abort()
  }, [node])
  useEffect(() => {
    const leave = (e: BeforeUnloadEvent) => {
      if (localError || queue?.busy || queue?.localPending) {
        e.preventDefault()
        e.returnValue = ''
      }
    }
    window.addEventListener('beforeunload', leave)
    return () => window.removeEventListener('beforeunload', leave)
  }, [localError, queue])

  function writeDraft(key: string, value: Draft) {
    if (!queue) return
    setLocalError('')
    void queue
      .update({
        ...queue.value,
        drafts: { ...queue.value.drafts, [key]: value },
      })
      .catch((e: unknown) => setLocalError(errorMessage(e)))
  }
  // stage queues one canvas edit: painted at once, sent in the background.
  // Resolves true when the server confirms it.
  function stage(
    intent: Omit<Intent, 'id' | 'canvasID' | 'state'>,
    options: { select?: string[]; draftKey?: string } = {},
  ): Promise<boolean> {
    if (!queue || !view || view.id !== canvasID || !editable) return Promise.resolve(false)
    setError('')
    const staged = queue.stageIntent(canvasID, {
      ...intent,
      draftKey: options.draftKey,
      draftValue: options.draftKey ? queue.value.drafts[options.draftKey] : undefined,
    })
    if (options.select?.length) onSelection(options.select)
    return staged.done.then(
      (receipt) => {
        if (receipt.omitted_reference_ids.length)
          setCopyNotice(
            `已复制选中节点；${receipt.omitted_reference_ids.length} 条指向选区外的引用未复制。`,
          )
        const created = receipt.created_ids.filter((id) =>
          id.startsWith('cwnode_'),
        )
        if (created.length && !options.select?.length)
          deferredSelection.current = {
            ids: created,
            revision: receipt.result_revision,
          }
        // Effects the projection cannot paint need the authoritative snapshot
        // now; everything else just refreshes the canvas in the background.
        if (!projected(intent)) refresh()
        else void pollCanvas.current()
        return true
      },
      (e: unknown) => {
        // A rejection is surfaced by the recovery notice, a cancellation by
        // nothing at all; only unexpected failures become local errors.
        if (!isWithdrawn(e)) setLocalError(errorMessage(e))
        return false
      },
    )
  }
  async function perform(path: string, value: unknown, draftKey?: string) {
    if (!queue || blocked || queue.busy || queue.value.job) return false
    if (path.startsWith('/canvases/')) return false
    setError('')
    setLoading(true)
    try {
      const result = await queue.enqueue(path, value, draftKey)
      if (
        result &&
        typeof result === 'object' &&
        'omitted_reference_ids' in result &&
        Array.isArray(result.omitted_reference_ids) &&
        result.omitted_reference_ids.length
      ) {
        setCopyNotice(
          `已复制选中节点；${result.omitted_reference_ids.length} 条指向选区外的引用未复制。`,
        )
      }
      if (
        result &&
        typeof result === 'object' &&
        'created_ids' in result &&
        Array.isArray(result.created_ids)
      ) {
        const ids = result.created_ids.filter(
          (v): v is string => typeof v === 'string' && v.startsWith('cwnode_'),
        )
        if (ids.length) onSelection(ids)
      } else if (
        result &&
        typeof result === 'object' &&
        'node_id' in result &&
        typeof result.node_id === 'string'
      ) {
        onSelection([result.node_id])
      }
      if (!queue.value.job) {
        setForm(null)
        if (draftKey?.startsWith('node:')) setEditing(false)
        refresh()
        return true
      } else {
        setLoading(false)
      }
    } catch (e) {
      setLoading(false)
      setLocalError(errorMessage(e))
    }
    return false
  }
  async function retry() {
    if (!queue) return
    setLocalError('')
    setLoading(true)
    try {
      await queue.flush()
      if (!queue.value.job) {
        refresh()
      } else {
        setLoading(false)
      }
    } catch (e) {
      setLoading(false)
      setLocalError(errorMessage(e))
    }
  }
  const history = view
    ? historyOf(view, outbox, redoStack)
    : { canUndo: false, canRedo: false, undo: undefined, redo: undefined }
  function graphActions(actions: api.GraphAction[]) {
    if (!view || !editable) return
    setRedoStack([])
    void stage({ kind: 'actions', actions })
  }
  // Undo of an edit that has not left this machine cancels it outright and
  // keeps it for redo; anything already sent is reversed on the server.
  function reverseChange(redo = false) {
    if (!queue || !view || !editable) return
    if (!redo) {
      const cancelled = queue.cancelTail(canvasID)
      if (cancelled) {
        setRedoStack((old) => [...old, cancelled])
        return
      }
      if (!history.canUndo) return
      setRedoStack([])
      void stage({ kind: 'undo' })
      return
    }
    const cancelled = queue.cancelTail(canvasID)
    if (cancelled && cancelled.kind === 'undo') return
    const local = redoStack.at(-1)
    if (local && local.canvasID === canvasID) {
      setRedoStack((old) => old.slice(0, -1))
      void stage({ kind: 'actions', actions: local.actions, preview: local.preview })
      return
    }
    if (!history.canRedo) return
    void stage({ kind: 'redo' })
  }
  // A drop is just another canvas edit: staged, painted, sent in order.
  function moveSelection(
    moves: { node: api.CanvasNode; x: number; y: number }[],
  ) {
    if (!queue || !view || view.id !== canvasID || !canMove) return
    const staged = queue.stageMoves(view, moves)
    if (!staged) return
    setRedoStack([])
    void staged.done.catch((e: unknown) => {
      if (!isWithdrawn(e)) setLocalError(errorMessage(e))
    })
  }
  function addEmptyNode(
    kind: 'text' | 'link' | MediaKind,
    x = 80 + (view?.nodes.length ?? 0) * 20,
    y = 80 + (view?.nodes.length ?? 0) * 20,
  ) {
    setAddMenu(false)
    if (!view || !editable) return
    const id = `cwnode_${crypto.randomUUID()}`
    setRedoStack([])
    void stage(
      {
        kind: 'actions',
        actions: [{ type: 'add_node', node_id: id, type_key: `core.${kind}`, x, y }],
      },
      { select: [id] },
    )
  }
  const mediaKindOfFile = (file: File): MediaKind | null =>
    uploads.capabilities
      ? ((uploads.capabilities.formats.find(
          (f) =>
            f.mime === file.type.split(';')[0].toLowerCase() ||
            f.extensions.includes(
              '.' + (file.name.split('.').pop() ?? '').toLowerCase(),
            ),
        )?.kind as MediaKind | undefined) ?? null)
      : null
  // Import into the library: each file becomes an asset in the current group.
  function importToLibrary(files: File[], groupID: string) {
    if (!uploads.capabilities || uploads.capabilitiesError) {
      setLocalError(uploads.capabilitiesError || '媒体上传暂不可用')
      return
    }
    uploads.start(files, (file) => ({
      kind: 'asset',
      asset: {
        title:
          readableFileName(file.name).replace(/\.[^.]+$/, '').slice(0, 200) ||
          readableFileName(file.name),
        group_ids: groupID ? [groupID] : [],
      },
    }))
  }
  // Drop files on the canvas: create empty media nodes first, then upload
  // into each node so the publication binds by node target. The nodes must
  // exist on the server before an upload can target them.
  async function dropFiles(files: File[], x: number, y: number) {
    if (!view || !editable || !uploads.capabilities) return
    const usable = files.filter((f) => mediaKindOfFile(f))
    if (!usable.length) {
      setLocalError('拖入的文件不是支持的图片、视频或音频格式')
      return
    }
    const ids = usable.map(() => `cwnode_${crypto.randomUUID()}`)
    setRedoStack([])
    const ok = await stage(
      {
        kind: 'actions',
        actions: usable.map((file, i) => ({
          type: 'add_node' as const,
          node_id: ids[i]!,
          type_key: `core.${mediaKindOfFile(file)!}` as
            | 'core.image'
            | 'core.video'
            | 'core.audio',
          title: readableFileName(file.name).replace(/\.[^.]+$/, '').slice(0, 200),
          x: x + i * 36,
          y: y + i * 36,
        })),
      },
      { select: ids },
    )
    if (!ok) return
    uploads.start(usable, (_, i) => ({
      kind: 'node',
      node: { canvas_id: canvasID, node_id: ids[i]!, expected_data_revision: '1' },
    }))
  }
  function uploadToNode(id: string, kind: MediaKind) {
    nodeUploadTarget.current = { id, kind }
    nodeFileInput.current?.click()
  }
  function nodeFileChosen(files: File[]) {
    const target = nodeUploadTarget.current
    nodeUploadTarget.current = null
    const n = view?.nodes.find((n) => n.id === target?.id)
    if (!target || !n || !view || !files.length) return
    const file = files[0]!
    if (mediaKindOfFile(file) !== target.kind) {
      setLocalError(`请选择${{ image: '图片', video: '视频', audio: '音频' }[target.kind]}文件`)
      return
    }
    uploads.start([file], () => ({
      kind: 'node',
      node: { canvas_id: view.id, node_id: n.id, expected_data_revision: n.data_revision },
    }))
  }
  async function downloadNode(id: string) {
    const n = view?.nodes.find((n) => n.id === id)
    if (!n?.content) return
    try {
      await downloadMedia(n.content, n.metadata.title || n.id)
    } catch (e) {
      setLocalError(errorMessage(e))
    }
  }
  // Several selected assets land in one batch so the queue sends one command.
  function addAssets(dropped: api.Asset[], x = 80, y = 80) {
    if (!view || !editable) return
    const usable = dropped.filter((a) => !a.unavailable)
    if (!usable.length) return
    const ids = usable.map(() => `cwnode_${crypto.randomUUID()}`)
    const preview: Record<string, api.Content> = {}
    for (const [i, asset] of usable.entries())
      if (asset.content) preview[ids[i]!] = asset.content
    setRedoStack([])
    void stage(
      {
        kind: 'actions',
        preview,
        actions: usable.map((asset, i) => ({
          type: 'add_node' as const,
          node_id: ids[i]!,
          type_key: `core.${asset.kind}` as
            | 'core.text'
            | 'core.link'
            | 'core.image'
            | 'core.video'
            | 'core.audio',
          title: asset.title,
          x: x + i * 36,
          y: y + i * 36,
          asset: {
            asset_id: asset.id,
            expected_asset_revision: asset.revision,
            content_revision_id: asset.content_revision_id,
          },
        })),
      },
      { select: ids },
    )
  }
  function addAsset(asset: api.Asset, x = 80, y = 80) {
    addAssets([asset], x, y)
  }
  function saveNode(content: api.ContentDraft, reapply?: api.CanvasNode) {
    if (!editable || !view || !node || !draft || (reapply && reapply.id !== node.id))
      return
    if (
      !reapply &&
      (node.data_revision !== draft.dataRevision ||
        node.data.content_revision_id !== draft.contentRevision)
    ) {
      setCompare({ content, node: structuredClone(node) })
      return
    }
    setRedoStack([])
    void stage(
      {
        kind: 'actions',
        actions: [
          ...(draft.title !== node.metadata.title
            ? [
                {
                  type: 'update_metadata' as const,
                  node_id: node.id,
                  title: draft.title,
                  intent: node.metadata.intent,
                },
              ]
            : []),
          { type: 'replace_content', node_id: node.id, payload: content.payload },
        ],
      },
      { draftKey: nodeKey },
    )
    setEditing(false)
  }
  const activeProject =
    canvas?.id === canvasID
      ? {
          id: canvas.project_id,
          name: canvas.project_name,
          revision: canvas.project_revision,
        }
      : undefined
  useEffect(() => {
    const previous = document.title
    document.title = `${canvas?.project_name ?? (canvasID ? '创作画布' : '个人资产库')} · 创意空间`
    return () => {
      document.title = previous
    }
  }, [canvas?.project_name, canvasID])
  const assetDraft = queue?.value.drafts['asset-form'] ?? blankDraft()
  const job = queue?.value.job
  const hasDraft = !!queue?.value.drafts[nodeKey]
  const needsRecovery = !!job && !queue?.busy && !loading
  const showNotice = !!(
    error ||
    storageError ||
    localError ||
    !online ||
    needsRecovery ||
    copyNotice
  )
  return (
    <main
      style={{ '--shelf-width': `${libraryWidth}px` } as CSSProperties}
      className={`cc-workspace ${!canvasID ? 'is-library' : ''} ${libraryOpen ? 'has-library' : ''}`}
    >
      <header className="cc-header">
        <div className="cc-heading">
          <Link
            className="cc-home"
            to="/creative"
            aria-label="返回创意空间"
            title="返回创意空间"
          >
            <ArrowLeft size={17} />
          </Link>
          <Aperture className="cc-brand-icon" size={25} strokeWidth={1.4} />
          <span className="cc-brand">创意空间</span>
          <span className="cc-divider" />
          {activeProject ? (
            <button
              className="cc-project-name"
              aria-label="改名"
              disabled={blocked || !canWrite}
              onClick={() => setRename(activeProject)}
            >
              <h1>{activeProject.name}</h1>
              <ChevronDown size={13} />
            </button>
          ) : (
            <h1>个人资产库</h1>
          )}
          <span className="cc-private">
            <LockKeyhole size={12} /> 私人创作
          </span>
        </div>
        <nav className="cc-actions">
          {activeProject && (
            <button
              className="btn cc-quiet"
              disabled={blocked}
              onClick={() =>
                void perform(
                  `/projects/${activeProject.id}/${canvas?.archived ? 'restore' : 'archive'}`,
                  { expected_revision: activeProject.revision },
                )
              }
            >
              {canvas?.archived ? '恢复项目' : '归档项目'}
            </button>
          )}
          <Link className="btn" to="/creative">
            所有项目
          </Link>
        </nav>
      </header>
      <nav className="cc-tool-rail" aria-label="创作工具">
        <button
          className="cc-add-tool"
          aria-label="新建节点"
          title="新建节点"
          aria-expanded={addMenu}
          aria-haspopup="menu"
          disabled={blocked || !canWrite}
          onClick={() => setAddMenu(!addMenu)}
        >
          <Plus size={24} />
        </button>
        <span className="cc-rail-line" />
        <button
          className={libraryOpen ? 'is-active' : ''}
          aria-label="资产库"
          aria-expanded={libraryOpen}
          aria-controls="creative-library"
          title="资产库"
          onClick={() => setLibraryOpen(!libraryOpen)}
        >
          <Library size={20} strokeWidth={1.5} />
          <span>资产</span>
        </button>
        <span className="cc-rail-line" />
        <button
          aria-label="选择工具"
          title="选择工具"
          aria-pressed={!panMode}
          onClick={() => setPanMode(false)}
        >
          <MousePointer2 size={20} strokeWidth={1.5} />
        </button>
        <button
          aria-label="移动画布"
          title="移动画布 · 中键 / 空格"
          aria-pressed={panMode}
          onClick={() => setPanMode(true)}
        >
          <Hand size={20} strokeWidth={1.5} />
        </button>
        <span className="cc-rail-spacer" />
        <button
          aria-label="画布快捷键"
          title="画布快捷键"
          onClick={() => setHelp(true)}
        >
          <CircleHelp size={19} strokeWidth={1.5} />
        </button>
      </nav>
      {addMenu && (
        <>
          <button
            className="cc-menu-dismiss"
            aria-label="关闭新建菜单"
            onClick={() => setAddMenu(false)}
          />
          <div
            className="cc-add-menu cc-glass"
            role="menu"
            aria-label="新建节点"
          >
            {(
              [
                ['text', '文字', <Type size={17} key="t" />],
                ['link', '链接', <Link2 size={17} key="l" />],
                ['image', '图片', <ImageIcon size={17} key="i" />],
                ['video', '视频', <Film size={17} key="v" />],
                ['audio', '音频', <AudioLines size={17} key="a" />],
              ] as const
            ).map(([kind, label, icon]) => (
              <button
                key={kind}
                role="menuitem"
                aria-label={`新增${label}节点`}
                disabled={blocked || !canWrite}
                onClick={() => addEmptyNode(kind)}
              >
                {icon}
                <span>{label}</span>
              </button>
            ))}
          </div>
        </>
      )}
      {showNotice && (
        <section className="cc-save-status" aria-live="polite">
          {copyNotice && (
            <p>
              {copyNotice}{' '}
              <button className="btn" onClick={() => setCopyNotice('')}>
                知道了
              </button>
            </p>
          )}
          {!online && <span>当前离线 · 输入仍保留在本机</span>}
          {(error || storageError || localError) && (
            <p role="alert">
              {error || storageError || localError}{' '}
              <button
                className="btn"
                onClick={() => {
                  refresh()
                  if (localError && queue)
                    void queue
                      .update(queue.value)
                      .then(() => setLocalError(''))
                      .catch((e: unknown) => setLocalError(errorMessage(e)))
                }}
              >
                重试
              </button>
            </p>
          )}
          {needsRecovery && job && (
            <div>
              <p>
                {job.state === 'rejected'
                  ? job.message || '保存被拒绝，请处理后继续。'
                  : '上次保存未能确认，请查询并恢复后继续。'}
              </p>
              {job.state === 'rejected' ? (
                <button
                  className="btn"
                  disabled={queue?.busy}
                  onClick={() => {
                    void queue
                      ?.dismissRejected()
                      .then(refresh)
                      .catch((e: unknown) => setLocalError(errorMessage(e)))
                  }}
                >
                  {job.draftKey
                    ? '关闭被拒绝的请求，保留草稿'
                    : '放弃被拒绝的请求'}
                </button>
              ) : (
                <button
                  className="btn"
                  disabled={queue?.busy}
                  onClick={() => void retry()}
                >
                  查询并恢复原保存
                </button>
              )}
            </div>
          )}
        </section>
      )}
      <div className="cc-layout">
        <LibraryPanel
          disabled={blocked}
          busy={!!queue?.busy || loading}
          hidden={!!canvasID && !libraryOpen}
          standalone={!canvasID}
          tick={tick}
          onCreate={() => setForm('asset')}
          onClose={() => setLibraryOpen(false)}
          onWidth={setLibraryWidth}
          onAssets={setAssets}
          onLoading={setLibraryLoading}
          onCatalog={setCatalog}
          onCommand={perform}
          canDrop={!!view && editable}
          onDrop={addAsset}
          onImport={importToLibrary}
          importDisabled={blocked || !uploads.capabilities}
        />
        <section
          className="cc-stage"
          aria-label="创作画布"
          data-sync={
            outbox.some(
              (i) => i.canvasID === canvasID && i.state !== 'confirmed',
            )
              ? 'pending'
              : 'idle'
          }
        >
          {view && view.id === canvasID ? (
            <>
              <div className="cc-canvas-caption">
                <i />
                主画布 <span>/</span> {view.nodes.length} 个节点{' '}
                {view.archived && ' · 已归档'}
              </div>
              <ReactFlowProvider key={view.id}>
                <CanvasView
                  account={account}
                  canvas={view}
                  selected={selected}
                  selection={selection}
                  onSelection={onSelection}
                  onActions={graphActions}
                  onMoveSelection={moveSelection}
                  onUndo={() => reverseChange()}
                  onRedo={() => reverseChange(true)}
                  canUndo={history.canUndo}
                  canRedo={history.canRedo}
                  onSelect={(id) => {
                    setSelected(id)
                  }}
                  onEdit={(id) => {
                    setSelected(id)
                    const n = view.nodes.find((n) => n.id === id)
                    if (
                      n?.metadata.type_key === 'internal.document' &&
                      n.data.document_id
                    ) {
                      setMaximized({ id: n.data.document_id, libraryOpen })
                      setLibraryOpen(false)
                      return
                    }
                    if (n?.metadata.type_key === 'core.group') {
                      setGroupEditing(n)
                      return
                    }
                    if (
                      n &&
                      ![
                        'core.text',
                        'core.link',
                        'core.image',
                        'core.video',
                        'core.audio',
                      ].includes(n.metadata.type_key)
                    )
                      return
                    // Media nodes: an empty node edits by uploading; a filled node
                    // edits its caption. Preview stays on the toolbar button.
                    if (n && isMediaKind(n.metadata.type_key.slice(5))) {
                      if (!n.content) {
                        uploadToNode(n.id, n.metadata.type_key.slice(5) as MediaKind)
                        return
                      }
                    }
                    setEditing(true)
                  }}
                  onAdd={(x, y) => addEmptyNode('text', x, y)}
                  panMode={panMode}
                  disabled={!editable}
                  moveDisabled={!canMove}
                  assets={assets.items}
                  onDropAsset={addAsset}
                  onDropAssets={addAssets}
                  onDropFiles={(files, x, y) => void dropFiles(files, x, y)}
                  onUploadToNode={uploadToNode}
                  onDownload={(id) => void downloadNode(id)}
                  onSaveToLibrary={(id) =>
                    setSavingNode(view.nodes.find((n) => n.id === id) ?? null)
                  }
                  onPreview={setPreview}
                  onMove={(n, x, y) => moveSelection([{ node: n, x, y }])}
                />
              </ReactFlowProvider>
            </>
          ) : (
            <div className="cc-canvas-empty">
              <h2>{error ? '画布暂时没有打开' : '正在打开画布…'}</h2>
            </div>
          )}
        </section>
        {(form || (editing && node)) && (
          <StudioDialog
            title={form === 'asset' ? '存入个人库' : '编辑节点'}
            onClose={() => {
              setForm(null)
              setEditing(false)
            }}
          >
            {form === 'asset' ? (
              <ContentForm
                key="asset"
                asset
                draft={assetDraft}
                onChange={(d) => writeDraft('asset-form', d)}
                disabled={blocked}
                extraFields={
                  <OrganizationPicker
                    groups={catalog.groups}
                    tags={catalog.tags}
                    categories={catalog.categories}
                    value={assetDraft.organization ?? emptyOrganization()}
                    onChange={(organization) =>
                      writeDraft('asset-form', { ...assetDraft, organization })
                    }
                    disabled={blocked}
                  />
                }
                label="保存资产"
                onSave={(content) =>
                  void perform(
                    '/assets',
                    {
                      title: assetDraft.title,
                      content,
                      group_ids: assetDraft.organization?.groupIDs ?? [],
                      tag_ids: assetDraft.organization?.tagIDs ?? [],
                      new_tags: assetDraft.organization?.newTags ?? [],
                    },
                    'asset-form',
                  )
                }
              />
            ) : node && draft ? (
              <>
                <p className="cc-muted">
                  {hasDraft
                    ? '已恢复本机草稿，保存前会检查服务端版本。'
                    : '此处修改不会改变原资产或其他节点。'}
                </p>
                <ContentForm
                  key={node.id}
                  draft={draft}
                  disabled={
                    !!storageError ||
                    !!localError ||
                    !canEditLocal ||
                    node.status.content_state === 'unavailable'
                  }
                  submitDisabled={!editable}
                  onChange={(d) => writeDraft(nodeKey, d)}
                  label="保存到节点"
                  onSave={(content) => saveNode(content)}
                />
                {hasDraft && (
                  <button
                    className="btn"
                    disabled={blocked}
                    onClick={() => setDiscard(nodeKey)}
                  >
                    放弃本机修改
                  </button>
                )}
              </>
            ) : (
              <p>正在读取完整正文…</p>
            )}
          </StudioDialog>
        )}
      </div>
      {maximized && (
        <DocumentView
          id={maximized.id}
          onClose={() => {
            setLibraryOpen(maximized.libraryOpen)
            setMaximized(null)
          }}
        />
      )}
      {groupEditing && (
        <StudioDialog title="分组名称" onClose={() => setGroupEditing(null)}>
          <form
            noValidate
            onSubmit={(e) => {
              e.preventDefault()
              if (!canvas) return
              const name = String(
                new FormData(e.currentTarget).get('title') ?? '',
              ).trim()
              if (!name) {
                setError('请填写分组名称')
                return
              }
              setRedoStack([])
              void stage({
                kind: 'actions',
                actions: [
                  {
                    type: 'update_metadata',
                    node_id: groupEditing.id,
                    title: name,
                    intent: groupEditing.metadata.intent,
                  },
                ],
              })
              setGroupEditing(null)
            }}
          >
            <label>
              分组名称
              <input
                className="input"
                name="title"
                defaultValue={groupEditing.metadata.title}
                maxLength={200}
                autoFocus
              />
            </label>
            <button className="btn btn-primary" disabled={blocked}>
              保存名称
            </button>
          </form>
        </StudioDialog>
      )}
      <input
        ref={nodeFileInput}
        type="file"
        hidden
        accept="image/jpeg,image/png,image/webp,video/mp4,video/webm,audio/mpeg,audio/wav"
        aria-label="选择节点媒体文件"
        onChange={(e) => {
          const files = Array.from(e.target.files ?? [])
          e.target.value = ''
          nodeFileChosen(files)
        }}
      />
      <UploadTray
        items={uploads.items}
        onRetry={uploads.retry}
        onDismiss={uploads.dismiss}
      />
      {preview &&
        (() => {
          const n = view?.nodes.find((n) => n.id === preview)
          return n?.content && isMediaKind(n.metadata.type_key.slice(5)) ? (
            <MediaPreview
              content={n.content}
              kind={n.metadata.type_key.slice(5)}
              title={n.metadata.title || '媒体节点'}
              onClose={() => setPreview(null)}
            />
          ) : null
        })()}
      {savingNode && view && (
        <StudioDialog title="存入个人资产库" onClose={() => setSavingNode(null)}>
          <form
            className="cc-form"
            noValidate
            onSubmit={(e) => {
              e.preventDefault()
              const data = new FormData(e.currentTarget)
              const title = String(data.get('title') ?? '').trim()
              if (!title) {
                setError('请填写资产名称')
                return
              }
              void perform('/assets/from-canvas-node', {
                canvas_id: view.id,
                node_id: savingNode.id,
                expected_data_revision: savingNode.data_revision,
                target: { title },
              }).then((ok) => {
                if (ok) setSavingNode(null)
              })
            }}
          >
            <p className="cc-muted">
              固定当前节点内容为一条新的个人库资产；后续修改节点不会影响它。
            </p>
            <label>
              资产名称
              <input
                className="input"
                name="title"
                defaultValue={savingNode.metadata.title}
                maxLength={200}
                autoFocus
              />
            </label>
            <button className="btn btn-primary" disabled={blocked}>
              保存资产
            </button>
          </form>
        </StudioDialog>
      )}
      {help && (
        <StudioDialog title="画布快捷键" onClose={() => setHelp(false)}>
          <dl className="cc-shortcuts">
            <dt>平移画布</dt>
            <dd>滚轮 / 中键拖动 / 按住空格拖动</dd>
            <dt>缩放画布</dt>
            <dd>Ctrl / ⌘ + 滚轮</dd>
            <dt>编辑节点</dt>
            <dd>双击节点 / 选中后点击编辑</dd>
            <dt>新建文字</dt>
            <dd>双击画布空白处</dd>
            <dt>打组 / 解组</dt>
            <dd>⌘ / Ctrl + G，按住 Shift 解组</dd>
            <dt>复制 / 撤销 / 重做</dt>
            <dd>⌘ / Ctrl + D / Z / Shift + Z</dd>
            <dt>断开参考</dt>
            <dd>双击参考连线</dd>
            <dt>微调节点</dt>
            <dd>选中节点后按方向键</dd>
          </dl>
        </StudioDialog>
      )}
      {discard && (
        <ConfirmDialog
          title="放弃本机草稿？"
          body="只删除这份尚未保存的本机修改，服务器内容保持不变。"
          danger
          confirmLabel="放弃草稿"
          onCancel={() => setDiscard(null)}
          onConfirm={() => {
            if (!queue) return
            const drafts = { ...queue.value.drafts }
            delete drafts[discard]
            setDiscard(null)
            void queue
              .update({ ...queue.value, drafts })
              .catch((e: unknown) => setLocalError(errorMessage(e)))
          }}
        />
      )}
      {compare && node && (
        <ConfirmDialog
          title="节点已有新的内容"
          body={[
            '你的草稿仍保留。确认后将基于当前版本保存草稿，其他节点不受影响。',
            `当前内容：${compare.node.content ? contentText(compare.node.content.payload) : '空节点'}`,
          ]}
          confirmLabel="将草稿应用到当前版本"
          onCancel={() => setCompare(null)}
          onConfirm={() => {
            const content = compare
            setCompare(null)
            saveNode(content.content, content.node)
          }}
        />
      )}
      {rename && (
        <ConfirmDialog
          title="修改项目名称"
          body={`当前名称：${rename.name}`}
          requiredInput={{ label: '新名称', maxLength: 200 }}
          confirmLabel="保存名称"
          onCancel={() => setRename(null)}
          onConfirm={(name) => {
            setRename(null)
            void perform(`/projects/${rename.id}/rename`, {
              name,
              expected_revision: rename.revision,
            })
          }}
        />
      )}
    </main>
  )
}
