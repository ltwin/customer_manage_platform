import { Link } from 'react-router-dom'
import { CalendarRange, Plus, X } from 'lucide-react'

import type { ScheduleSlotListItem } from '../../api/client'
import PlanningSummaryLink from '../../components/PlanningSummaryLink'
import EmptyState from '../../components/EmptyState'
import { useFocusTrap } from '../../components/useFocusTrap'
import {
  calendarProjectionTimeLabel,
  type CalendarDayModel,
  type CalendarProjection,
} from './model'
import type { CalendarLayoutMode } from './layoutMode'
import type { CalendarFilters } from './types'
import {
  calendarOrderStatusLabel,
  calendarShootTypeLabel,
  calendarSlotTitle,
  calendarSlotTypeLabel,
  formatCalendarDay,
  formatCalendarMoney,
} from './format'

export default function DayDetailPanel({
  day,
  mode,
  open,
  writesEnabled,
  filters,
  selectedSlotID,
  lastDeletedShoot,
  onClose,
  onCreate,
  onEdit,
  onDelete,
}: {
  day: CalendarDayModel | undefined
  mode: CalendarLayoutMode
  open: boolean
  writesEnabled: boolean
  filters: CalendarFilters
  selectedSlotID: string
  lastDeletedShoot: { slot: ScheduleSlotListItem; date: string } | null
  onClose(): void
  onCreate(): void
  onEdit(slot: ScheduleSlotListItem): void
  onDelete(slot: ScheduleSlotListItem): void
}) {
  const projections = (day?.projections ?? []).filter((projection) => filters[projection.slot.type])
  const active = projections.filter((projection) => !projection.cancelled)
  const cancelled = projections.filter((projection) => projection.cancelled)
  const modal = mode === 'bottom-sheet'
  const dialogRef = useFocusTrap<HTMLElement>(modal && open, onClose)
  return (
    <aside
      ref={dialogRef}
      className={`calendar-v2-detail${open ? ' open' : ''}`}
      data-layout-mode={mode}
      role={modal ? 'dialog' : undefined}
      aria-modal={modal ? true : undefined}
      aria-labelledby="calendarV2DetailTitle"
      aria-hidden={!open}
      tabIndex={modal ? -1 : undefined}
    >
      <div className="calendar-v2-detail-head">
        <div>
          <span>当日详情</span>
          <h2 id="calendarV2DetailTitle">{formatCalendarDay(day?.date ?? '')}</h2>
        </div>
        <button className="icon-btn" type="button" onClick={onClose} aria-label="关闭当日详情"><X aria-hidden="true" /></button>
      </div>
      <div className="calendar-v2-detail-body">
        {lastDeletedShoot?.slot.type === 'shoot' && lastDeletedShoot.date === day?.date && (
          <div className="schedule-preview-clear">
            档期已删除，订单仍保留。
            <Link to={`/customers/${lastDeletedShoot.slot.customer_id}?tab=orders&order=${lastDeletedShoot.slot.order_id}`}>查看订单</Link>
          </div>
        )}
        {active.length === 0 && cancelled.length === 0 ? (
          <EmptyState icon={CalendarRange} title="这天暂无档期" hint="可以直接在当天新建拍摄、预留或个人占用" inline />
        ) : (
          <>
            {active.map((projection) => (
              <SlotDetail
                projection={projection}
                timeLabel={day ? calendarProjectionTimeLabel(projection, day.timeline) : undefined}
                turnaround={day?.tightTurnarounds.find((item) => item.currentSlotID === projection.slot.id)}
                turnaroundThresholdMinutes={day?.turnaroundThresholdMinutes ?? null}
                previousProjection={day?.projections.find((item) =>
                  item.slot.id === day.tightTurnarounds.find((turnaround) => turnaround.currentSlotID === projection.slot.id)?.previousSlotID)}
                selected={projection.slot.id === selectedSlotID}
                writesEnabled={writesEnabled}
                key={projection.slot.id}
                onEdit={onEdit}
                onDelete={onDelete}
              />
            ))}
            {cancelled.length > 0 && (
              <details
                className="calendar-v2-cancelled-details"
                open={cancelled.some((projection) => projection.slot.id === selectedSlotID) || undefined}
              >
                <summary>{cancelled.length} 条已取消档期（不占可约时间）</summary>
                {cancelled.map((projection) => (
                  <SlotDetail
                    projection={projection}
                    timeLabel={day ? calendarProjectionTimeLabel(projection, day.timeline) : undefined}
                    selected={projection.slot.id === selectedSlotID}
                    writesEnabled={writesEnabled}
                    key={projection.slot.id}
                    onEdit={onEdit}
                    onDelete={onDelete}
                  />
                ))}
              </details>
            )}
          </>
        )}
      </div>
      <div className="calendar-v2-detail-foot">
        <button className="btn btn-primary" type="button" disabled={!writesEnabled} onClick={onCreate}>
          <Plus aria-hidden="true" />
          在这天加档期
        </button>
      </div>
    </aside>
  )
}

