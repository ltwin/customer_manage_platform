import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useViewport } from '@xyflow/react'
import type { components } from '../../api/schema'
import * as api from './api.ts'
import { graphActionReadSet, graphReadSet, worldPoint } from './graph.ts'
import { errorMessage, type SaveQueue } from './queue.ts'
import {
  assertGenerationSnapshot,
  executionLabel,
  generationReadSet,
  parameterValue,
  promptFromNode,
  type Execution,
  type GenerationCatalog,
  type Prompt,
  type Versions,
} from './generation.ts'
import { MediaFigure } from './MediaView.tsx'
import './generation.css'

type Props = {
  leftInset: number
  canvas: api.Canvas
  node: api.CanvasNode
  queue: SaveQueue
  disabled: boolean
  assets: api.Asset[]
  refresh: () => Promise<void>
}

// 本容器拥有读取与队列接线；展示表单只接收数据和回调。
export default function NodeGenerationPanel({
  leftInset,
  canvas,
  node,
  queue,
  disabled,
  assets,
  refresh,
}: Props) {
  const viewport = useViewport()
  const point = worldPoint(canvas.nodes, node.id)
  const key = `prompt:${canvas.id}:${node.id}`
  const local = queue.value.drafts[key]?.prompt
  const prompt = local ?? promptFromNode(canvas, node)
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
    if (!local && node.prompt) return node.prompt.draft_revision
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
  ) {
    const actions: api.GraphAction[] = [
      { type, node_id: node.id, version_id: id },
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
    (local.expected_draft_revision !== (node.prompt?.draft_revision ?? null) ||
      local.expected_topology_revision !== canvas.topology_revision)
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
  const panelTop = `clamp(44px, ${(point.y + node.metadata.height) * viewport.zoom + viewport.y + 12}px, max(44px, 100% - 330px))`
  return createPortal(
    <aside
      className="cc-generation nodrag nopan nowheel"
      aria-label="节点生成"
      style={{
        left: `clamp(min(${leftInset}px, max(8px, 100% - 376px)), ${point.x * viewport.zoom + viewport.x + 82}px, max(8px, 100% - 376px))`,
        top: panelTop,
        maxHeight: `min(560px, calc(100% - ${panelTop} - 12px))`,
      }}
      onKeyDown={(e) => e.stopPropagation()}
      onPointerDown={(e) => e.stopPropagation()}
    >
      <header>
        <strong>{node.metadata.title || '节点'} · 创作</strong>
        <span role="status">{executionLabel(state)}</span>
      </header>
      <nav aria-label="生成面板">
        <button
          type="button"
          aria-pressed={tab === 'prompt'}
          onClick={() => setTab('prompt')}
        >
          提示词
        </button>
        <button
          type="button"
          aria-pressed={tab === 'versions'}
          onClick={() => setTab('versions')}
        >
          作品版本{versions ? ` · ${versions.items.length}` : ''}
        </button>
      </nav>
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
          <label>
            提示词
            <textarea
              aria-label="提示词"
              rows={3}
              maxLength={32000}
              value={prompt.text}
              disabled={locked}
              onChange={(e) => edit({ ...prompt, text: e.target.value })}
              placeholder="描述你想创作的内容…"
            />
          </label>
          <label>
            生成模型
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
                  {prompt.model_key}（当前不可用）
                </option>
              )}
              {catalog?.models.map((m) => (
                <option key={m.key} value={m.key}>
                  {m.label}
                </option>
              ))}
            </select>
          </label>
          {model?.parameters.map((field) => (
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
                      const v = parameterValue(field, e.target.value)
                      if (v === undefined) delete parameters[field.key]
                      else parameters[field.key] = v
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
                      {v === 'true' ? '开启' : v === 'false' ? '关闭' : v}
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
                      const v = parameterValue(field, e.target.value)
                      if (v === undefined) delete parameters[field.key]
                      else parameters[field.key] = v
                      edit({ ...prompt, parameters })
                    } catch (e) {
                      setError(errorMessage(e))
                    }
                  }}
                />
              )}
            </label>
          ))}
          {!model && Object.keys(prompt.parameters).length > 0 && (
            <p>已保留历史模型参数，选择可用模型后可编辑。</p>
          )}
          <fieldset disabled={locked}>
            <legend>参考素材 · {prompt.references.length}</legend>
            {prompt.references.map((r, i) => (
              <div
                className="cc-generation-ref"
                key={`${r.source_node_id ?? r.content_revision_id}:${i}`}
              >
                <span>
                  {r.source_node_id
                    ? canvas.nodes.find((n) => n.id === r.source_node_id)
                        ?.metadata.title || '画布节点'
                    : assets.find(
                        (a) => a.content_revision_id === r.content_revision_id,
                      )?.title || '固定素材'}{' '}
                  · {r.role}
                </span>
                <button
                  type="button"
                  aria-label={`移除参考 ${i + 1}`}
                  onClick={() =>
                    edit({
                      ...prompt,
                      references: prompt.references.filter((_, j) => j !== i),
                    })
                  }
                >
                  移除
                </button>
              </div>
            ))}
            <select
              aria-label="添加参考素材"
              value=""
              disabled={prompt.references.length >= 50}
              onChange={(e) => {
                const [kind, id] = e.target.value.split('|')
                if (!id) return
                const ordinal =
                  Math.max(-1, ...prompt.references.map((r) => r.ordinal)) + 1
                edit({
                  ...prompt,
                  references: [
                    ...prompt.references,
                    {
                      slot: 'reference',
                      role: 'reference',
                      ordinal,
                      source_node_id: kind === 'node' ? id : null,
                      content_revision_id: kind === 'revision' ? id : null,
                    },
                  ],
                })
              }}
            >
              <option value="">添加画布节点或个人库素材…</option>
              <optgroup label="画布节点">
                {canvas.nodes
                  .filter(
                    (n) =>
                      n.id !== node.id &&
                      n.data.content_revision_id &&
                      !prompt.references.some((r) => r.source_node_id === n.id),
                  )
                  .map((n) => (
                    <option key={n.id} value={`node|${n.id}`}>
                      {n.metadata.title || '未命名节点'}
                    </option>
                  ))}
              </optgroup>
              <optgroup label="个人库">
                {assets
                  .filter(
                    (a) =>
                      !prompt.references.some(
                        (r) => r.content_revision_id === a.content_revision_id,
                      ),
                  )
                  .map((a) => (
                    <option
                      key={a.id}
                      value={`revision|${a.content_revision_id}`}
                    >
                      {a.title || '未命名素材'}
                    </option>
                  ))}
              </optgroup>
            </select>
            <small>
              连线与节点内参考会在提交时一并读取；素材使用仍需生成授权。
            </small>
          </fieldset>
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
            <span>
              {local
                ? '本机草稿待保存'
                : node.prompt
                  ? '草稿已保存'
                  : '尚无草稿'}
            </span>
            <button
              disabled={locked || conflict}
              onClick={() =>
                void act(async () => {
                  await save()
                })
              }
            >
              保存草稿
            </button>
            {node.status.active_execution_id ? (
              <button
                disabled={locked}
                onClick={() =>
                  void act(async () => {
                    await queue.enqueue(`${path}/commands`, {
                      type: 'cancel_execution',
                      execution_id: node.status.active_execution_id,
                    })
                  })
                }
              >
                停止
              </button>
            ) : (
              <button
                className="cc-generation-primary"
                disabled={locked || conflict || unsupported || !model}
                onClick={() => void act(generate)}
              >
                生成新版本
              </button>
            )}
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
