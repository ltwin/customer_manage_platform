import { useEffect, useMemo, useRef, useState } from 'react'
import { useShallow } from 'zustand/react/shallow'
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
  isActiveRun,
  pendingAgentAction,
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
import { AgentMaterialPicker } from './AgentMaterials.tsx'
import { Markdown, RunTurn } from './AgentBlocks.tsx'
import { currentStep } from './agentBlocks.ts'
import {
  AgentTurnContext,
  followRun,
  forgetRuns,
  useAgentRuns,
  type TurnContext,
} from './agentRuns.ts'
import './agent.css'

type Catalog = components['schemas']['CreativeAgentCatalog']
type Conversation = components['schemas']['CreativeAgentConversation']
type MessagePage = components['schemas']['CreativeAgentMessagePage']
type Consent = components['schemas']['CreativeEgressConsent']
type Message = MessagePage['items'][number]
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
  // Runs are followed for as long as this canvas is open, panel or no panel.
  useEffect(() => forgetRuns, [account, canvasID])
  const state = useAgentRuns((store) => {
    const runs = Object.values(store.runs)
    if (runs.some((view) => view && isActiveRun(view.run))) return 'run'
    return runs.some((view) => view?.run.state === 'waiting_input')
      ? 'wait'
      : undefined
  })
  return (
    <>
      <button
        className="cc-agent-launch"
        onClick={() => setOpen(true)}
        aria-label="打开创作助手"
        data-state={state}
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
  const [model, setModel] = useState('')
  const [skill, setSkill] = useState('')
  const [draft, setDraft] = useState('')
  const [materials, setMaterials] = useState<AgentMaterial[]>([])
  const [pickingMaterial, setPickingMaterial] = useState(false)
  const references = agentReferences(canvas, materials)
  const referenced = referencedMaterials(draft, references)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [storageBroken, setStorageBroken] = useState(false)
  const [pending, setPending] = useState<PendingAgentAction | null>(null)
  const [refresh, setRefresh] = useState(0)
  const pendingRef = useRef<PendingAgentAction | null>(null)
  const mounted = useRef(false)
  const performing = useRef(false)
  const history = useRef<HTMLDivElement>(null)
  const pinned = useRef(true)
  // The list is read once; what a run is doing now comes from its stream.
  const followed = useAgentRuns(
    useShallow((store) => runs.map((run) => store.runs[run.id]?.run)),
  )
  const live = runs.map((run, index) => followed[index] ?? run)
  const active = live.find(isActiveRun)
  const waiting = live.find((run) => run.state === 'waiting_input')
  const latest = active ?? waiting
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
      ...historyIDs,
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
    void Promise.all([
      api.read<components['schemas']['CreativeAgentRunPage']>(
        `/conversations/${conversation}/runs`,
        abort.signal,
      ),
      api.read<MessagePage>(
        `/conversations/${conversation}/messages`,
        abort.signal,
      ),
    ])
      .then(([page, thread]) => {
        if (abort.signal.aborted) return
        setRuns(page.items)
        setMessages(thread.items)
      })
      .catch((e: unknown) => {
        if (!abort.signal.aborted)
          setError(e instanceof Error ? e.message : '对话暂时无法更新')
      })
    return () => abort.abort()
  }, [conversation, refresh])
  useEffect(() => {
    for (const run of runs) followRun(run.id)
  }, [runs])
  // A run that moved on (finished, started waiting) changes what the lists hold.
  const moved = runs.some(
    (run, index) => followed[index] && followed[index].state !== run.state,
  )
  useEffect(() => {
    if (moved) setRefresh((value) => value + 1)
  }, [moved])
  // Follow the newest content unless the photographer has scrolled away.
  useEffect(() => {
    const stick = () => {
      const log = history.current
      if (log && pinned.current) log.scrollTop = log.scrollHeight
    }
    stick()
    return useAgentRuns.subscribe(() => requestAnimationFrame(stick))
  }, [messages])

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
      if (current.nextRun && 'approved_revisions' in reply) {
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
        pinned.current = true
      } else if ('title' in reply) {
        setConversation(reply.id)
      }
      savePending(current, null)
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
  // The first message opens the conversation; nobody should have to create
  // an empty one before they can type.
  async function openConversation(): Promise<string> {
    if (conversation) return conversation
    const command = action(`/canvases/${canvasID}/conversations`, {
      title: '创作对话',
    })
    const created = await api.send<Conversation>(
      account,
      command.path,
      command.body,
      command.operation,
    )
    setConversations((old) => [created, ...old])
    setConversation(created.id)
    return created.id
  }
  async function sendDraft() {
    if (!draft.trim() || locked || active) return
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
          { conversation, model, skill, text: draft, materials: referenced },
        ),
      )
      return
    }
    if (!chosenModel?.available || unsupportedImage || referenceLimit) return
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
    let target: string
    setBusy(true)
    try {
      target = await openConversation()
    } catch (e) {
      setError(e instanceof Error ? e.message : '暂时无法开始对话')
      return
    } finally {
      setBusy(false)
    }
    if (!mounted.current) return
    // Sending is the consent: the grant names exactly what this message uses,
    // and the server checks every run against it.
    void perform(
      action(
        `/conversations/${target}/egress-consents`,
        {
          purpose: 'creative_assistance',
          mode: 'selected_revisions',
          data_classes: [
            'text',
            'canvas_metadata',
            ...(chosenModel.capability.image_input ? ['image_preview'] : []),
          ],
          selected_revision_ids: approvedIDs,
        },
        {
          model_key: model,
          egress_consent_id: '',
          instruction: { schema_version: 1, instruction_segments: segments },
        },
        {
          conversation: target,
          model,
          skill,
          text: draft,
          materials: referenced,
        },
      ),
    )
  }
  const turn = useMemo<TurnContext>(
    () => ({
      canvas,
      retry: locked
        ? undefined
        : (runID, stepID) => {
            const run = runs.find((item) => item.id === runID)
            if (run)
              void perform(
                action(`/agent-runs/${runID}/retry`, {
                  step_ids: [stepID],
                  egress_consent_id: run.egress_consent_id,
                }),
              )
          },
    }),
    // perform only reads refs and setters; it is stable in what it does.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [canvas, locked, runs],
  )
  const triggers = new Map(runs.map((run) => [run.trigger_message_id, run.id]))
  const thread = [...messages].sort((a, b) =>
    BigInt(a.ordinal) < BigInt(b.ordinal) ? -1 : 1,
  )
  const sendable =
    !locked &&
    !active &&
    !!draft.trim() &&
    (!!waiting ||
      (!!chosenModel?.available && !unsupportedImage && !referenceLimit))
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
      {pending && !busy && (
        <div className="cc-agent-notice" role="status">
          <span>上一条还没有确认送达。</span>
          <button
            onClick={() => {
              const saved = pendingRef.current
              if (saved) void perform(saved)
            }}
          >
            重新发送
          </button>
        </div>
      )}
      <div
        className="cc-agent-history"
        aria-label="对话消息"
        role="log"
        ref={history}
        onScroll={(event) => {
          const log = event.currentTarget
          pinned.current =
            log.scrollHeight - log.scrollTop - log.clientHeight < 48
        }}
      >
        {!thread.length && (
          <div className="cc-agent-empty">
            <h3>想拍点什么？</h3>
            <p>
              把想法写下来，助手会读画布上的内容，陪你把方案想清楚。输入 @
              引用画布里的文本或图片。
            </p>
          </div>
        )}
        <AgentTurnContext.Provider value={turn}>
          {thread.map((message) => {
            const runID = triggers.get(message.id)
            if (message.role === 'user')
              return (
                <div className="cc-agent-exchange" key={message.id}>
                  <UserMessage message={message} references={references} />
                  {runID && <RunTurn runID={runID} />}
                </div>
              )
            // Runs from before blocks existed have only their messages.
            return message.role === 'assistant' ? (
              <LegacyAnswer key={message.id} message={message} />
            ) : null
          })}
        </AgentTurnContext.Provider>
      </div>
      <form
        className="cc-agent-compose"
        noValidate
        onSubmit={(event) => {
          event.preventDefault()
          void sendDraft()
        }}
      >
        {latest && (
          <RunStatus
            run={latest}
            disabled={locked}
            onStop={() => {
              void perform(action(`/agent-runs/${latest.id}/cancel`, {}))
            }}
            onRelease={() => {
              void perform(
                action(`/agent-runs/${latest.id}/close-reconciliation`, {}),
              )
            }}
          />
        )}
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
              placeholder="补充原任务所需的信息…"
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
              }}
              onGenerate={() => void sendDraft()}
              onLibrary={() => setPickingMaterial(true)}
            />
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
                <button
                  className="cc-agent-attach"
                  type="button"
                  aria-label="引用素材"
                  title="引用素材库里的文本或图片"
                  disabled={locked || !!active}
                  onClick={() => setPickingMaterial(true)}
                >
                  <Plus size={15} />
                </button>
                <select
                  aria-label="模型"
                  value={model}
                  disabled={locked || !!active}
                  onChange={(event) => setModel(event.target.value)}
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
              disabled={!sendable}
            >
              <ArrowUp size={17} />
            </button>
          </div>
        </div>
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
            setPickingMaterial(false)
          }}
        />
      )}
    </aside>
  )
}

