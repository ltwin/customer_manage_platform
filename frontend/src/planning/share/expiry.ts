// 分享到期时间的时区边界：契约与存储始终是 UTC ISO，摄影师看到和输入的始终是本地时刻。
// datetime-local 控件的值是「不带时区的墙上时间」，所以两个方向必须成对走本地换算——
// 一边本地一边 UTC 会让东八区用户选的 18:00 存成 UTC 18:00（本地次日 02:00）。

export function toDatetimeLocal(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

// 空值与非法输入回落到「此刻」，与控件清空时的既有行为一致；越界由服务端裁决。
export function fromDatetimeLocal(value: string): string {
  if (!value) return new Date().toISOString()
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? new Date().toISOString() : parsed.toISOString()
}
