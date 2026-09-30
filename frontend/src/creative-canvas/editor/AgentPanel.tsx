import { useEffect, useRef, useState } from 'react'
import { MessageCircle, X, Plus, ArrowUp, ChevronDown } from 'lucide-react'
import type { components } from '../../api/schema'
import { ApiError } from '../../api/transport.ts'
import * as api from './api.ts'
import {
  agentAction,
  consentedRunAction,
  replacePendingAction,
  ownsPendingAction,
  type AgentDraft,
  hasUnsettledCost,
  isActiveRun,
  pendingAgentAction,
  pollDelay,
  runLabels,
  runFailure,
  stepLabels,
  type AgentRun,
  type PendingAgentAction,
} from './agentState.ts'
import PromptEditor from './PromptEditor.tsx'
import {
  agentReferences,
  agentSegments,
  referencedMaterials,
  type AgentMaterial,
} from './agentReferences.ts'
import { AgentMaterialPicker, AgentReference } from './AgentMaterials.tsx'
import './agent.css'

type Catalog = components['schemas']['CreativeAgentCatalog']
type Conversation = components['schemas']['CreativeAgentConversation']
type MessagePage = components['schemas']['CreativeAgentMessagePage']
type Consent = components['schemas']['CreativeEgressConsent']
type Step = components['schemas']['CreativeAgentStepSummary']
type Reply = AgentRun | Conversation | Consent

