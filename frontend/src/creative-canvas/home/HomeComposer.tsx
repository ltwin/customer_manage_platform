import {
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
  type Ref,
} from 'react'
import { useNavigate } from 'react-router-dom'
import { ArrowUp, Plus, RotateCw, Sparkles, X } from 'lucide-react'
import type { components } from '../../api/schema'
import * as api from '../editor/api.ts'
import {
  agentPendingKey,
  ownsPendingChain,
  pendingAgentChain,
  performAgentChain,
  replacePendingChain,
  sendRefusal,
  type PendingAgentChain,
} from '../editor/agentState.ts'
import {
  agentModelOptions,
  agentSkillOptions,
  defaultAgentModel,
} from '../editor/agentOptions.tsx'
import { AgentMaterialPicker } from '../editor/AgentMaterials.tsx'
import { useEgressNotice } from '../editor/EgressNotice.tsx'
import Picker from '../editor/Picker.tsx'
import { errorMessage } from '../editor/queue.ts'
import { useJournal } from '../editor/useJournal.ts'
import '../editor/agent.css'
import './composer.css'
import {
  attachmentCap,
  savedAttachments,
  useChatAttachments,
  type ChatAttachment,
} from './chatAttachments.ts'
import { CoverMedia } from './ProjectCover.tsx'
import {
  homeChain,
  homeStartKey,
  projectNameFrom,
  readHomeStart,
  recoverHomeStart,
  type HomeStart,
} from './startProject.ts'

type Catalog = components['schemas']['CreativeAgentCatalog']
export type ComposerControls = { addReference(id: string, title: string): void }

// 场景 chip：点一下填进示例文字，目录里有对应的平台 Skill 就一并选上；不发送。
// 只写已经上线的 Skill，以后加了新的再补（owner 2026-10-11）。
const scenes = [
  {
    label: '参考整理与创作方向',
    text: '春季品牌 Lookbook，低饱和暖调、自然光。从我的素材库里找一组参考，整理成情绪板，再写一段创作方向。',
    slug: 'reference-direction',
  },
]

function createdProject(
  result: unknown,
): { id: string; canvasID: string; name: string } | null {
  if (
    result &&
    typeof result === 'object' &&
    'id' in result &&
    typeof result.id === 'string' &&
    'default_canvas_id' in result &&
    typeof result.default_canvas_id === 'string' &&
    result.default_canvas_id &&
    'name' in result &&
    typeof result.name === 'string'
  )
    return {
      id: result.id,
      canvasID: result.default_canvas_id,
      name: result.name,
    }
  return null
}