// What the photographer sent: their words, with what they referenced as chips.
function UserMessage({
  message,
  references,
}: {
  message: Message
  references: { revisionID?: string | null; title: string }[]
}) {
  return (
    <div className="cc-agent-user">
      {message.body.blocks.map((block, index) =>
        block.type === 'text' ? (
          <span key={index}>{block.text}</span>
        ) : (
          <span className="cc-agent-chip" key={index}>
            {block.skill?.display_name ??
              references.find((item) => item.revisionID === block.ref_id)
                ?.title ??
              '引用素材'}
          </span>
        ),
      )}
    </div>
  )
}

function LegacyAnswer({ message }: { message: Message }) {
  const hasBlocks = useAgentRuns(
    (store) => !!store.runs[message.run_id ?? '']?.blocks.length,
  )
  const words = message.body.blocks
    .map((block) => (block.type === 'text' ? (block.text ?? '') : ''))
    .join('')
  if (hasBlocks || !words) return null
  return <Markdown source={words} />
}

// The row above the input while a run is going: what it is doing, and stop.
function RunStatus({
  run,
  disabled,
  onStop,
  onRelease,
}: {
  run: AgentRun
  disabled: boolean
  onStop: () => void
  onRelease: () => void
}) {
  const step = useAgentRuns((store) => {
    const view = store.runs[run.id]
    return view ? currentStep(view) : ''
  })
  const waiting = run.state === 'waiting_input'
  return (
    <div className="cc-agent-status" role="status" aria-label="运行状态">
      <span className={waiting ? 'cc-agent-dot is-wait' : 'cc-agent-dot'} />
      <span className={`cc-agent-status-label${waiting ? '' : ' cc-agent-shimmer'}`}>
        {waiting ? '等你补充后继续' : step || '正在处理'}
      </span>
      {run.state === 'reconciling' && (
        <button type="button" disabled={disabled} onClick={onRelease}>
          结束等待
        </button>
      )}
      <button type="button" disabled={disabled} onClick={onStop}>
        停止
      </button>
    </div>
  )
}
