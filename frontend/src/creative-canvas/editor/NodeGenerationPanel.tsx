import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from 'react'
import { createPortal } from 'react-dom'
import { useStore, useViewport } from '@xyflow/react'
import type { components } from '../../api/schema'
import * as api from './api.ts'
import { graphActionReadSet, graphReadSet, worldPoint } from './graph.ts'
import { errorMessage, type SaveQueue } from './queue.ts'
import {
  assertGenerationSnapshot,
  executionLabel,
  generationReadSet,
  imageParameterKeys,
  imageSummary,
  parameterValue,
  selectedOption,
  promptFromNode,
  type Execution,
  type GenerationCatalog,
  type Prompt,
  type Versions,
} from './generation.ts'
import { MediaFigure } from './MediaView.tsx'
import {
  ArrowUp,
  ChevronDown,
  History,
  LoaderCircle,
  Save,
  SlidersHorizontal,
  X,
} from 'lucide-react'
import ImageParameters, { RatioGlyph } from './ImageParameters.tsx'
import GenerationReference from './GenerationReference.tsx'
import PromptEditor, { type PromptEditorHandle } from './PromptEditor.tsx'
import {
  cleanMentions,
  connectedReferences,
  promptReferences,
} from './promptReferences.ts'
import { mediaTicket, pickRendition } from './media.ts'
import './generation.css'

type Props = {
  leftInset: number
  canvas: api.Canvas
  node: api.CanvasNode
  queue: SaveQueue
  disabled: boolean
  dragging: boolean
  assets: api.Asset[]
  refresh: () => Promise<void>
  onPickReferences: () => void
  onDisconnect: (edgeID: string) => void
  referenceRequest: { seq: number; targetID: string; ids: string[] } | null
  onReferencesInserted: () => void
  /** 待处理的聚焦请求：面板解锁后才真正聚焦，随后由 onFocused 清掉。 */
  focusRequest: { seq: number; nodeID: string } | null
  onFocused: () => void
}

// 本容器拥有读取与队列接线；展示表单只接收数据和回调。
// 版本从哪里来：人工放进节点的素材也是版本，生成不会让它从列表消失。
const versionOrigins: Record<string, string> = {
  generation: '生成',
  upload: '上传',
  asset: '资产库',
  copy: '复制',
}

