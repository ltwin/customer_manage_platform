export type PriceDraft = string

const maxPackagePriceCents = 2 ** 31 - 1

export function isPackagePriceYuanInputAllowed(value: string): boolean {
  return value === '' || /^(?:\d+|\d*\.\d{0,2})$/.test(value)
}

export function validatePackagePriceYuan(value: PriceDraft): string | null {
  const normalized = value.trim()
  if (normalized === '') return '基础价必填'
  if (normalized.startsWith('-')) return '基础价不能为负'
  if (!isPackagePriceYuanInputAllowed(normalized)) {
    if (/^\d*\.\d{3,}$/.test(normalized)) return '基础价最多保留两位小数'
    return '基础价必须是有效数字'
  }
  if (normalized === '.') return '基础价必须是有效数字'
  if (parsePackagePriceYuanToCents(normalized) > maxPackagePriceCents) return '基础价超出范围'
  return null
}

export function packagePriceYuanToCents(value: PriceDraft): number {
  if (validatePackagePriceYuan(value) != null) {
    throw new Error('package price precision exceeds cents')
  }
  return parsePackagePriceYuanToCents(value.trim())
}

export function packagePriceCentsToYuan(value: number): PriceDraft {
  const yuan = Math.trunc(value / 100)
  const cents = Math.abs(value % 100)
  if (cents === 0) return String(yuan)
  if (cents % 10 === 0) return `${yuan}.${cents / 10}`
  return `${yuan}.${String(cents).padStart(2, '0')}`
}

function parsePackagePriceYuanToCents(value: string): number {
  const [yuan = '0', fraction = ''] = value.split('.')
  return Number(yuan || '0') * 100 + Number(fraction.padEnd(2, '0'))
}
