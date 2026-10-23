// Builds src/creative-scene/world/cities.json, the physical sky's city search
// (W2 design「城市数据」, research city-data.md): GeoNames cities15000 for
// places, coordinates, IANA time zones and population (CC BY 4.0), Chinese
// names from Wikidata labels by GeoNames ID (CC0) and GeoNames' tagged
// alternate names, then patches.json. All of mainland China, Hong Kong,
// Macau and Taiwan, and abroad the 5,000 most populous. A developer runs it
// when the data is to be updated, not CI; the output is committed.
//
//   node scripts/cities/build.mjs <data dir>
//
// The data dir keeps the downloads, each in a directory of its own, and is
// filled with what is missing: gn-15000/, gn-5000/ (patches' include),
// gn-altnames/ (205 MB zipped) and wd-labels-15000/ (about 86 SPARQL
// queries, four minutes). Nothing is run from it.
import { execFileSync } from 'node:child_process'
import { createReadStream, existsSync, mkdirSync, readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { createInterface } from 'node:readline'
import { gzipSync } from 'node:zlib'
import { hasTradChar, isZhName } from './zh.mjs'

const dataDir = process.argv[2]
if (!dataDir) throw new Error('usage: node scripts/cities/build.mjs <data dir>')
const here = new URL('.', import.meta.url)
const out = new URL('../../src/creative-scene/world/cities.json', here)
const patches = JSON.parse(readFileSync(new URL('patches.json', here), 'utf8'))

const CN_GROUP = new Set(['CN', 'HK', 'MO', 'TW'])
const ABROAD = 5000

// --- 1. Downloads -----------------------------------------------------------

function download(name, url) {
  const dir = `${dataDir}/${name}`
  const zip = `${dir}/${url.split('/').pop()}`
  if (existsSync(zip)) return dir
  mkdirSync(dir, { recursive: true })
  console.log(`downloading ${url}`)
  execFileSync('curl', ['-sSfL', '-o', zip, url], { stdio: 'inherit' })
  execFileSync('unzip', ['-o', '-q', zip, '-d', dir], { stdio: 'inherit', cwd: '/' })
  return dir
}
const gn15000 = `${download('gn-15000', 'https://download.geonames.org/export/dump/cities15000.zip')}/cities15000.txt`
const gn5000 = `${download('gn-5000', 'https://download.geonames.org/export/dump/cities5000.zip')}/cities5000.txt`
const altnames = `${download('gn-altnames', 'https://download.geonames.org/export/dump/alternateNamesV2.zip')}/alternateNamesV2.txt`
const fetched = statSync(gn15000).mtime.toISOString().slice(0, 10)

// --- 2. Places --------------------------------------------------------------

function readCities(path) {
  const cities = new Map()
  for (const line of readFileSync(path, 'utf8').split('\n')) {
    if (!line) continue
    const c = line.split('\t')
    cities.set(Number(c[0]), {
      geonames: Number(c[0]),
      name: c[1],
      lat: Number(c[4]),
      lon: Number(c[5]),
      cc: c[8],
      admin1: c[10],
      pop: Number(c[14]) || 0,
      tz: c[17],
      // The cities file's untagged alternate names in Han script.
      altCjk: (c[3] || '').split(',').filter(isZhName),
    })
  }
  return cities
}
const cities = readCities(gn15000)
if (patches.include.length) {
  const more = readCities(gn5000)
  for (const { geonames } of patches.include) {
    const c = more.get(geonames)
    if (!c) throw new Error(`include: ${geonames} is not in cities5000`)
    cities.set(geonames, c)
  }
}

// GeoNames' tagged names: Chinese (simplified, plain zh, traditional) and English.
const SIMP = new Set(['zh-CN', 'zh-Hans', 'zh-SG'])
const TRAD = new Set(['zh-TW', 'zh-Hant', 'zh-HK', 'zh-MO'])
const zhNames = new Map()
const enNames = new Map()
for await (const line of createInterface({ input: createReadStream(altnames, { encoding: 'utf8' }), crlfDelay: Infinity })) {
  const t = line.split('\t')
  const id = Number(t[1])
  if (!cities.has(id)) continue
  const [lang, name, pref, short, colloquial, historic, from, to] = [t[2], t[3], t[4] === '1', t[5] === '1', t[6] === '1', t[7] === '1', t[8]?.trim(), t[9]?.trim()]
  if (colloquial || historic) continue
  if (lang === 'en') {
    if (!enNames.has(id)) enNames.set(id, [])
    enNames.get(id).push({ name, pref, short })
  } else if (lang.startsWith('zh') && !from && !to && isZhName(name)) {
    if (!zhNames.has(id)) zhNames.set(id, [])
    zhNames.get(id).push({ lang, name, pref })
  }
}

// --- 3. Wikidata's Chinese labels -----------------------------------------------

const wdDir = `${dataDir}/wd-labels-15000`
mkdirSync(wdDir, { recursive: true })
{
  const ids = [...cities.keys()].map(String)
  const langs = ['zh-hans', 'zh-cn', 'zh', 'zh-hant', 'zh-tw', 'zh-hk']
  const v = (l) => `?${l.replace('-', '_')}`
  const optional = langs.map((l) => `OPTIONAL { ?item rdfs:label ${v(l)} FILTER(LANG(${v(l)}) = "${l}") }`).join(' ')
  const size = 400
  for (let i = 0, b = 0; i < ids.length; i += size, b++) {
    const file = `${wdDir}/batch-${String(b).padStart(3, '0')}.json`
    if (existsSync(file)) continue
    const query = `SELECT ?gn ?item ${langs.map(v).join(' ')} WHERE { VALUES ?gn { ${ids.slice(i, i + size).map((id) => `"${id}"`).join(' ')} } ?item wdt:P1566 ?gn . ${optional} }`
    for (let attempt = 0; ; attempt++) {
      const res = await fetch('https://query.wikidata.org/sparql', {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded', Accept: 'application/sparql-results+json', 'User-Agent': 'creative-scene-cities/1.0 (offline gazetteer build)' },
        body: new URLSearchParams({ query }),
      })
      if (res.ok) {
        writeFileSync(file, await res.text())
        break
      }
      if (attempt === 3) throw new Error(`Wikidata batch ${b}: ${res.status}`)
      await new Promise((r) => setTimeout(r, 5000 * (attempt + 1)))
    }
    await new Promise((r) => setTimeout(r, 1000))
  }
}
const wd = new Map()
for (const f of readdirSync(wdDir).filter((n) => n.endsWith('.json'))) {
  for (const b of JSON.parse(readFileSync(`${wdDir}/${f}`, 'utf8')).results.bindings) {
    const id = Number(b.gn.value)
    // An ID linked to more than one item takes the first.
    if (!wd.has(id)) wd.set(id, {})
    const e = wd.get(id)
    for (const k of ['zh_hans', 'zh_cn', 'zh', 'zh_hant', 'zh_tw', 'zh_hk']) if (b[k] && !e[k]) e[k] = b[k].value
  }
}

// --- 4. Names ---------------------------------------------------------------

/** A display name: no trailing disambiguation, no trailing 市 or 特别市 when two characters are left. */
function clean(s) {
  if (!s) return s
  let t = s.trim().replace(/\s*[（(][^）)]*[）)]\s*$/, '')
  for (const suffix of ['特别市', '市']) if (t.endsWith(suffix) && [...t].length - [...suffix].length >= 2) t = t.slice(0, -suffix.length)
  return t
}

