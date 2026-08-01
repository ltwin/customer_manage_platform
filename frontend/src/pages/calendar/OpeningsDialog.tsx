import { useEffect, useRef, useState } from 'react'
import { Copy, X } from 'lucide-react'

import StateNotice from '../../components/StateNotice'
import { useFocusTrap } from '../../components/useFocusTrap'
import type { Opening } from './model'
import { groupOpeningsByDate, openingsText } from './openings'

export default function OpeningsDialog({
  open,
  openings,
  loading,
  error,
  onClose,
  onRetry,
  onCopy,
}: {
  open: boolean
  openings: Opening[]
  loading: boolean
  error: string | null
  onClose(): void
  onRetry(): void
  onCopy(text: string): Promise<void> | void
}) {
  const [draft, setDraft] = useState('')
  const [draftDirty, setDraftDirty] = useState(false)
  const wasOpenRef = useRef(false)
  const dialogRef = useFocusTrap<HTMLElement>(open, onClose, !loading)
  useEffect(() => {
    if (!open) {
      wasOpenRef.current = false
      setDraftDirty(false)
      return
    }
    if (!wasOpenRef.current) {
      wasOpenRef.current = true
      setDraftDirty(false)
      setDraft(openingsText(openings))
      return
    }
    if (!draftDirty) setDraft(openingsText(openings))
  }, [draftDirty, open, openings])
  const openingDays = groupOpeningsByDate(openings)
  if (!open) return null
  return (
    <div className="calendar-v2-dialog-overlay" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
      <section ref={dialogRef} className="calendar-v2-openings-dialog" role="dialog" aria-modal="true" aria-labelledby="calendarOpeningsTitle" tabIndex={-1}>
        <div className="calendar-v2-dialog-head">
          <div>
            <span>从明天起未来 14 天</span>
            <h2 id="calendarOpeningsTitle">可约空档</h2>
          </div>
          <button className="icon-btn" type="button" onClick={onClose} aria-label="关闭空档弹层"><X aria-hidden="true" /></button>
        </div>
        <div className="calendar-v2-dialog-body">
          {loading && <StateNotice kind="loading" message="正在计算未来空档" />}
          {error && <StateNotice kind="error" message={error} retryable onRetry={onRetry} />}
          {!loading && !error && openings.length === 0 && <StateNotice kind="empty" message="当前设置下未来 14 天没有达到最小时长的空档" />}
          {!loading && !error && openings.length > 0 && (
            <>
              <ol className="calendar-v2-opening-list">
                {openingDays.map((day) => (
                  <li key={day.date}>
                    <b>{day.date}</b>
                    <span>{day.openings.map((opening) => `${opening.start}–${opening.end}`).join('、')}</span>
                  </li>
                ))}
              </ol>
              <label className="calendar-v2-copy-draft">
                <span>复制前可编辑（文案取前 5 天）</span>
                <textarea rows={7} value={draft} onChange={(event) => { setDraft(event.target.value); setDraftDirty(true) }} />
              </label>
            </>
          )}
        </div>
        <div className="calendar-v2-dialog-foot">
          <button className="btn" type="button" onClick={onClose}>关闭</button>
          <button className="btn btn-primary" type="button" disabled={!draft.trim() || loading || Boolean(error)} onClick={() => { void onCopy(draft) }}>
            <Copy aria-hidden="true" />
            复制文案
          </button>
        </div>
      </section>
    </div>
  )
}
