import type { Opening } from './model.ts'

export interface OpeningDay {
  date: string
  openings: Opening[]
}

export function groupOpeningsByDate(openings: Opening[], maxDays = Number.POSITIVE_INFINITY): OpeningDay[] {
  const byDate = new Map<string, Opening[]>()
  for (const opening of openings) {
    const entries = byDate.get(opening.date)
    if (entries) entries.push(opening)
    else byDate.set(opening.date, [opening])
  }
  return Array.from(byDate, ([date, entries]) => ({ date, openings: entries })).slice(0, maxDays)
}

export function openingsForFirstDays(openings: Opening[], maxDays: number): Opening[] {
  return groupOpeningsByDate(openings, maxDays).flatMap((day) => day.openings)
}

export function openingsText(openings: Opening[], maxDays = 5): string {
  const days = groupOpeningsByDate(openings, maxDays)
  if (days.length === 0) return ''
  return [
    '近期可约时间：',
    ...days.map((day) => `${day.date} ${day.openings.map((opening) => `${opening.start}–${opening.end}`).join('、')}`),
    '以上时间仅供参考，最终以确认为准。',
  ].join('\n')
}