const records = [...cities.values()].map((c) => {
  const w = wd.get(c.geonames) ?? {}
  const zs = zhNames.get(c.geonames) ?? []
  const simp = zs.filter((z) => SIMP.has(z.lang))
  const plain = zs.filter((z) => z.lang === 'zh')
  const trad = zs.filter((z) => TRAD.has(z.lang))
  const tagged = (simp.find((z) => z.pref) ?? plain.find((z) => z.pref && !hasTradChar(z.name)) ?? simp[0] ?? plain.find((z) => !hasTradChar(z.name)))?.name
  // Simplified, in the research's order: Wikidata zh-hans, zh-cn; GeoNames' tagged; Wikidata zh; GeoNames' other; in China the untagged.
  const simplified = (n) => (n && !hasTradChar(n) ? n : undefined)
  const zh = [w.zh_hans, w.zh_cn, simplified(tagged), simplified(w.zh), zs.map((z) => z.name).find((n) => !hasTradChar(n)), CN_GROUP.has(c.cc) ? c.altCjk.find((n) => !hasTradChar(n)) : undefined].find((n) => n && isZhName(n))
  const zht = [w.zh_hant, w.zh_tw, w.zh_hk, (trad.find((z) => z.pref) ?? trad[0])?.name].find((n) => n && isZhName(n))
  const ens = enNames.get(c.geonames) ?? []
  const en = (ens.find((e) => e.pref && !e.short) ?? ens.find((e) => e.pref))?.name ?? c.name
  return { ...c, zh: clean(zh) ?? '', zht: clean(zht) ?? '', en }
})

