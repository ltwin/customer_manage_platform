// dashboard-v2 消费模型：把 GET /dashboard/v2 的聚合读模型整理为页面展示结构。
// 类型一律来自契约 codegen（src/api/schema.d.ts 经 client.ts），禁手写 DTO。
import type {
  CustomerChannel,
  HealthTiers,
  DashboardV2,
  DashboardV2DeliveryItem,
  DashboardV2Waterfall,
  ScheduleAvailability,
  ScheduleSlotListItem,
} from '../../api/client.ts'
import { buildCalendarModel } from '../calendar/model.ts'
import type { Opening } from '../calendar/model.ts'

export const channelLabels: Record<CustomerChannel, string> = {
  xiaohongshu: '小红书',
  douyin: '抖音',
  weibo: '微博',
  referral: '转介绍',
  other: '其他',
}

const shootTypeLabels: Record<string, string> = {
  portrait: '写真',
  cosplay: 'Cosplay',
  other: '其他',
  unattributed: '未归因',
}

/** 交付队列阶段标签：status → 后期阶段（与 /dashboard/v2 delivery_queue 契约一致） */
export const deliveryStageLabels: Record<'shot' | 'selected' | 'retouching', string> = {
  shot: '待选片',
  selected: '待精修',
  retouching: '精修中',
}

export function formatYuan(cents: number | null | undefined): string {
  if (cents == null) return '—'
  return `¥${Math.round(cents / 100).toLocaleString('zh-CN')}`
}

export function formatShortYuan(cents: number): string {
  const yuan = Math.round(cents / 100)
  return yuan >= 10000 ? `¥${(yuan / 10000).toFixed(1)}万` : `¥${yuan.toLocaleString('zh-CN')}`
}

export function formatPercent(ratio: number | null | undefined, digits = 2): string {
  if (ratio == null) return '—'
  const percent = ratio * 100
  const rounded = Number(percent.toFixed(digits))
  return `${rounded}%`
}

function localCalendarFields(
  iso: string,
  timezone: string | null,
): { daySerial: number; hour: number; minute: number } {
  const options: Intl.DateTimeFormatOptions = {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }
  if (timezone) options.timeZone = timezone
  let parts: Intl.DateTimeFormatPart[]
  try {
    parts = new Intl.DateTimeFormat('en-CA', options).formatToParts(new Date(iso))
  } catch {
    const { timeZone: _ignored, ...rest } = options
    parts = new Intl.DateTimeFormat('en-CA', rest).formatToParts(new Date(iso))
  }
  const valueOf = (type: string) => Number(parts.find((part) => part.type === type)?.value ?? '0')
  const year = valueOf('year')
  const month = valueOf('month')
  const day = valueOf('day')
  const daySerial = Math.round(Date.UTC(year, month - 1, day) / 86400000)
  return { daySerial, hour: valueOf('hour'), minute: valueOf('minute') }
}

function dateOnlySerial(date: string): number {
  // 必须与 localCalendarFields 的 Date.UTC 基准一致（UTC 零点）；用 12:00Z 锚会在
  // Math.round 的 .5 进位下与次日 serial 碰撞，跨日分钟被折回 0。
  return Math.round(Date.parse(`${date}T00:00:00Z`) / 86400000)
}

/**
 * 时刻在账号时区的本地分钟。baseDate 缺省时以该时刻所在本地日为基准（0..1439）；
 * 给定 baseDate（date-only）时返回相对该日的分钟数，跨日槽位可为负或 ≥1440。
 */
export function localMinutesOfInstant(iso: string, timezone: string | null, baseDate?: string): number {
  const { daySerial, hour, minute } = localCalendarFields(iso, timezone)
  const anchor = baseDate ? dateOnlySerial(baseDate) : daySerial
  return (daySerial - anchor) * 1440 + hour * 60 + minute
}

