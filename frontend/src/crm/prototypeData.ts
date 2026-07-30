export const TODAY = '2026-07-06'

export const DICT = {
  channel: {
    xiaohongshu: '小红书',
    douyin: '抖音',
    weibo: '微博',
    referral: '转介绍',
    other: '其他',
  },
  platform: {
    wechat: '微信',
    qq: 'QQ',
    telegram: 'Telegram',
    xiaohongshu: '小红书',
    douyin: '抖音',
    weibo: '微博',
    other: '其他',
  },
  shootType: { portrait: '写真', cosplay: 'Cosplay', other: '其他' },
  pricingMode: { per_duration: '按时长', per_photo: '按张计价', fixed: '一口价' },
  orderStatus: {
    consulting: '咨询中',
    scheduled: '已定档',
    shot: '已拍摄',
    selected: '已选片',
    retouching: '精修中',
    delivered: '已交付',
    closed: '已完结',
    cancelled: '已取消',
  },
  slotType: { shoot: '拍摄', hold: '预留', busy: '占用' },
  reminderType: { birthday: '生日', follow_up: '拍后回访', churn: '流失预警', custom: '自定义' },
} as const

export type Channel = keyof typeof DICT.channel
export type Platform = keyof typeof DICT.platform
export type ShootType = keyof typeof DICT.shootType
export type PricingMode = keyof typeof DICT.pricingMode
export type OrderStatus = keyof typeof DICT.orderStatus
export type SlotType = keyof typeof DICT.slotType
export type ReminderType = keyof typeof DICT.reminderType
export type CustomerStatus = 'active' | 'merged' | 'archived'
export type PackageStatus = 'active' | 'archived'
export type ReminderStatus = 'pending' | 'done' | 'dismissed'

export interface SocialIdentity {
  platform: Platform
  handle: string
  remark: string
}

export interface CustomerNote {
  time: string
  content: string
}

export interface Customer {
  id: string
  display_name: string
  real_name: string
  phone: string
  birthday: string
  channel: Channel
  referrer_customer_id: string | null
  status: CustomerStatus
  avatar: string
  identities: SocialIdentity[]
  notes: CustomerNote[]
  created_at: string
}

export interface Package {
  id: string
  name: string
  shoot_type: ShootType
  pricing_mode: PricingMode
  base_price: number
  duration_minutes: number
  shot_count_min: number
  shot_count_max: number
  raw_delivery_count: number | null
  retouch_count: number
  status: PackageStatus
  note: string
}

export interface Order {
  id: string
  customer_id: string
  package_id: string
  title: string
  status: OrderStatus
  price: number | null
  deposit_paid: boolean
  balance_paid: boolean
  shot_at: string | null
  delivered_at: string | null
  created_at: string
}

export interface Slot {
  id: string
  date: string
  start: string
  end: string
  type: SlotType
  order_id: string | null
  note: string
}

export interface Reminder {
  id: string
  type: ReminderType
  customer_id: string | null
  order_id: string | null
  due_date: string
  content: string
  status: ReminderStatus
}

export const packages: Package[] = [
  {
    id: 'p1',
    name: '日系写真 · 基础',
    shoot_type: 'portrait',
    pricing_mode: 'per_duration',
    base_price: 68000,
    duration_minutes: 90,
    shot_count_min: 60,
    shot_count_max: 80,
    raw_delivery_count: null,
    retouch_count: 8,
    status: 'active',
    note: '底片全送，含 1 套服装造型',
  },
  {
    id: 'p2',
    name: '日系写真 · 双人进阶',
    shoot_type: 'portrait',
    pricing_mode: 'per_duration',
    base_price: 128000,
    duration_minutes: 150,
    shot_count_min: 100,
    shot_count_max: 140,
    raw_delivery_count: null,
    retouch_count: 15,
    status: 'active',
    note: '底片全送，含 2 套造型 + 外景转场',
  },
  {
    id: 'p3',
    name: 'Cosplay 棚拍 · 单人',
    shoot_type: 'cosplay',
    pricing_mode: 'fixed',
    base_price: 98000,
    duration_minutes: 120,
    shot_count_min: 80,
    shot_count_max: 120,
    raw_delivery_count: 40,
    retouch_count: 10,
    status: 'active',
    note: '含棚租与基础灯光，妆造自备',
  },
  {
    id: 'p4',
    name: 'Cosplay 外景联动',
    shoot_type: 'cosplay',
    pricing_mode: 'per_photo',
    base_price: 4500,
    duration_minutes: 180,
    shot_count_min: 30,
    shot_count_max: 60,
    raw_delivery_count: null,
    retouch_count: 0,
    status: 'active',
    note: '¥45/张起拍 30 张，精修按 ¥30/张另计',
  },
  {
    id: 'p5',
    name: '毕业季小团体（旧）',
    shoot_type: 'other',
    pricing_mode: 'fixed',
    base_price: 158000,
    duration_minutes: 120,
    shot_count_min: 120,
    shot_count_max: 160,
    raw_delivery_count: null,
    retouch_count: 12,
    status: 'archived',
    note: '2025 毕业季限定，已下架',
  },
]

