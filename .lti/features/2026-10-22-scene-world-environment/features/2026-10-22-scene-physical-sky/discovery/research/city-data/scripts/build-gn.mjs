// GeoNames citiesN.txt + alternateNamesV2.txt -> 统一记录格式
// 用法：node build-gn.mjs <citiesN.txt> <alternateNamesV2.txt> <out.json> [stats.json]
// 中文名选取规则（简体 zh）：
//   1. zh-CN / zh-Hans / zh-SG 且 isPreferredName=1
//   2. zh 且 isPreferredName=1，且不含繁体字
//   3. zh-CN / zh-Hans / zh-SG（任意）
//   4. zh 中不含繁体字的第一个
//   5. 仍没有时，取 zh 的第一个（可能是繁体，计入统计）
// 繁体 zht：zh-TW / zh-Hant / zh-HK，优先 isPreferredName。
// 排除 isHistoric=1、isColloquial=1 的名称，以及 from/to 有值（有时效）的名称。
import { createReadStream, readFileSync, writeFileSync } from 'node:fs';
import { createInterface } from 'node:readline';
import { hasTradChar, isZhName } from './zh-util.mjs';

const [citiesPath, altPath, out, statsOut] = process.argv.slice(2);
if (!citiesPath || !altPath || !out) throw new Error('usage: node build-gn.mjs <cities.txt> <alternateNamesV2.txt> <out.json> [stats.json]');

const cities = new Map();
for (const line of readFileSync(citiesPath, 'utf8').split('\n')) {
  if (!line) continue;
  const c = line.split('\t');
  cities.set(Number(c[0]), {
    id: `gn:${c[0]}`,
    geonames: Number(c[0]),
    name: c[1],
    ascii: c[2],
    lat: Number(c[4]),
    lon: Number(c[5]),
    fcode: c[7],
    cc: c[8],
    admin1: c[10],
    pop: Number(c[14]) || 0,
    tz: c[17] || null,
    altCjk: (c[3] || '').split(',').filter((n) => isZhName(n)), // cities 文件自带、无语言标记的汉字别名
  });
}
console.log('cities:', cities.size);

const SIMP_LANGS = new Set(['zh-CN', 'zh-Hans', 'zh-SG']);
const TRAD_LANGS = new Set(['zh-TW', 'zh-Hant', 'zh-HK', 'zh-MO']);
const zhNames = new Map(); // id -> [{lang, name, pref}]
const enNames = new Map(); // id -> [{name, pref, short}]
const wkdt = new Map();
const langCounts = {};

const rl = createInterface({ input: createReadStream(altPath, { encoding: 'utf8' }), crlfDelay: Infinity });
let lines = 0;
for await (const line of rl) {
  lines++;
  const t = line.split('\t');
  const gid = Number(t[1]);
  if (!cities.has(gid)) continue;
  const lang = t[2];
  const name = t[3];
  const pref = t[4] === '1';
  const short = t[5] === '1';
  const colloquial = t[6] === '1';
  const historic = t[7] === '1';
  const timed = (t[8] && t[8].trim()) || (t[9] && t[9].trim());
  if (lang === 'wkdt') {
    wkdt.set(gid, name);
    continue;
  }
  if (lang === 'en' && !colloquial && !historic) {
    if (!enNames.has(gid)) enNames.set(gid, []);
    enNames.get(gid).push({ name, pref, short });
    continue;
  }
  if (!lang.startsWith('zh')) continue;
  langCounts[lang] = (langCounts[lang] || 0) + 1;
  if (historic || colloquial || timed) continue;
  if (!isZhName(name)) continue;
  if (!zhNames.has(gid)) zhNames.set(gid, []);
  zhNames.get(gid).push({ lang, name, pref });
}
console.log('altname lines scanned:', lines);

const stats = { cities: cities.size, langCounts, zhRule: { 1: 0, 2: 0, 3: 0, 4: 0, 5: 0, none: 0 }, zhFromTradFallback: 0, zhtFilled: 0, altCjkOnly: 0, wkdt: wkdt.size };
const records = [];
for (const [gid, c] of cities) {
  const zs = zhNames.get(gid) || [];
  const simp = zs.filter((z) => SIMP_LANGS.has(z.lang));
  const plain = zs.filter((z) => z.lang === 'zh');
  const trad = zs.filter((z) => TRAD_LANGS.has(z.lang));
  let zh = null;
  let rule = 'none';
  const pick = (arr, r) => {
    if (!zh && arr.length) {
      zh = arr[0].name;
      rule = r;
    }
  };
  pick(simp.filter((z) => z.pref), 1);
  pick(plain.filter((z) => z.pref && !hasTradChar(z.name)), 2);
  pick(simp, 3);
  pick(plain.filter((z) => !hasTradChar(z.name)), 4);
  pick(plain, 5);
  stats.zhRule[rule]++;
  if (rule === 5) stats.zhFromTradFallback++;
  const zht = (trad.find((z) => z.pref) || trad[0] || {}).name || null;
  if (zht) stats.zhtFilled++;
  if (!zh && c.altCjk.length) stats.altCjkOnly++;
  const ens = enNames.get(gid) || [];
  const en = (ens.find((e) => e.pref && !e.short) || ens.find((e) => e.pref) || {}).name || c.name;
  records.push({
    id: c.id,
    geonames: gid,
    wikidata: wkdt.get(gid) || null,
    zh,
    zht,
    zhAlts: [...new Set(zs.map((z) => z.name))].filter((n) => n !== zh && n !== zht),
    altCjk: c.altCjk,
    en,
    native: c.name,
    ascii: c.ascii,
    lat: c.lat,
    lon: c.lon,
    tz: c.tz,
    pop: c.pop,
    cc: c.cc,
    admin1: c.admin1,
    fcode: c.fcode,
  });
}
writeFileSync(out, JSON.stringify(records));
console.log(JSON.stringify(stats, null, 1));
if (statsOut) writeFileSync(statsOut, JSON.stringify(stats, null, 1));