export function localDateOfInstant(iso: string, timezone: string | null): string {
  const options: Intl.DateTimeFormatOptions = { year: 'numeric', month: '2-digit', day: '2-digit' }
  if (timezone) options.timeZone = timezone
  try {
    // en-CA 产出 YYYY-MM-DD，便于与其他 date-only 字符串直接比较
    return new Intl.DateTimeFormat('en-CA', options).format(new Date(iso))
  } catch {
    return new Intl.DateTimeFormat('en-CA', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    }).format(new Date(iso))
  }
}

function hhmmToMinutes(value: string): number {
  const [hour, minute] = value.split(':').map(Number)
  return (hour ?? 0) * 60 + (minute ?? 0)
}

export function minutesOfHHMM(value: string): number {
  return hhmmToMinutes(value)
}

export function minutesToHHMM(minutes: number): string {
  const clamped = Math.max(0, Math.min(24 * 60, minutes))
  const hour = Math.floor(clamped / 60)
  const minute = clamped % 60
  return `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`
}

function daysBetweenDateOnly(from: string, to: string): number {
  return Math.round(
    (Date.parse(`${to}T12:00:00Z`) - Date.parse(`${from}T12:00:00Z`)) / 86400000,
  )
}

const weekdayLabels = ['日', '一', '二', '三', '四', '五', '六']

function weekdayOfDateOnly(date: string): string {
  return weekdayLabels[new Date(`${date}T12:00:00Z`).getUTCDay()] ?? ''
}

/* ================= L1 · 下一场拍摄焦点 ================= */

export interface V2FocusModel {
  state: 'today' | 'later' | 'none'
  slot: ScheduleSlotListItem | null
  customerName: string | null
  title: string | null
  note: string | null
  startLabel: string
  endLabel: string
  whenLabel: string
  countdownLabel: string | null
  durationMinutes: number
}

function slotDisplayTitle(slot: ScheduleSlotListItem): string {
  if (slot.type === 'shoot') {
    return slot.order_title ?? slot.package_name ?? '未命名订单'
  }
  return slot.note ?? (slot.type === 'hold' ? '预留' : '个人占用')
}

export function v2FocusModel(
  nextShoot: DashboardV2['next_shoot'],
  timezone: string | null,
  now: Date,
): V2FocusModel {
  if (!nextShoot) {
    return {
      state: 'none',
      slot: null,
      customerName: null,
      title: null,
      note: null,
      startLabel: '',
      endLabel: '',
      whenLabel: '',
      countdownLabel: null,
      durationMinutes: 0,
    }
  }
  const startLabel = minutesToHHMM(localMinutesOfInstant(nextShoot.start_at, timezone) % 1440)
  const endLabel = minutesToHHMM(localMinutesOfInstant(nextShoot.end_at, timezone) % 1440)
  const durationMinutes = Math.max(
    0,
    Math.round((Date.parse(nextShoot.end_at) - Date.parse(nextShoot.start_at)) / 60000),
  )
  const todayDate = localDateOfInstant(now.toISOString(), timezone)
  const startDate = localDateOfInstant(nextShoot.start_at, timezone)
  const state = startDate === todayDate ? 'today' : 'later'
  const nowMinutes = localMinutesOfInstant(now.toISOString(), timezone, todayDate)
  const startMinutes = localMinutesOfInstant(nextShoot.start_at, timezone, todayDate)
  const diffMinutes = startMinutes - nowMinutes
  let countdownLabel: string | null = null
  if (state === 'today') {
    countdownLabel = diffMinutes > 0
      ? diffMinutes >= 60
        ? `${Math.floor(diffMinutes / 60)} 小时 ${diffMinutes % 60} 分后`
        : `${diffMinutes} 分钟后`
      : '正在进行'
  }
  const month = Number(startDate.slice(5, 7))
  const day = Number(startDate.slice(8, 10))
  return {
    state,
    slot: nextShoot,
    customerName: nextShoot.type === 'shoot' ? nextShoot.customer_display_name : null,
    title: slotDisplayTitle(nextShoot),
    note: nextShoot.note ?? null,
    startLabel,
    endLabel,
    whenLabel: `${month}/${day}（周${weekdayOfDateOnly(startDate)}）`,
    countdownLabel,
    durationMinutes,
  }
}

