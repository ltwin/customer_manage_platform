import { CornerDownRight, X } from 'lucide-react'
import type { MessagePart } from './agentOutgoing.ts'
import type { QueuedMessage } from './agentQueue.ts'

// 运行中写下、等这一轮结束再发的消息。每条可以插进正在进行的这一轮，或删掉；
// 本轮没有正常结束时整列暂停，点「继续发送」才往下发。
export function AgentQueue({
  items,
  paused,
  locked,
  draftOccupied,
  steerReason,
  onSteer,
  onRemove,
  onResume,
  onRestore,
}: {
  items: QueuedMessage[]
  paused: boolean
  locked: boolean
  draftOccupied: boolean
  // null：没有可以插进去的运行，不出现插队；空串：可以插队；其余是不能插队的原因。
  steerReason: (item: QueuedMessage) => string | null
  onSteer: (item: QueuedMessage) => void
  onRemove: (id: string) => void
  onResume: () => void
  onRestore: (item: QueuedMessage) => void
}) {
  return (
    <section className="cc-agent-queue" aria-label="排队的消息">
      {paused && (
        <div className="cc-agent-queue-paused" role="status">
          <span>已暂停，不会自动发出</span>
          {!items[0]?.sceneUnavailable && (
            <button type="button" disabled={locked} onClick={onResume}>
              继续发送
            </button>
          )}
        </div>
      )}
      <ol>
        {items.map((item) => {
          const reason = item.sending ? null : steerReason(item)
          return (
            <li key={item.id} aria-busy={item.sending || undefined}>
              <span className="cc-agent-queue-text">
                <QueuedText parts={item.parts} />
              </span>
              {item.sending ? (
                <span className="cc-agent-queue-state">发送中</span>
              ) : (
                <>
                  {item.sceneUnavailable ? (
                    <button
                      type="button"
                      disabled={locked}
                      aria-disabled={draftOccupied || undefined}
                      title={draftOccupied ? '先处理输入框里正在写的消息' : '移回输入框更换场景引用'}
                      onClick={() => onRestore(item)}
                    >
                      重新选择引用
                    </button>
                  ) : reason !== null && (
                    // 不能插队时仍可聚焦、可悬停，原因写在提示里。
                    <button
                      type="button"
                      className="cc-agent-queue-steer"
                      aria-disabled={!!reason || undefined}
                      title={reason || '插进正在进行的这一轮'}
                      onClick={() => {
                        if (!reason) onSteer(item)
                      }}
                    >
                      <CornerDownRight size={13} />
                      插队
                    </button>
                  )}
                  <button
                    type="button"
                    className="cc-agent-queue-remove"
                    aria-label="删除这条"
                    title="删除"
                    disabled={locked}
                    onClick={() => onRemove(item.id)}
                  >
                    <X size={13} />
                  </button>
                </>
              )}
            </li>
          )
        })}
      </ol>
    </section>
  )
}

function QueuedText({ parts }: { parts: MessagePart[] }) {
  return parts.map((part, index) =>
    part.kind === 'text' ? (
      <span key={index}>{part.text}</span>
    ) : part.kind === 'chip' ? (
      <span className="cc-agent-chip" key={index}>
        {part.title}
      </span>
    ) : part.kind === 'command' ? (
      <span key={index}>/{part.command}</span>
    ) : null,
  )
}
