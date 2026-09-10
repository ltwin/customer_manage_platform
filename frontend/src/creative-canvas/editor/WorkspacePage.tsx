import {
  Aperture,
  ArrowLeft,
  Library,
  Plus,
  Type,
  Link2,
  LockKeyhole,
  PanelLeftClose,
  PanelRightClose,
} from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ReactFlowProvider } from '@xyflow/react'
import ConfirmDialog from '../../components/ConfirmDialog'
import { useOnline } from './useOnline.ts'
import { useJournal } from './useJournal.ts'
import { errorMessage } from './queue.ts'
import type { Draft } from './journal.ts'

import * as api from './api.ts'
import { newerCanvas, matchesCanvas } from './snapshot.ts'
import CanvasView from './CanvasView.tsx'
import ContentForm from './ContentForm.tsx'
import { blankDraft } from './content.ts'
import '@xyflow/react/dist/style.css'
import './workspace.css'

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
  const [libraryOpen, setLibraryOpen] = useState(
    () => !window.matchMedia('(max-width: 760px)').matches,
  )
  const [compactPanels, setCompactPanels] = useState(
    () => window.matchMedia('(max-width: 1100px)').matches,
  )
  useEffect(() => {
    const query = window.matchMedia('(max-width: 1100px)')
    const changed = () => setCompactPanels(query.matches)
    query.addEventListener('change', changed)
    return () => query.removeEventListener('change', changed)
  }, [])
  const { canvasID = '' } = useParams<{ canvasID: string }>()
  const [assets, setAssets] = useState<api.AssetPage>({
    items: [],
    next_cursor: '',
    total_count: 0,
    library_revision: '1',
  })
  const [canvas, setCanvas] = useState<api.Canvas | null>(null)
  const [selected, setSelected] = useState<string | null>(null)
  const [full, setFull] = useState<Draft | null>(null)
  const [error, setError] = useState('')
  const [localError, setLocalError] = useState('')
  const [loading, setLoading] = useState(true)
  const [discard, setDiscard] = useState<string | null>(null)
  const [form, setForm] = useState<'asset' | null>(null)
  const [compare, setCompare] = useState<{
    content: api.ContentDraft
    node: api.CanvasNode
  } | null>(null)
  const [rename, setRename] = useState<{
    id: string
    name: string
    revision: string
  } | null>(null)
  const [tick, setTick] = useState(0)
  const readSequence = useRef(0)
  const canvasReadSequence = useRef(0)
  const node = canvas?.nodes.find((n) => n.id === selected)
  const nodeKey = `node:${canvasID}:${selected ?? ''}`
  const draft = queue?.value.drafts[nodeKey] ?? full
  const blocked =
    !queue ||
    queue.busy ||
    !!queue.value.job ||
    Object.values(queue.value.positions ?? {}).some(
      (p) => p.canvasID === canvasID && !p.synced,
    ) ||
    !!storageError ||
    !!localError ||
    loading
  const canEditLocal = matchesCanvas(canvas, canvasID) && !canvas?.archived
  const canWrite = canEditLocal && !error
  const refresh = useCallback(() => setTick((v) => v + 1), [])
  const positions = queue?.value.positions
  const canMove = !!queue && canEditLocal && !storageError && !localError
  useEffect(() => {
    if (!queue || !canvas || canvas.id !== canvasID) return
    void queue
      .reconcilePositions(canvas)
      .catch((e: unknown) => setLocalError(errorMessage(e)))
  }, [queue, canvas, canvasID, positions])
  useEffect(() => {
    if (
      !queue ||
      !canEditLocal ||
      !online ||
      storageError ||
      localError ||
      queue.busy ||
      queue.value.job ||
      !Object.values(queue.value.positions ?? {}).some(
        (p) => p.canvasID === canvasID && !p.synced,
      ) ||
      loading
    )
      return
    // Only the final local position in this short burst becomes a command.
    const timer = window.setTimeout(() => {
      void queue
        .sendNextPosition(canvasID)
        .catch((e: unknown) => setLocalError(errorMessage(e)))
    }, 120)
    return () => window.clearTimeout(timer)
  }, [
    queue,
    queue?.value,
    queue?.busy,
    canvasID,
    canEditLocal,
    online,
    storageError,
    localError,
    loading,
  ])
  function moveNode(n: api.CanvasNode, x: number, y: number) {
    if (!queue || !canMove) return
    void queue
      .stagePosition(canvasID, n, x, y)
      .catch((e: unknown) => setLocalError(errorMessage(e)))
  }

  useEffect(() => {
    const controller = new AbortController()
    const seq = ++readSequence.current
    setLoading(true)
    void (async () => {
      const canvasSeq = ++canvasReadSequence.current
      const [a, c] = await Promise.all([
        api.read<api.AssetPage>('/assets', controller.signal),
        canvasID
          ? api.read<api.Canvas>(
              `/canvases/${encodeURIComponent(canvasID)}`,
              controller.signal,
            )
          : Promise.resolve(null),
      ])
      if (controller.signal.aborted || seq !== readSequence.current) return
      setAssets(a)
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
    const timer = window.setInterval(() => void poll(), 5000)
    const visible = () => {
      if (!document.hidden) refresh()
    }
    window.addEventListener('online', visible)
    document.addEventListener('visibilitychange', visible)
    return () => {
      controller.abort()
      clearInterval(timer)
      window.removeEventListener('online', visible)
      document.removeEventListener('visibilitychange', visible)
    }
  }, [refresh, canvasID])
  useEffect(() => {
    setCanvas(null)
    setSelected(null)
    setFull(null)
    setCompare(null)
    setRename(null)
    setDiscard(null)
  }, [canvasID])
  useEffect(() => {
    const controller = new AbortController()
    setFull(null)
    if (!node) return
    const base: Draft = {
      ...blankDraft(node.type_key === 'core.link' ? 'link' : 'text'),
      title: node.title,
      dataRevision: node.data_revision,
      contentRevision: node.content_revision_id,
    }
    if (!node.content_revision_id) {
      setFull(base)
      return
    }
    void api
      .read<api.Content>(
        `/content-revisions/${encodeURIComponent(node.content_revision_id)}`,
        controller.signal,
      )
      .then((r) => {
        if (!controller.signal.aborted)
          setFull({
            ...base,
            value: 'body' in r.payload ? r.payload.body : r.payload.url,
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
  async function perform(path: string, value: unknown, draftKey?: string) {
    if (!queue || blocked) return
    if (
      path.startsWith('/canvases/') &&
      (!canWrite || path !== `/canvases/${canvasID}/commands`)
    )
      return
    setError('')
    setLoading(true)
    try {
      await queue.enqueue(path, value, draftKey)
      if (!queue.value.job) {
        setForm(null)
        refresh()
      } else {
        setLoading(false)
      }
    } catch (e) {
      setLoading(false)
      setLocalError(errorMessage(e))
    }
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
  function addEmptyNode(kind: 'text' | 'link') {
    if (!canvas || !canWrite) return
    void perform(`/canvases/${canvas.id}/commands`, {
      type: 'add_node',
      node_id: `cwnode_${crypto.randomUUID()}`,
      type_key: `core.${kind}`,
      x: 80 + canvas.nodes.length * 20,
      y: 80 + canvas.nodes.length * 20,
      expected_topology_revision: canvas.topology_revision,
    } satisfies api.Command)
  }
  function addAsset(asset: api.Asset, x = 80, y = 80) {
    if (!canvas || !canWrite || asset.unavailable) return
    void perform(`/canvases/${canvas.id}/commands`, {
      type: 'add_node',
      node_id: `cwnode_${crypto.randomUUID()}`,
      type_key: `core.${asset.kind}`,
      title: asset.title,
      x,
      y,
      expected_topology_revision: canvas.topology_revision,
      asset: {
        asset_id: asset.id,
        expected_asset_revision: asset.revision,
        content_revision_id: asset.content_revision_id,
      },
    } satisfies api.Command)
  }
  function saveNode(content: api.ContentDraft, reapply?: api.CanvasNode) {
    if (
      !canWrite ||
      !canvas ||
      !node ||
      !draft ||
      (reapply && reapply.id !== node.id)
    )
      return
    if (
      !reapply &&
      (node.data_revision !== draft.dataRevision ||
        node.content_revision_id !== draft.contentRevision)
    ) {
      setCompare({ content, node: structuredClone(node) })
      return
    }
    void perform(
      `/canvases/${canvas.id}/commands`,
      {
        type: 'replace_content',
        node_id: node.id,
        expected_data_revision: reapply
          ? reapply.data_revision
          : draft.dataRevision!,
        expected_content_revision_id: reapply
          ? reapply.content_revision_id
          : (draft.contentRevision ?? null),
        payload: content.payload,
        ...(!node.content_revision_id ? { rights: content.rights } : {}),
      } satisfies api.Command,
      nodeKey,
    )
  }
  async function loadMore() {
    const seq = readSequence.current
    try {
      const page = await api.read<api.AssetPage>(
        `/assets?cursor=${encodeURIComponent(assets.next_cursor)}`,
      )
      if (seq === readSequence.current)
        setAssets((a) => ({
          ...page,
          items: [
            ...a.items,
            ...page.items.filter((n) => !a.items.some((x) => x.id === n.id)),
          ],
        }))
    } catch (e) {
      setError(errorMessage(e))
    }
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
    hasDraft ||
    needsRecovery
  )
  return (
    <main
      className={`cc-workspace ${libraryOpen ? 'has-library' : ''} ${form || node ? 'has-inspector' : ''}`}
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
          <h1>{activeProject?.name ?? '我的工作台'}</h1>
          <span className="cc-private">
            <LockKeyhole size={12} /> 私人创作
          </span>
        </div>
        <nav className="cc-actions">
          <Link className="btn" to="/creative">
            所有项目
          </Link>
        </nav>
      </header>
      <nav className="cc-tool-rail" aria-label="创作工具">
        <button
          className={libraryOpen ? 'is-active' : ''}
          aria-label="资产库"
          aria-expanded={libraryOpen && !(compactPanels && (form || node))}
          aria-controls="creative-library"
          title="资产库"
          onClick={() => {
            setLibraryOpen((v) => (compactPanels && (form || node) ? true : !v))
            if (compactPanels) {
              setForm(null)
              setSelected(null)
            }
          }}
        >
          <Library size={21} strokeWidth={1.5} />
          <span>资产</span>
        </button>
        <span className="cc-rail-line" />
        <button
          aria-label="新增文字节点"
          title="新增文字节点"
          disabled={blocked || !canWrite}
          onClick={() => addEmptyNode('text')}
        >
          <Type size={21} strokeWidth={1.5} />
          <span>文字</span>
        </button>
        <button
          aria-label="新增链接节点"
          title="新增链接节点"
          disabled={blocked || !canWrite}
          onClick={() => addEmptyNode('link')}
        >
          <Link2 size={20} strokeWidth={1.5} />
          <span>链接</span>
        </button>
      </nav>
      {showNotice && (
        <section className="cc-save-status" aria-live="polite">
          {hasDraft && <span>有本机草稿 · 尚未保存到项目</span>}
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
                  {job.position
                    ? '放弃本机位置，载入服务器位置'
                    : job.draftKey
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
        <aside
          className="cc-library"
          id="creative-library"
          aria-label="个人资产库"
          hidden={!libraryOpen}
        >
          <div className="cc-library-heading">
            <div>
              <span className="cc-eyebrow">LIBRARY</span>
              <h2>个人资产库</h2>
            </div>
            <button
              className="cc-icon-button"
              aria-label="新增资产"
              title="新增资产"
              onClick={() => setForm('asset')}
              disabled={blocked}
            >
              <Plus size={18} />
            </button>
            <button
              className="cc-icon-button"
              aria-label="收起资产库"
              title="收起资产库"
              onClick={() => setLibraryOpen(false)}
            >
              <PanelLeftClose size={18} />
            </button>
          </div>
          <div className="cc-section-title">
            <h2>个人库</h2>
            <span>{assets.total_count} 项</span>
          </div>
          <p className="cc-muted">独立于项目保存。放入画布后可分别编辑。</p>
          <div className="cc-assets">
            {assets.items.map((a) => (
              <article
                className="cc-asset"
                key={a.id}
                draggable={!!canvas && canWrite && !blocked && !a.unavailable}
                onDragStart={(e) =>
                  e.dataTransfer.setData('application/creative-asset', a.id)
                }
              >
                <span className="cc-kind">
                  {a.kind === 'text' ? '文字' : '链接'}
                </span>
                <h3>{a.title}</h3>
                <p>
                  {a.unavailable
                    ? '当前无权展示'
                    : a.content && 'body' in a.content.payload
                      ? a.content.payload.body
                      : a.content && 'url' in a.content.payload
                        ? a.content.payload.url
                        : ''}
                </p>
                <button
                  className="btn"
                  disabled={!canvas || blocked || !canWrite || a.unavailable}
                  onClick={() => addAsset(a)}
                >
                  放入画布
                </button>
              </article>
            ))}
          </div>
          {!loading && !assets.items.length && (
            <p className="cc-muted">把一句想法或一个链接存进个人库。</p>
          )}
          {assets.next_cursor && (
            <button className="btn" onClick={() => void loadMore()}>
              加载更多资产
            </button>
          )}
        </aside>
        <section className="cc-stage" aria-label="创作画布">
          {canvas && canvas.id === canvasID ? (
            <>
              <div className="cc-toolbar">
                <div className="cc-actions">
                  {(['text', 'link'] as const).map((kind) => (
                    <button
                      className="btn"
                      key={kind}
                      disabled={blocked || !canWrite}
                      onClick={() => addEmptyNode(kind)}
                    >
                      ＋ {kind === 'text' ? '文字' : '链接'}
                    </button>
                  ))}
                </div>
                <div className="cc-actions">
                  {canvas.archived && <span>已归档 · 只读</span>}
                  {activeProject && (
                    <>
                      <button
                        className="btn"
                        disabled={blocked || !canWrite}
                        onClick={() => setRename(activeProject)}
                      >
                        改名
                      </button>
                      <button
                        className="btn"
                        disabled={blocked}
                        onClick={() =>
                          void perform(
                            `/projects/${activeProject.id}/${canvas.archived ? 'restore' : 'archive'}`,
                            { expected_revision: activeProject.revision },
                          )
                        }
                      >
                        {canvas.archived ? '恢复项目' : '归档项目'}
                      </button>
                    </>
                  )}
                </div>
              </div>
              <ReactFlowProvider key={canvas.id}>
                <CanvasView
                  account={account}
                  canvas={canvas}
                  selected={selected}
                  onSelect={(id) => {
                    setForm(null)
                    setSelected(id)
                    if (window.innerWidth <= 760) setLibraryOpen(false)
                  }}
                  disabled={blocked || !canWrite}
                  moveDisabled={!canMove}
                  positions={positions}
                  assets={assets.items}
                  onDropAsset={addAsset}
                  onMove={moveNode}
                />
              </ReactFlowProvider>
            </>
          ) : (
            <div className="cc-welcome">
              <span className="cc-welcome-mark">
                <Aperture size={44} strokeWidth={0.8} />
              </span>
              <span className="cc-kind">A LITTLE ROOM FOR POSSIBILITY</span>
              <h2>
                让想法，
                <br />
                自由生长。
              </h2>
              <p>
                一段文字，一个链接，一份尚未成形的灵感。
                <br />
                从一张画布开始，把它们慢慢连成你的作品。
              </p>
              <Link className="btn btn-primary cc-start" to="/creative">
                <Plus size={17} /> 新建项目
              </Link>
            </div>
          )}
        </section>
        {(form || node) && (
          <aside className="cc-inspector" aria-label="内容编辑">
            <div className="cc-section-title">
              <h2>{form === 'asset' ? '存入个人库' : '编辑节点'}</h2>
              <button
                className="btn"
                aria-label="收起编辑面板"
                onClick={() => {
                  if (compactPanels) setLibraryOpen(false)
                  setForm(null)
                  setSelected(null)
                }}
              >
                <PanelRightClose size={18} />
              </button>
            </div>
            {form === 'asset' ? (
              <ContentForm
                key="asset"
                asset
                draft={assetDraft}
                onChange={(d) => writeDraft('asset-form', d)}
                disabled={blocked}
                label="保存资产"
                onSave={(content) =>
                  void perform(
                    '/assets',
                    { title: assetDraft.title, content },
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
                  needsRights={node.content_revision_id === null}
                  disabled={blocked || !canEditLocal || node.unavailable}
                  submitDisabled={!canWrite}
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
                <fieldset className="cc-position" disabled={!canMove}>
                  <legend>位置调整</legend>
                  {[
                    ['←', -20, 0],
                    ['↑', 0, -20],
                    ['↓', 0, 20],
                    ['→', 20, 0],
                  ].map(([label, x, y]) => (
                    <button
                      className="btn"
                      type="button"
                      key={label}
                      aria-label={`向${label}移动`}
                      onClick={() => {
                        const position =
                          positions?.[`${canvasID}:${node.id}`] ?? node
                        moveNode(
                          node,
                          position.x + Number(x),
                          position.y + Number(y),
                        )
                      }}
                    >
                      {label}
                    </button>
                  ))}
                </fieldset>
              </>
            ) : (
              <p>正在读取完整正文…</p>
            )}
          </aside>
        )}
      </div>
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
            `当前内容：${compare.node.content ? ('body' in compare.node.content.payload ? compare.node.content.payload.body : compare.node.content.payload.url) : '空节点'}`,
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