/* ================= L1 · 今日时间轴 ================= */

export interface V2TimelineItem {
  id: string
  kind: ScheduleSlotListItem['type']
  startMin: number
  endMin: number
  title: string
  statusLabel: string | null
  orderId: string | null
  customerId: string | null
}

export interface V2TimelineModel {
  dayStart: number
  dayEnd: number
  items: V2TimelineItem[]
  openings: Array<{ start: string; end: string }>
  nowMin: number | null
}

const orderStatusTimelineLabels: Record<string, string> = {
  consulting: '咨询',
  scheduled: '待拍',
  shot: '已拍',
  selected: '已选',
  retouching: '精修',
  delivered: '已交付',
  closed: '已完成',
  cancelled: '已取消',
}

export function v2TimelineModel(
  todaySlots: DashboardV2['today_slots'],
  workingWindow: DashboardV2['today_openings']['working_window'],
  openings: DashboardV2['today_openings']['openings'],
  timezone: string | null,
  now: Date,
): V2TimelineModel {
  const todayDate = localDateOfInstant(now.toISOString(), timezone)
  const items: V2TimelineItem[] = todaySlots
    .map((slot) => ({
      id: slot.id,
      kind: slot.type,
      startMin: localMinutesOfInstant(slot.start_at, timezone, todayDate),
      endMin: localMinutesOfInstant(slot.end_at, timezone, todayDate),
      title: slot.type === 'shoot'
        ? `${slot.customer_display_name}${slot.order_title ? ` · ${slot.order_title}` : ''}`
        : slot.note ?? (slot.type === 'hold' ? '预留' : '个人占用'),
      statusLabel: slot.type === 'shoot'
        ? orderStatusTimelineLabels[slot.order_status] ?? slot.order_status
        : null,
      orderId: slot.type === 'shoot' ? slot.order_id : null,
      customerId: slot.type === 'shoot' ? slot.customer_id : null,
    }))
    .sort((a, b) => a.startMin - b.startMin)

  const itemStart = items.length > 0 ? Math.min(...items.map((item) => item.startMin)) : null
  const itemEnd = items.length > 0 ? Math.max(...items.map((item) => item.endMin)) : null
  const windowStart = workingWindow ? hhmmToMinutes(workingWindow.start) : null
  const windowEnd = workingWindow ? hhmmToMinutes(workingWindow.end) : null
  const dayStart = Math.max(0, Math.min(windowStart ?? 9 * 60, itemStart ?? windowStart ?? 9 * 60))
  const dayEnd = Math.min(24 * 60, Math.max(windowEnd ?? 21 * 60, itemEnd ?? windowEnd ?? 21 * 60))

  const nowMin = localMinutesOfInstant(now.toISOString(), timezone, todayDate)
  return {
    dayStart,
    dayEnd: Math.max(dayEnd, dayStart + 60),
    items,
    openings: openings.map((opening) => ({ start: opening.start, end: opening.end })),
    nowMin: nowMin >= dayStart && nowMin <= dayEnd ? nowMin : null,
  }
}

/* ================= L2 · 收入瀑布 ================= */

export interface V2WaterfallSegment {
  key: 'confirmed' | 'receivable' | 'pipeline'
  label: string
  valueCents: number
  percent: number
  note: string
}

export interface V2Kpi {
  label: string
  value: string
  note: string
}

export interface V2WaterfallModel {
  confirmedCents: number
  trendLabel: string | null
  trendUp: boolean | null
  segments: V2WaterfallSegment[]
  kpis: V2Kpi[]
}