export default function AgentPanel({
  account,
  canvasID,
  canvas,
}: {
  account: string
  canvasID: string
  canvas?: api.Canvas | null
}) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button
        className="cc-agent-launch"
        onClick={() => setOpen(true)}
        aria-label="打开创作助手"
      >
        <MessageCircle size={18} /> 创作助手
      </button>
      {open && (
        <AgentConversation
          key={`${account}:${canvasID}`}
          account={account}
          canvasID={canvasID}
          canvas={canvas}
          onClose={() => setOpen(false)}
        />
      )}
    </>
  )
}
function AgentConversation({
  account,
  canvasID,
  canvas,
  onClose,
}: {
  account: string
  canvasID: string
  canvas?: api.Canvas | null
  onClose: () => void
}) {
  const storageKey = `creative-agent-pending:${account}:${canvasID}`
  const threadMenu = useRef<HTMLDetailsElement>(null)
  const threadButton = useRef<HTMLElement>(null)
  const [catalog, setCatalog] = useState<Catalog | null>(null)
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [conversation, setConversation] = useState('')
  const [messages, setMessages] = useState<MessagePage['items']>([])
  const [runs, setRuns] = useState<AgentRun[]>([])
  const [steps, setSteps] = useState<Step[]>([])
  const [model, setModel] = useState('')
  const [skill, setSkill] = useState('')
  const [draft, setDraft] = useState('')
  const [materials, setMaterials] = useState<AgentMaterial[]>([])
  const [pickingMaterial, setPickingMaterial] = useState(false)
  const references = agentReferences(canvas, materials)
  const referenced = referencedMaterials(draft, references)
  const [consented, setConsented] = useState(false)
  const [includeHistoryMaterials, setIncludeHistoryMaterials] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [feedback, setFeedback] = useState('')
  const [storageBroken, setStorageBroken] = useState(false)
  const [pending, setPending] = useState<PendingAgentAction | null>(null)
  const [refresh, setRefresh] = useState(0)
  const pendingRef = useRef<PendingAgentAction | null>(null)
  const mounted = useRef(false)
  const performing = useRef(false)
  const active = runs.find(isActiveRun)
  const waiting = runs.find((run) => run.state === 'waiting_input')
  const latest = active ?? waiting ?? runs[0]
  const historyIDs = [
    ...new Set(
      messages
        .slice()
        .sort((a, b) => (BigInt(a.ordinal) > BigInt(b.ordinal) ? -1 : 1))
        .slice(0, 20)
        .flatMap((message) =>
          message.body.blocks.flatMap((block) =>
            block.type === 'content_ref' && block.ref_id ? [block.ref_id] : [],
          ),
        ),
    ),
  ]
  const approvedIDs = [
    ...new Set([
      ...(includeHistoryMaterials ? historyIDs : []),
      ...referenced.map((item) => item.id),
    ]),
  ]
  const chosenModel = catalog?.models.find((item) => item.model_key === model)
  const locked = busy || !!pending || storageBroken
  const unsupportedImage =
    referenced.some((item) => item.kind === 'image') &&
    !chosenModel?.capability.image_input
  const referenceLimit =
    approvedIDs.length > 200 ||
    referenced.length > (catalog?.limits.attachments ?? 10) ||
    referenced.filter((item) => item.kind === 'image').length >
      (catalog?.limits.image_preview_count ?? 8)

  useEffect(() => {
    const close = (event: PointerEvent) => {
      if (
        event.target instanceof Node &&
        !threadMenu.current?.contains(event.target) &&
        threadMenu.current
      )
        threadMenu.current.open = false
    }
    document.addEventListener('pointerdown', close)
    return () => document.removeEventListener('pointerdown', close)
  }, [])
  function closeThreadMenu() {
    if (threadMenu.current) threadMenu.current.open = false
    threadButton.current?.focus()
  }

  function restoreDraft(value?: AgentDraft) {
    if (!value) return
    setConversation(value.conversation)
    setModel(value.model)
    setSkill(value.skill)
    setMaterials(value.materials ?? [])
    setDraft(value.text)
    setConsented(false)
  }
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  useEffect(() => {
    try {
      const saved = pendingAgentAction(sessionStorage.getItem(storageKey))
      pendingRef.current = saved
      setPending(saved)
      restoreDraft(saved?.draft)
    } catch (e) {
      setStorageBroken(true)
      setError(e instanceof Error ? e.message : '待确认操作暂不可读')
    }
  }, [storageKey])
  useEffect(() => {
    const abort = new AbortController()
    void Promise.all([
      api.read<Catalog>('/agent/catalog', abort.signal),
      api.read<components['schemas']['CreativeAgentConversationPage']>(
        `/canvases/${encodeURIComponent(canvasID)}/conversations`,
        abort.signal,
      ),
    ])
      .then(([directory, page]) => {
        if (abort.signal.aborted) return
        setCatalog(directory)
        setConversations(page.items)
        setModel(
          (old) =>
            old ||
            directory.models.find((item) => item.available)?.model_key ||
            '',
        )
        setConversation((old) => old || page.items[0]?.id || '')
      })
      .catch((e: unknown) => {
        if (!abort.signal.aborted)
          setError(e instanceof Error ? e.message : '暂时无法加载对话')
      })
    return () => abort.abort()
  }, [canvasID, refresh])
  useEffect(() => {
    if (!conversation) return
    const abort = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    async function poll() {
      try {
        const [page, history] = await Promise.all([
          api.read<components['schemas']['CreativeAgentRunPage']>(
            `/conversations/${conversation}/runs`,
            abort.signal,
          ),
          api.read<MessagePage>(
            `/conversations/${conversation}/messages`,
            abort.signal,
          ),
        ])
        if (abort.signal.aborted) return
        setRuns(page.items)
        setMessages(history.items)
        const focus =
          page.items.find(isActiveRun) ??
          page.items.find((run) => run.state === 'waiting_input') ??
          page.items[0]
        if (focus) {
          const detail = await api.read<
            components['schemas']['CreativeAgentStepPage']
          >(`/agent-runs/${focus.id}/steps`, abort.signal)
          if (!abort.signal.aborted) setSteps(detail.items)
        } else setSteps([])
      } catch (e) {
        if (abort.signal.aborted) return
        setError(e instanceof Error ? e.message : '对话暂时无法更新')
        if (e instanceof ApiError && [401, 403, 404].includes(e.status)) return
      }
      if (!abort.signal.aborted)
        timer = setTimeout(() => {
          void poll()
        }, pollDelay(document.hidden))
    }
    void poll()
    return () => {
      abort.abort()
      if (timer) clearTimeout(timer)
    }
  }, [conversation, refresh])

  function savePending(
    expected: PendingAgentAction | null,
    value: PendingAgentAction | null,
  ): boolean {
    if (
      !mounted.current ||
      !replacePendingAction(sessionStorage, storageKey, expected, value)
    )
      return false
    pendingRef.current = value
    setPending(value)
    return true
  }
  function owns(command: PendingAgentAction): boolean {
    return (
      mounted.current && ownsPendingAction(sessionStorage, storageKey, command)
    )
  }
  const action = agentAction
  async function perform(command: PendingAgentAction) {
    if (performing.current || !mounted.current) return
    performing.current = true
    setBusy(true)
    setError('')
    setFeedback('')
    let current = command
    try {
      if (!savePending(pendingRef.current, command)) return
      let reply = await api.send<Reply>(
        account,
        current.path,
        current.body,
        current.operation,
      )
      if (!owns(current)) return
      if (current.nextRun && 'vendor_key' in reply) {
        const next = consentedRunAction(current, reply.id)
        if (!savePending(current, next)) return
        current = next
        reply = await api.send<Reply>(
          account,
          current.path,
          current.body,
          current.operation,
        )
        if (!owns(current)) return
      }
      if ('trigger_message_id' in reply) {
        setConversation(reply.conversation_id)
        setDraft('')
        setMaterials([])
        setRuns((old) => [reply, ...old.filter((run) => run.id !== reply.id)])
      } else if ('title' in reply) {
        setConversation(reply.id)
        setFeedback('对话已创建，可以开始输入问题。')
      }
      savePending(current, null)
      setConsented(false)
      setRefresh((value) => value + 1)
    } catch (e) {
      if (!owns(current)) return
      setError(
        e instanceof Error ? e.message : '提交结果暂不确定，请确认原操作',
      )
      if (
        e instanceof ApiError &&
        e.status >= 400 &&
        e.status < 500 &&
        ![408, 429].includes(e.status)
      ) {
        restoreDraft(current.draft)
        savePending(current, null)
      }
    } finally {
      performing.current = false
      if (mounted.current) setBusy(false)
    }
  }
  function sendDraft() {
    if (!conversation || !draft.trim() || locked || active) return
    const draftState = {
      conversation,
      model,
      skill,
      text: draft,
      materials: referenced,
    }
    if (waiting) {
      void perform(
        action(
          `/agent-runs/${waiting.id}/supplements`,
          {
            expected_revision: waiting.revision,
            waiting_token: waiting.waiting_token,
            text: draft,
          },
          undefined,
          draftState,
        ),
      )
      return
    }
    if (
      !chosenModel?.available ||
      !consented ||
      unsupportedImage ||
      referenceLimit
    )
      return
    const selected = catalog?.skills.find(
      (item) => item.skill_version_id === skill,
    )
    let segments: components['schemas']['CreativeAgentInstructionSegment'][]
    try {
      segments = agentSegments(draft, references)
    } catch (e) {
      setError(e instanceof Error ? e.message : '引用不可用')
      return
    }
    if (selected)
      segments.unshift({
        type: 'skill_ref',
        skill_id: selected.skill_id,
        skill_version_id: selected.skill_version_id,
      })
    const nextRun: components['schemas']['CreativeAgentRunPayload'] = {
      model_key: model,
      egress_consent_id: '',
      instruction: { schema_version: 1, instruction_segments: segments },
    }
    void perform(
      action(
        `/conversations/${conversation}/egress-consents`,
        {
          vendor_key: chosenModel.vendor_key,
          purpose: 'creative_assistance',
          mode: 'selected_revisions',
          data_classes: [
            'text',
            'canvas_metadata',
            ...(chosenModel.capability.image_input ? ['image_preview'] : []),
          ],
          selected_revision_ids: approvedIDs,
        },
        nextRun,
        draftState,
      ),
    )
  }
  return (
    <aside className="cc-agent-panel" aria-label="创作助手">
      <header>
        <details
          className="cc-agent-thread"
          ref={threadMenu}
          onKeyDown={(event) => {
            if (event.key === 'Escape') {
              event.preventDefault()
              closeThreadMenu()
            }
          }}
        >
          <summary ref={threadButton} aria-label="对话菜单">
            创作助手 <ChevronDown size={13} />
          </summary>
          <div className="cc-agent-conversations">
            <select
              aria-label="当前对话"
              disabled={locked}
              value={conversation}
              onChange={(event) => {
                setConversation(event.target.value)
                setMessages([])
                setRuns([])
                setSteps([])
                setConsented(false)
                setFeedback('')
                closeThreadMenu()
              }}
            >
              <option value="">选择对话</option>
              {conversations.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.title}
                </option>
              ))}
            </select>
            <button
              disabled={locked}
              onClick={() => {
                closeThreadMenu()
                void perform(
                  action(`/canvases/${canvasID}/conversations`, {
                    title: '创作对话',
                  }),
                )
              }}
              aria-label="新建对话"
              title="新建对话"
            >
              <Plus size={17} />
              <span>新建对话</span>
            </button>
          </div>
        </details>
        <button
          className="cc-icon-button"
          onClick={onClose}
          aria-label="收起创作助手"
          title="收起创作助手"
        >
          <X size={18} />
        </button>
      </header>
      {error && (
        <div className="cc-agent-error" role="alert">
          {error}
        </div>
      )}
      <div className="cc-agent-feedback" role="status" aria-live="polite">
        {busy
          ? pending?.path.endsWith('/conversations')
            ? '正在创建对话…'
            : '正在提交…'
          : !pending && feedback}
      </div>
      {pending && !busy && (
        <div className="cc-agent-notice" role="status">
          <span>上次提交的结果尚未确认。请确认原操作，避免重复提交。</span>
          <button
            onClick={() => {
              const saved = pendingRef.current
              if (saved) void perform(saved)
            }}
          >
            确认原操作
          </button>
        </div>
      )}
      <div className="cc-agent-history" aria-label="对话消息">
        {!messages.length && (
          <p className="cc-agent-empty">
            把你的想法写下来，从一个创作问题开始。
            <br />
            支持文字对话、素材引用与固定 Skill。输入 @ 引用画布中的文本或图片。
          </p>
        )}
        {[...messages]
          .sort((a, b) => (BigInt(a.ordinal) < BigInt(b.ordinal) ? -1 : 1))
          .map((message) => (
            <article
              className={`cc-agent-message role-${message.role}`}
              key={message.id}
            >
              <small>
                {message.role === 'assistant'
                  ? '创作助手'
                  : message.role === 'user'
                    ? '我'
                    : '运行记录'}
              </small>
              {message.body.tool_calls?.length ? (
                <details>
                  <summary>
                    调用 {message.body.tool_calls.length} 个工具
                  </summary>
                  {message.body.tool_calls.map((call) => (
                    <p key={call.id}>{call.name}</p>
                  ))}
                </details>
              ) : null}
              {message.body.blocks.map((block, index) =>
                block.type === 'content_ref' && block.ref_id ? (
                  <AgentReference key={index} id={block.ref_id} />
                ) : message.role === 'tool' ? (
                  <details key={index}>
                    <summary>{message.body.tool_name || '工具结果'}</summary>
                    <pre>{block.text}</pre>
                  </details>
                ) : (
                  <p key={index}>
                    {block.text ||
                      block.skill?.display_name ||
                      (block.type === 'reference' ? '引用内容' : '')}
                  </p>
                ),
              )}
            </article>
          ))}
      </div>
      {latest && (
        <section className="cc-agent-status" aria-label="运行状态">
          <strong>{runLabels[latest.state]}</strong>
          <span>
            {hasUnsettledCost(latest)
              ? '费用待核实，取消不会抹去已发生的费用。'
              : latest.settlement_state === 'settled'
                ? '用量已结算'
                : '尚未产生模型用量'}
          </span>
          {latest.error_code && (
            <span>停止原因：{runFailure(latest.error_code)}</span>
          )}
          {steps.length > 0 && (
            <details>
              <summary>查看 {steps.length} 个步骤</summary>
              <ol>
                {steps.map((step) => (
                  <li key={step.id}>
                    {step.tool_key ||
                      (step.kind === 'model' ? '模型回复' : '恢复检查')}{' '}
                    · {stepLabels[step.state]}
                    {latest.finished_at &&
                      step.state === 'failed' &&
                      step.tool_key === 'read_skill_resource' && (
                        <button
                          disabled={locked}
                          onClick={() => {
                            void perform(
                              action(`/agent-runs/${latest.id}/retry`, {
                                step_ids: [step.id],
                                egress_consent_id: latest.egress_consent_id,
                              }),
                            )
                          }}
                        >
                          恢复此读取
                        </button>
                      )}
                  </li>
                ))}
              </ol>
            </details>
          )}
          {!latest.finished_at && (
            <div className="cc-agent-controls">
              <button
                disabled={locked}
                onClick={() => {
                  void perform(action(`/agent-runs/${latest.id}/cancel`, {}))
                }}
              >
                取消运行
              </button>
              {latest.state === 'reconciling' && (
                <button
                  disabled={locked}
                  onClick={() => {
                    void perform(
                      action(
                        `/agent-runs/${latest.id}/close-reconciliation`,
                        {},
                      ),
                    )
                  }}
                >
                  结束等待，释放运行占用
                </button>
              )}
            </div>
          )}
        </section>
      )}
      <form
        className="cc-agent-compose"
        noValidate
        onSubmit={(event) => {
          event.preventDefault()
          sendDraft()
        }}
      >
        <div className="cc-agent-box">
          <label className="sr-only" htmlFor="creative-agent-draft">
            {waiting ? '补充内容' : '创作问题'}
          </label>
          {waiting ? (
            <textarea
              id="creative-agent-draft"
              className="resize-none"
              value={draft}
              disabled={locked || !!active}
              onChange={(event) => setDraft(event.target.value)}
              onKeyDown={(event) => {
                if (
                  event.key === 'Enter' &&
                  !event.shiftKey &&
                  !event.nativeEvent.isComposing
                ) {
                  event.preventDefault()
                  event.currentTarget.form?.requestSubmit()
                }
              }}
              placeholder={
                waiting ? '补充原任务所需的信息…' : '想讨论什么创作方向？'
              }
              rows={3}
            />
          ) : (
            <PromptEditor
              text={draft}
              references={references}
              thumbnails={{}}
              disabled={locked || !!active}
              label="创作问题"
              referenceTitle="可引用的画布素材"
              placeholder="想讨论什么创作方向？输入 @ 引用画布素材…"
              onChange={(text) => {
                setMaterials(referencedMaterials(text, references))
                setDraft(text)
                setConsented(false)
              }}
              onGenerate={sendDraft}
              onLibrary={() => setPickingMaterial(true)}
            />
          )}
          {!waiting && (
            <button
              className="cc-agent-add-material"
              type="button"
              disabled={locked || !!active}
              onClick={() => setPickingMaterial(true)}
            >
              <Plus size={14} /> 引用素材
            </button>
          )}
          {!waiting && unsupportedImage && (
            <p className="cc-agent-hint" role="status">
              当前模型不支持图片理解，请切换视觉模型或移除图片引用。
            </p>
          )}
          {!waiting && referenceLimit && (
            <p className="cc-agent-hint" role="status">
              引用数量超过本轮上限，请减少素材。
            </p>
          )}
          <div className="cc-agent-composer-foot">
            {!waiting && (
              <div className="cc-agent-options">
                <select
                  aria-label="模型"
                  value={model}
                  disabled={locked || !!active}
                  onChange={(event) => {
                    setModel(event.target.value)
                    setConsented(false)
                  }}
                >
                  {catalog?.models.map((item) => (
                    <option
                      key={item.model_key}
                      value={item.model_key}
                      disabled={!item.available}
                    >
                      {item.display_name}
                      {!item.available ? '（不可用）' : ''}
                    </option>
                  ))}
                </select>
                <select
                  aria-label="Skill"
                  value={skill}
                  disabled={locked || !!active}
                  onChange={(event) => setSkill(event.target.value)}
                >
                  <option value="">普通对话</option>
                  {catalog?.skills
                    .filter((item) => item.available)
                    .map((item) => (
                      <option
                        key={item.skill_version_id}
                        value={item.skill_version_id}
                      >
                        {item.display_name}
                      </option>
                    ))}
                </select>
              </div>
            )}
            <button
              className="cc-agent-send"
              aria-label={waiting ? '补充并继续' : '发送'}
              title={waiting ? '补充并继续 · Enter' : '发送 · Enter'}
              type="submit"
              disabled={
                locked ||
                !!active ||
                !conversation ||
                !draft.trim() ||
                (!waiting &&
                  (!consented ||
                    !chosenModel?.available ||
                    unsupportedImage ||
                    referenceLimit))
              }
            >
              <ArrowUp size={17} />
            </button>
          </div>
        </div>
        {!waiting && historyIDs.length > 0 && (
          <label className="cc-agent-consent">
            <input
              type="checkbox"
              checked={includeHistoryMaterials}
              disabled={locked || !!active}
              onChange={(event) => {
                setIncludeHistoryMaterials(event.target.checked)
                setConsented(false)
              }}
            />
            带入最近对话引用的 {historyIDs.length}{' '}
            份素材；取消后保留文字与引用标识。
          </label>
        )}
        {!waiting && (
          <label className="cc-agent-consent">
            <input
              type="checkbox"
              checked={consented}
              disabled={locked || !!active}
              onChange={(event) => setConsented(event.target.checked)}
            />
            同意将会话文字与工具记录、画布概览、本次与最近对话引用的{' '}
            {approvedIDs.length} 份素材，以及选定 Skill 指令和资源发送给{' '}
            {chosenModel?.vendor_key || '所选模型供应商'}
            ，用于创作协助。图片仅发送给支持视觉输入的模型。
          </label>
        )}
      </form>
      {pickingMaterial && (
        <AgentMaterialPicker
          onClose={() => setPickingMaterial(false)}
          onPick={(item) => {
            setMaterials((old) => [
              ...old.filter((m) => m.id !== item.id),
              item,
            ])
            setDraft(
              (old) =>
                `${old}${old && !old.endsWith(' ') ? ' ' : ''}@[${item.id}] `,
            )
            setConsented(false)
            setPickingMaterial(false)
          }}
        />
      )}
    </aside>
  )
}