export default function HomeComposer({
  account,
  controls,
}: {
  account: string
  controls?: Ref<ComposerControls>
}) {
  const navigate = useNavigate()
  const { queue, error: storageError } = useJournal(account)
  const egress = useEgressNotice(account)
  const storeKey = homeStartKey(account)
  const [catalog, setCatalog] = useState<Catalog | null>(null)
  const [catalogError, setCatalogError] = useState('')
  const [text, setText] = useState('')
  const [model, setModel] = useState('')
  const [skill, setSkill] = useState('')
  const attachments = useChatAttachments(attachmentCap(catalog?.limits))
  const { restore } = attachments
  // 本机记录的镜像：项目建好之后再发送都用这个项目。
  const [start, setStart] = useState<HomeStart | null>(null)
  // 结果未知、只能原样重放的链；记录读不出来时只能进画布处理。
  const [pending, setPending] = useState<
    PendingAgentChain | 'unreadable' | null
  >(null)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [picking, setPicking] = useState(false)
  const [dragging, setDragging] = useState(false)
  const files = useRef<HTMLInputElement>(null)
  const alive = useRef(false)
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

  // 刷新后找回这次提交（design §4.3 的四种情况）。
  useEffect(() => {
    let saved: HomeStart | null = null
    try {
      saved = readHomeStart(sessionStorage.getItem(storeKey))
    } catch {
      return
    }
    if (!saved) return
    let slot: PendingAgentChain | null = null
    let unreadable = false
    if (saved.canvasID && saved.chain)
      try {
        slot = pendingAgentChain(
          sessionStorage.getItem(agentPendingKey(account, saved.canvasID)),
        )
      } catch {
        unreadable = true
      }
    const recovery = unreadable
      ? ({ kind: 'unknown', start: saved, chain: null } as const)
      : recoverHomeStart(saved, slot)
    if (recovery.kind === 'none') return
    if (recovery.kind === 'settled') {
      try {
        sessionStorage.removeItem(storeKey)
      } catch {
        // 清不掉也只是下次再判断一次。
      }
      return
    }
    setStart(recovery.start)
    setText(recovery.start.text)
    setModel(recovery.start.model)
    setSkill(recovery.start.skill)
    restore(recovery.start.attachments)
    if (recovery.kind === 'unknown') setPending(recovery.chain ?? 'unreadable')
  }, [account, storeKey, restore])

  useEffect(() => {
    const abort = new AbortController()
    void api
      .read<Catalog>('/agent/catalog', abort.signal)
      .then((value) => {
        if (abort.signal.aborted) return
        setCatalog(value)
        setModel((old) => old || defaultAgentModel(value))
      })
      .catch((e: unknown) => {
        if (!abort.signal.aborted)
          setCatalogError(e instanceof Error ? e.message : '助手暂时不可用')
      })
    return () => abort.abort()
  }, [])

  const job = queue?.value.job
  const locked = busy || !!pending || !!job || !!storageError
  const chosen = catalog?.models.find((item) => item.model_key === model)
  const images = attachments.items.length > 0
  const unsupportedImage = images && !!chosen && !chosen.capability.image_input
  // 发送前的检查全在本地完成，不发任何请求。
  const blockedReason = !catalog
    ? ''
    : !chosen?.available
      ? '所选模型暂不可用，请换一个模型'
      : attachments.uploading
        ? '参考图上传完成后才能发送'
        : attachments.failed
          ? '有参考图上传失败，重试或移除后再发送'
          : unsupportedImage
            ? '所选模型不支持图片，换一个识图模型或移除图片'
            : ''
  const canSend =
    !locked && !!catalog && !blockedReason && !!text.trim() && !!queue

  useImperativeHandle(
    controls,
    () => ({
      addReference(id, title) {
        if (!locked) attachments.addRevision(id, title)
      },
    }),
    [locked, attachments],
  )

  function save(next: HomeStart | null) {
    setStart(next)
    try {
      if (next) sessionStorage.setItem(storeKey, JSON.stringify(next))
      else sessionStorage.removeItem(storeKey)
    } catch {
      // 本机存不下只是少了刷新后的找回；画布槽位和 journal 照常保护重放。
    }
  }
  // 必须在 journal 清掉建项目的操作前落盘，不能只靠 enqueue 的返回值交接。
  function rememberProject(
    current: HomeStart,
    result: unknown,
    operation: string,
  ): HomeStart {
    const project = createdProject(result)
    if (!project) throw new Error('项目回执暂不可读，请恢复原保存')
    const next = {
      ...current,
      projectID: project.id,
      projectOperation: operation,
      canvasID: project.canvasID,
      name: project.name,
    }
    try {
      sessionStorage.setItem(storeKey, JSON.stringify(next))
    } catch {
      throw new Error('无法保存项目恢复记录，请保留输入并恢复原保存')
    }
    setStart(next)
    return next
  }
  function slotFor(canvasID: string) {
    const key = agentPendingKey(account, canvasID)
    return {
      key,
      owns: (chain: PendingAgentChain) =>
        alive.current && ownsPendingChain(sessionStorage, key, chain),
      save: (expected: PendingAgentChain, next: PendingAgentChain | null) =>
        alive.current &&
        replacePendingChain(sessionStorage, key, expected, next),
    }
  }
  function openCanvas(canvasID: string) {
    void navigate(`/creative/canvases/${encodeURIComponent(canvasID)}`, {
      state: { openAgent: true },
    })
  }
  async function run(current: HomeStart, chain: PendingAgentChain) {
    const canvasID = current.canvasID
    if (!canvasID) return
    setPending(chain)
    const outcome = await performAgentChain(
      chain,
      slotFor(canvasID),
      (path, body, operation) => api.send(account, path, body, operation),
    )
    if (!alive.current) return
    switch (outcome.kind) {
      case 'done':
        save(null)
        setPending(null)
        openCanvas(canvasID)
        return
      case 'rejected': {
        // 链已清掉；文字和参考图留着，改了再发仍用这个项目和已建好的对话。
        const conversation =
          outcome.chain.draft?.conversation || current.conversation
        save({
          ...current,
          chain: undefined,
          ...(conversation ? { conversation } : {}),
        })
        setPending(null)
        setNotice(sendRefusal(outcome.error, current.attachments.length > 0))
        return
      }
      case 'unknown':
        setPending(outcome.chain)
        return
      case 'lost':
        // 槽位被画布里的面板接手了，交给它处理。
        save({ ...current, chain: undefined })
        setPending(null)
        setNotice('这条消息已在画布里处理，进入画布查看')
    }
  }
  async function sendMessage(current: HomeStart) {
    const canvasID = current.canvasID
    if (!canvasID || !catalog || !chosen) return
    const selected = catalog.skills.find(
      (item) => item.skill_version_id === current.skill,
    )
    const chain = homeChain(
      { ...current, canvasID },
      { imageInput: chosen.capability.image_input },
      selected,
    )
    if (
      !replacePendingChain(sessionStorage, slotFor(canvasID).key, null, chain)
    )
      throw new Error('这张画布上还有一条消息没确认送达，请进入画布处理')
    const next = {
      ...current,
      chain: { operations: chain.steps.map((step) => step.operation) },
    }
    save(next)
    await run(next, chain)
  }
  async function send() {
    if (!canSend || !queue) return
    // 首次外发说明放在建项目之前：取消时什么都不建，输入和参考图原样留着。
    if (!(await egress.confirm()) || !alive.current) return
    setNotice('')
    setBusy(true)
    let current: HomeStart = {
      ...(start?.projectID
        ? {
            projectID: start.projectID,
            canvasID: start.canvasID,
            name: start.name,
            ...(start.conversation ? { conversation: start.conversation } : {}),
          }
        : {}),
      text,
      model,
      skill,
      attachments: savedAttachments(attachments.items),
    }
    try {
      save(current)
      if (!current.projectID) {
        await queue.enqueue(
          '/projects',
          { name: projectNameFrom(text) },
          'project-form',
          (result, job) => {
            current = rememberProject(current, result, job.operation)
          },
        )
        if (!alive.current || queue.value.job) return
      }
      await sendMessage(current)
    } catch (e) {
      if (alive.current) setNotice(errorMessage(e))
    } finally {
      if (alive.current) setBusy(false)
    }
  }
  async function retry() {
    if (busy || !pending || pending === 'unreadable' || !start) return
    setNotice('')
    setBusy(true)
    try {
      await run(start, pending)
    } finally {
      if (alive.current) setBusy(false)
    }
  }
  // 建项目的结果不确定：按 journal 的操作身份查回执，查到了接着发消息。
  async function recover() {
    if (!queue || queue.busy || busy) return
    setNotice('')
    setBusy(true)
    try {
      const job = queue.value.job
      const creating = job?.path === '/projects'
      const homeProject =
        creating &&
        start &&
        (!start.projectID || start.projectOperation === job.operation)
      let recovered: HomeStart | undefined
      const result = await queue.flush(
        true,
        homeProject
          ? (reply, job) => {
              recovered = rememberProject(start, reply, job.operation)
            }
          : undefined,
      )
      if (!alive.current || queue.value.job || !creating) return
      const project = createdProject(result)
      if (!project) return
      if (!homeProject) {
        // 不是这次一句话开始建的项目（例如项目页的新建），直接打开它。
        void navigate(
          `/creative/canvases/${encodeURIComponent(project.canvasID)}`,
        )
        return
      }
      if (recovered) await sendMessage(recovered)
    } catch (e) {
      if (alive.current) setNotice(errorMessage(e))
    } finally {
      if (alive.current) setBusy(false)
    }
  }
  async function blank() {
    if (locked || !queue) return
    setNotice('')
    setBusy(true)
    try {
      const result = await queue.enqueue(
        '/projects',
        { name: '未命名项目' },
        'project-form',
      )
      if (!alive.current || queue.value.job) return
      const project = createdProject(result)
      if (project)
        void navigate(
          `/creative/canvases/${encodeURIComponent(project.canvasID)}`,
        )
    } catch (e) {
      if (alive.current) setNotice(errorMessage(e))
    } finally {
      if (alive.current) setBusy(false)
    }
  }
  function edit(value: string) {
    setText(value)
    // 被拒后摄影师自己清空了输入框：这次提交就此放下，下次从头开始。
    if (!value.trim() && !attachments.items.length && start && !pending) {
      save(null)
      setNotice('')
    }
  }
  function pickScene(scene: (typeof scenes)[number]) {
    if (locked) return
    setText(scene.text)
    const match = catalog?.skills.find(
      (item) =>
        item.slug === scene.slug &&
        item.origin === 'platform' &&
        item.available,
    )
    if (match) setSkill(match.skill_version_id)
  }

  const name = start?.name ?? ''
  return (
    <div className="hc">
      <form
        className={`hc-ask${dragging ? ' dragging' : ''}`}
        aria-label="一句话开始"
        noValidate
        onSubmit={(event) => {
          event.preventDefault()
          void send()
        }}
        onDragOver={(event) => {
          if (locked || !event.dataTransfer.types.includes('Files')) return
          event.preventDefault()
          setDragging(true)
        }}
        onDragLeave={(event) => {
          if (!event.currentTarget.contains(event.relatedTarget as Node | null))
            setDragging(false)
        }}
        onDrop={(event) => {
          event.preventDefault()
          setDragging(false)
          if (!locked)
            void attachments.addFiles(Array.from(event.dataTransfer.files))
        }}
      >
        {!!attachments.items.length && (
          <ul className="hc-refs" aria-label="参考图">
            {attachments.items.map((item) => (
              <Reference
                key={item.key}
                item={item}
                locked={locked}
                onRetry={() => attachments.retry(item.key)}
                onRemove={() => attachments.remove(item.key)}
              />
            ))}
          </ul>
        )}
        <textarea
          aria-label="创作想法"
          rows={2}
          placeholder="说说这次拍摄的想法，助手会在新画布上帮你展开…"
          value={text}
          readOnly={locked}
          onChange={(event) => edit(event.target.value)}
          onKeyDown={(event) => {
            if (
              event.key === 'Enter' &&
              !event.shiftKey &&
              !event.nativeEvent.isComposing
            ) {
              event.preventDefault()
              void send()
            }
          }}
        />
        <div className="hc-foot">
          <Picker
            className="hc-tool"
            label="添加参考图"
            value=""
            minWidth={160}
            disabled={locked}
            options={[
              { value: 'upload', label: '上传图片' },
              { value: 'library', label: '从资产库选' },
            ]}
            onChange={(value) => {
              if (value === 'upload') files.current?.click()
              else setPicking(true)
            }}
          >
            <Plus size={16} />
          </Picker>
          <Picker
            className="hc-chip"
            label="Skill"
            value={skill}
            options={agentSkillOptions(catalog)}
            minWidth={260}
            disabled={locked || !catalog}
            onChange={setSkill}
          />
          <Picker
            className="hc-chip"
            label="模型"
            value={model}
            options={agentModelOptions(catalog)}
            minWidth={240}
            disabled={locked || !catalog}
            onChange={setModel}
          />
          <span className="hc-grow" />
          <button
            type="button"
            className="hc-blank"
            disabled={locked || !queue}
            onClick={() => void blank()}
          >
            空白画布
          </button>
          <button
            type="submit"
            className="hc-send"
            aria-label="交给助手"
            disabled={!canSend}
            aria-busy={busy}
          >
            <ArrowUp size={16} />
          </button>
        </div>
        <input
          ref={files}
          type="file"
          accept="image/*"
          multiple
          hidden
          onChange={(event) => {
            const picked = Array.from(event.target.files ?? [])
            event.target.value = ''
            void attachments.addFiles(picked)
          }}
        />
        <div className="hc-drop" aria-hidden="true">
          松手放入参考
        </div>
      </form>

      <div className="hc-notes">
        {(storageError || catalogError) && (
          <p role="alert">{storageError || catalogError}</p>
        )}
        {attachments.message && <p role="status">{attachments.message}</p>}
        {blockedReason && <p role="status">{blockedReason}</p>}
        {pending && !busy && (
          <section className="hc-notice" role="status">
            <p>
              {pending === 'unreadable'
                ? `项目「${name}」已建好，这条消息的待确认记录暂不可读，请进入画布查看。`
                : `项目「${name}」已建好，消息还没确认送达。`}
            </p>
            {pending !== 'unreadable' && (
              <button
                className="btn"
                type="button"
                onClick={() => void retry()}
              >
                <RotateCw size={14} />
                重试发送
              </button>
            )}
            <button
              className="btn"
              type="button"
              onClick={() => start?.canvasID && openCanvas(start.canvasID)}
            >
              进入画布
            </button>
          </section>
        )}
        {!pending && start?.projectID && !busy && (
          <p role="status">项目「{name}」已建好，消息还没发出。</p>
        )}
        {notice && <p role="alert">{notice}</p>}
        {job && !queue?.busy && !busy && (
          <section className="hc-notice" aria-live="polite">
            <p>
              {job.state === 'rejected'
                ? job.message
                : '新项目还没确认建好，查询后会接着发出这条消息。'}
            </p>
            {job.state === 'rejected' ? (
              <button
                className="btn"
                type="button"
                onClick={() => {
                  void queue
                    ?.dismissRejected()
                    .catch((e: unknown) => setNotice(errorMessage(e)))
                }}
              >
                关闭被拒绝的请求，保留输入
              </button>
            ) : (
              <button
                className="btn"
                type="button"
                onClick={() => void recover()}
              >
                查询并恢复原保存
              </button>
            )}
          </section>
        )}
      </div>

      <div className="hc-scenes" aria-label="从一个场景开始">
        {scenes.map((scene) => (
          <button
            key={scene.label}
            type="button"
            className="hc-scene"
            disabled={locked}
            onClick={() => pickScene(scene)}
          >
            <Sparkles size={14} />
            {scene.label}
          </button>
        ))}
      </div>
      {picking && (
        <AgentMaterialPicker
          imagesOnly
          onClose={() => setPicking(false)}
          onPick={(item) => {
            attachments.addRevision(item.id, item.title)
            setPicking(false)
          }}
        />
      )}
      {egress.dialog}
    </div>
  )
}

function Reference({
  item,
  locked,
  onRetry,
  onRemove,
}: {
  item: ChatAttachment
  locked: boolean
  onRetry: () => void
  onRemove: () => void
}) {
  return (
    <li className="hc-ref" data-state={item.state} title={item.title}>
      <span className="hc-ref-pic">
        {item.preview ? (
          <img src={item.preview} alt="" draggable={false} />
        ) : item.revisionID ? (
          <CoverMedia revision={item.revisionID} />
        ) : null}
        {item.state === 'uploading' && <small>上传中</small>}
      </span>
      {item.state === 'failed' && (
        <span className="hc-ref-error">
          {item.title}：{item.error}
          <button
            type="button"
            disabled={locked}
            onClick={onRetry}
            aria-label={`重试上传：${item.title}`}
          >
            重试
          </button>
        </span>
      )}
      <button
        type="button"
        className="hc-ref-remove"
        disabled={locked}
        onClick={onRemove}
        aria-label={`移除参考图：${item.title}`}
      >
        <X size={11} />
      </button>
    </li>
  )
}
