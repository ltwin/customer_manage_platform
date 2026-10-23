// The physical sky's city search (W2 design「城市数据」): cities.json, built
// by scripts/cities/build.mjs from GeoNames, Wikidata and a patch table,
// loaded the first time the search box is used and searched in the page.
// Choosing a city writes its numbers into the scene; the scene never refers
// to the table, so a newer table changes no scene.

/** [simplified name or '', English or pinyin name, latitude × 100, longitude × 100, time zone index, country code, traditional name?] */
type Row = [string, string, number, number, number, string, string?]

export type CityTable = {
  attribution: { text: string; source: string; license: string; changes: string; disclaimer: string; fetched: string }
  /** Time zones, the most used first. */
  tz: string[]
  /** Most populous first: the order results of one kind come in. */
  c: Row[]
}

export type City = {
  /** The name shown: simplified Chinese, or English (pinyin in China) where there is none. */
  name: string
  en: string
  latitude: number
  longitude: number
  timezone: string
  country: string
  /** 「伦敦 · 英国」: the name and the country, which tells apart cities of one name. */
  label: string
}

let loading: Promise<CityTable> | null = null

/** The table, fetched once: about 160 KB gzipped, kept out of the editor's first load. */
export function loadCities(): Promise<CityTable> {
  loading ??= import('./cities.json').then((m) => m.default as unknown as CityTable)
  return loading
}

/** Hong Kong, Macau and Taiwan as the editor names them; Intl's 「中国香港特别行政区」 is long for a list. */
const regionNames: Record<string, string> = { HK: '中国香港', MO: '中国澳门', TW: '中国台湾' }
let regions: Intl.DisplayNames | null = null
function countryName(code: string): string {
  if (regionNames[code]) return regionNames[code]
  regions ??= new Intl.DisplayNames('zh-CN', { type: 'region' })
  return regions.of(code) ?? code
}

/** Lower case without accents: 「Zürich」 and 「zurich」 are one. */
const fold = (s: string) => s.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase()

/**
 * Up to limit cities for a query: those with a name beginning with it
 * (simplified, traditional, English or pinyin), then those with it inside a
 * name, each kind most populous first. 「京都」 finds Kyoto before 东京都.
 */
export function searchCities(table: CityTable, query: string, limit = 8): City[] {
  const q = fold(query.trim())
  if (!q) return []
  const prefix: Row[] = []
  const inside: Row[] = []
  for (const row of table.c) {
    const names = [row[0], row[6] ?? '', fold(row[1])]
    if (names.some((n) => n.startsWith(q))) prefix.push(row)
    else if (inside.length < limit && names.some((n) => n.includes(q))) inside.push(row)
    if (prefix.length >= limit) break
  }
  return [...prefix, ...inside].slice(0, limit).map((row) => cityOf(table, row))
}

function cityOf(table: CityTable, [zh, en, lat, lon, tz, country]: Row): City {
  const name = zh || en
  return { name, en, latitude: lat / 100, longitude: lon / 100, timezone: table.tz[tz], country, label: `${name} · ${countryName(country)}` }
}
