import type { SaveStatus } from '../state/sync.ts'

const saveText: Record<SaveStatus, string> = {
  loading: '正在读取',
  saved: '已保存',
  saving: '保存中',
  offline: '离线，修改尚未保存',
  conflict: '有冲突待处理',
  readonly: '只读',
  missing: '场景已不存在',
}

export function StatusBar({ hint, status, notice, device, activity = '' }: { hint: string; status: SaveStatus; notice: string; device: string; activity?: string }) {
  return (
    <footer className="cc-scene-status" aria-live="polite">
      <span className="cc-scene-hint">{notice || hint}</span>
      <span className="cc-scene-device">{device}</span>
      {activity && <span className="cc-scene-activity">{activity}</span>}
      <span className={`cc-scene-save is-${status}`}>{saveText[status]}</span>
    </footer>
  )
}