export const customers: Customer[] = [
  {
    id: 'c1',
    display_name: '阿茶',
    real_name: '陈茉莉',
    phone: '138****2046',
    birthday: '03-14',
    channel: 'xiaohongshu',
    referrer_customer_id: null,
    status: 'active',
    avatar: 'a1',
    identities: [
      { platform: 'wechat', handle: 'tea_moli', remark: '主要联系方式' },
      { platform: 'xiaohongshu', handle: '阿茶不加糖', remark: '来源账号' },
    ],
    notes: [
      { time: '2026-06-21 20:14', content: '偏好日系奶油色调，不喜欢大逆光；下次想试外景双人。' },
      { time: '2026-03-02 11:30', content: '介绍了朋友 Ki酱 过来，记转介绍。' },
    ],
    created_at: '2025-11-08',
  },
  {
    id: 'c2',
    display_name: 'Ki酱',
    real_name: '',
    phone: '155****7311',
    birthday: '07-09',
    channel: 'referral',
    referrer_customer_id: 'c1',
    status: 'active',
    avatar: 'a2',
    identities: [
      { platform: 'qq', handle: '38402216', remark: '' },
      { platform: 'wechat', handle: 'kiki_cos', remark: '发片用' },
    ],
    notes: [
      { time: '2026-07-01 22:05', content: '7/12 出《葬送的芙莉莲》费伦，道具杖自带，需要留 30 分钟试装。' },
    ],
    created_at: '2026-03-02',
  },
  {
    id: 'c3',
    display_name: '陆明轩',
    real_name: '陆明轩',
    phone: '186****9028',
    birthday: '11-02',
    channel: 'douyin',
    referrer_customer_id: null,
    status: 'active',
    avatar: 'a3',
    identities: [{ platform: 'wechat', handle: 'lmx_1102', remark: '' }],
    notes: [{ time: '2026-06-28 18:40', content: '成片已交付，反馈很满意；提到年底想拍全家福。' }],
    created_at: '2026-05-17',
  },
  {
    id: 'c4',
    display_name: 'Momo',
    real_name: '',
    phone: '',
    birthday: '09-26',
    channel: 'weibo',
    referrer_customer_id: null,
    status: 'active',
    avatar: 'a4',
    identities: [{ platform: 'telegram', handle: '@momo_lens', remark: '' }],
    notes: [{ time: '2025-12-20 15:02', content: '圣诞主题拍完，选片很快；说春天再来拍樱花场。' }],
    created_at: '2025-09-14',
  },
  {
    id: 'c5',
    display_name: '苏晚',
    real_name: '',
    phone: '',
    birthday: '',
    channel: 'xiaohongshu',
    referrer_customer_id: null,
    status: 'active',
    avatar: 'a5',
    identities: [{ platform: 'wechat', handle: 'suwan_0430', remark: '' }],
    notes: [{ time: '2026-07-04 21:18', content: '咨询基础写真，预算 ¥700 内，倾向 7 月下旬周末。' }],
    created_at: '2026-07-04',
  },
  {
    id: 'c6',
    display_name: 'CanFly',
    real_name: '',
    phone: '137****5566',
    birthday: '01-19',
    channel: 'referral',
    referrer_customer_id: 'c2',
    status: 'active',
    avatar: 'a1',
    identities: [
      { platform: 'telegram', handle: '@canfly_cos', remark: '' },
      { platform: 'qq', handle: '2201884930', remark: '' },
    ],
    notes: [{ time: '2026-06-15 13:22', content: '外景联动 4 人团，她是队长，沟通都走她。' }],
    created_at: '2026-04-22',
  },
  {
    id: 'c7',
    display_name: '周雨桐',
    real_name: '周雨桐',
    phone: '150****3378',
    birthday: '05-08',
    channel: 'douyin',
    referrer_customer_id: null,
    status: 'active',
    avatar: 'a2',
    identities: [{ platform: 'qq', handle: '77120945', remark: '' }],
    notes: [{ time: '2026-06-30 10:11', content: '成片已发，尾款说月初转，7/8 前跟一次。' }],
    created_at: '2026-06-02',
  },
  {
    id: 'c8',
    display_name: '老白',
    real_name: '白峻',
    phone: '',
    birthday: '',
    channel: 'other',
    referrer_customer_id: null,
    status: 'archived',
    avatar: 'a3',
    identities: [{ platform: 'wechat', handle: 'baijun_photo', remark: '同行' }],
    notes: [],
    created_at: '2025-08-01',
  },
]

