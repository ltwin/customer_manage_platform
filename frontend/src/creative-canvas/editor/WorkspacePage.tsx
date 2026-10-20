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
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'
import { ReactFlowProvider } from '@xyflow/react'
import ConfirmDialog from '../../components/ConfirmDialog'
import { useOnline } from './useOnline.ts'
import { useJournal } from './useJournal.ts'
import { errorMessage, isWithdrawn } from './queue.ts'
import type { Draft } from './journal.ts'

import * as api from './api.ts'
import { assetActions, isCanvasAsset } from './assetPlacement.ts'
import { useCanvasSnapshot } from './useCanvasSnapshot.ts'
import { useAgentRuns } from './agentRuns.ts'
import { isActiveRun } from './agentState.ts'
import { useSceneHost, type SceneJobLauncher } from '../../creative-scene/host/launcher.ts'
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
import SaveModelDialog from './SaveModelDialog.tsx'
import type { SaveModelRequest } from '../../creative-scene/saveModel.ts'

type SceneEditorProps = { documentID: string; onClose: () => void; host?: SceneJobLauncher; canvasID?: string; sceneNodeID?: string; onSaveModel?: (request: SaveModelRequest) => void; onOpenLibrary?: (place: (asset: api.Asset) => void) => void }
// three 和 R3F 只在打开场景时加载（AC-10）；加载失败给出关闭入口，不让画布卡住。
const SceneEditor = lazy(() =>
  import('../../creative-scene/SceneEditor.tsx').catch(() => ({
    default: function SceneEditorUnavailable({ onClose }: SceneEditorProps) {
      return (
        <section className="cc-maximized-document cc-surface" aria-label="3D 场景">
          <div className="cc-document-body" role="alert">
            <p>3D 编辑器加载失败，请检查网络后刷新页面</p>
            <button className="btn" onClick={onClose}>
              回到画布
            </button>
          </div>
        </section>
      )
    },
  })),
)
import CanvasView from './CanvasView.tsx'
import NodeGenerationPanel from './NodeGenerationPanel.tsx'
import ContentForm from './ContentForm.tsx'
import StudioDialog from './StudioDialog.tsx'
import { contentText, payload } from './content.ts'
import '@xyflow/react/dist/style.css'
import './workspace.css'
import LibraryDrawer, { type LibraryTask } from './LibraryDrawer.tsx'
import AgentPanel from './AgentPanel.tsx'
import { emptyCatalog, groupOptions } from './libraryState.ts'
import Picker from './Picker.tsx'
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
  type MediaKind,
} from './media.ts'
import { fileTitle, importPlan, importSlot } from './fileImport.ts'
import { localMedia, readMediaSize } from './localMedia.ts'
import {
  catalogRejections,
  keepGenerationCatalogFresh,
  refreshGenerationCatalog,
} from './generationCatalog.ts'

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
  // 模型目录与画布无关：进工作区就预取，切换画布不重读。
  useEffect(() => keepGenerationCatalogFresh(), [])
  // 生成被服务端明确拒绝可能是目录过期（模型撤下、配置版本变化）：
  // 每个被拒操作只刷新一次目录，不改原请求，也不重新提交。
  const rejected = queue?.value.job
  const staleCatalog =
    rejected?.state === 'rejected' &&
    /^\/canvases\/[^/]+\/executions$/.test(rejected.path) &&
    !!rejected.code &&
    catalogRejections.includes(rejected.code)
      ? rejected.operation
      : ''
  useEffect(() => {
    if (staleCatalog) refreshGenerationCatalog()
  }, [staleCatalog])
  const [libraryWidth, setLibraryWidth] = useState(344)
  // v6：资产库默认收起，从工具轨打开。
  const [libraryOpen, setLibraryOpen] = useState(false)
  const [projectMenu, setProjectMenu] = useState(false)
  const [maximized, setMaximized] = useState<{
    id: string
    typeKey: string
    libraryOpen: boolean
    /** The node it was opened from: a scene's exports go beside it. */
    nodeID: string
  } | null>(null)
  const [editing, setEditing] = useState(false)
  const [addRequest, setAddRequest] = useState<{
    seq: number
    kind?: 'text' | 'image' | 'video' | 'audio'
    anchor?: { x: number; y: number }
  }>({ seq: 0 })
  // 把焦点交给某个节点的生成输入框；面板解锁并聚焦后清掉（PRD CV-53/54）。
  // generate 为真时是失败卡片上的重试：面板解锁后直接按当前提示词生成。
  const [promptFocus, setPromptFocus] = useState<{
    seq: number
    nodeID: string
    generate?: boolean
  } | null>(null)
  const focusPrompt = useCallback(
    (nodeID: string, generate = false) =>
      setPromptFocus((v) => ({ seq: (v?.seq ?? 0) + 1, nodeID, generate })),
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
  // 首页一句话开始进入画布时带着 openAgent，助手面板直接打开。
  const openAgent = (useLocation().state as { openAgent?: boolean } | null)?.openAgent === true
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
  // The assistant's scene jobs run on this page, scene editor open or not.
  const sceneHost = useSceneHost(account)
  const agentLive = useAgentRuns((store) =>
    Object.values(store.runs).some((view) => !!view && isActiveRun(view.run)),
  )
  const {
    canvas,
    syncError,
    loading: canvasLoading,
    refreshCanvas,
  } = useCanvasSnapshot(canvasID, tick, !!sceneHost.state.holding || agentLive)
  const loading = canvasLoading || mutationLoading
  const [discard, setDiscard] = useState<string | null>(null)
  const [preview, setPreview] = useState<{
    nodeID: string
    revision?: string
  } | null>(null)
  const [savingNode, setSavingNode] = useState<api.CanvasNode | null>(null)
  const [savingModel, setSavingModel] = useState<SaveModelRequest | null>(null)
  // 场景里「从资产库添加…」：抽屉点中的模型交回编辑器放置。
  const scenePlace = useRef<((asset: api.Asset) => void) | null>(null)
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
  // 空媒体节点的比例标签跟随面板里尚未保存的选择（V6-021 AC-03）。
  const draftRatios = useMemo(() => {
    const prefix = `prompt:${view?.id ?? ''}:`
    const out: Record<string, string> = {}
    for (const [key, draft] of Object.entries(queue?.value.drafts ?? {})) {
      const ratio = draft.prompt?.parameters.aspect_ratio
      if (key.startsWith(prefix) && typeof ratio === 'string')
        out[key.slice(prefix.length)] = ratio
    }
    return out
  }, [queue?.value.drafts, view?.id])
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
    setTick((v) => v + 1)
  }, [])
  const canMove = editable
  const uploads = useUploads(
    (item) => {
      const nodeID = item.nodeID
      if (!nodeID) {
        if (item.upload?.binding?.status === 'needs_review')
          setCopyNotice('文件已上传，但目标已变化；请在待处理项中选择放入位置。')
        refresh()
        return
      }
      if (item.stage === 'failed') {
        localMedia.patch(nodeID, { failed: true })
        return
      }
      if (item.batch) pendingFills.current.delete(item.batch)
      // 上传期间节点被删掉或已经有了别的内容：不再放进去，放弃暂存。
      const target = view?.nodes.find((n) => n.id === nodeID)
      if (!target || target.content) {
        localMedia.clear(nodeID)
        if (item.upload) uploads.discardStaged(item.upload)
        return
      }
      // 暂存就绪：放进节点也是一次用户编辑，和建节点同组、按顺序发出。
      uploads.markPlaced(item.id)
      const g = item.batch ? importGroups.current.get(item.batch) : undefined
      if (g) g.fills++
      void stage({
        kind: 'actions',
        changeGroupID: item.batch,
        actions: [
          { type: 'fill_from_upload', node_id: nodeID, upload_id: item.upload!.id },
        ],
      })
    },
    (item, sent) => {
      if (item.nodeID)
        localMedia.patch(item.nodeID, { progress: item.size ? sent / item.size : 1 })
    },
  )
  // 单节点填充还在上传时，⌘Z 先撤掉这次上传，不越过它去撤更早的编辑。
  // edits 记下开始上传时的编辑序号：之后有新编辑，就轮不到它。
  const pendingFills = useRef(
    new Map<string, { nodes: string[]; edits: number }>(),
  )
  const editCount = useRef(0)
  // 每个导入组建了哪些节点（created）或填了哪个已有节点，撤销时据此立刻画出结果。
  // edits/fills 用来判断这一批之后有没有插进别的编辑：插进了就只会撤掉其中一步，
  // 不能提前把整批画没。
  const importGroups = useRef(
    new Map<
      string,
      { nodes: string[]; created: boolean; edits: number; fills: number }
    >(),
  )
  const undoPaint = (group: string | undefined) => {
    const g = group ? importGroups.current.get(group) : undefined
    if (!g || editCount.current !== g.edits + g.fills) return {}
    return g.created ? { hides: g.nodes } : { empties: g.nodes }
  }
  // 撤掉一批导入时，本机预览随节点一起消失。
  const clearLocal = (group: string) => {
    for (const item of uploads.items)
      if (item.batch === group && item.nodeID) localMedia.clear(item.nodeID)
  }
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
    editCount.current++
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
      await queue.flush(true)
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
  // 单节点填充还在上传时也可以撤销（撤销即取消这次上传）。
  const fillUploading = uploads.items.some(
    (i) =>
      !!i.batch &&
      pendingFills.current.has(i.batch) &&
      !['done', 'failed', 'rejected', 'cancelled'].includes(i.stage),
  )
  const history = view
    ? historyOf(view, outbox, redoStack)
    : {
        canUndo: false,
        canRedo: false,
        undo: undefined,
        undoStepGroup: undefined,
        redo: undefined,
      }
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
      // 单节点填充还在上传、之后没有别的编辑：这次上传就是最近的一步。
      const fill = [...pendingFills.current].at(-1)
      if (fill && fill[1].edits === editCount.current) {
        pendingFills.current.delete(fill[0])
        uploads.cancelBatch(fill[0])
        fill[1].nodes.forEach((id) => localMedia.clear(id))
        return
      }
      // 一批导入（建节点与已入队的填充）是一步：本机还没发出的部分一起撤回，
      // 已经发出的按组交给服务器撤销。
      const tail = queue.outbox.filter((i) => i.canvasID === canvasID).at(-1)
      const group = tail?.kind === 'actions' ? tail.changeGroupID : undefined
      if (group) {
        const withdrawn = queue.cancelTailGroup(canvasID, group)
        uploads.cancelBatch(group)
        clearLocal(group)
        const created = withdrawn.filter((i) =>
          i.actions?.some((a) => a.type === 'add_node'),
        )
        if (created.length) setRedoStack((old) => [...old, ...created])
        else {
          setRedoStack([])
          void stage({ kind: 'undo', ...undoPaint(group) })
        }
        return
      }
      const cancelled = queue.cancelTail(canvasID)
      if (cancelled) {
        if (cancelled.changeGroupID) uploads.cancelBatch(cancelled.changeGroupID)
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
      // 撤销整批导入时，这一批还在上传的文件一并取消。
      if (history.undoStepGroup) {
        uploads.cancelBatch(history.undoStepGroup)
        clearLocal(history.undoStepGroup)
      }
      void stage({ kind: 'undo', ...undoPaint(history.undoStepGroup) })
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
        changeGroupID: local.changeGroupID,
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
    editCount.current++
    setRedoStack([])
    void staged.done.catch((e: unknown) => {
      if (!isWithdrawn(e)) setLocalError(errorMessage(e))
    })
  }
  // Drop files on the canvas. Each file shows at once from the local blob and
  // uploads into staging; the canvas only changes through the user's own
  // ordered edits — the node creation now, a fill_from_upload once the bytes
  // are ready — so an undo at any moment never races a server-side write.
  function dropFiles(files: File[], x: number, y: number) {
    if (!view || !editable || !uploads.capabilities) return
    const plan = importPlan(files, uploads.capabilities)
    uploads.reject(plan.rejected)
    if (!plan.accepted.length) return
    const ids = plan.accepted.map(() => `cwnode_${crypto.randomUUID()}`)
    const group = `import_${crypto.randomUUID()}`
    setRedoStack([])
    plan.accepted.forEach(({ file, kind }, i) => showLocal(ids[i]!, file, kind))
    void stage(
      {
        kind: 'actions',
        changeGroupID: group,
        actions: plan.accepted.map(({ file, kind }, i) => {
          const at = importSlot(i, { x, y }, `core.${kind}`)
          return {
            type: 'add_node' as const,
            node_id: ids[i]!,
            type_key: `core.${kind}` as const,
            title: fileTitle(file.name),
            x: at.x,
            y: at.y,
          }
        }),
      },
      { select: ids },
    )
    importGroups.current.set(group, {
      nodes: ids,
      created: true,
      edits: editCount.current,
      fills: 0,
    })
    uploads.start(
      plan.accepted.map((a) => a.file),
      () => ({ kind: 'staged' }),
      group,
      ids,
    )
  }
  // Filling one empty node takes the same path: local preview now, a fill
  // edit once staged. It is its own undo group; undo cancels the upload.
  function uploadIntoNode(n: api.CanvasNode, file: File) {
    const kind = n.metadata.type_key.slice(5) as MediaKind
    const group = `import_${crypto.randomUUID()}`
    showLocal(n.id, file, kind)
    importGroups.current.set(group, {
      nodes: [n.id],
      created: false,
      edits: editCount.current,
      fills: 0,
    })
    pendingFills.current.set(group, { nodes: [n.id], edits: editCount.current })
    uploads.start([file], () => ({ kind: 'staged' }), group, [n.id])
  }
  function showLocal(nodeID: string, file: File, kind: MediaKind) {
    const entry = localMedia.set(nodeID, file, kind)
    void readMediaSize(entry.url, kind).then((size) => {
      if (size) localMedia.patch(nodeID, size)
    })
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
    uploadIntoNode(n, file)
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
    uploadIntoNode(n, file)
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
    const live = dropped.filter((a) => !a.unavailable)
    const usable = live.filter(isCanvasAsset)
    if (!usable.length) {
      if (live.length) setCopyNotice('模型只能放进 3D 场景')
      return
    }
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
        actions: assetActions(usable, ids, (i, asset) => ({
          x: x0 + i * (NODE_WIDTH + gap),
          y: at.y - defaultNodeSize(`core.${asset.kind}`).height / 2,
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
  function pickReferences(targetID: string, list: api.Asset[]) {
    const picked = list.filter(isCanvasAsset)
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
        <AgentPanel key={canvasID} account={account} canvasID={canvasID} canvas={view} defaultOpen={openAgent} />
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
        {sceneHost.state.holding && (
          <span className="cc-scene-host" role="status">
            {sceneHost.state.resumed === sceneHost.state.holding.id
              ? '已继续助手的任务'
              : sceneHost.state.holding.kind === 'render_view'
                ? '助手正在用这个页面截图'
                : '助手正在用这个页面生成模型'}
            ，关闭页面会中断
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
            <Link role="menuitem" to="/creative/projects">
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
        <LibraryDrawer
          hidden={!libraryOpen}
          tick={tick}
          task={libraryTask}
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
          onScenePlace={(asset) => {
            const place = scenePlace.current
            closeLibrary()
            place?.(asset)
          }}
          onClose={closeLibrary}
          onWidth={setLibraryWidth}
          onAssets={setAssets}
          onCatalog={setCatalog}
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
                  canUndo={history.canUndo || fillUploading}
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
                  draftRatios={draftRatios}
                  onEdit={(id) => {
                    setSelected(id)
                    const n = view.nodes.find((n) => n.id === id)
                    if (
                      n &&
                      ['internal.document', 'core.scene'].includes(
                        n.metadata.type_key,
                      ) &&
                      n.data.document_id
                    ) {
                      setMaximized({
                        id: n.data.document_id,
                        typeKey: n.metadata.type_key,
                        libraryOpen,
                        nodeID: n.id,
                      })
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
        {editing && node && (
          <StudioDialog title="编辑节点" onClose={() => setEditing(false)}>
            {draft ? (
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
      {maximized?.typeKey === 'core.scene' ? (
        <Suspense
          fallback={
            <section className="cc-maximized-document cc-surface" aria-label="3D 场景">
              <div className="cc-document-body" role="status">
                <p>正在加载 3D 编辑器…</p>
              </div>
            </section>
          }
        >
          <SceneEditor
            documentID={maximized.id}
            canvasID={canvasID}
            sceneNodeID={maximized.nodeID}
            host={sceneHost.launcher}
            onSaveModel={setSavingModel}
            onOpenLibrary={(place) => {
              scenePlace.current = place
              setLibraryTask({ kind: 'scene-model' })
              setLibraryOpen(true)
            }}
            onClose={() => {
              setSavingModel(null)
              scenePlace.current = null
              setLibraryTask(null)
              setLibraryOpen(maximized.libraryOpen)
              setMaximized(null)
            }}
          />
        </Suspense>
      ) : (
        maximized && (
          <DocumentView
            id={maximized.id}
            onClose={() => {
              setLibraryOpen(maximized.libraryOpen)
              setMaximized(null)
            }}
          />
        )
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
              <GroupField groups={catalog.groups} />
            </label>
            <button className="btn btn-primary" disabled={blocked}>
              保存资产
            </button>
          </form>
        </StudioDialog>
      )}
      {savingModel && (
        <SaveModelDialog
          key={savingModel.target.objectID}
          request={savingModel}
          groupField={<GroupField groups={catalog.groups} />}
          groupName={(id) =>
            catalog.groups.find((g) => g.id === id)?.name ?? '分组'
          }
          onSaved={setCopyNotice}
          onClose={() => setSavingModel(null)}
        />
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
              <GroupField groups={catalog.groups} />
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

// 存入资产库时选分组：表单照常从 FormData 读 group。
function GroupField({ groups }: { groups: api.Group[] }) {
  const [group, setGroup] = useState('')
  return (
    <Picker
      variant="field"
      label="归入分组"
      name="group"
      value={group}
      options={[
        { value: '', label: '不归类，只存入' },
        ...groupOptions(groups),
      ]}
      onChange={setGroup}
    />
  )
}
