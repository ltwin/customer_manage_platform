import { useState } from 'react'
import { ApiError, mergeCustomer } from '../../api/client'
import type { Customer } from '../../api/client'

type Props = {
  targetId: string
  targetName: string
  onMerged: (target: Customer) => void
  onConflict: () => void
  onClose: () => void
  onUnauthorized: () => void
}

// merge 对话框（design D5 / 假设⑤）：收到 409 merge_conflict 先让父层重取
// 双方详情再提示——超时重试场景第一次可能已成功。
export default function MergeDialog({ targetId, targetName, onMerged, onConflict, onClose, onUnauthorized }: Props) {
  const [sourceId, setSourceId] = useState('')
  const [merging, setMerging] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit() {
    setError(null)
    if (!sourceId.trim()) {
      setError('请填写来源客户 ID')
      return
    }
    setMerging(true)
    try {
      const target = await mergeCustomer(targetId, sourceId.trim())
      onMerged(target)
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        onUnauthorized()
        return
      }
      if (err instanceof ApiError && err.code === 'merge_conflict') {
        onConflict()
        setError('合并冲突：双方必须都是经营中客户（若刚重试过，档案可能已合并成功，已为你刷新）')
        return
      }
      setError(err instanceof Error ? err.message : '合并失败')
    } finally {
      setMerging(false)
    }
  }

  return (
    <div className="overlay open" role="dialog" aria-modal="true" aria-label="合并客户">
      <section className="dialog">
        <h2>合并进「{targetName}」</h2>
        <p className="dialog-sub">
          来源客户的私域账号与备注会全部并入本档案，指向来源客户的转介绍关系
          会重定向到本客户；来源客户将标记为已合并、档案只读。此操作不可撤销。
        </p>
        {error && <div className="form-error">{error}</div>}
        <div className="field">
          <label htmlFor="merge-source-id">来源客户 ID *（将被合并的重复档案）</label>
          <input
            id="merge-source-id"
            className="input"
            value={sourceId}
            autoFocus
            onChange={(event) => setSourceId(event.target.value)}
          />
        </div>
        <div className="dialog-actions">
          <button className="btn" type="button" onClick={onClose} disabled={merging}>取消</button>
          <button className="btn btn-primary" type="button" onClick={() => { void submit() }} disabled={merging}>
            {merging ? '合并中…' : '确认合并'}
          </button>
        </div>
      </section>
    </div>
  )
}