export function v2WaterfallModel(waterfall: DashboardV2Waterfall): V2WaterfallModel {
  const confirmed = waterfall.confirmed.current
  const receivable = waterfall.receivable.total
  const pipeline = waterfall.pipeline.total
  const total = confirmed + receivable + pipeline

  let trendLabel: string | null
  let trendUp: boolean | null
  if (waterfall.confirmed.change_ratio == null) {
    trendLabel = '上期无数据'
    trendUp = null
  } else {
    const ratio = waterfall.confirmed.change_ratio
    trendUp = ratio > 0 ? true : ratio < 0 ? false : null
    const percent = Number((ratio * 100).toFixed(0))
    trendLabel = `${ratio > 0 ? '+' : ''}${percent}% 环比`
  }

  const segment = (
    key: V2WaterfallSegment['key'],
    label: string,
    valueCents: number,
    note: string,
  ): V2WaterfallSegment => ({
    key,
    label,
    valueCents,
    percent: total > 0 ? (valueCents / total) * 100 : 0,
    note,
  })

  const kpis: V2Kpi[] = [
    {
      label: '近 30 天已收现金',
      value: formatYuan(waterfall.cash_received_30d),
      note: '按 paid_at 落窗的 amount_paid 汇总',
    },
    {
      label: '客单价',
      value: waterfall.average_order_value == null ? '—' : formatYuan(waterfall.average_order_value),
      note: '当前窗已确认收入 ÷ 计入订单笔数',
    },
    {
      label: '90 天复购占比',
      value: formatPercent(waterfall.repeat_customer_ratio_90d),
      note: '近 90 天拍摄 ≥2 单客户占比',
    },
    {
      label: '待收账龄',
      value: waterfall.oldest_receivable_age_days == null
        ? '—'
        : `${waterfall.oldest_receivable_age_days} 天`,
      note: `最早未结清已交付订单至今 · 共 ${waterfall.receivable.count} 笔`,
    },
  ]

  return {
    confirmedCents: confirmed,
    trendLabel,
    trendUp,
    segments: [
      segment('confirmed', '已确认', confirmed, '近 30 天尾款结清收入'),
      segment('receivable', '待收尾款', receivable, `${waterfall.receivable.count} 笔已交付未结清`),
      segment('pipeline', '在途', pipeline, `${waterfall.pipeline.count} 笔已定档～精修中`),
    ],
    kpis,
  }
}

/* ================= L1.5 · 交付队列 ================= */

export interface V2DeliveryRowModel {
  orderId: string
  title: string
  customerName: string
  customerId: string | null
  stageLabel: string
  daysSinceShot: number | null
  dueDate: string | null
  dueLabel: string
  overdue: boolean
  progressPct: number | null
}

export function v2DeliveryRows(
  items: DashboardV2DeliveryItem[],
  slaDays: number,
  timezone: string | null,
  today: string,
): V2DeliveryRowModel[] {
  return items.map((item) => {
    const stage = deliveryStageLabels[item.order.status as keyof typeof deliveryStageLabels]
    const shotLocalDate = item.order.shot_at
      ? localDateOfInstant(item.order.shot_at, timezone)
      : null
    const daysSinceShot = shotLocalDate ? daysBetweenDateOnly(shotLocalDate, today) : null
    let dueLabel: string
    if (item.overdue && item.days_left != null) {
      dueLabel = `已逾期 ${-item.days_left} 天`
    } else if (item.days_left != null) {
      dueLabel = `剩 ${item.days_left} 天`
    } else {
      dueLabel = '无应交日'
    }
    const progressPct = item.overdue
      ? 100
      : daysSinceShot != null && slaDays > 0
        ? Math.min(100, Math.round((daysSinceShot / slaDays) * 100))
        : null
    return {
      orderId: item.order.id,
      title: item.order.title ?? item.order.package_name ?? '未命名订单',
      customerName: item.order.customer_display_name,
      customerId: item.order.customer_id ?? null,
      stageLabel: stage ?? item.order.status,
      daysSinceShot,
      dueDate: item.order.delivery_due_at ?? null,
      dueLabel,
      overdue: item.overdue,
      progressPct,
    }
  })
}

/* ================= L3 · 渠道矩阵 ================= */

export interface V2MatrixSegment {
  type: 'portrait' | 'cosplay' | 'other' | 'unattributed'
  label: string
  cents: number
  percent: number
}

