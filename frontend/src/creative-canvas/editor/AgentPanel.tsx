import { useEffect, useMemo, useRef, useState } from 'react'
import { useShallow } from 'zustand/react/shallow'
import {
  ArrowRight,
  ArrowUp,
  ClipboardList,
  Cpu,
  History,
  MessageCircle,
  Minus,
  Paperclip,
  Plus,
  ScanSearch,
  SquarePen,
  Wand2,
  Sparkles,
  type LucideIcon,
} from 'lucide-react'
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
  recalledModel,
  type AgentRun,
  type PendingAgentAction,
} from './agentState.ts'
import PromptEditor, { type PromptEditorHandle } from './PromptEditor.tsx'
import Picker, { type PickerOption } from './Picker.tsx'
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

// 新会话先用这个名字；第一次回答完成后由服务端按对话内容改名。
const untitled = '新对话'
// 自动命名在回答结束后异步完成，期间按这个间隔重读几次会话列表。
const namingPolls = 4
const namingInterval = 2500

// 空会话的起点：点一下填进输入框，摄影师改完再发。
const starters: { icon: LucideIcon; label: string; text?: string }[] = [
  {
    icon: ScanSearch,
    label: '读一读画布，理清下一步',
    text: '读一下当前画布，帮我梳理已有的内容，并建议下一步可以做什么。',
  },
  {
    icon: ClipboardList,
    label: '为这次拍摄写一份方案',
    text: '根据画布上的内容，帮我写一份拍摄方案：主题、场景、光线、服装道具和镜头安排。',
  },
  {
    icon: Wand2,
    label: '优化画布里的提示词',
    text: '帮我检查画布上图片节点的提示词，把画面描述改得更具体、更好出图。',
  },
  { icon: Paperclip, label: '引用素材开始讨论' },
]

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
  const editor = useRef<PromptEditorHandle>(null)
  const loaded = useRef(false)
  const recalled = useRef('')
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
  const [naming, setNaming] = useState(0)
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
        // 只在第一次打开时落到最近的会话；之后空会话是摄影师自己选的「新对话」。
        if (!loaded.current) {
          loaded.current = true
          setConversation((old) => old || page.items[0]?.id || '')
        }
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
  // 每进入一个会话沿用一次它上次的模型；之后在本会话里改选的不再被刷新覆盖。
  // 待确认的草稿记着摄影师当时选的模型，以草稿为准。
  useEffect(() => {
    if (!catalog || recalled.current === conversation) return
    if (!runs.some((run) => run.conversation_id === conversation)) return
    recalled.current = conversation
    if (pendingRef.current?.draft?.conversation === conversation) return
    const key = recalledModel(runs, conversation, catalog.models)
    if (key) setModel(key)
  }, [catalog, conversation, runs])
  // A run that moved on (finished, started waiting) changes what the lists hold.
  const moved = runs.some(
    (run, index) => followed[index] && followed[index].state !== run.state,
  )
  useEffect(() => {
    if (!moved) return
    setRefresh((value) => value + 1)
    setNaming(namingPolls)
  }, [moved])
  const current = conversations.find((item) => item.id === conversation)
  const waitingForName = naming > 0 && current?.title === untitled
  // 服务端在第一次回答之后才起名，这里短暂地多读几次列表把新名字接回来。
  useEffect(() => {
    if (!waitingForName) return
    const abort = new AbortController()
    const timer = setTimeout(() => {
      void api
        .read<components['schemas']['CreativeAgentConversationPage']>(
          `/canvases/${encodeURIComponent(canvasID)}/conversations`,
          abort.signal,
        )
        .then((page) => {
          if (!abort.signal.aborted) setConversations(page.items)
        })
        .catch(() => undefined)
        .finally(() => {
          if (!abort.signal.aborted) setNaming((value) => value - 1)
        })
    }, namingInterval)
    return () => {
      clearTimeout(timer)
      abort.abort()
    }
  }, [waitingForName, naming, canvasID])
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
      title: untitled,
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
  const modelOptions: PickerOption[] =
    catalog?.models.map((item) => ({
      value: item.model_key,
      label: item.display_name,
      icon: <Cpu size={15} />,
      meta: item.capability.image_input ? '识图' : undefined,
      description: item.available
        ? undefined
        : item.unavailable_reason || '暂不可用',
      disabled: !item.available,
    })) ?? []
  const skillOptions: PickerOption[] = [
    {
      value: '',
      label: '普通对话',
      description: '自由讨论创作想法，助手按需读写画布',
      icon: <MessageCircle size={15} />,
    },
    ...(catalog?.skills
      .filter((item) => item.available)
      .map((item) => ({
        value: item.skill_version_id,
        label: item.display_name,
        description: item.description,
        icon: <Wand2 size={15} />,
      })) ?? []),
  ]
  const conversationOptions: PickerOption[] = conversations.map((item) => ({
    value: item.id,
    label: item.title,
    meta: shortTime(item.updated_at),
  }))
  function startNew() {
    setConversation('')
    setMessages([])
    setRuns([])
    setError('')
    editor.current?.focus()
  }
  function applyStarter(starter: (typeof starters)[number]) {
    if (!starter.text) {
      setPickingMaterial(true)
      return
    }
    setDraft(starter.text)
    requestAnimationFrame(() => editor.current?.focus())
  }
  const sendable =
    !locked &&
    !active &&
    !!draft.trim() &&
    (!!waiting ||
      (!!chosenModel?.available && !unsupportedImage && !referenceLimit))
  return (
    <aside className="cc-agent-panel" aria-label="创作助手">
      <header>
        <h2 className="cc-agent-title" title={current?.title ?? untitled}>
          {current?.title ?? untitled}
        </h2>
        <button
          className="cc-agent-tool"
          disabled={locked || !conversation}
          onClick={startNew}
          aria-label="新对话"
          title="新对话"
        >
          <SquarePen size={16} />
        </button>
        <Picker
          className="cc-agent-tool"
          label="历史对话"
          align="end"
          minWidth={280}
          value={conversation}
          options={conversationOptions}
          disabled={locked}
          empty="还没有历史对话"
          onChange={(id) => {
            setConversation(id)
            setMessages([])
            setRuns([])
          }}
        >
          <History size={16} />
        </Picker>
        <button
          className="cc-agent-tool"
          onClick={onClose}
          aria-label="收起创作助手"
          title="收起"
        >
          <Minus size={18} />
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
            <span className="cc-agent-empty-mark" aria-hidden="true">
              <Sparkles size={22} />
            </span>
            <h3>让创作助手陪你把想法落到画布上</h3>
            <p>助手会读画布，输入 @ 引用画布里的文本或图片</p>
            {!waiting && (
              <ul className="cc-agent-starters" aria-label="从这里开始">
                {starters.map((starter) => (
                  <li key={starter.label}>
                    <button
                      type="button"
                      disabled={locked}
                      onClick={() => applyStarter(starter)}
                    >
                      <starter.icon size={16} />
                      <span>{starter.label}</span>
                      <ArrowRight size={15} className="cc-agent-starter-go" />
                    </button>
                  </li>
                ))}
              </ul>
            )}
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
              ref={editor}
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
                <Picker
                  className="cc-agent-mode"
                  label="Skill"
                  value={skill}
                  options={skillOptions}
                  minWidth={260}
                  disabled={locked || !!active}
                  onChange={setSkill}
                />
                <Picker
                  className="cc-agent-model"
                  label="模型"
                  align="end"
                  value={model}
                  options={modelOptions}
                  minWidth={240}
                  disabled={locked || !!active}
                  onChange={setModel}
                />
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

function shortTime(value: string): string {
  const date = new Date(value)
  return date.toDateString() === new Date().toDateString()
    ? date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
    : date.toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' })
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
