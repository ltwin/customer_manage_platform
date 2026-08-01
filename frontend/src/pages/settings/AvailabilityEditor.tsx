import type { ScheduleAvailabilityWindow } from '../../api/client'
import {
  ISO_WEEKDAYS,
  type AvailabilitySettingsDraft,
} from './availabilityDraft'
import './settings.css'

const weekdayLabels: Record<(typeof ISO_WEEKDAYS)[number], string> = {
  1: '周一',
  2: '周二',
  3: '周三',
  4: '周四',
  5: '周五',
  6: '周六',
  7: '周日',
}

export default function AvailabilityEditor({
  draft,
  disabled,
  onChange,
}: {
  draft: AvailabilitySettingsDraft
  disabled: boolean
  onChange(draft: AvailabilitySettingsDraft): void
}) {
  function toggleDay(day: (typeof ISO_WEEKDAYS)[number], enabled: boolean) {
    onChange({
      ...draft,
      weekly: {
        ...draft.weekly,
        [day]: enabled ? defaultWindow(day) : null,
      },
    })
  }

  function changeWindow(
    day: (typeof ISO_WEEKDAYS)[number],
    field: keyof ScheduleAvailabilityWindow,
    value: string,
  ) {
    const current = draft.weekly[day]
    if (!current) return
    onChange({
      ...draft,
      weekly: {
        ...draft.weekly,
        [day]: { ...current, [field]: value },
      },
    })
  }

  return (
    <section className="availability-settings" aria-labelledby="availabilitySettingsTitle">
      <div className="availability-settings-head">
        <div>
          <h2 id="availabilitySettingsTitle">可约时段</h2>
          <p className="sub">按账号时区设置每周单段工作窗口；关闭的日期不会生成可约空档。</p>
        </div>
      </div>

      <fieldset className="availability-week">
        <legend>每周工作窗口</legend>
        {ISO_WEEKDAYS.map((day) => {
          const window = draft.weekly[day]
          return (
            <div className="availability-day" key={day} data-enabled={Boolean(window)}>
              <label className="availability-day-toggle">
                <input
                  type="checkbox"
                  checked={Boolean(window)}
                  disabled={disabled}
                  onChange={(event) => toggleDay(day, event.target.checked)}
                />
                <span>{weekdayLabels[day]}</span>
              </label>
              <div className="availability-day-times">
                <label>
                  <span>开始</span>
                  <input
                    type="time"
                    step={300}
                    value={window?.start ?? ''}
                    disabled={disabled || !window}
                    data-availability-field={`weekly.${day}.start`}
                    onChange={(event) => changeWindow(day, 'start', event.target.value)}
                  />
                </label>
                <span aria-hidden="true">—</span>
                <label>
                  <span>结束</span>
                  <input
                    type="time"
                    step={300}
                    value={window?.end ?? ''}
                    disabled={disabled || !window}
                    data-availability-field={`weekly.${day}.end`}
                    onChange={(event) => changeWindow(day, 'end', event.target.value)}
                  />
                </label>
              </div>
            </div>
          )
        })}
      </fieldset>

      <div className="availability-thresholds">
        <label>
          最小可报空档（分钟）
          <input
            type="number"
            min={15}
            max={480}
            step={1}
            value={draft.minOpeningMinutes}
            disabled={disabled}
            data-availability-field="minOpeningMinutes"
            onChange={(event) => onChange({
              ...draft,
              minOpeningMinutes: Number(event.target.value),
            })}
          />
          <span className="sub">15–480 分钟；更短的碎片不会对外展示。</span>
        </label>
        <label>
          转场缓冲（分钟）
          <input
            type="number"
            min={0}
            max={240}
            step={1}
            value={draft.turnaroundMinutes}
            disabled={disabled}
            data-availability-field="turnaroundMinutes"
            onChange={(event) => onChange({
              ...draft,
              turnaroundMinutes: Number(event.target.value),
            })}
          />
          <span className="sub">0–240 分钟；不足时只提醒，不阻止保存档期。</span>
        </label>
      </div>
    </section>
  )
}

function defaultWindow(day: (typeof ISO_WEEKDAYS)[number]): ScheduleAvailabilityWindow {
  return day >= 6
    ? { start: '09:00', end: '20:00' }
    : { start: '10:00', end: '19:00' }
}
