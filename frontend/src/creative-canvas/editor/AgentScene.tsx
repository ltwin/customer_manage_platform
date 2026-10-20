import { useCallback, useContext, useEffect, useState } from 'react'
import { Box, X } from 'lucide-react'
import {
  commandScene,
  readRunChanges,
  type RestoreBatch,
  type RunChanges,
} from '../../creative-scene/api.ts'
import { text, type AgentBlock } from './agentBlocks.ts'
import { AgentTurnContext } from './agentRuns.ts'
import {
  blockedText,
  changeLabels,
  documentOf,
  finishedDetail,
  outcomeText,
  restorePayload,
  sceneWriteTools,
  selectionLabel,
  screenshotRevision,
  submitBatches,
} from './agentScene.ts'
import { mediaTicket } from './media.ts'

// The objects selected in the scene editor: above the input while they will
// go with the message, and on the message once sent.
export function SceneSelectionTag({ names }: { names: string[] }) {
  const { label, detail } = selectionLabel(names)
  return (
    <span className="cc-agent-scene-tag" title={`选中的物体：${detail}`}>
      <Box size={13} aria-hidden="true" />
      <span className="sr-only">选中的物体：</span>
      <span>{label}</span>
    </span>
  )
}

// A render_view screenshot under its tool row. Screenshots stay staged and
// have a 1024 long side, under the display rendition's threshold: the
// original is what there is to show.
export function SceneShot({ block }: { block: AgentBlock }) {
  const [url, setURL] = useState<string | null>(null)
  const [missing, setMissing] = useState(false)
  const [open, setOpen] = useState(false)
  useEffect(() => {
    let live = true
    finishedDetail(block)
      .then((detail) => {
        const revision = screenshotRevision(detail)
        if (!revision) throw new Error('没有截图')
        return mediaTicket(revision, 'original', 'display')
      })
      .then(
        (ticket) => live && setURL(ticket.url),
        () => live && setMissing(true),
      )
    return () => {
      live = false
    }
  }, [block])
  useEffect(() => {
    if (!open) return
    const close = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    window.addEventListener('keydown', close)
    return () => window.removeEventListener('keydown', close)
  }, [open])
  if (missing) return <p className="cc-agent-peek">截图已不可用</p>
  if (!url) return null
  return (
    <>
      <button
        type="button"
        className="cc-agent-shot"
        aria-label="查看截图"
        onClick={() => setOpen(true)}
      >
        <img src={url} alt="助手的截图" onError={() => setMissing(true)} />
      </button>
      {open && (
        <div
          className="cc-agent-shot-view"
          role="dialog"
          aria-label="截图"
          onClick={() => setOpen(false)}
        >
          <img src={url} alt="助手的截图" />
          <button
            type="button"
            aria-label="关闭"
            onClick={() => setOpen(false)}
          >
            <X size={18} />
          </button>
        </div>
      )}
    </>
  )
}

// What a finished run changed in each scene it wrote, with 「恢复这一处」
// and 「撤销本轮」. The scenes come from the write tools' arguments.
export function SceneRunChanges({
  runID,
  blocks,
}: {
  runID: string
  blocks: readonly AgentBlock[]
}) {
  const writes = blocks.filter(
    (b) =>
      b.type === 'tool' &&
      b.state !== 'running' &&
      sceneWriteTools.has(text(b.payload.tool_key)),
  )
  const key = writes.map((b) => b.id).join(',')
  const [documents, setDocuments] = useState<string[]>([])
  useEffect(() => {
    let live = true
    void Promise.allSettled(writes.map((b) => finishedDetail(b))).then(
      (results) => {
        const ids = results.flatMap((r) => {
          const id = r.status === 'fulfilled' ? documentOf(r.value) : null
          return id ? [id] : []
        })
        if (live) setDocuments([...new Set(ids)])
      },
    )
    return () => {
      live = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- key stands for the write blocks
  }, [key])
  return documents.map((id) => (
    <SceneChangeList key={id} runID={runID} documentID={id} />
  ))
}

function SceneChangeList({
  runID,
  documentID,
}: {
  runID: string
  documentID: string
}) {
  const { canvas, account } = useContext(AgentTurnContext)
  const [changes, setChanges] = useState<RunChanges | null>(null)
  const [failed, setFailed] = useState(false)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  // Revisions this list wrote: what they block reads as put back, not as someone's edit.
  const [restored, setRestored] = useState<ReadonlySet<string>>(new Set())
  const load = useCallback(
    () =>
      readRunChanges(documentID, runID).then(
        (next) => {
          setChanges(next)
          setFailed(false)
        },
        () => setFailed(true),
      ),
    [documentID, runID],
  )
  useEffect(() => {
    void load()
  }, [load])
  const title =
    canvas?.nodes.find((n) => n.data.document_id === documentID)?.metadata
      .title || '场景'
  if (failed && !changes)
    return <p className="cc-agent-note">「{title}」的本轮改动暂时读不出来</p>
  if (!changes?.revisions.length || !changes.entries.length) return null

  const restore = async (batches: readonly RestoreBatch[], name?: string) => {
    if (!account || busy) return
    setBusy(true)
    setNotice('')
    const outcome = await submitBatches(batches, (batch) =>
      commandScene(
        account,
        documentID,
        crypto.randomUUID(),
        new Date().toISOString(),
        restorePayload(batch),
      ),
    )
    setRestored((prev) => new Set([...prev, ...outcome.written]))
    setNotice(outcomeText(outcome, name))
    await load()
    setBusy(false)
  }

  return (
    <section
      className="cc-agent-scene-changes"
      aria-label={`本轮对「${title}」的改动`}
    >
      <header>
        <span>本轮改动 · {title}</span>
        <button
          type="button"
          disabled={!account || busy || !changes.undo_all.length}
          onClick={() => void restore(changes.undo_all)}
        >
          撤销本轮
        </button>
      </header>
      <ul>
        {changes.entries.map((entry) => {
          const id = entry.target.object_id ?? entry.target.unit ?? ''
          const back =
            entry.blocked?.reason === 'changed_by_others' &&
            restored.has(entry.blocked.revision)
          return (
            <li key={id}>
              <span className="cc-agent-scene-name">
                {entry.name || '未命名'}
              </span>
              <span className="cc-agent-scene-change">
                {changeLabels[entry.change]}
              </span>
              {entry.restore ? (
                <button
                  type="button"
                  disabled={!account || busy}
                  onClick={() =>
                    entry.restore &&
                    void restore([entry.restore], entry.name || '未命名')
                  }
                >
                  恢复这一处
                </button>
              ) : back ? (
                <span className="cc-agent-scene-blocked">已恢复</span>
              ) : (
                entry.blocked && (
                  <span className="cc-agent-scene-blocked">
                    不可恢复：{blockedText(entry.blocked)}
                  </span>
                )
              )}
            </li>
          )
        })}
      </ul>
      {notice && (
        <p className="cc-agent-note" role="status">
          {notice}
        </p>
      )}
    </section>
  )
}
