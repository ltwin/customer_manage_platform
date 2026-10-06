import type { Job } from './journal.ts'
import type { Intent } from './outbox.ts'

export type SaveStatus =
  | 'idle'
  | 'saving'
  | 'saved'
  | 'offline'
  | 'conflict'
  | 'rejected'
  | 'unconfirmed'

export const saveStatusText: Record<Exclude<SaveStatus, 'idle'>, string> = {
  saving: '保存中',
  saved: '已保存',
  offline: '离线 · 修改保留在本机',
  conflict: '保存冲突 · 待处理',
  rejected: '保存被拒绝',
  unconfirmed: '保存待确认',
}

// 画布的保存状态只由本机 outbox、单通道 job 和网络推出，不进撤销栈。
// 「已保存」只在本会话发出的修改全部拿到服务端回执后出现，不提前显示成功。
export function saveStatus({
  canvasID,
  outbox,
  job,
  busy,
  online,
  comparing,
  confirmedOnce,
}: {
  canvasID: string
  outbox: Intent[]
  job: Job | null | undefined
  // 单通道请求正在发送中。
  busy: boolean
  online: boolean
  // 另一窗口改过同一节点，正在对比两份内容。
  comparing: boolean
  // 本会话至少有一次画布修改得到服务端确认。
  confirmedOnce: boolean
}): SaveStatus {
  const waiting = outbox.some(
    (i) => i.canvasID === canvasID && i.state !== 'confirmed',
  )
  const canvasJob =
    job && job.path === `/canvases/${canvasID}/commands` ? job : null
  if (comparing || canvasJob?.conflict) return 'conflict'
  if (!online) return waiting || canvasJob ? 'offline' : 'idle'
  if (canvasJob?.state === 'rejected') return 'rejected'
  if (canvasJob?.state === 'unknown' && !busy) return 'unconfirmed'
  if (waiting || canvasJob) return 'saving'
  return confirmedOnce ? 'saved' : 'idle'
}
