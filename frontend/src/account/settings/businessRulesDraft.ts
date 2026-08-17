import type { PlanningBusinessRuleOverrides } from '../../api/client.ts'
import {
  packagePriceCentsToYuan,
  packagePriceYuanToCents,
  validatePackagePriceYuan,
} from '../../pages/packagePrice.ts'

export type BusinessRuleKey = keyof PlanningBusinessRuleOverrides
export type BusinessRuleMode = 'inherit' | 'unknown' | 'value'
export type BusinessRuleDraft = Record<BusinessRuleKey, {
  mode: BusinessRuleMode
  value: string
}>

export interface BusinessRuleDescriptor {
  key: BusinessRuleKey
  label: string
  unit: 'count' | 'money'
}

export const businessRuleDescriptors: readonly BusinessRuleDescriptor[] = [
  { key: 'included_look_count', label: '包含造型数', unit: 'count' },
  { key: 'extra_look_unit_amount', label: '额外造型单价', unit: 'money' },
  { key: 'rented_location_unit_amount', label: '付费场地单价', unit: 'money' },
  { key: 'assistant_unit_amount', label: '助理单价', unit: 'money' },
  { key: 'included_retouched_photo_count', label: '包含精修张数', unit: 'count' },
  { key: 'extra_retouch_unit_amount', label: '额外精修单价', unit: 'money' },
  { key: 'included_shot_count', label: '包含镜头数', unit: 'count' },
  { key: 'extra_shot_unit_amount', label: '额外镜头单价', unit: 'money' },
] as const

export function businessRuleDraftFromOverrides(
  overrides: PlanningBusinessRuleOverrides,
): BusinessRuleDraft {
  return Object.fromEntries(businessRuleDescriptors.map((descriptor) => {
    if (!Object.prototype.hasOwnProperty.call(overrides, descriptor.key)) {
      return [descriptor.key, { mode: 'inherit', value: '' }]
    }
    const value = overrides[descriptor.key]
    if (value === null || value === undefined) {
      return [descriptor.key, { mode: 'unknown', value: '' }]
    }
    return [descriptor.key, {
      mode: 'value',
      value: descriptor.unit === 'money' ? packagePriceCentsToYuan(value) : String(value),
    }]
  })) as BusinessRuleDraft
}

export function businessRuleOverridesFromDraft(
  draft: BusinessRuleDraft,
): PlanningBusinessRuleOverrides {
  const result: PlanningBusinessRuleOverrides = {}
  for (const descriptor of businessRuleDescriptors) {
    const entry = draft[descriptor.key]
    if (entry.mode === 'inherit') continue
    if (entry.mode === 'unknown') {
      result[descriptor.key] = null
      continue
    }
    result[descriptor.key] = parseBusinessRuleValue(descriptor, entry.value)
  }
  return result
}

export function validateBusinessRuleDraft(draft: BusinessRuleDraft): string | null {
  for (const descriptor of businessRuleDescriptors) {
    const entry = draft[descriptor.key]
    if (entry.mode !== 'value') continue
    try {
      parseBusinessRuleValue(descriptor, entry.value)
    } catch (reason) {
      return `${descriptor.label}：${reason instanceof Error ? reason.message : '输入无效'}`
    }
  }
  return null
}

function parseBusinessRuleValue(descriptor: BusinessRuleDescriptor, raw: string): number {
  const value = raw.trim()
  if (descriptor.unit === 'money') {
    const error = validatePackagePriceYuan(value)
    if (error) throw new Error(error.replace('基础价', '金额'))
    return packagePriceYuanToCents(value)
  }
  if (!/^\d+$/.test(value)) throw new Error('数量须为非负整数')
  const parsed = Number(value)
  if (!Number.isSafeInteger(parsed) || parsed > 100000) throw new Error('数量超出范围')
  return parsed
}
