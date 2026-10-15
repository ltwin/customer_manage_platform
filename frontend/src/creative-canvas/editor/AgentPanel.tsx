import { useEffect, useMemo, useRef, useState } from 'react'
import { useShallow } from 'zustand/react/shallow'
import {
  ArrowRight,
  ArrowUp,
  AtSign,
  ClipboardList,
  History,
  Library,
  MessageCircle,
  Minimize2,
  Minus,
  MoreHorizontal,
  Paperclip,
  Plus,
  ScanSearch,
  SquarePen,
  Wand2,
  Sparkles,
  type LucideIcon,
} from 'lucide-react'
import type { components } from '../../api/schema'
import * as api from './api.ts'
import {
  agentPendingKey,
  agentStep,
  conversationSlot,
  conversationStep,
  isActiveRun,
  messageSteps,
  ownsPendingChain,
  pendingAgentChain,
  performAgentChain,
  recalledModel,
  replacePendingChain,
  stepBody,
  untitledConversation as untitled,
  type AgentDraft,
  type AgentRun,
  type PendingAgentChain,
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
import { useEgressNotice } from './EgressNotice.tsx'
import {
  agentModelOptions,
  agentSkillOptions,
  defaultAgentModel,
} from './agentOptions.tsx'
import { RunTurn, TurnLoading } from './AgentBlocks.tsx'
import { AgentMarkdown } from './AgentMarkdown.tsx'
import { useAgentScroll } from './useAgentScroll.ts'
import {
  joinLatest,
  openWindow,
  prependTurns,
  readSince,
  readTurns,
  type PageReader,
  type ThreadWindow,
} from './agentThread.ts'
import { currentStep } from './agentBlocks.ts'
import {
  AgentTurnContext,
  followRun,
  forgetRuns,
  useAgentRuns,
  type TurnContext,
} from './agentRuns.ts'
import { attachmentCap, useChatAttachments } from '../home/chatAttachments.ts'
import {
  commandSegment,
  commandSuggestions,
  completeCommand,
  latestUsage,
  parseCommand,
  ringView,
  type ContextUsage,
} from './agentContext.ts'
import { AgentContextRing } from './AgentContextRing.tsx'
import {
  acceptOutgoing,
  messageParts,
  outgoingIn,
  outgoingParts,
  refreshAfterSend,
  type MessagePart,
  type Outgoing,
} from './agentOutgoing.ts'
import './agent.css'

type Catalog = components['schemas']['CreativeAgentCatalog']
type Conversation = components['schemas']['CreativeAgentConversation']
type MessagePage = components['schemas']['CreativeAgentMessagePage']
type Consent = components['schemas']['CreativeEgressConsent']
type Message = MessagePage['items'][number]
type Reply = AgentRun | Conversation | Consent
// One empty list, so an empty thread is the same value from render to render.
const noMessages: Message[] = []

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

// onUsage hears the context usage the newest page carries; older pages have none.
function messageReader(
  conversation: string,
  signal: AbortSignal,
  onUsage?: (usage: ContextUsage | null) => void,
): PageReader {
  return (before, limit) => {
    const query = new URLSearchParams({ limit: String(limit) })
    if (before) query.set('before_ordinal', before)
    return api
      .read<MessagePage>(`/conversations/${conversation}/messages?${query}`, signal)
      .then((page) => {
        if (!before) onUsage?.(page.context_usage ?? null)
        return page
      })
  }
}

// 只有一步的待确认链：补充、取消、结束核实、重试。
function single(
  path: string,
  payload: Record<string, unknown>,
  draft?: AgentDraft,
): PendingAgentChain {
  return { steps: [agentStep(path, payload)], ...(draft ? { draft } : {}) }
}

export default function AgentPanel({
  account,
  canvasID,
  canvas,
  defaultOpen = false,
}: {
  account: string
  canvasID: string
  canvas?: api.Canvas | null
  // 从首页一句话开始进来时，面板直接打开。
  defaultOpen?: boolean
}) {
  const [open, setOpen] = useState(defaultOpen)
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
  const storageKey = agentPendingKey(account, canvasID)
  const notice = useEgressNotice(account)
  const editor = useRef<PromptEditorHandle>(null)
  const loaded = useRef(false)
  const recalled = useRef('')
  const [catalog, setCatalog] = useState<Catalog | null>(null)
  const [conversations, setConversations] = useState<Conversation[]>([])
  // 读回来之前不画空状态：先闪一下「新对话」再跳出历史，像是丢了东西。
  const [listRead, setListRead] = useState<'loading' | 'ready' | 'failed'>(
    'loading',
  )
  const [threadRead, setThreadRead] = useState<{
    id: string
    failed: boolean
  } | null>(null)
  // 已归档的对话单独一份列表；查看时整个面板只读，恢复后才能继续。
  const [archivedView, setArchivedView] = useState(false)
  const [archived, setArchived] = useState<Conversation[] | null>(null)
  const [renaming, setRenaming] = useState<string | null>(null)
  const cancelRename = useRef(false)
  const [conversation, setConversation] = useState('')
  // 从最新一轮往前读到哪里，往上滚时再接上更早的。
  const [threadWindow, setThreadWindow] = useState<ThreadWindow | null>(null)
  const windowRef = useRef<ThreadWindow | null>(null)
  windowRef.current = threadWindow
  const [older, setOlder] = useState<'idle' | 'loading' | 'failed'>('idle')
  const olderAbort = useRef<AbortController | null>(null)
  const [runs, setRuns] = useState<AgentRun[]>([])
  // 会话最新一页带回的上下文用量；运行中的每次请求另由事件更新。
  const [pageUsage, setPageUsage] = useState<{
    conversation: string
    usage: ContextUsage | null
  } | null>(null)
  const [model, setModel] = useState('')
  const [skill, setSkill] = useState('')
  const [draft, setDraft] = useState('')
  const [materials, setMaterials] = useState<AgentMaterial[]>([])
  const attachments = useChatAttachments(attachmentCap(catalog?.limits))
  const uploadInput = useRef<HTMLInputElement>(null)
  const [pickingMaterial, setPickingMaterial] = useState(false)
  const references = agentReferences(canvas, materials)
  const mentioned = referencedMaterials(draft, references)
  const uploaded: AgentMaterial[] = attachments.items.flatMap((item) =>
    item.state === 'ready' && item.revisionID ? [{id:item.revisionID, title:item.title, kind:'image' as const}] : [],
  )
  const referenced = [...new Map([...mentioned, ...uploaded].map((item) => [item.id,item])).values()]
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [storageBroken, setStorageBroken] = useState(false)
  const [pending, setPending] = useState<PendingAgentChain | null>(null)
  const [refresh, setRefresh] = useState(0)
  // 只重读当前会话的消息和运行：往已有会话里发一条，用不着连目录和列表一起读。
  const [threadRefresh, setThreadRefresh] = useState(0)
  // 刚发出的一条：点发送就画出来，不等两次请求和重读走完。
  const [outgoing, setOutgoing] = useState<Outgoing | null>(null)
  const [naming, setNaming] = useState(0)
  const pendingRef = useRef<PendingAgentChain | null>(null)
  const mounted = useRef(false)
  const performing = useRef(false)
  const thread =
    threadWindow?.conversation === conversation ? threadWindow.items : noMessages
  const {
    history,
    content: historyContent,
    onScroll: onHistoryScroll,
    follow: followLatest,
    hold: holdPosition,
    nearTop,
  } = useAgentScroll(conversation, thread[0]?.id ?? '')
  // The list is read once; what a run is doing now comes from its stream.
  const followed = useAgentRuns(
    useShallow((store) => runs.map((run) => store.runs[run.id]?.run)),
  )
  const runUsages = useAgentRuns(
    useShallow((store) => runs.map((run) => store.runs[run.id]?.usage)),
  )
  const live = runs.map((run, index) => followed[index] ?? run)
  const active = live.find(isActiveRun)
  const waiting = live.find((run) => run.state === 'waiting_input')
  const latest = active ?? waiting
  const approvedIDs = referenced.map((item) => item.id)
  const chosenModel = catalog?.models.find((item) => item.model_key === model)
  const usage = conversation
    ? latestUsage([
        pageUsage?.conversation === conversation ? pageUsage.usage : null,
        ...runUsages,
      ])
    : null
  const compactPercent = catalog?.limits.auto_compact_percent ?? 0
  const ring = ringView(usage, chosenModel, compactPercent)
  const command = waiting ? null : parseCommand(draft)
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
      const saved = pendingAgentChain(sessionStorage.getItem(storageKey))
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
        setListRead('ready')
        setModel((old) => old || defaultAgentModel(directory))
        // 只在第一次打开时落到最近的会话；之后空会话是摄影师自己选的「新对话」。
        if (!loaded.current) {
          loaded.current = true
          setConversation((old) => old || page.items[0]?.id || '')
        }
      })
      .catch((e: unknown) => {
        if (abort.signal.aborted) return
        setError(e instanceof Error ? e.message : '暂时无法加载对话')
        setListRead((old) => (old === 'ready' ? old : 'failed'))
      })
    return () => abort.abort()
  }, [canvasID, refresh])
  useEffect(() => {
    if (!archivedView) return
    const abort = new AbortController()
    void api
      .read<components['schemas']['CreativeAgentConversationPage']>(
        `/canvases/${encodeURIComponent(canvasID)}/conversations?archived=true`,
        abort.signal,
      )
      .then((page) => {
        if (abort.signal.aborted) return
        setArchived(page.items)
        // 进来时落到最近归档的那个，免得面板空着。
        setConversation((old) => old || page.items[0]?.id || '')
      })
      .catch((e: unknown) => {
        if (!abort.signal.aborted)
          setError(e instanceof Error ? e.message : '暂时无法读取已归档的对话')
      })
    return () => abort.abort()
  }, [archivedView, canvasID, refresh])
  useEffect(() => {
    if (!conversation) return
    const abort = new AbortController()
    const read = messageReader(conversation, abort.signal, (value) => {
      if (!abort.signal.aborted) setPageUsage({ conversation, usage: value })
    })
    // 已经读过这个会话就只补新的一段，往前翻出来的历史留着；否则从最新一轮读起。
    const known = windowRef.current
    const update: Promise<(old: ThreadWindow | null) => ThreadWindow | null> =
      known?.conversation === conversation && known.newest
        ? readSince(read, known.newest).then(
            (fresh) => (old) =>
              old?.conversation === conversation ? joinLatest(old, fresh) : old,
          )
        : readTurns(read).then(
            (turns) => () => openWindow(conversation, turns),
          )
    void Promise.all([
      api.read<components['schemas']['CreativeAgentRunPage']>(
        `/conversations/${conversation}/runs`,
        abort.signal,
      ),
      update,
    ])
      .then(([page, apply]) => {
        if (abort.signal.aborted) return
        setRuns(page.items)
        setThreadWindow(apply)
        setThreadRead({ id: conversation, failed: false })
      })
      .catch((e: unknown) => {
        if (abort.signal.aborted) return
        setError(e instanceof Error ? e.message : '对话暂时无法更新')
        // 已经读出来的内容留着，只有一次都没读到才算读取失败。
        setThreadRead((old) =>
          old?.id === conversation ? old : { id: conversation, failed: true },
        )
      })
    return () => abort.abort()
  }, [conversation, refresh, threadRefresh])
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
  const listed = archivedView ? (archived ?? []) : conversations
  const current = listed.find((item) => item.id === conversation)
  const waitingForName = naming > 0 && current?.title_source === 'default'
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
  function savePending(
    expected: PendingAgentChain | null,
    value: PendingAgentChain | null,
  ): boolean {
    if (
      !mounted.current ||
      !replacePendingChain(sessionStorage, storageKey, expected, value)
    )
      return false
    pendingRef.current = value
    setPending(value)
    return true
  }
  function owns(chain: PendingAgentChain): boolean {
    return (
      mounted.current && ownsPendingChain(sessionStorage, storageKey, chain)
    )
  }
  // keepDraft leaves the composer alone: compacting from the ring is not
  // sending what the creator is still writing.
  async function perform(command: PendingAgentChain, keepDraft = false) {
    if (performing.current || !mounted.current) return
    performing.current = true
    setBusy(true)
    setError('')
    try {
      if (!savePending(pendingRef.current, command)) return
      const outcome = await performAgentChain(
        command,
        { owns, save: savePending },
        (path, body, operation) =>
          api.send<Reply>(account, path, body, operation),
      )
      if (outcome.kind === 'lost') {
        if (mounted.current) setOutgoing(null)
        return
      }
      if (outcome.kind === 'done') {
        const reply = outcome.reply as Reply
        if ('trigger_message_id' in reply) {
          setOutgoing((old) => old && acceptOutgoing(old, reply))
          setConversation(reply.conversation_id)
          if (!keepDraft) {
            setDraft('')
            setMaterials([])
            attachments.clear()
          }
          setRuns((old) => [reply, ...old.filter((run) => run.id !== reply.id)])
          followLatest()
        }
      } else {
        setError(
          outcome.error instanceof Error
            ? outcome.error.message
            : '提交结果暂不确定，请确认原操作',
        )
        // 原文回到输入框：明确被拒时链已清掉，可以改了再发；结果未知时链还在，
        // 输入框随链锁着，等确认送达或重新发送。建好的对话照样留着。
        setOutgoing(null)
        restoreDraft(outcome.chain.draft)
      }
      if (
        outcome.kind === 'done' &&
        refreshAfterSend(command, outcome.reply) === 'thread'
      )
        setThreadRefresh((value) => value + 1)
      else setRefresh((value) => value + 1)
    } finally {
      performing.current = false
      if (mounted.current) setBusy(false)
    }
  }
  // /compact: the whole submission is the command, with its focus if any.
  function sendCommand(
    value: NonNullable<ReturnType<typeof parseCommand>>,
    keepDraft = false,
  ) {
    if (locked || active || waiting) return
    if (!conversation) {
      setError('新对话还没有可以整理的内容')
      return
    }
    if (!chosenModel?.available) return
    const chain: PendingAgentChain = {
      steps: messageSteps(conversation, {
        model,
        imageInput: chosenModel.capability.image_input,
        revisionIDs: [],
        segments: [commandSegment(value)],
      }),
      ...(keepDraft
        ? {}
        : { draft: { conversation, model, skill, text: draft, materials: [] } }),
    }
    void notice.confirm().then((ok) => {
      if (!ok) return
      showOutgoing(outgoingParts([commandSegment(value)], titleOf), !keepDraft)
      void perform(chain, keepDraft)
    })
  }
  function sendDraft() {
    // 只敲了命令的开头时，回车补全命令，不当文字发出去。
    const completed = waiting ? null : completeCommand(draft)
    if (completed) {
      setDraft(completed)
      requestAnimationFrame(() => editor.current?.focus())
      return
    }
    if (command) {
      sendCommand(command)
      return
    }
    if ((!draft.trim() && !uploaded.length) || locked || active || attachments.uploading || attachments.failed) return
    const saved = {
      conversation,
      model,
      skill,
      text: [draft, ...uploaded.filter((item) => !mentioned.some((m) => m.id === item.id)).map((item) => `@[${item.id}]`)].filter(Boolean).join(' '),
      materials: referenced,
    }
    if (waiting) {
      void perform(
        single(
          `/agent-runs/${waiting.id}/supplements`,
          {
            expected_revision: waiting.revision,
            waiting_token: waiting.waiting_token,
            text: draft,
          },
          saved,
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
      for (const item of uploaded) {
        if (!mentioned.some((m) => m.id === item.id)) segments.push({type:'content_ref',content_revision_id:item.id})
      }
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
    // 第一条消息顺带建对话，三步一起记进待确认链：建对话的结果不确定时，
    // 重放的也是同一个操作，不会多出空对话。
    const chain = {
      steps: [
        ...(conversation ? [] : [conversationStep(canvasID)]),
        ...messageSteps(conversation || conversationSlot, {
          model,
          imageInput: chosenModel.capability.image_input,
          revisionIDs: approvedIDs,
          segments,
        }),
      ],
      draft: saved,
    }
    // 新消息才会带出新内容，先过首次外发说明；取消时草稿原样留在输入框。
    void notice.confirm().then((ok) => {
      if (!ok) return
      showOutgoing(outgoingParts(segments, titleOf, selected?.display_name))
      void perform(chain)
    })
  }
  // 点发送就把这条画进对话、清空输入框；草稿已记在待确认链里，没送达会放回来。
  function showOutgoing(parts: MessagePart[], clear = true) {
    setOutgoing({ conversation, parts })
    if (clear) {
      setDraft('')
      setMaterials([])
      attachments.clear()
    }
    followLatest()
  }
  // 引用的标题：画布或素材库里选的，或者刚上传的附件。
  function titleOf(revisionID: string) {
    return (
      references.find((item) => item.revisionID === revisionID)?.title ??
      uploaded.find((item) => item.id === revisionID)?.title
    )
  }
  const turn = useMemo<TurnContext>(
    () => ({
      canvas,
      retry:
        locked || archivedView
          ? undefined
          : (runID, stepID) => {
              const run = runs.find((item) => item.id === runID)
              if (run)
                void perform(
                  single(`/agent-runs/${runID}/retry`, {
                    step_ids: [stepID],
                    egress_consent_id: run.egress_consent_id,
                  }),
                )
            },
    }),
    // perform only reads refs and setters; it is stable in what it does.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [canvas, locked, archivedView, runs],
  )
  const triggers = new Map(runs.map((run) => [run.trigger_message_id, run.id]))
  const followedRuns = new Set(triggers.values())
  // 往前接上整轮。同时只读一次；读的过程中换了会话或刷新换掉了窗口，结果作废。
  function loadOlder() {
    const from = windowRef.current
    if (
      !from ||
      from.conversation !== conversation ||
      !from.before ||
      olderAbort.current
    )
      return
    const abort = new AbortController()
    olderAbort.current = abort
    setOlder('loading')
    readTurns(messageReader(from.conversation, abort.signal), from)
      .then((turns) => {
        if (abort.signal.aborted) return
        holdPosition()
        setThreadWindow((old) =>
          old?.conversation === from.conversation && old.before === from.before
            ? prependTurns(old, turns)
            : old,
        )
        setOlder('idle')
      })
      .catch(() => {
        if (!abort.signal.aborted) setOlder('failed')
      })
      .finally(() => {
        if (olderAbort.current === abort) olderAbort.current = null
      })
  }
  // 换会话时丢掉还在读的更早内容。
  useEffect(() => {
    setOlder('idle')
    return () => {
      olderAbort.current?.abort()
      olderAbort.current = null
    }
  }, [conversation])
  // 内容还没铺满或停在顶部附近时，接着往前读，不用摄影师再滚一下。
  useEffect(() => {
    if (older === 'idle' && threadWindow?.before && nearTop()) loadOlder()
    // loadOlder 只读 ref 和当前会话；按窗口变化判断即可。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [threadWindow, older])
  function onThreadScroll() {
    onHistoryScroll()
    if (older === 'idle' && nearTop()) loadOlder()
  }
  // 刚发出的运行已经在列表里，它的消息还在路上：同样算在读。
  const reading: 'loading' | 'failed' | null =
    listRead !== 'ready'
      ? listRead
      : conversation && threadRead?.id !== conversation
        ? 'loading'
        : threadRead?.failed
          ? 'failed'
          : !thread.length && runs.length
            ? 'loading'
            : null
  const sending = outgoingIn(outgoing, conversation, thread)
  // 读回了真消息，刚发出的那条就功成身退。
  useEffect(() => {
    if (outgoing?.trigger && !outgoingIn(outgoing, outgoing.conversation, thread))
      setOutgoing(null)
  }, [outgoing, thread])
  function reread() {
    setError('')
    if (listRead === 'failed') setListRead('loading')
    setThreadRead(null)
    setRefresh((value) => value + 1)
  }
  const modelOptions = agentModelOptions(catalog)
  const skillOptions = agentSkillOptions(catalog)
  const conversationOptions: PickerOption[] = listed.map((item) => ({
    value: item.id,
    label: item.title,
    meta: shortTime(item.updated_at),
  }))
  function show(id: string) {
    attachments.clear()
    setConversation(id)
    setThreadWindow(null)
    setRuns([])
    setRenaming(null)
  }
  function startNew() {
    setArchivedView(false)
    show('')
    setError('')
    editor.current?.focus()
  }
  // 改名、归档、恢复只是整理列表，不涉及外发，按操作标识重放即可，
  // 不进待确认的那套存储。
  async function manage(
    path: string,
    payload: Record<string, string>,
    apply: (reply: Conversation) => void,
  ): Promise<boolean> {
    if (locked) return false
    setBusy(true)
    setError('')
    try {
      const step = agentStep(path, payload)
      const reply = await api.send<Conversation>(
        account,
        step.path,
        stepBody(step),
        step.operation,
      )
      if (mounted.current) apply(reply)
      return true
    } catch (e) {
      if (mounted.current)
        setError(e instanceof Error ? e.message : '操作没有完成，请重试')
      return false
    } finally {
      if (mounted.current) setBusy(false)
    }
  }
  const replace = (reply: Conversation) => (old: Conversation[]) =>
    old.map((item) => (item.id === reply.id ? reply : item))
  function commitRename(value: string) {
    setRenaming(null)
    const title = value.trim()
    const target = current
    if (!target || !title || title === target.title) return
    // 先显示新名字；没改成就换回原来的。
    setConversations(replace({ ...target, title }))
    void manage(`/conversations/${target.id}/rename`, { title }, (reply) =>
      setConversations(replace(reply)),
    ).then((done) => {
      if (!done && mounted.current) setConversations(replace(target))
    })
  }
  function archiveCurrent() {
    const target = current
    if (!target) return
    void manage(`/conversations/${target.id}/archive`, {}, (reply) => {
      const rest = conversations.filter((item) => item.id !== reply.id)
      setConversations(rest)
      setArchived((old) => old && [reply, ...old])
      show(rest[0]?.id ?? '')
    })
  }
  function restoreCurrent() {
    const target = current
    if (!target) return
    void manage(`/conversations/${target.id}/restore`, {}, (reply) => {
      setArchived((old) => old && old.filter((item) => item.id !== reply.id))
      setConversations((old) =>
        [reply, ...old.filter((item) => item.id !== reply.id)].sort(
          (a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at),
        ),
      )
      setArchivedView(false)
    })
  }
  // 还有没结束的运行时不能归档：服务端同样会拒绝。
  const unfinished = live.some(
    (run) => run.conversation_id === conversation && !run.finished_at,
  )
  const manageOptions: PickerOption[] = archivedView
    ? [{ value: 'leave', label: '返回对话列表' }]
    : [
        ...(current
          ? [
              { value: 'rename', label: '重命名' },
              {
                value: 'archive',
                label: '归档',
                description: unfinished ? '这次运行结束后才能归档' : undefined,
                disabled: unfinished,
              },
            ]
          : []),
        { value: 'archived', label: '查看已归档' },
      ]
  function onManage(value: string) {
    if (value === 'rename' && current) setRenaming(current.title)
    else if (value === 'archive') archiveCurrent()
    else if (value === 'archived') {
      setArchivedView(true)
      setArchived(null)
      show('')
    } else if (value === 'leave') {
      setArchivedView(false)
      show(conversations[0]?.id ?? '')
    }
  }
  function applyStarter(starter: (typeof starters)[number]) {
    if (!starter.text) {
      setPickingMaterial(true)
      return
    }
    setDraft(starter.text)
    requestAnimationFrame(() => editor.current?.focus())
  }
  const suggestions = waiting || locked || active ? [] : commandSuggestions(draft)
  const compactBlocked = !conversation
    ? '新对话还没有可以整理的内容'
    : active
      ? '这次运行结束后才能压缩'
      : waiting
        ? '补充完成后才能压缩'
        : locked
          ? '请稍候'
          : !chosenModel?.available
            ? '当前模型不可用'
            : undefined
  // 输入框的「+」：把素材带进来的几种方式，加上平时藏在斜杠后面的命令。
  const addOptions: PickerOption[] = [
    {
      value: 'upload',
      label: '上传图片',
      description: '也可以粘贴或拖进来，不存入资产库',
      icon: <Paperclip size={15} />,
      group: '添加',
    },
    {
      value: 'library',
      label: '从资产库选',
      description: '文本或图片，引用固定版本',
      icon: <Library size={15} />,
      group: '添加',
    },
    {
      value: 'canvas',
      label: '引用画布节点',
      description: '同输入 @',
      icon: <AtSign size={15} />,
      group: '添加',
    },
    {
      value: 'compact',
      label: '压缩上下文',
      description: compactBlocked ?? '把较早的对话整理成摘要',
      icon: <Minimize2 size={15} />,
      meta: '/compact',
      group: '命令',
      disabled: !!compactBlocked,
    },
  ]
  function add(value: string) {
    if (value === 'upload') uploadInput.current?.click()
    else if (value === 'library') setPickingMaterial(true)
    else if (value === 'canvas') editor.current?.openPicker()
    else if (value === 'compact')
      sendCommand({ name: 'compact', args: '' }, true)
  }
  const sendable =
    !locked &&
    !active &&
    (!!draft.trim() || uploaded.length > 0) &&
    !attachments.uploading && !attachments.failed &&
    (!!waiting ||
      (!!chosenModel?.available && !unsupportedImage && !referenceLimit))
  return (
    <aside className="cc-agent-panel" aria-label="创作助手">
      <header>
        {renaming !== null ? (
          <input
            className="cc-agent-title-input"
            aria-label="对话名称"
            value={renaming}
            maxLength={120}
            autoFocus
            onFocus={(event) => event.currentTarget.select()}
            onChange={(event) => setRenaming(event.target.value)}
            onKeyDown={(event) => {
              if (event.nativeEvent.isComposing) return
              if (event.key === 'Escape') cancelRename.current = true
              if (event.key === 'Enter' || event.key === 'Escape') {
                event.preventDefault()
                event.currentTarget.blur()
              }
            }}
            onBlur={(event) => {
              if (cancelRename.current) {
                cancelRename.current = false
                setRenaming(null)
              } else commitRename(event.currentTarget.value)
            }}
          />
        ) : (
          <h2 className="cc-agent-title" title={current?.title ?? untitled}>
            {listRead === 'loading' ? (
              <>
                <span className="sr-only">正在读取</span>
                <span
                  className="cc-agent-bone cc-agent-title-bone"
                  aria-hidden="true"
                />
              </>
            ) : (
              (current?.title ?? (archivedView ? '已归档的对话' : untitled))
            )}
          </h2>
        )}
        <Picker
          className="cc-agent-tool"
          label="对话操作"
          align="end"
          minWidth={180}
          value=""
          options={manageOptions}
          disabled={locked}
          onChange={onManage}
        >
          <MoreHorizontal size={16} />
        </Picker>
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
          empty={archivedView ? '没有已归档的对话' : '还没有历史对话'}
          onChange={show}
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
        aria-busy={reading === 'loading'}
        onScroll={onThreadScroll}
      >
        <div ref={historyContent} className="cc-agent-history-content">
          {!reading && threadWindow?.before && (
            <div className="cc-agent-older" role="status">
              {older === 'failed' ? (
                <>
                  <span>更早的对话没有读出来。</span>
                  <button type="button" onClick={loadOlder}>
                    重新读取
                  </button>
                </>
              ) : (
                <span>正在读取更早的对话…</span>
              )}
            </div>
          )}
          {reading === 'loading' && !sending && <ThreadLoading />}
          {reading === 'failed' && (
            <div className="cc-agent-read-failed" role="status">
              <span>对话没有读出来。</span>
              <button type="button" onClick={reread}>
                重新读取
              </button>
            </div>
          )}
          {!reading && !thread.length && !archivedView && !sending && (
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
                    <UserMessage parts={messageParts(message.body.blocks, titleOf)} />
                    {runID && <RunTurn runID={runID} />}
                  </div>
                )
              // Runs from before blocks existed have only their messages.
              return message.role === 'assistant' ? (
                <LegacyAnswer
                  key={message.id}
                  message={message}
                  followed={followedRuns.has(message.run_id ?? '')}
                />
              ) : null
            })}
            {sending && (
              <div className="cc-agent-exchange is-sending">
                <UserMessage parts={sending.parts} sending />
                {sending.run && <RunTurn runID={sending.run} />}
              </div>
            )}
          </AgentTurnContext.Provider>
        </div>
      </div>
      {archivedView ? (
        <div className="cc-agent-archived" role="status">
          {current ? (
            <>
              <span>这个对话已归档，恢复后可以继续。</span>
              <button type="button" disabled={locked} onClick={restoreCurrent}>
                恢复
              </button>
            </>
          ) : (
            <span>
              {archived ? '没有已归档的对话' : '正在读取已归档的对话…'}
            </span>
          )}
        </div>
      ) : (
        <form
          className="cc-agent-compose"
          onDragOver={(event) => { if (!locked && !active && !waiting && event.dataTransfer.types.includes('Files')) event.preventDefault() }}
          onDrop={(event) => {
            if (!event.dataTransfer.files.length) return
            event.preventDefault()
            if (!locked && !active && !waiting) void attachments.addFiles(Array.from(event.dataTransfer.files))
          }}
          noValidate
          onSubmit={(event) => {
            event.preventDefault()
            sendDraft()
          }}
        >
          {latest && (
            <RunStatus
              run={latest}
              disabled={locked}
              onStop={() => {
                void perform(single(`/agent-runs/${latest.id}/cancel`, {}))
              }}
              onRelease={() => {
                void perform(
                  single(`/agent-runs/${latest.id}/close-reconciliation`, {}),
                )
              }}
            />
          )}
          <div className="cc-agent-box">
            {suggestions.length > 0 && (
              <ul className="cc-agent-commands" aria-label="命令">
                {suggestions.map((item) => (
                  <li key={item.name}>
                    <button
                      type="button"
                      onMouseDown={(event) => event.preventDefault()}
                      onClick={() => {
                        setDraft(`/${item.name} `)
                        requestAnimationFrame(() => editor.current?.focus())
                      }}
                    >
                      <code>/{item.name}</code>
                      <span>{item.description}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
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
                onGenerate={() => sendDraft()}
                onLibrary={() => setPickingMaterial(true)}
                onFiles={(files) => { if (!locked && !active) void attachments.addFiles(files) }}
              />
            )}
            {!waiting && attachments.items.length > 0 && (
              <ul className="cc-agent-uploads" aria-label="上传的参考图">
                {attachments.items.map((item) => <li key={item.key}>
                  {item.preview && <img src={item.preview} alt={item.title} />}
                  <span>{item.title}{item.state === 'uploading' ? ' · 上传中' : ''}{item.error ? ` · ${item.error}` : ''}</span>
                  {item.state === 'failed' && <button type="button" disabled={locked || !!active} onClick={() => attachments.retry(item.key)}>重试</button>}
                  <button type="button" disabled={locked || !!active} onClick={() => attachments.remove(item.key)}>移除</button>
                </li>)}
              </ul>
            )}
            {!waiting && attachments.message && <p role="status">{attachments.message}</p>}
            {command && (
              <p className="cc-agent-hint" role="status">
                发送后会把较早的对话整理成交接摘要，最近两轮保持原样；可以接着写要特别保留的内容。
              </p>
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
                  <Picker
                    className="cc-agent-add"
                    label="添加"
                    value=""
                    options={addOptions}
                    minWidth={300}
                    disabled={locked || !!active}
                    action
                    onChange={add}
                  >
                    <Plus size={15} />
                  </Picker>
                  <input ref={uploadInput} type="file" accept="image/*" multiple hidden onChange={(event) => { const files = Array.from(event.target.files ?? []); event.target.value = ''; if (!locked && !active) void attachments.addFiles(files) }} />
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
              {!waiting && (
                <AgentContextRing
                  view={ring}
                  compactPercent={compactPercent}
                  compactDisabled={compactBlocked}
                  onCompact={() => sendCommand({ name: 'compact', args: '' }, true)}
                />
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
      )}
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
      {notice.dialog}
    </aside>
  )
}

function shortTime(value: string): string {
  const date = new Date(value)
  return date.toDateString() === new Date().toDateString()
    ? date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
    : date.toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' })
}

// What the creator sent: their words, with what they referenced as chips. A
// message still on its way is drawn the same, only fainter.
function UserMessage({
  parts,
  sending = false,
}: {
  parts: MessagePart[]
  sending?: boolean
}) {
  return (
    <div className="cc-agent-user" aria-busy={sending || undefined}>
      {parts.map((part, index) =>
        part.kind === 'command' ? (
          <span key={index}>
            <span className="cc-agent-command">/{part.command}</span>
            {part.text && ` ${part.text}`}
          </span>
        ) : part.kind === 'text' ? (
          <span key={index}>{part.text}</span>
        ) : (
          <span className="cc-agent-chip" key={index}>
            {part.title}
          </span>
        ),
      )}
      {sending && <span className="sr-only">发送中</span>}
    </div>
  )
}

// A followed run is still being read until its snapshot arrives; showing the
// plain answer meanwhile would swap it for the blocks a moment later.
function LegacyAnswer({
  message,
  followed,
}: {
  message: Message
  followed: boolean
}) {
  const view = useAgentRuns((store) => store.runs[message.run_id ?? ''])
  const words = message.body.blocks
    .map((block) => (block.type === 'text' ? (block.text ?? '') : ''))
    .join('')
  if ((followed && !view) || view?.blocks.length || !words) return null
  return <AgentMarkdown source={words} />
}

// 读取中的对话：一问一答的轮廓，位置和真内容一致，读到后原地替换。
function ThreadLoading() {
  return (
    <div className="cc-agent-loading" role="status">
      <span className="sr-only">正在读取对话</span>
      {[0, 1].map((index) => (
        <div className="cc-agent-loading-turn" key={index} aria-hidden="true">
          <span className="cc-agent-bone cc-agent-bone-ask" />
          <TurnLoading />
        </div>
      ))}
    </div>
  )
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
      <span
        className={`cc-agent-status-label${waiting ? '' : ' cc-agent-shimmer'}`}
      >
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