export default function NodeGenerationPanel({
  leftInset,
  canvas,
  node,
  queue,
  disabled,
  dragging,
  assets,
  refresh,
  onPickReferences,
  onDisconnect,
  referenceRequest,
  onReferencesInserted,
  focusRequest,
  onFocused,
}: Props) {
  const viewport = useViewport()
  const point = worldPoint(canvas.nodes, node.id)
  const renderedHeight = useStore(
    (state) =>
      state.nodes.find((n) => n.id === node.id)?.measured?.height ??
      node.metadata.height,
  )
  const key = `prompt:${canvas.id}:${node.id}`
  const local = queue.value.drafts[key]?.prompt
  const rawPrompt = local ?? promptFromNode(canvas, node)
  const references = promptReferences(canvas, node, rawPrompt, assets)
  const connected = connectedReferences(references)
  const allowed = new Set(
    connected.flatMap((r) => (r.sourceID ? [r.sourceID] : [])),
  )
  const prompt: Prompt = {
    ...rawPrompt,
    text: cleanMentions(rawPrompt.text, allowed),
    // 草稿文字的 CAS 不变；图关系采用当前快照，由生成准入再次复核。
    expected_topology_revision: canvas.topology_revision,
    read_set: graphReadSet(canvas),
  }
  const editor = useRef<PromptEditorHandle>(null)
  const insertedRequest = useRef(0)
  const [thumbnails, setThumbnails] = useState<Record<string, string>>({})
  const mediaRefs = useMemo(
    () =>
      canvas.nodes.filter((n) =>
        canvas.edges.some(
          (e) => e.target_node_id === node.id && e.source_node_id === n.id,
        ),
      ),
    [canvas, node.id],
  )
  useEffect(() => {
    let live = true
    void Promise.all(
      mediaRefs.map(async (ref) => {
        const media = pickRendition(ref.content?.media, 'display')
        if (ref.content?.kind !== 'image' || !media) return null
        try {
          const ticket = await mediaTicket(
            ref.content.id,
            media.role === 'original' ? 'original' : 'display',
            'display',
          )
          return { id: ref.content.id, url: ticket.url }
        } catch {
          return null
        }
      }),
    ).then((values) => {
      if (live)
        setThumbnails(
          Object.fromEntries(values.flatMap((v) => (v ? [[v.id, v.url]] : []))),
        )
    })
    return () => {
      live = false
    }
  }, [mediaRefs])
  const [catalog, setCatalog] = useState<GenerationCatalog | null>(null)
  const [versions, setVersions] = useState<Versions | null>(null)
  const [execution, setExecution] = useState<Execution | null>(null)
  const [preview, setPreview] = useState<api.Content | null>(null)
  const [previewID, setPreviewID] = useState('')
  const [error, setError] = useState('')
  const [readError, setReadError] = useState('')
  const [busy, setBusy] = useState(false)
  const [tab, setTab] = useState<'prompt' | 'versions'>('prompt')
  const [reload, setReload] = useState(0)
  const [optionsOpen, setOptionsOpen] = useState(false)
  const options = useRef<HTMLDivElement>(null)
  const optionsButton = useRef<HTMLButtonElement>(null)
  const panel = useRef<HTMLElement>(null)
  const [optionsPlace, setOptionsPlace] = useState<CSSProperties>()
  const mounted = useRef(true)
  const busyRef = useRef(false)
  const path = `/canvases/${encodeURIComponent(canvas.id)}`
  const nodePath = `${path}/nodes/${encodeURIComponent(node.id)}`
  const executionID =
    node.status.active_execution_id ?? node.status.latest_execution_id
  const latestRefresh = useRef(refresh)
  latestRefresh.current = refresh
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  useEffect(() => {
    const controller = new AbortController()
    void Promise.all([
      api.read<GenerationCatalog>(
        `${nodePath}/generation-catalog`,
        controller.signal,
      ),
      api.read<Versions>(`${nodePath}/versions`, controller.signal),
    ])
      .then(([c, v]) => {
        if (!controller.signal.aborted) {
          setCatalog(c)
          setVersions(v)
          setReadError('')
        }
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted) setReadError(errorMessage(e))
      })
    return () => controller.abort()
  }, [nodePath, canvas.revision, reload])
  useEffect(() => {
    if (!executionID) return
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout>
    const read = async () => {
      try {
        const e = await api.read<Execution>(
          `${path}/executions/${encodeURIComponent(executionID)}`,
          controller.signal,
        )
        if (controller.signal.aborted) return
        setExecution(e)
        if (
          ['queued', 'running', 'reconciling'].includes(e.state) ||
          (e.state === 'succeeded' && e.apply_state === 'pending')
        ) {
          timer = setTimeout(
            () => {
              if (!document.hidden && navigator.onLine) void read()
              else timer = setTimeout(() => void read(), 10000)
            },
            e.state === 'reconciling' ? 15000 : 3000,
          )
        } else {
          await latestRefresh.current()
        }
      } catch (e) {
        if (!controller.signal.aborted) setReadError(errorMessage(e))
      }
    }
    void read()
    return () => {
      controller.abort()
      clearTimeout(timer)
    }
  }, [executionID, path, reload])
  useEffect(() => {
    if (!previewID) return
    const controller = new AbortController()
    void api
      .read<api.Content>(
        `/content-revisions/${encodeURIComponent(previewID)}`,
        controller.signal,
      )
      .then((c) => {
        if (!controller.signal.aborted) setPreview(c)
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted) setReadError(errorMessage(e))
      })
    return () => controller.abort()
  }, [previewID, reload])

  useEffect(() => {
    if (!optionsOpen) return
    const close = (event: PointerEvent) => {
      if (
        event.target instanceof Node &&
        !options.current?.contains(event.target)
      )
        setOptionsOpen(false)
    }
    document.addEventListener('pointerdown', close)
    return () => document.removeEventListener('pointerdown', close)
  }, [optionsOpen])
  useEffect(
    () => setOptionsOpen(false),
    [viewport.x, viewport.y, viewport.zoom],
  )

  function edit(next: Prompt) {
    void queue
      .update({
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
      .catch((e: unknown) => setError(errorMessage(e)))
  }
  const locked = disabled || busy || queue.busy || !!queue.value.job
  // 新建节点在服务端确认前是锁定的；请求一直保留，解锁后再把焦点交给输入框。
  useEffect(() => {
    if (!focusRequest || focusRequest.nodeID !== node.id || locked) return
    editor.current?.focus()
    onFocused()
  }, [focusRequest, node.id, locked, onFocused])
  useEffect(() => {
    if (
      !referenceRequest ||
      referenceRequest.targetID !== node.id ||
      referenceRequest.seq === insertedRequest.current ||
      locked ||
      !referenceRequest.ids.every((id) =>
        connected.some((r) => r.sourceID === id),
      )
    )
      return
    insertedRequest.current = referenceRequest.seq
    editor.current?.insertReferences(
      referenceRequest.ids.filter((id) => !prompt.text.includes(`@[${id}]`)),
    )
    onReferencesInserted()
  }, [
    referenceRequest,
    locked,
    connected,
    prompt.text,
    node.id,
    onReferencesInserted,
  ])
  async function act(work: () => Promise<void>) {
    if (locked || busyRef.current) return
    busyRef.current = true
    setBusy(true)
    setError('')
    try {
      await work()
      if (mounted.current) {
        await latestRefresh.current()
        setReload((n) => n + 1)
      }
    } catch (e) {
      if (mounted.current) setError(errorMessage(e))
    } finally {
      busyRef.current = false
      if (mounted.current) setBusy(false)
    }
  }
  async function save() {
    if (!local && node.prompt && prompt.text === node.prompt.text)
      return node.prompt.draft_revision
    const result = await queue.enqueue(`${path}/commands`, prompt, key)
    if (
      !result ||
      typeof result !== 'object' ||
      !('draft_revision' in result) ||
      typeof result.draft_revision !== 'string'
    )
      throw new Error('草稿保存结果待确认，请重新读取')
    return result.draft_revision
  }
  async function generate() {
    const savedRevision = await save()
    // 保存成功后再读权威版本；失联时停止，不猜测草稿revision或重建操作身份。
    const fresh = await api.read<api.Canvas>(path)
    if (!mounted.current) return
    const n = assertGenerationSnapshot(canvas, fresh, node.id, savedRevision)
    if (!n.prompt) throw new Error('提示词尚未保存')
    const value: components['schemas']['CreativeRequestExecutionPayload'] = {
      configuration_revision: model?.configuration_revision ?? '',
      node_id: n.id,
      action_key: 'gateway.generation',
      draft_revision: n.prompt.draft_revision,
      expected_data_revision: n.data_revision,
      read_set: generationReadSet(fresh, n.id),
    }
    await queue.enqueue(`${path}/executions`, value)
  }
  async function versionAction(
    type: 'select_version' | 'delete_version',
    id: string,
    followLatest = false,
  ) {
    const actions: api.GraphAction[] = [
      type === 'select_version'
        ? {
            type,
            node_id: node.id,
            version_id: id,
            follow_latest: followLatest,
          }
        : { type, node_id: node.id, version_id: id },
    ]
    const value: api.Command = {
      type: 'batch',
      expected_topology_revision: canvas.topology_revision,
      read_set: graphActionReadSet(canvas, actions),
      actions,
    }
    await queue.enqueue(`${path}/commands`, value)
  }
  const conflict =
    !!local &&
    local.expected_draft_revision !== (node.prompt?.draft_revision ?? null)
  const model = catalog?.models.find((m) => m.key === prompt.model_key)
  const unsupported = ![
    'core.text',
    'core.image',
    'core.video',
    'core.audio',
  ].includes(node.metadata.type_key)
  const state =
    execution?.execution_id === executionID
      ? execution.state
      : node.status.generation_state
  const canGenerate =
    !locked &&
    !conflict &&
    !unsupported &&
    !!model &&
    (!!prompt.text.trim() ||
      prompt.references.length > 0 ||
      canvas.edges.some((e) => e.target_node_id === node.id) ||
      canvas.node_inputs.some((r) => r.node_id === node.id))
  const imageFields =
    model?.parameters.filter(
      (f) =>
        (imageParameterKeys as readonly string[]).includes(f.key) &&
        f.options.length > 0,
    ) ?? []
  const otherFields =
    model?.parameters.filter((f) => !imageFields.includes(f)) ?? []
  const ratioField = imageFields.find((f) => f.key === 'aspect_ratio')
  const ratio = ratioField ? selectedOption(ratioField, prompt.parameters) : ''
  const parametersLabel =
    [
      imageSummary(imageFields, prompt.parameters),
      ...otherFields.map((f) => {
        const value = prompt.parameters[f.key]
        return value === undefined ? '' : String(value)
      }),
    ]
      .filter(Boolean)
      .join(' · ') || '参数'
  // 空图片/视频节点随比例调整高度，保持当前宽度（V6-021 AC-03）；已有作品不受草稿影响。
  async function fitEmptyNode(next: string) {
    const [w, h] = next.split(':').map(Number)
    if (
      node.content ||
      !w ||
      !h ||
      !['core.image', 'core.video'].includes(node.metadata.type_key)
    )
      return
    const height = Math.round((node.metadata.width * h) / w)
    if (Math.abs(height - node.metadata.height) < 1) return
    const actions: api.GraphAction[] = [
      {
        type: 'resize_node',
        node_id: node.id,
        width: node.metadata.width,
        height,
      },
    ]
    await queue.enqueue(`${path}/commands`, {
      type: 'batch',
      expected_topology_revision: canvas.topology_revision,
      read_set: graphActionReadSet(canvas, actions),
      actions,
    })
  }
  // 贴在节点下方 14px（PRD GN-01）。
  const panelTop = `clamp(64px, ${(point.y + renderedHeight) * viewport.zoom + viewport.y + 14}px, max(64px, 100% - ${tab === 'versions' ? 340 : 220}px))`
  return createPortal(
    <aside
      ref={panel}
      className={`cc-generation nodrag nopan nowheel ${node.status.active_execution_id ? 'is-running' : ''}`}
      aria-label="节点生成"
      hidden={dragging}
      style={{
        left: `clamp(min(${leftInset}px, max(8px, 100% - 468px)), ${(point.x + node.metadata.width / 2) * viewport.zoom + viewport.x - 230}px, max(8px, 100% - 468px))`,
        top: panelTop,
        maxHeight: `min(560px, calc(100% - ${panelTop} - 12px))`,
      }}
      onKeyDown={(e) => {
        e.stopPropagation()
        if (e.key === 'Escape' && optionsOpen) {
          e.preventDefault()
          setOptionsOpen(false)
          optionsButton.current?.focus()
        }
      }}
      onPointerDown={(e) => e.stopPropagation()}
    >
      <header>
        <span role="status">{executionLabel(state)}</span>
        <button
          type="button"
          className="cc-generation-history"
          aria-pressed={tab === 'versions'}
          onClick={() => {
            setTab(tab === 'versions' ? 'prompt' : 'versions')
            setOptionsOpen(false)
          }}
        >
          <History size={13} />
          {tab === 'versions'
            ? '返回提示词'
            : `作品版本${versions?.items.length ? ` · ${versions.items.length}` : ''}`}
        </button>
      </header>
      {(error || readError) && (
        <p role="alert">
          {error || readError}{' '}
          <button onClick={() => setReload((n) => n + 1)}>重新读取</button>
        </p>
      )}
      {tab === 'prompt' ? (
        <>
          {unsupported && (
            <p>
              {node.metadata.type_key === 'core.group'
                ? '组任务将在 Agent 工具接入后开放，当前可保存任务描述。'
                : '此类型暂未开放生成，当前可保存提示词。'}
            </p>
          )}
          <div
            className="cc-generation-refs"
            role="group"
            aria-label="参考素材"
          >
            {references.map((r) => (
              <GenerationReference
                key={r.key}
                title={r.title}
                label={r.label}
                content={r.content}
                updated={
                  !!r.sourceID &&
                  !!versions?.items[0]?.generation_provenance
                    ?.reference_sources &&
                  typeof versions.items[0].generation_provenance
                    .reference_sources === 'object' &&
                  Object.entries(
                    versions.items[0].generation_provenance.reference_sources,
                  ).some(
                    ([source, revision]) =>
                      source === r.sourceID &&
                      typeof revision === 'string' &&
                      revision !== r.revisionID,
                  )
                }
                disabled={locked}
                onMention={
                  r.connected && r.sourceID
                    ? () => editor.current?.insertReferences([r.sourceID!])
                    : undefined
                }
                onRemove={
                  r.draftIndex !== undefined
                    ? () =>
                        edit({
                          ...prompt,
                          references: prompt.references.filter(
                            (_, i) => i !== r.draftIndex,
                          ),
                        })
                    : r.edgeID
                      ? () => onDisconnect(r.edgeID!)
                      : undefined
                }
              />
            ))}
            <button
              type="button"
              className="cc-generation-ref-add"
              disabled={locked}
              aria-label="引用参考"
              title="引用已连接的参考"
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => editor.current?.openPicker()}
            >
              @
            </button>
          </div>
          <PromptEditor
            ref={editor}
            text={prompt.text}
            references={connected}
            thumbnails={thumbnails}
            disabled={locked}
            onChange={(text) => edit({ ...prompt, text })}
            onGenerate={() => {
              if (canGenerate && !node.status.active_execution_id)
                void act(generate)
            }}
            onLibrary={onPickReferences}
          />
          {conflict && (
            <p role="alert">
              提示词或参考关系已变化。本机草稿已保留，请核对后处理。
              <button
                disabled={locked}
                onClick={() =>
                  edit({
                    ...prompt,
                    expected_draft_revision:
                      node.prompt?.draft_revision ?? null,
                    expected_topology_revision: canvas.topology_revision,
                    read_set: graphReadSet(canvas),
                  })
                }
              >
                保留本机内容并更新保存基线
              </button>
            </p>
          )}
          <p className="cc-generation-note">
            {catalog?.disabled_reason || (!catalog ? '正在读取模型目录…' : '')}
          </p>
          <footer>
            <div className="cc-generation-model">
              <select
                aria-label="生成模型"
                value={prompt.model_key}
                disabled={locked || unsupported}
                onChange={(e) =>
                  edit({ ...prompt, model_key: e.target.value, parameters: {} })
                }
              >
                <option value="">选择模型</option>
                {prompt.model_key && !model && (
                  <option value={prompt.model_key}>
                    {prompt.model_key}（不可用）
                  </option>
                )}
                {catalog?.models.map((m) => (
                  <option key={m.key} value={m.key}>
                    {m.label}
                  </option>
                ))}
              </select>
              <ChevronDown size={12} aria-hidden="true" />
            </div>
            {!!model?.parameters.length && (
              <div className="cc-generation-options" ref={options}>
                <button
                  ref={optionsButton}
                  type="button"
                  className="cc-generation-chip"
                  aria-label="生成参数"
                  aria-expanded={optionsOpen}
                  aria-controls="cc-generation-parameters"
                  disabled={locked}
                  onClick={() => {
                    setOptionsPlace(placeOptions(panel.current))
                    setOptionsOpen(!optionsOpen)
                  }}
                >
                  {ratio ? (
                    <RatioGlyph ratio={ratio} size={14} />
                  ) : (
                    <SlidersHorizontal size={13} />
                  )}
                  <span>{parametersLabel}</span>
                  <ChevronDown size={12} />
                </button>
                {optionsOpen && (
                  <div
                    id="cc-generation-parameters"
                    className="cc-generation-popover"
                    style={optionsPlace}
                    role="group"
                    aria-label="生成参数设置"
                  >
                    <div className="cc-generation-popover-head">
                      <span>生成参数</span>
                      <button
                        type="button"
                        aria-label="关闭参数"
                        onClick={() => {
                          setOptionsOpen(false)
                          optionsButton.current?.focus()
                        }}
                      >
                        <X size={14} />
                      </button>
                    </div>
                    {imageFields.length > 0 && (
                      <ImageParameters
                        fields={imageFields}
                        values={prompt.parameters}
                        disabled={locked}
                        onError={setError}
                        onChange={(key, value) => {
                          edit({
                            ...prompt,
                            parameters: { ...prompt.parameters, [key]: value },
                          })
                          if (key === 'aspect_ratio')
                            void fitEmptyNode(String(value)).catch(
                              (e: unknown) => setError(errorMessage(e)),
                            )
                        }}
                      />
                    )}
                    {otherFields.map((field) => (
                      <label key={field.key}>
                        {field.label}
                        {field.required && ' *'}
                        {field.options.length || field.type === 'boolean' ? (
                          <select
                            aria-label={field.label}
                            disabled={locked}
                            value={String(prompt.parameters[field.key] ?? '')}
                            onChange={(e) => {
                              try {
                                const parameters = { ...prompt.parameters }
                                const value = parameterValue(
                                  field,
                                  e.target.value,
                                )
                                if (value === undefined)
                                  delete parameters[field.key]
                                else parameters[field.key] = value
                                edit({ ...prompt, parameters })
                              } catch (e) {
                                setError(errorMessage(e))
                              }
                            }}
                          >
                            <option value="">使用模型默认值</option>
                            {(field.type === 'boolean'
                              ? ['true', 'false']
                              : field.options
                            ).map((v) => (
                              <option key={v} value={v}>
                                {v === 'true'
                                  ? '开启'
                                  : v === 'false'
                                    ? '关闭'
                                    : v}
                              </option>
                            ))}
                          </select>
                        ) : (
                          <input
                            aria-label={field.label}
                            disabled={locked}
                            type={field.type === 'string' ? 'text' : 'number'}
                            step={field.type === 'integer' ? 1 : 'any'}
                            value={String(prompt.parameters[field.key] ?? '')}
                            placeholder="使用模型默认值"
                            onChange={(e) => {
                              try {
                                const parameters = { ...prompt.parameters }
                                const value = parameterValue(
                                  field,
                                  e.target.value,
                                )
                                if (value === undefined)
                                  delete parameters[field.key]
                                else parameters[field.key] = value
                                edit({ ...prompt, parameters })
                              } catch (e) {
                                setError(errorMessage(e))
                              }
                            }}
                          />
                        )}
                      </label>
                    ))}
                  </div>
                )}
              </div>
            )}
            <span className="cc-generation-grow" />
            <button
              className="cc-generation-save"
              aria-label="保存草稿"
              title="保存草稿"
              disabled={locked || conflict}
              onClick={() =>
                void act(async () => {
                  await save()
                })
              }
            >
              <Save size={15} />
            </button>
            {node.status.active_execution_id ? (
              // 生成一经受理就会跑完并计费，不提供停止（owner 2026-10-09）。
              <button
                className="cc-generation-send"
                aria-label="生成中"
                title="生成中，完成后自动写入作品版本"
                disabled
              >
                <LoaderCircle size={16} className="cc-spin" />
              </button>
            ) : (
              <button
                className="cc-generation-send"
                aria-label="生成新版本"
                title="生成新版本 · Enter"
                disabled={!canGenerate}
                onClick={() => void act(generate)}
              >
                <ArrowUp size={18} />
              </button>
            )}
            <span className="cc-generation-draft" role="status">
              {local
                ? '本机草稿待保存'
                : node.prompt
                  ? '草稿已保存'
                  : '尚无草稿'}
            </span>
          </footer>
        </>
      ) : (
        <>
          {!versions && !readError && <p>正在读取作品版本…</p>}
          {versions?.items.length === 0 && (
            <p>尚无作品版本。生成失败不会创建空白版本。</p>
          )}
          {versions?.items.map((v) => (
            <div className="cc-generation-version" key={v.id}>
              <strong>
                版本 {v.version_no}
                {versionOrigins[v.origin_kind]
                  ? ` · ${versionOrigins[v.origin_kind]}`
                  : ''}
                {versions.selected_version_id === v.id ? ' · 当前采用' : ''}
              </strong>
              <small>{new Date(v.created_at).toLocaleString()}</small>
              <div>
                <button
                  onClick={() => {
                    if (previewID !== v.content_revision_id) setPreview(null)
                    setPreviewID(v.content_revision_id)
                  }}
                >
                  预览
                </button>
                <button
                  disabled={locked || versions.selected_version_id === v.id}
                  onClick={() =>
                    void act(() => versionAction('select_version', v.id))
                  }
                >
                  用于节点
                </button>
                <button
                  disabled={locked || !!local || v.origin_kind !== 'generation'}
                  onClick={() =>
                    void act(async () => {
                      await queue.enqueue(`${path}/commands`, {
                        type: 'reuse_version_prompt',
                        node_id: node.id,
                        version_id: v.id,
                        expected_draft_revision:
                          node.prompt?.draft_revision ?? null,
                        expected_topology_revision: canvas.topology_revision,
                        read_set: graphReadSet(canvas, [v.id]),
                      })
                      setTab('prompt')
                    })
                  }
                >
                  复用参数
                </button>
                <button
                  disabled={locked || versions.selected_version_id === v.id}
                  onClick={() =>
                    void act(() => versionAction('delete_version', v.id))
                  }
                >
                  删除
                </button>
              </div>
            </div>
          ))}
          {previewID && (
            <section aria-label="版本预览">
              <p>仅预览，不改变节点当前内容</p>
              {preview?.id === previewID ? (
                preview.kind === 'text' ? (
                  <pre>
                    {'body' in preview.payload ? preview.payload.body : ''}
                  </pre>
                ) : (
                  <MediaFigure
                    content={preview}
                    kind={preview.kind}
                    title="作品版本"
                    controls
                  />
                )
              ) : (
                <p>正在读取预览…</p>
              )}
            </section>
          )}
        </>
      )}
      {execution?.error && (
        <p role="status">
          {execution.error === 'partial_import'
            ? '部分结果未能入库，已成功的作品仍可查看。'
            : execution.error === 'generation_verification_required'
              ? '生成结果待核实，现有作品已保留。'
              : '生成未完成，现有作品已保留，请稍后重试或联系管理员核实。'}
        </p>
      )}
      {execution?.apply_state === 'conflicted' && (
        <p>节点或参考已变化，结果已保留为未采用版本。</p>
      )}
    </aside>,
    document.querySelector('.cc-layout') ?? document.body,
  )
}

// 参数面板浮在输入框外侧（优先上方），不被输入框自身的滚动区裁切。
function placeOptions(panel: HTMLElement | null): CSSProperties | undefined {
  const r = panel?.getBoundingClientRect()
  if (!r) return undefined
  const above = r.top - 16
  const below = window.innerHeight - r.bottom - 16
  const base = {
    position: 'fixed',
    left: r.left,
    width: r.width,
    right: 'auto',
  } as const
  return above >= 320 || above >= below
    ? {
        ...base,
        top: 'auto',
        bottom: window.innerHeight - r.top + 8,
        maxHeight: Math.min(560, above),
      }
    : {
        ...base,
        bottom: 'auto',
        top: r.bottom + 8,
        maxHeight: Math.min(560, below),
      }
}
