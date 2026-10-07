import {
  Archive,
  ArchiveRestore,
  ArrowLeft,
  LayoutGrid,
  Library,
  PencilLine,
  Plus,
  ChevronDown,
  MousePointer2,
  Hand,
  CircleHelp,
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
import { useCanvasSnapshot } from './useCanvasSnapshot.ts'
import { useNodeContent } from './useNodeContent.ts'
import { matchesCanvas } from './snapshot.ts'
import {
  previewAliases,
  resolvePreviewID,
  remapActions,
} from './structuralPreview.ts'
import {
  defaultNodeSize,
  defaultNodeWidth,
  historyOf,
  projectCanvas,
  type Intent,
} from './outbox.ts'
import { saveStatus, saveStatusText } from './saveStatus.ts'
import DocumentView from './DocumentView.tsx'
import CanvasView from './CanvasView.tsx'
import NodeGenerationPanel from './NodeGenerationPanel.tsx'
import ContentForm from './ContentForm.tsx'
import StudioDialog from './StudioDialog.tsx'
import { blankDraft, contentText, payload } from './content.ts'
import '@xyflow/react/dist/style.css'
import './workspace.css'
import LibraryPanel, { type LibraryTask } from './LibraryPanel.tsx'
import AgentPanel from './AgentPanel.tsx'
import { emptyCatalog, emptyOrganization } from './libraryState.ts'
import OrganizationPicker from './OrganizationPicker.tsx'
import { useUploads } from './uploads.ts'
import UploadTray from './UploadTray.tsx'
import MediaPreview from './MediaPreview.tsx'
import { promptFromNode } from './generation.ts'
import { mentionToken } from './promptReferences.ts'
import { downloadMedia } from './MediaView.tsx'
import {
  isMediaKind,
  classifyFile,
  mediaFileAccept,
  readableFileName,
  type MediaKind,
} from './media.ts'

// 与后端新建节点的默认尺寸一致。
const NODE_WIDTH = defaultNodeWidth

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
  const [libraryWidth, setLibraryWidth] = useState(344)
  // v6：资产库默认收起，从工具轨打开。
  const [libraryOpen, setLibraryOpen] = useState(false)
  const [projectMenu, setProjectMenu] = useState(false)
  const [maximized, setMaximized] = useState<{
    id: string
    libraryOpen: boolean
  } | null>(null)
  const [editing, setEditing] = useState(false)
  const [addRequest, setAddRequest] = useState<{
    seq: number
    kind?: 'text' | 'image' | 'video' | 'audio'
    anchor?: { x: number; y: number }
  }>({ seq: 0 })
  // 把焦点交给某个节点的生成输入框；面板解锁并聚焦后清掉（PRD CV-53/54）。
  const [promptFocus, setPromptFocus] = useState<{
    seq: number
    nodeID: string
  } | null>(null)
  const focusPrompt = useCallback(
    (nodeID: string) =>
      setPromptFocus((v) => ({ seq: (v?.seq ?? 0) + 1, nodeID })),
    [],
  )
  const clearPromptFocus = useCallback(() => setPromptFocus(null), [])
  // 新建的文本节点等它出现在投影里再进入编辑。
  const pendingTextEdit = useRef<string | null>(null)
  // 派生节点的预填提示词：节点出现在投影里后写进本机草稿（PRD CV-71/72）。
  const pendingPrefill = useRef<{ id: string; text: string } | null>(null)
  const [help, setHelp] = useState(false)
  const [panMode, setPanMode] = useState(false)
  const { canvasID = '' } = useParams<{ canvasID: string }>()
  // 资产库当前已载入的素材（结果网格与分组预览），供画布拖放与参考标题查找。
  const [assets, setAssets] = useState<api.Asset[]>([])
  const [referenceRequest, setReferenceRequest] = useState<{
    seq: number
    targetID: string
    ids: string[]
  } | null>(null)
  const referenceSeq = useRef(0)
  const [nodeDragging, setNodeDragging] = useState(false)
  const [libraryTask, setLibraryTask] = useState<LibraryTask | null>(null)
  const viewCenter = useRef<(() => { x: number; y: number }) | null>(null)
  const [catalog, setCatalog] = useState(emptyCatalog)
  const [libraryLoading, setLibraryLoading] = useState(true)
  const [selected, setSelected] = useState<string | null>(null)
  const [selection, setSelection] = useState<string[]>([])
  const [inlineEditing, setInlineEditing] = useState<{
    id: string
    draft: Draft | null
    previousDraft?: Draft
  } | null>(null)
  const inlineRequest = useRef(0)
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
  const [actionError, setActionError] = useState('')
  const [copyNotice, setCopyNotice] = useState('')
  const [localError, setLocalError] = useState('')
  const [mediaError, setMediaError] = useState('')
  const [mutationLoading, setLoading] = useState(false)
  const {
    canvas,
    syncError,
    loading: canvasLoading,
    refreshCanvas,
  } = useCanvasSnapshot(canvasID, tick)
  const loading = canvasLoading || mutationLoading
  const [discard, setDiscard] = useState<string | null>(null)
  const [form, setForm] = useState<'asset' | null>(null)
  const [preview, setPreview] = useState<{
    nodeID: string
    revision?: string
  } | null>(null)
  const [savingNode, setSavingNode] = useState<api.CanvasNode | null>(null)
  // 多选「存入资产库…」：一次选分组，逐个入库（PRD CV-81）。
  const [savingMany, setSavingMany] = useState<string[] | null>(null)
  const nodeFileInput = useRef<HTMLInputElement>(null)
  const canvasFileInput = useRef<HTMLInputElement>(null)
  const canvasFileTarget = useRef({ x: 0, y: 0 })
  const nodeUploadTarget = useRef<{ id: string; kind: MediaKind } | null>(null)
  const [compare, setCompare] = useState<{
    content: api.ContentDraft
    node: api.CanvasNode
    inlineDraft?: Draft
  } | null>(null)
  const [rename, setRename] = useState<{
    id: string
    name: string
    revision: string
  } | null>(null)
  const outbox = useMemo(() => queue?.value.outbox ?? [], [queue?.value.outbox])
  // 本会话在哪张画布上拿到过服务端保存回执；之前不显示「已保存」。
  const [confirmedCanvas, setConfirmedCanvas] = useState<string | null>(null)
  useEffect(() => {
    if (
      canvasID &&
      outbox.some((i) => i.canvasID === canvasID && i.state === 'confirmed')
    )
      setConfirmedCanvas(canvasID)
  }, [outbox, canvasID])
  // The canvas the photographer sees: snapshot plus local edits on their way.
  const view = useMemo(
    () =>
      canvas && canvas.id === canvasID ? projectCanvas(canvas, outbox) : canvas,
    [canvas, canvasID, outbox],
  )
  useEffect(() => {
    if (!view) return
    const live = new Set(view.nodes.map((n) => n.id))
    const aliases = Object.assign({}, ...outbox.map(previewAliases)) as Record<
      string,
      string
    >
    const resolve = (id: string) => resolvePreviewID(id, aliases)
    setSelection((old) =>
      old.every((id) => live.has(id))
        ? old
        : old.map(resolve).filter((id) => live.has(id)),
    )
    setSelected((old) => (old && live.has(resolve(old)) ? resolve(old) : null))
    if (Object.keys(aliases).length)
      setRedoStack((old) =>
        old.map((i) =>
          i.actions ? { ...i, actions: remapActions(i.actions, aliases) } : i,
        ),
      )
  }, [view, outbox])
  // Nodes created by an effect the projection cannot paint
  // are selected once the snapshot that contains them has arrived.
  const deferredSelection = useRef<{
    ids: string[]
    revision: string
  } | null>(null)
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
  const { full, error: nodeError, retry: retryNode } = useNodeContent(node)
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
  const canWrite =
    canEditLocal && !syncError && !!queue && !storageError && !localError
  // Local edits stay enabled while a rejected request awaits a decision only
  // if it is not a canvas command; a refused canvas edit must be resolved first.
  const editable =
    canWrite &&
    !(queue.value.job?.state === 'rejected' && queue.value.job.intent)
  const refresh = useCallback(() => {
    setLibraryLoading(true)
    setTick((v) => v + 1)
  }, [])
  const canMove = editable
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
    setSelected(null)
    setActionError('')
    setSelection([])
    setInlineEditing(null)
    inlineRequest.current++
    setMaximized(null)
    setEditing(false)
    setCompare(null)
    setRename(null)
    setDiscard(null)
    setRedoStack([])
    setLibraryTask(null)
  }, [canvasID])
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
    if (!queue || !view || view.id !== canvasID || !editable)
      return Promise.resolve(false)
    setActionError('')
    const staged = queue.stageIntent(canvasID, {
      ...intent,
      draftKey: options.draftKey,
      draftValue: options.draftKey
        ? queue.value.drafts[options.draftKey]
        : undefined,
    })
    const existing = new Set(view.nodes.map((n) => n.id))
    const createdLocally = projectCanvas(canvas!, queue.outbox)
      .nodes.filter((n) => !existing.has(n.id))
      .map((n) => n.id)
    if (options.select?.length) onSelection(options.select)
    else if (createdLocally.length) onSelection(createdLocally)
    return staged.done.then(
      (receipt) => {
        if (receipt.omitted_reference_ids.length)
          setCopyNotice(
            `已复制选中节点；${receipt.omitted_reference_ids.length} 条指向选区外的引用未复制。`,
          )
        const created = receipt.created_ids.filter((id) =>
          id.startsWith('cwnode_'),
        )
        if (created.length && !options.select?.length && !createdLocally.length)
          deferredSelection.current = {
            ids: created,
            revision: receipt.result_revision,
          }
        // Refresh the canvas without reloading the library or blocking edits.
        void refreshCanvas.current()
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
  async function perform(
    path: string,
    value: unknown,
    draftKey?: string,
    onResult?: (result: unknown) => void,
  ) {
    if (!queue || blocked || queue.busy || queue.value.job) return false
    if (path.startsWith('/canvases/')) return false
    setActionError('')
    setLoading(true)
    try {
      const result = await queue.enqueue(path, value, draftKey)
      onResult?.(result)
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
        setLoading(false)
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
        setLoading(false)
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
        if (
          cancelled.draftKey &&
          JSON.stringify(queue.value.drafts[cancelled.draftKey]) ===
            JSON.stringify(cancelled.draftValue)
        ) {
          const drafts = { ...queue.value.drafts }
          delete drafts[cancelled.draftKey]
          void queue
            .update({ ...queue.value, drafts })
            .catch((error: unknown) => setLocalError(errorMessage(error)))
        }
        setRedoStack((old) => [...old, cancelled])
        return
      }
      if (!history.canUndo) return
      setRedoStack([])
      void stage({ kind: 'undo' })
      return
    }
    if (!history.canRedo) return
    const cancelled = queue.cancelTail(canvasID, 'undo')
    if (cancelled) return
    const local = redoStack.at(-1)
    if (local && local.canvasID === canvasID) {
      setRedoStack((old) => old.slice(0, -1))
      void stage({
        kind: 'actions',
        actions: local.actions,
        preview: local.preview,
      })
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
  const mediaKindOfFile = (file: File): MediaKind | null => {
    if (!uploads.capabilities) return null
    const classified = classifyFile(file, uploads.capabilities)
    return 'error' in classified ? null : classified.kind
  }
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
          readableFileName(file.name)
            .replace(/\.[^.]+$/, '')
            .slice(0, 200) || readableFileName(file.name),
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
            'core.image' | 'core.video' | 'core.audio',
          title: readableFileName(file.name)
            .replace(/\.[^.]+$/, '')
            .slice(0, 200),
          x: x + i * 36,
          y: y + i * 36,
        })),
      },
      { select: ids },
    )
    if (!ok) return
    uploads.start(usable, (_, i) => ({
      kind: 'node',
      node: {
        canvas_id: canvasID,
        node_id: ids[i]!,
        expected_data_revision: '1',
      },
    }))
  }
  function uploadToNode(id: string, kind: MediaKind) {
    if (!uploads.capabilities || uploads.capabilitiesError) {
      setMediaError(uploads.capabilitiesError || '媒体上传暂不可用')
      return
    }
    const input = nodeFileInput.current
    if (!input) return
    const accept = mediaFileAccept(uploads.capabilities, kind)
    if (!accept) {
      setMediaError('当前未启用此类媒体上传')
      return
    }
    nodeUploadTarget.current = { id, kind }
    input.accept = accept
    input.click()
  }
  function nodeFileChosen(files: File[]) {
    const target = nodeUploadTarget.current
    nodeUploadTarget.current = null
    const n = view?.nodes.find((n) => n.id === target?.id)
    if (!target || !n || !view || !files.length || !uploads.capabilities) return
    const file = files[0]!
    const classified = classifyFile(file, uploads.capabilities, target.kind)
    if ('error' in classified) {
      setMediaError(classified.error)
      return
    }
    setMediaError('')
    uploads.start([file], () => ({
      kind: 'node',
      node: {
        canvas_id: view.id,
        node_id: n.id,
        expected_data_revision: n.data_revision,
      },
    }))
  }
  function uploadFileToNode(id: string, file: File) {
    const n = view?.nodes.find((v) => v.id === id)
    if (!n || !view || !uploads.capabilities) return
    const kind = n.metadata.type_key.slice(5) as MediaKind
    const classified = classifyFile(file, uploads.capabilities, kind)
    if ('error' in classified) {
      setMediaError(classified.error)
      return
    }
    setMediaError('')
    uploads.start([file], () => ({
      kind: 'node',
      node: {
        canvas_id: view.id,
        node_id: n.id,
        expected_data_revision: n.data_revision,
      },
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
  // Several selected assets land in one batch so the queue sends one command:
  // a horizontal row centred on the drop point (or the visible centre).
  function addAssets(dropped: api.Asset[], x?: number, y?: number) {
    if (!view || !editable) return
    const usable = dropped.filter((a) => !a.unavailable)
    if (!usable.length) return
    const at =
      x === undefined || y === undefined
        ? (viewCenter.current?.() ?? { x: 80, y: 80 })
        : { x, y }
    const gap = 40
    const x0 =
      at.x - ((usable.length - 1) * (NODE_WIDTH + gap)) / 2 - NODE_WIDTH / 2
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
          x: x0 + i * (NODE_WIDTH + gap),
          y: at.y - defaultNodeSize(`core.${asset.kind}`).height / 2,
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
  function addAsset(asset: api.Asset, x?: number, y?: number) {
    addAssets([asset], x, y)
  }
  const assetRef = (a: api.Asset) => ({
    asset_id: a.id,
    expected_asset_revision: a.revision,
    content_revision_id: a.content_revision_id,
  })
  // 空节点直接用这份素材，不另建节点；节点没有名称时沿用素材名。
  function fillNode(id: string, asset: api.Asset) {
    const n = view?.nodes.find((v) => v.id === id)
    if (
      !n ||
      n.content ||
      asset.unavailable ||
      n.metadata.type_key !== `core.${asset.kind}`
    )
      return
    setRedoStack([])
    void stage(
      {
        kind: 'actions',
        preview: asset.content ? { [id]: asset.content } : undefined,
        titles: { [id]: asset.title },
        actions: [
          { type: 'fill_from_asset', node_id: id, asset: assetRef(asset) },
        ],
      },
      { select: [id] },
    )
  }
  // 为节点选参考：素材竖排在目标左侧并连线；左边已有节点就继续往左让。
  function pickReferences(targetID: string, picked: api.Asset[]) {
    const target = view?.nodes.find((n) => n.id === targetID)
    if (!view || !target || !queue || !editable || !picked.length) return
    const heights = picked.map((a) => {
      const m = a.content?.media?.find((x) => x.role === 'original')
      return (a.kind === 'image' || a.kind === 'video') && m?.width && m.height
        ? (NODE_WIDTH * m.height) / m.width
        : a.kind === 'image' || a.kind === 'video'
          ? NODE_WIDTH
          : defaultNodeSize(`core.${a.kind}`).height
    })
    const gap = 32
    const column =
      heights.reduce((sum, h) => sum + h, 0) + gap * (heights.length - 1)
    const top = target.metadata.y + target.metadata.height / 2 - column / 2
    const others = view.nodes.filter(
      (n) =>
        n.id !== targetID &&
        n.metadata.type_key !== 'core.group' &&
        !n.parent_id,
    )
    const hit = (x: number) =>
      others.some(
        (n) =>
          n.metadata.x < x + NODE_WIDTH + 24 &&
          n.metadata.x + n.metadata.width + 24 > x &&
          n.metadata.y < top + column + 24 &&
          n.metadata.y + n.metadata.height + 24 > top,
      )
    let x = target.metadata.x - NODE_WIDTH - 100
    for (let i = 0; i < 8 && hit(x); i++) x -= NODE_WIDTH + 60
    const ids = picked.map(() => `cwnode_${crypto.randomUUID()}`)
    const preview: Record<string, api.Content> = {}
    let y = top
    const actions: api.GraphAction[] = []
    for (const [i, a] of picked.entries()) {
      if (a.content) preview[ids[i]!] = a.content
      actions.push({
        type: 'add_node',
        node_id: ids[i]!,
        type_key: `core.${a.kind}`,
        title: a.title,
        x,
        y,
        asset: assetRef(a),
      })
      y += heights[i]! + gap
    }
    for (const id of ids)
      actions.push({
        type: 'connect_reference',
        source_node_id: id,
        target_node_id: targetID,
        source_port: 'output',
        target_port: 'reference',
        role: 'reference',
      })
    closeLibrary()
    setRedoStack([])
    void stage({ kind: 'actions', preview, actions }, { select: [targetID] })
      .then(async (ok) => {
        if (!ok) return
        // 即使选材期间关闭输入框或切到另一个节点，引用也要留在目标的本机草稿中。
        const key = `prompt:${view.id}:${targetID}`
        const base =
          queue.value.drafts[key]?.prompt ?? promptFromNode(view, target)
        const missing = ids.filter(
          (id) => !base.text.includes(mentionToken(id)),
        )
        const next = {
          ...base,
          text: [base.text.trimEnd(), ...missing.map(mentionToken)]
            .filter(Boolean)
            .join(' '),
        }
        await queue.update({
          ...queue.value,
          drafts: {
            ...queue.value.drafts,
            [key]: {
              kind: 'text',
              title: next.model_key,
              value: next.text,
              prompt: next,
            },
          },
        })
        setReferenceRequest({ seq: ++referenceSeq.current, targetID, ids })
      })
      .catch((e) => setLocalError(errorMessage(e)))
    setCopyNotice(`已连接 ${picked.length} 个参考`)
  }
  function openLibraryTask(task: LibraryTask) {
    setLibraryTask(task)
    setLibraryOpen(true)
  }
  function closeLibrary() {
    setLibraryTask(null)
    setLibraryOpen(false)
  }
  // 节点存回资产库：同一内容已在库里时只追加归类，不重复建资产。
  async function saveNodes(ids: string[], groupID: string | null) {
    if (!view) return
    const nodes = ids
      .map((id) => view.nodes.find((n) => n.id === id))
      .filter(
        (n): n is api.CanvasNode =>
          !!n &&
          !!n.content &&
          n.capabilities.actions.includes('save_to_library'),
      )
    if (!nodes.length) {
      setCopyNotice('这些节点还没有内容，无法存入资产库')
      return
    }
    let fresh = 0,
      filed = 0
    for (const n of nodes) {
      const ok = await perform(
        '/assets/from-canvas-node',
        {
          canvas_id: view.id,
          node_id: n.id,
          expected_data_revision: n.data_revision,
          target: {
            title:
              n.metadata.title ||
              (n.content?.kind === 'text' ? '文字素材' : '未命名素材'),
            group_ids: groupID ? [groupID] : [],
          },
        },
        undefined,
        (result) => {
          if (
            result &&
            typeof result === 'object' &&
            'reused' in result &&
            result.reused
          )
            filed++
          else fresh++
        },
      )
      if (!ok) return
    }
    const where = groupID
      ? `「${catalog.groups.find((g) => g.id === groupID)?.name ?? '分组'}」`
      : '资产库'
    setCopyNotice(
      fresh
        ? `已存入${where} ${fresh} 项${filed ? `，${filed} 项已在库中，已归入` : ''}`
        : `已归入${where}`,
    )
  }
  function editInline(n: api.CanvasNode) {
    const request = ++inlineRequest.current
    const key = `node:${canvasID}:${n.id}`
    const existing = queue?.value.drafts[key]
    const base: Draft = existing ?? {
      kind: n.metadata.type_key === 'core.link' ? 'link' : 'text',
      title: n.metadata.title,
      value: contentText(n.content?.payload),
      dataRevision: n.data_revision,
      contentRevision: n.data.content_revision_id,
    }
    setInlineEditing({
      id: n.id,
      draft: existing || !n.content?.truncated ? base : null,
      previousDraft: existing,
    })
    if (!existing && n.content?.truncated && n.data.content_revision_id)
      void api
        .read<api.Content>(
          `/content-revisions/${encodeURIComponent(n.data.content_revision_id)}`,
        )
        .then((content) => {
          if (inlineRequest.current === request)
            setInlineEditing({
              id: n.id,
              draft: { ...base, value: contentText(content.payload) },
            })
        })
        .catch((error: unknown) => {
          if (inlineRequest.current === request) {
            setInlineEditing(null)
            setActionError(errorMessage(error))
          }
        })
  }
  useEffect(() => {
    const p = pendingPrefill.current
    if (!p || !queue || !view || !canvasID) return
    const n = view.nodes.find((v) => v.id === p.id)
    if (!n) return
    pendingPrefill.current = null
    const key = `prompt:${canvasID}:${n.id}`
    if (queue.value.drafts[key]) return
    const prompt = { ...promptFromNode(view, n), text: p.text }
    void queue
      .update({
        ...queue.value,
        drafts: {
          ...queue.value.drafts,
          [key]: { kind: 'text', title: prompt.model_key, value: prompt.text, prompt },
        },
      })
      .catch((e: unknown) => setLocalError(errorMessage(e)))
  })
  // 新建文本节点后直接进入卡片内编辑（PRD CV-53）。
  useEffect(() => {
    const id = pendingTextEdit.current
    if (!id || !editable) return
    const n = view?.nodes.find((v) => v.id === id)
    if (!n) return
    pendingTextEdit.current = null
    editInline(n)
  })
  function saveNode(
    content: api.ContentDraft,
    reapply?: api.CanvasNode,
    inlineDraft?: Draft,
    inlineID?: string,
  ) {
    const draft = inlineDraft ?? queue?.value.drafts[nodeKey] ?? full
    const node = view?.nodes.find((n) => n.id === (inlineID ?? selected))
    const saveKey = `node:${canvasID}:${node?.id ?? ''}`
    if (
      !editable ||
      !view ||
      !node ||
      !draft ||
      (reapply && reapply.id !== node.id)
    )
      return
    if (
      !reapply &&
      (node.data_revision !== draft.dataRevision ||
        node.data.content_revision_id !== draft.contentRevision)
    ) {
      setCompare({ content, node: structuredClone(node), inlineDraft })
      return
    }
    setRedoStack([])
    void stage(
      {
        kind: 'actions',
        actions: [
          ...(!inlineDraft && draft.title !== node.metadata.title
            ? [
                {
                  type: 'update_metadata' as const,
                  node_id: node.id,
                  title: draft.title,
                  intent: node.metadata.intent,
                },
              ]
            : []),
          {
            type: 'replace_content',
            node_id: node.id,
            payload: content.payload,
          },
        ],
      },
      { draftKey: saveKey },
    )
    setInlineEditing(null)
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
  // v6 单键快捷键：V 选择 / H 平移 / T I O 新建文字、图片、视频（落在视口中心）。
  // 画布编辑走本机 outbox，不被资产库/项目请求占用的单通道阻塞。
  const shortcutsLive = !!view && editable
  useEffect(() => {
    const keydown = (e: KeyboardEvent) => {
      const t = e.target
      if (
        e.defaultPrevented ||
        e.isComposing ||
        e.metaKey ||
        e.ctrlKey ||
        e.altKey ||
        !(t instanceof HTMLElement) ||
        t.isContentEditable ||
        t.closest(
          'input,textarea,select,[role="textbox"],[role="dialog"],[role="menu"],[aria-modal="true"]',
        )
      )
        return
      const k = e.key.toLowerCase()
      if (k === 'v') setPanMode(false)
      else if (k === 'h') setPanMode(true)
      else if (
        shortcutsLive &&
        (k === 't' || k === 'i' || k === 'o' || k === 'a')
      ) {
        const kind =
          k === 't' ? 'text' : k === 'i' ? 'image' : k === 'o' ? 'video' : 'audio'
        setAddRequest((v) => ({ seq: v.seq + 1, kind }))
      } else return
      e.preventDefault()
    }
    window.addEventListener('keydown', keydown)
    return () => window.removeEventListener('keydown', keydown)
  }, [shortcutsLive])
  const assetDraft = queue?.value.drafts['asset-form'] ?? blankDraft()
  const job = queue?.value.job
  const hasDraft = !!queue?.value.drafts[nodeKey]
  const needsRecovery = !!job && !queue?.busy && !loading
  const status = canvasID
    ? saveStatus({
        canvasID,
        outbox,
        job,
        busy: !!queue?.busy,
        online,
        comparing: !!compare,
        confirmedOnce: confirmedCanvas === canvasID,
      })
    : 'idle'
  const showNotice = !!(
    syncError ||
    nodeError ||
    actionError ||
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
      {canvasID && (
        <AgentPanel key={canvasID} account={account} canvasID={canvasID} canvas={view} />
      )}
      <header className="cc-header">
        <Link
          className="cc-home"
          to="/creative"
          aria-label="返回创意空间"
          title="返回创意空间"
        >
          <ArrowLeft size={16} />
        </Link>
        {activeProject ? (
          <button
            className="cc-project-name"
            aria-label="项目菜单"
            aria-haspopup="menu"
            aria-expanded={projectMenu}
            onClick={() => setProjectMenu(!projectMenu)}
          >
            <h1>{activeProject.name}</h1>
            <ChevronDown size={14} />
          </button>
        ) : (
          <h1 className="cc-project-name">个人资产库</h1>
        )}
        {canvas?.archived && <span className="cc-header-note">已归档</span>}
        {status !== 'idle' && (
          <span
            className="cc-save-state"
            role="status"
            data-state={status}
          >
            {saveStatusText[status]}
          </span>
        )}
      </header>
      {projectMenu && activeProject && (
        <>
          <button
            className="cc-menu-dismiss"
            aria-label="关闭项目菜单"
            onClick={() => setProjectMenu(false)}
          />
          <div
            className="cc-menu cc-project-menu"
            role="menu"
            aria-label="项目"
          >
            <button
              role="menuitem"
              disabled={blocked || !canWrite}
              onClick={() => {
                setProjectMenu(false)
                setRename(activeProject)
              }}
            >
              <PencilLine size={16} />
              <span>重命名</span>
            </button>
            <Link role="menuitem" to="/creative">
              <LayoutGrid size={16} />
              <span>所有项目</span>
            </Link>
            <span className="cc-menu-sep" />
            <button
              role="menuitem"
              className={canvas?.archived ? '' : 'is-danger'}
              disabled={blocked}
              onClick={() => {
                setProjectMenu(false)
                void perform(
                  `/projects/${activeProject.id}/${canvas?.archived ? 'restore' : 'archive'}`,
                  { expected_revision: activeProject.revision },
                )
              }}
            >
              {canvas?.archived ? (
                <ArchiveRestore size={16} />
              ) : (
                <Archive size={16} />
              )}
              <span>{canvas?.archived ? '恢复项目' : '归档项目'}</span>
            </button>
          </div>
        </>
      )}
      <nav className="cc-tool-rail" aria-label="创作工具">
        <button
          className="cc-add-tool"
          aria-label="新建节点"
          title="新建节点"
          aria-haspopup="menu"
          disabled={!editable || !view}
          onClick={(e) => {
            const r = e.currentTarget.getBoundingClientRect()
            setAddRequest((v) => ({
              seq: v.seq + 1,
              anchor: { x: r.right + 8, y: r.top },
            }))
          }}
        >
          <Plus size={18} />
        </button>
        <span className="cc-rail-line" />
        <button
          aria-label="选择工具"
          title="选择 · V"
          aria-pressed={!panMode}
          onClick={() => setPanMode(false)}
        >
          <MousePointer2 size={18} strokeWidth={1.6} />
        </button>
        <button
          aria-label="移动画布"
          title="平移 · H / 中键 / 空格"
          aria-pressed={panMode}
          onClick={() => setPanMode(true)}
        >
          <Hand size={18} strokeWidth={1.6} />
        </button>
        <span className="cc-rail-line" />
        <button
          aria-label="资产库"
          aria-pressed={libraryOpen}
          aria-controls="creative-library"
          title="资产库"
          onClick={() => (libraryOpen ? closeLibrary() : setLibraryOpen(true))}
        >
          <Library size={18} strokeWidth={1.6} />
        </button>
        <button
          aria-label="画布快捷键"
          title="快捷键"
          onClick={() => setHelp(true)}
        >
          <CircleHelp size={18} strokeWidth={1.6} />
        </button>
      </nav>
      {(showNotice || mediaError) && (
        <section className="cc-save-status" aria-live="polite">
          {mediaError && (
            <p role="alert">
              {mediaError}{' '}
              <button className="btn" onClick={() => setMediaError('')}>
                知道了
              </button>
            </p>
          )}
          {copyNotice && (
            <p>
              {copyNotice}{' '}
              <button className="btn" onClick={() => setCopyNotice('')}>
                知道了
              </button>
            </p>
          )}
          {!online && <span>当前离线 · 输入仍保留在本机</span>}
          {(syncError ||
            nodeError ||
            actionError ||
            storageError ||
            localError) && (
            <p role="alert">
              {syncError ||
                nodeError ||
                actionError ||
                storageError ||
                localError}{' '}
              <button
                className="btn"
                onClick={() => {
                  setActionError('')
                  retryNode()
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
          task={canvasID ? libraryTask : null}
          onCreate={() => setForm('asset')}
          onClose={closeLibrary}
          onWidth={setLibraryWidth}
          onAssets={setAssets}
          onLoading={setLibraryLoading}
          onCatalog={setCatalog}
          onCommand={(path, payload) => perform(path, payload)}
          canPlace={!!view && editable}
          onPlace={(list) => addAssets(list)}
          onFill={(asset) => {
            if (libraryTask?.kind !== 'fill') return
            const id = libraryTask.nodeID
            closeLibrary()
            fillNode(id, asset)
          }}
          onPick={(list) => {
            if (libraryTask?.kind === 'pick')
              pickReferences(libraryTask.nodeID, list)
          }}
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
                  onNotice={setCopyNotice}
                  onSelect={(id) => {
                    setSelected(id)
                  }}
                  onCreated={(id, kind, prompt) => {
                    if (kind === 'text') pendingTextEdit.current = id
                    else {
                      if (prompt) pendingPrefill.current = { id, text: prompt }
                      focusPrompt(id)
                    }
                  }}
                  onFocusPrompt={focusPrompt}
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
                    // Empty media nodes upload through the explicit upload action.
                    if (
                      n &&
                      isMediaKind(n.metadata.type_key.slice(5)) &&
                      !n.content
                    )
                      return
                    if (
                      !n ||
                      !editable ||
                      !n.capabilities.actions.includes('edit') ||
                      n.status.content_state === 'unavailable'
                    )
                      return
                    if (
                      ['core.text', 'core.link'].includes(n.metadata.type_key)
                    ) {
                      onSelection([id])
                      editInline(n)
                    } else setEditing(true)
                  }}
                  inlineContent={
                    inlineEditing
                      ? {
                          nodeID: inlineEditing.id,
                          draft: inlineEditing.draft,
                          disabled: !editable,
                          onChange: (d) =>
                            writeDraft(
                              `node:${canvasID}:${inlineEditing.id}`,
                              d,
                            ),
                          onSave: (d) =>
                            saveNode(
                              {
                                kind: d.kind,
                                payload: payload(d.kind, d.value),
                              },
                              undefined,
                              d,
                              inlineEditing.id,
                            ),
                          onCancel: (original) => {
                            if (queue) {
                              const key = `node:${canvasID}:${inlineEditing.id}`
                              const drafts = { ...queue.value.drafts }
                              if (inlineEditing.previousDraft)
                                drafts[key] = original
                              else delete drafts[key]
                              void queue
                                .update({ ...queue.value, drafts })
                                .catch((error: unknown) =>
                                  setLocalError(errorMessage(error)),
                                )
                            }
                            inlineRequest.current++
                            setInlineEditing(null)
                          },
                        }
                      : undefined
                  }
                  onPickFiles={(x, y) => {
                    canvasFileTarget.current = { x, y }
                    canvasFileInput.current?.click()
                  }}
                  onOpenLibrary={() => {
                    setLibraryTask(null)
                    setLibraryOpen(true)
                  }}
                  addRequest={addRequest}
                  panMode={panMode}
                  disabled={!editable}
                  moveDisabled={!canMove}
                  assets={assets}
                  onDropAsset={addAsset}
                  onDropAssets={addAssets}
                  onDropFiles={(files, x, y) => void dropFiles(files, x, y)}
                  onUploadToNode={uploadToNode}
                  onDownload={(id) => void downloadNode(id)}
                  onSaveToLibrary={(id) =>
                    setSavingNode(view.nodes.find((n) => n.id === id) ?? null)
                  }
                  onSaveNodes={(ids, groupID) => void saveNodes(ids, groupID)}
                  onSaveNodesAsk={setSavingMany}
                  onFillNode={fillNode}
                  onUploadFileToNode={uploadFileToNode}
                  onLibraryFill={(id, kind) => {
                    const n = view.nodes.find((v) => v.id === id)
                    if (!n || !isMediaKind(kind)) return
                    openLibraryTask({
                      kind: 'fill',
                      nodeID: id,
                      mediaKind: kind,
                      title: n.metadata.title || '节点',
                    })
                  }}
                  onPickReferences={(id) => {
                    const n = view.nodes.find((v) => v.id === id)
                    if (n)
                      openLibraryTask({
                        kind: 'pick',
                        nodeID: id,
                        title: n.metadata.title || '节点',
                      })
                  }}
                  centerRef={viewCenter}
                  onDraggingChange={setNodeDragging}
                  onPreview={(nodeID, revision) =>
                    setPreview({ nodeID, revision })
                  }
                  onMove={(n, x, y) => moveSelection([{ node: n, x, y }])}
                />
                {/* 只在单选节点时出现（PRD GN-01）：多选时不显示，避免误以为只对焦点节点生成。 */}
                {node &&
                  queue &&
                  !inlineEditing &&
                  selection.length <= 1 &&
                  node.capabilities.prompt_mode !== 'unavailable' && (
                    <NodeGenerationPanel
                      key={`${canvasID}:${node.id}`}
                      canvas={view}
                      node={node}
                      queue={queue}
                      assets={assets}
                      dragging={nodeDragging}
                      leftInset={libraryOpen ? libraryWidth + 80 : 64}
                      disabled={
                        !editable ||
                        blocked ||
                        !online ||
                        outbox.some(
                          (i) =>
                            i.canvasID === canvasID && i.state !== 'confirmed',
                        )
                      }
                      refresh={() => refreshCanvas.current()}
                      onPickReferences={() =>
                        openLibraryTask({
                          kind: 'pick',
                          nodeID: node.id,
                          title: node.metadata.title || '节点',
                        })
                      }
                      referenceRequest={referenceRequest}
                      onReferencesInserted={() => setReferenceRequest(null)}
                      focusRequest={promptFocus}
                      onFocused={clearPromptFocus}
                      onDisconnect={(edgeID) =>
                        graphActions([
                          { type: 'disconnect_reference', edge_id: edgeID },
                        ])
                      }
                    />
                  )}
              </ReactFlowProvider>
            </>
          ) : (
            <div className="cc-canvas-empty">
              <h2>{syncError ? '画布暂时没有打开' : '正在打开画布…'}</h2>
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
      <input
        ref={nodeFileInput}
        type="file"
        hidden
        aria-label="选择节点媒体文件"
        onChange={(e) => {
          const files = Array.from(e.target.files ?? [])
          e.target.value = ''
          nodeFileChosen(files)
        }}
      />
      <input
        ref={canvasFileInput}
        type="file"
        hidden
        multiple
        aria-label="选择要放入画布的文件"
        onChange={(e) => {
          const files = Array.from(e.target.files ?? [])
          e.target.value = ''
          const { x, y } = canvasFileTarget.current
          if (files.length) void dropFiles(files, x, y)
        }}
      />
      <UploadTray
        items={uploads.items}
        onRetry={uploads.retry}
        onDismiss={uploads.dismiss}
      />
      {preview &&
        (() => {
          const n = view?.nodes.find((n) => n.id === preview.nodeID)
          return n?.content && isMediaKind(n.metadata.type_key.slice(5)) ? (
            <MediaPreview
              initialRevision={preview.revision}
              content={n.content}
              kind={n.metadata.type_key.slice(5)}
              title={n.metadata.title || '媒体节点'}
              canvasID={canvasID}
              nodeID={n.id}
              dataRevision={`${n.data_revision}:${view?.revision}`}
              selectedVersion={n.data.selected_version_id}
              disabled={!editable || blocked || !online}
              onSelectVersion={(id) =>
                graphActions([
                  { type: 'select_version', node_id: n.id, version_id: id },
                ])
              }
              onClose={() => setPreview(null)}
            />
          ) : null
        })()}
      {savingNode && view && (
        <StudioDialog
          title="存入个人资产库"
          onClose={() => setSavingNode(null)}
        >
          <form
            className="cc-form"
            noValidate
            onSubmit={(e) => {
              e.preventDefault()
              const data = new FormData(e.currentTarget)
              const title = String(data.get('title') ?? '').trim()
              if (!title) {
                setActionError('请填写资产名称')
                return
              }
              const groupID = String(data.get('group') ?? '')
              let reused = false
              void perform(
                '/assets/from-canvas-node',
                {
                  canvas_id: view.id,
                  node_id: savingNode.id,
                  expected_data_revision: savingNode.data_revision,
                  target: { title, group_ids: groupID ? [groupID] : [] },
                },
                undefined,
                (result) => {
                  reused = !!(
                    result &&
                    typeof result === 'object' &&
                    'reused' in result &&
                    result.reused
                  )
                },
              ).then((ok) => {
                if (!ok) return
                setSavingNode(null)
                const where = groupID
                  ? `「${catalog.groups.find((g) => g.id === groupID)?.name ?? '分组'}」`
                  : '资产库'
                setCopyNotice(
                  reused
                    ? `这份内容已在资产库中，已归入${where}`
                    : `已存入${where}`,
                )
              })
            }}
          >
            <p className="cc-muted">
              固定当前节点内容为个人库资产；后续修改节点不会影响它。同一内容已在库中时只追加归类。
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
            <label>
              归入分组
              <select className="input" name="group" defaultValue="">
                <option value="">不归类，只存入</option>
                {catalog.groups
                  .filter((g) => !g.parent_id)
                  .flatMap((g) => [
                    g,
                    ...catalog.groups.filter((c) => c.parent_id === g.id),
                  ])
                  .map((g) => (
                    <option key={g.id} value={g.id}>
                      {g.parent_id ? `　${g.name}` : g.name}
                    </option>
                  ))}
              </select>
            </label>
            <button className="btn btn-primary" disabled={blocked}>
              保存资产
            </button>
          </form>
        </StudioDialog>
      )}
      {savingMany && view && (
        <StudioDialog
          title="存入个人资产库"
          onClose={() => setSavingMany(null)}
        >
          <form
            className="cc-form"
            noValidate
            onSubmit={(e) => {
              e.preventDefault()
              const groupID = String(
                new FormData(e.currentTarget).get('group') ?? '',
              )
              const ids = savingMany
              setSavingMany(null)
              void saveNodes(ids, groupID || null)
            }}
          >
            <p className="cc-muted">
              已选 {savingMany.length} 项，有内容的节点各存为一份资产，名称取节点标题。
            </p>
            <label>
              归入分组
              <select className="input" name="group" defaultValue="">
                <option value="">不归类，只存入</option>
                {catalog.groups
                  .filter((g) => !g.parent_id)
                  .flatMap((g) => [
                    g,
                    ...catalog.groups.filter((c) => c.parent_id === g.id),
                  ])
                  .map((g) => (
                    <option key={g.id} value={g.id}>
                      {g.parent_id ? `　${g.name}` : g.name}
                    </option>
                  ))}
              </select>
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
            <dd>滚轮 / 中键拖动 / 未选中时按住空格拖动</dd>
            <dt>缩放画布</dt>
            <dd>Ctrl / ⌘ + 滚轮；⌘+ / ⌘- 放大缩小，⌘0 回到 100%，⇧1 适应画布</dd>
            <dt>全屏查看</dt>
            <dd>选中一张有内容的图片或视频后按空格</dd>
            <dt>编辑节点</dt>
            <dd>双击标题改名，双击文字节点编辑正文；失焦保存，Esc 取消</dd>
            <dt>右键菜单</dt>
            <dd>右键画布添加节点，右键节点、分组或连线查看操作</dd>
            <dt>添加节点</dt>
            <dd>双击画布空白处，或按 T / I / O / A 直接新建文字、图片、视频、音频</dd>
            <dt>选择 / 平移工具</dt>
            <dd>V / H</dd>
            <dt>适应画布</dt>
            <dd>Shift + 1</dd>
            <dt>打组 / 解组</dt>
            <dd>⌘ / Ctrl + G，按住 Shift 解组</dd>
            <dt>复制 / 撤销 / 重做</dt>
            <dd>⌘ / Ctrl + D / Z / Shift + Z</dd>
            <dt>断开参考</dt>
            <dd>双击参考连线</dd>
            <dt>微调节点</dt>
            <dd>选中节点后按方向键，按住 Shift 每次 10px</dd>
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
      {compare && (
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
            saveNode(
              content.content,
              content.node,
              content.inlineDraft,
              content.node.id,
            )
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