export interface V2MatrixRowModel {
  channelLabel: string
  segments: V2MatrixSegment[]
  totalCents: number
  totalPercent: number
  orderCount: number
  customerCount: number
}

export interface V2MatrixModel {
  rows: V2MatrixRowModel[]
  grandTotalCents: number
  insight: string
}

export function v2MatrixModel(matrix: DashboardV2['channel_matrix']): V2MatrixModel {
  const rows: V2MatrixRowModel[] = matrix.rows.map((row) => ({
    channelLabel: channelLabels[row.channel] ?? row.channel,
    segments: (['portrait', 'cosplay', 'other', 'unattributed'] as const).map((type) => ({
      type,
      label: shootTypeLabels[type] ?? type,
      cents: row[type],
      percent: row.total > 0 ? (row[type] / row.total) * 100 : 0,
    })),
    totalCents: row.total,
    totalPercent: matrix.grand_total > 0 ? Math.round((row.total / matrix.grand_total) * 100) : 0,
    orderCount: row.order_count,
    customerCount: row.customer_count,
  }))
  const top = matrix.rows[0]
  if (!top || matrix.grand_total <= 0) {
    return { rows, grandTotalCents: matrix.grand_total, insight: '暂无已结清订单收入，交付并结清后按下单时归因展示。' }
  }
  const topTypes = (['portrait', 'cosplay', 'other', 'unattributed'] as const)
  const topType = topTypes.reduce((a, b) => (top[a] >= top[b] ? a : b))
  const topPercent = Math.round((top.total / matrix.grand_total) * 100)
  return {
    rows,
    grandTotalCents: matrix.grand_total,
    insight: `${channelLabels[top.channel] ?? top.channel} 贡献 ${topPercent}% 收入，${shootTypeLabels[topType] ?? topType} 占大头。`,
  }
}

/* ================= L2 · 未来可约空档（复用 Calendar 同源算法） ================= */

export function upcomingLocalDates(fromDate: string, days: number): string[] {
  const dates: string[] = []
  for (let index = 0; index < days; index += 1) {
    dates.push(
      new Date(Date.parse(`${fromDate}T12:00:00Z`) + index * 86400000)
        .toISOString()
        .slice(0, 10),
    )
  }
  return dates
}

export function computeUpcomingOpenings(
  slots: ScheduleSlotListItem[],
  fromDate: string,
  days: number,
  timezone: string,
  availability: ScheduleAvailability,
): Opening[] {
  const dates = upcomingLocalDates(fromDate, days)
  const model = buildCalendarModel(slots, dates, timezone, availability)
  return model.days.flatMap((day) => day.openings)
}

/* ================= L3 · 客户资产（健康度分层，customer-health-tiers） ================= */

export type V2HealthTierKey = 'active' | 'sleeping' | 'at_risk' | 'lost' | 'new'

export interface V2HealthTierMeta {
  key: V2HealthTierKey
  label: string
  tone: 'success' | 'accent' | 'warning' | 'danger' | 'muted'
  ctaLabel: string
  ctaKind: 'care' | 'winback' | 'first'
}

export const v2HealthTierOrder: readonly V2HealthTierKey[] = ['active', 'sleeping', 'at_risk', 'lost', 'new']

const HEALTH_TIER_META: Record<V2HealthTierKey, V2HealthTierMeta> = {
  active: { key: 'active', label: '活跃', tone: 'success', ctaLabel: '关怀', ctaKind: 'care' },
  sleeping: { key: 'sleeping', label: '沉睡', tone: 'accent', ctaLabel: '唤回文案', ctaKind: 'winback' },
  at_risk: { key: 'at_risk', label: '高危', tone: 'warning', ctaLabel: '唤回文案', ctaKind: 'winback' },
  lost: { key: 'lost', label: '已流失', tone: 'danger', ctaLabel: '最后唤回', ctaKind: 'winback' },
  new: { key: 'new', label: '新客', tone: 'muted', ctaLabel: '促成首单', ctaKind: 'first' },
}