// --- 5. The tier, the patches, Xinjiang ---------------------------------------

records.sort((a, b) => b.pop - a.pop)
const included = new Set(patches.include.map((p) => p.geonames))
const chosen = [...records.filter((r) => CN_GROUP.has(r.cc) || included.has(r.geonames)), ...records.filter((r) => !CN_GROUP.has(r.cc) && !included.has(r.geonames)).slice(0, ABROAD)]
const byId = new Map(chosen.map((r) => [r.geonames, r]))
for (const p of patches.include) Object.assign(byId.get(p.geonames), p.set ?? {})
for (const p of patches.override) {
  const r = byId.get(p.geonames)
  if (!r) throw new Error(`override: ${p.geonames} (${p.note ?? ''}) is not in the table`)
  Object.assign(r, p.set)
}
for (const p of patches.add) chosen.push({ geonames: null, zht: '', ...p.set })
// Xinjiang (admin1 13) on Beijing time, as its timetables and most visitors are (owner's choice, design「局部设计与偏差」).
let xinjiang = 0
for (const r of chosen) {
  if (r.cc === 'CN' && r.tz === 'Asia/Urumqi') {
    r.tz = 'Asia/Shanghai'
    xinjiang++
  }
}
chosen.sort((a, b) => b.pop - a.pop)

// --- 6. Shape C ---------------------------------------------------------------

const zoneCount = new Map()
for (const r of chosen) zoneCount.set(r.tz, (zoneCount.get(r.tz) ?? 0) + 1)
const zones = [...zoneCount.keys()].sort((a, b) => zoneCount.get(b) - zoneCount.get(a) || a.localeCompare(b))
const zoneIndex = new Map(zones.map((z, i) => [z, i]))
const rows = chosen.map((r) => {
  const zht = r.zht && r.zht !== r.zh ? r.zht : ''
  const row = [r.zh, r.en, Math.round(r.lat * 100), Math.round(r.lon * 100), zoneIndex.get(r.tz), r.cc]
  if (zht) row.push(zht)
  return row
})
const table = {
  attribution: {
    text: '城市数据：GeoNames（CC BY 4.0），已修改',
    source: 'https://www.geonames.org/',
    license: 'https://creativecommons.org/licenses/by/4.0/',
    changes: 'Fields trimmed to name, coordinates, time zone and country; Chinese names from Wikidata (CC0) and GeoNames alternate names; places added, renamed and moved by hand (scripts/cities/patches.json); Xinjiang on Asia/Shanghai.',
    disclaimer: 'The Data is provided "as is" without warranty or any representation of accuracy, timeliness or completeness.',
    fetched,
  },
  tz: zones,
  c: rows,
}
const json = JSON.stringify(table)
writeFileSync(out, `${json}\n`)

// --- 7. For a person to look over -----------------------------------------------

const bad = new Set()
for (const z of zones) {
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: z })
  } catch {
    bad.add(z)
  }
}
const missing = chosen.filter((r) => CN_GROUP.has(r.cc) && !r.zh && r.pop >= 100_000)
const traditional = chosen.filter((r) => hasTradChar(r.zh))
console.log(`${rows.length} places (${chosen.filter((r) => CN_GROUP.has(r.cc)).length} in China), ${zones.length} time zones, ${xinjiang} in Xinjiang on Asia/Shanghai; data of ${fetched}`)
console.log(`${json.length} bytes, ${gzipSync(json).length} gzipped`)
console.log(`time zones Intl does not know: ${[...bad].join(', ') || 'none'}`)
console.log(`in China, 100,000 people or more and no Chinese name (${missing.length}):`)
for (const r of missing) console.log(`  ${r.geonames}\t${r.en}\t${r.admin1}\t${r.pop}\t${r.lat},${r.lon}`)
console.log(`traditional characters left in the simplified name (${traditional.length}): ${traditional.map((r) => `${r.zh} (${r.geonames})`).join(', ')}`)