function SlotDetail({
  projection,
  timeLabel,
  turnaround,
  turnaroundThresholdMinutes,
  previousProjection,
  selected,
  writesEnabled,
  onEdit,
  onDelete,
}: {
  projection: CalendarProjection
  timeLabel?: string
  turnaround?: CalendarDayModel['tightTurnarounds'][number]
  turnaroundThresholdMinutes?: number | null
  previousProjection?: CalendarProjection
  selected: boolean
  writesEnabled: boolean
  onEdit(slot: ScheduleSlotListItem): void
  onDelete(slot: ScheduleSlotListItem): void
}) {
  const slot = projection.slot
  return (
    <article
      className={`calendar-v2-slot-detail${selected ? ' selected' : ''}${projection.cancelled ? ' cancelled' : ''}`}
      data-slot-id={slot.id}
      tabIndex={-1}
    >
      <div className="calendar-v2-slot-head">
        <span className={`calendar-v2-filter-dot slot-${slot.type}`} aria-hidden="true" />
        <span className="badge badge-muted">{calendarSlotTypeLabel(slot.type)}</span>
        <time>{timeLabel ?? (projection.allDay ? '全天' : `${projection.displayStart}–${projection.displayEnd}`)}</time>
      </div>
      <h3>{calendarSlotTitle(slot)}</h3>
      {slot.type === 'shoot' && (
        <div className="calendar-v2-shoot-meta">
          <span>{slot.package_name ?? '未选套系'} · {calendarShootTypeLabel(slot.package_shoot_type)}</span>
          <span>{calendarOrderStatusLabel(slot.order_status)} · {formatCalendarMoney(slot.order_price)}</span>
          <span>定金 {slot.order_deposit_paid ? '已收' : '未收'} · 尾款 {slot.order_balance_paid ? '已收' : '未收'}</span>
          {slot.customer_status === 'archived' && <span className="warning-text">客户已归档</span>}
          {slot.order_status === 'cancelled' && <span className="danger-text">订单已取消，档期保留但不占可约时间</span>}
        </div>
      )}
      {slot.type === 'shoot' && <PlanningSummaryLink summary={slot.planning_summary} />}
      {slot.note && <p>{slot.note}</p>}
      {projection.conflicting && <div className="calendar-v2-conflict">时间重叠 · 保存仍允许，请人工确认</div>}
      {turnaround && turnaroundThresholdMinutes !== null && (
        <div className="calendar-v2-turnaround">
          转场提醒 · 与上一场“{previousProjection ? calendarSlotTitle(previousProjection.slot) : '档期'}”间隔
          {' '}{formatGapMinutes(turnaround.gapMinutes)}，低于设置的 {turnaroundThresholdMinutes} 分钟缓冲
        </div>
      )}
      <div className="calendar-v2-slot-actions">
        {slot.type === 'shoot' && (
          <Link className="btn btn-sm" to={`/customers/${slot.customer_id}?tab=orders&order=${slot.order_id}`}>查看订单</Link>
        )}
        <button className="btn btn-sm" type="button" disabled={!writesEnabled} onClick={() => onEdit(slot)}>编辑</button>
        <button className="btn btn-sm btn-danger-ghost" type="button" onClick={() => onDelete(slot)}>删除</button>
      </div>
    </article>
  )
}

function formatGapMinutes(value: number): string {
  return `${Number.isInteger(value) ? value : Math.round(value * 10) / 10} 分钟`
}
