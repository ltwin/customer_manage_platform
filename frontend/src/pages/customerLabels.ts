import type { components } from '../api/schema'

export type CustomerChannel = components['schemas']['CustomerChannel']
export type SocialPlatform = components['schemas']['SocialPlatform']

export const channelLabels: Record<CustomerChannel, string> = {
  xiaohongshu: '小红书',
  douyin: '抖音',
  weibo: '微博',
  referral: '客户介绍',
  other: '其他',
}

export const platformLabels: Record<SocialPlatform, string> = {
  wechat: '微信',
  qq: 'QQ',
  telegram: 'Telegram',
  xiaohongshu: '小红书',
  douyin: '抖音',
  weibo: '微博',
  other: '其他',
}

export const channelOptions = Object.entries(channelLabels) as Array<[CustomerChannel, string]>
export const platformOptions = Object.entries(platformLabels) as Array<[SocialPlatform, string]>