export const orders: Order[] = [
  { id: 'o1', customer_id: 'c2', package_id: 'p3', title: '芙莉莲 · 费伦棚拍', status: 'scheduled', price: 98000, deposit_paid: true, balance_paid: false, shot_at: null, delivered_at: null, created_at: '2026-07-01' },
  { id: 'o2', customer_id: 'c5', package_id: 'p1', title: '基础写真咨询', status: 'consulting', price: null, deposit_paid: false, balance_paid: false, shot_at: null, delivered_at: null, created_at: '2026-07-04' },
  { id: 'o3', customer_id: 'c3', package_id: 'p2', title: '毕业双人写真', status: 'delivered', price: 128000, deposit_paid: true, balance_paid: true, shot_at: '2026-06-14', delivered_at: '2026-06-28', created_at: '2026-05-20' },
  { id: 'o4', customer_id: 'c7', package_id: 'p3', title: '明日方舟棚拍', status: 'delivered', price: 98000, deposit_paid: true, balance_paid: false, shot_at: '2026-06-07', delivered_at: '2026-06-30', created_at: '2026-06-02' },
  { id: 'o5', customer_id: 'c1', package_id: 'p2', title: '春日外景双人', status: 'closed', price: 128000, deposit_paid: true, balance_paid: true, shot_at: '2026-04-11', delivered_at: '2026-04-26', created_at: '2026-03-18' },
  { id: 'o6', customer_id: 'c6', package_id: 'p4', title: '外景联动 · 4 人团', status: 'retouching', price: 216000, deposit_paid: true, balance_paid: false, shot_at: '2026-06-21', delivered_at: null, created_at: '2026-06-15' },
  { id: 'o7', customer_id: 'c4', package_id: 'p1', title: '圣诞主题写真', status: 'closed', price: 68000, deposit_paid: true, balance_paid: true, shot_at: '2025-12-20', delivered_at: '2026-01-04', created_at: '2025-12-02' },
  { id: 'o8', customer_id: 'c1', package_id: 'p1', title: '盛夏和服写真', status: 'scheduled', price: 68000, deposit_paid: true, balance_paid: false, shot_at: null, delivered_at: null, created_at: '2026-06-21' },
]

export const slots: Slot[] = [
  { id: 's1', date: '2026-07-06', start: '14:00', end: '16:30', type: 'shoot', order_id: 'o8', note: '和服写真 · 城南日式庭院' },
  { id: 's2', date: '2026-07-06', start: '19:00', end: '20:00', type: 'busy', order_id: null, note: '镜头送保养取件' },
  { id: 's3', date: '2026-07-12', start: '09:30', end: '13:00', type: 'shoot', order_id: 'o1', note: '棚拍 · A 棚，留 30 分钟试装' },
  { id: 's4', date: '2026-07-12', start: '12:00', end: '14:30', type: 'hold', order_id: null, note: '苏晚 意向占位（未定）' },
  { id: 's5', date: '2026-07-19', start: '08:00', end: '12:00', type: 'shoot', order_id: 'o6', note: '外景联动补拍 · 植物园' },
  { id: 's6', date: '2026-07-25', start: '00:00', end: '23:59', type: 'busy', order_id: null, note: '休假，不接单' },
  { id: 's7', date: '2026-07-26', start: '15:00', end: '17:00', type: 'hold', order_id: null, note: '陆明轩 全家福意向（口头）' },
]

export const reminders: Reminder[] = [
  { id: 'r1', type: 'birthday', customer_id: 'c2', order_id: null, due_date: '2026-07-06', content: 'Ki酱 7-09 生日（提前 3 天提醒），可发祝福 + 生日拍摄券', status: 'pending' },
  { id: 'r2', type: 'follow_up', customer_id: 'c3', order_id: 'o3', due_date: '2026-07-05', content: '「毕业双人写真」已交付满 7 天，回访成片反馈、邀请晒图', status: 'pending' },
  { id: 'r3', type: 'churn', customer_id: 'c4', order_id: 'o7', due_date: '2026-07-06', content: 'Momo 距上次拍摄已超 180 天，发樱花场返场邀请', status: 'pending' },
  { id: 'r4', type: 'custom', customer_id: 'c7', order_id: 'o4', due_date: '2026-07-08', content: '跟进「明日方舟棚拍」尾款 ¥490（客户说月初转）', status: 'pending' },
  { id: 'r5', type: 'follow_up', customer_id: 'c1', order_id: 'o5', due_date: '2026-05-03', content: '「春日外景双人」拍后回访', status: 'done' },
]