export function v2HealthTierMeta(key: V2HealthTierKey): V2HealthTierMeta {
  return HEALTH_TIER_META[key]
}

// 层语境提示按当前生效阈值动态拼装——参数化后不得硬编码默认区间。
export function v2HealthTierHint(key: V2HealthTierKey, thresholds: HealthTiers): string {
  switch (key) {
    case 'active':
      return '在个人节奏内'
    case 'sleeping':
      return `超节奏 ${thresholds.sleeping_ratio}–${thresholds.at_risk_ratio} 倍`
    case 'at_risk':
      return `超节奏 ${thresholds.at_risk_ratio}–${thresholds.lost_ratio} 倍 · 按累计贡献排序`
    case 'lost':
      return `超节奏 ${thresholds.lost_ratio} 倍以上 · 按累计贡献排序`
    case 'new':
      return '尚无拍摄记录'
  }
}

export interface V2HealthRowModel {
  customerId: string
  displayName: string
  channelLabel: string
  createdDaysAgo: number
  sinceDays: number | null
  cadenceDays: number | null
  ratioText: string | null
  fallbackBaseline: boolean
  settledText: string
  unsettledText: string | null
  gaugePercent: number | null
}

// date-only 字符串自然日差（UTC 正午锚定避免跨时区日界漂移）。
function healthDaysBetween(fromDate: string, toDate: string): number {
  const from = Date.parse(`${fromDate}T12:00:00Z`)
  const to = Date.parse(`${toDate}T12:00:00Z`)
  return Math.round((to - from) / 86400000)
}

export function v2HealthRows(
  health: DashboardV2['customer_health'],
  tier: V2HealthTierKey,
  today: string,
): V2HealthRowModel[] {
  return health.tiers[tier].items.map((item) => ({
    customerId: item.customer_id,
    displayName: item.display_name,
    channelLabel: channelLabels[item.channel] ?? item.channel,
    createdDaysAgo: healthDaysBetween(item.created_at, today),
    sinceDays: item.since_days ?? null,
    cadenceDays: item.cadence_days ?? null,
    ratioText: item.ratio == null ? null : `${item.ratio}×`,
    fallbackBaseline: item.baseline === 'fallback',
    settledText: formatYuan(item.settled_ltv),
    unsettledText: item.unsettled_paid > 0 ? formatYuan(item.unsettled_paid) : null,
    gaugePercent: item.ratio == null
      ? null
      : Math.min(100, Math.round((item.ratio / health.thresholds.lost_ratio) * 100)),
  }))
}

export interface V2HealthCountModel {
  key: V2HealthTierKey
  count: number
  percent: number
}

export function v2HealthCounts(health: DashboardV2['customer_health']): V2HealthCountModel[] {
  return v2HealthTierOrder.map((key) => ({
    key,
    count: health.tiers[key].count,
    percent: health.total > 0 ? Math.round((health.tiers[key].count / health.total) * 100) : 0,
  }))
}

// 每层一个符合语境的触达动作；文案带具体事实（距上次拍摄/建档天数）——
// 健康度卡的价值就是把数字变成可直接粘贴的私域话术。
export function healthCopyText(kind: 'care' | 'winback' | 'first', row: V2HealthRowModel): string {
  const name = row.displayName
  if (kind === 'winback') {
    const gap = row.sinceDays != null ? `距上次拍摄已经 ${row.sinceDays} 天了，` : ''
    return `${name}，好久不见！${gap}翻到你那组照片还是好喜欢。最近想不想再来一组？老朋友我给你留优先档，想拍随时戳我～`
  }
  if (kind === 'first') {
    return `${name}，看到你 ${row.createdDaysAgo} 天前留了联系方式还没约上～这周刚好有档，要不要先约一组试试？第一次拍我多带些小道具～`
  }
  return `${name}，最近怎么样呀？翻相册看到给你拍的那组还是觉得好看，有空来唠嗑～`
}
