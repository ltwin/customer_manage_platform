// 合并：GeoNames 地点 + Wikidata 中文标签 (+ 可选 Natural Earth NAME_ZH) -> 统一记录
// 用法：node merge.mjs <gn-records.json> <wd-labels-dir> <out.json> [ne-records.json]
// 中文简体名（zh）优先级：
//   W1 Wikidata zh-hans  W2 Wikidata zh-cn  N  Natural Earth NAME_ZH（经 GEONAMESID 关联，可选）
//   G1 build-gn.mjs 选出的 GeoNames 中文名（不含繁体字）  W3 Wikidata zh（不含繁体字）  G2 GeoNames 其他中文别名（不含繁体字）
//   G3 仅中国大陆/港澳台：cities 文件里无语言标记的汉字别名（不含繁体字）
// 繁体名（zht）：Wikidata zh-hant > zh-tw > zh-hk > GeoNames zh-TW/zh-Hant；与 zh 相同则置空。
// 显示名清理：去掉结尾的消歧括注；去掉结尾的「市」「特别市」（剩余至少 2 个字时）。
import { readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { hasTradChar, isZhName } from './zh-util.mjs';

const [gnSrc, wdDir, out, neSrc] = process.argv.slice(2);
if (!gnSrc || !wdDir || !out) throw new Error('usage: node merge.mjs <gn.json> <wd-dir> <out.json> [ne.json]');
const gn = JSON.parse(readFileSync(gnSrc, 'utf8'));

const wd = new Map(); // geonameid -> { 'zh-hans': [...], ... , items:Set }
for (const f of readdirSync(wdDir).filter((n) => n.endsWith('.json'))) {
  const j = JSON.parse(readFileSync(`${wdDir}/${f}`, 'utf8'));
  for (const b of j.results.bindings) {
    const id = Number(b.gn.value);
    if (!wd.has(id)) wd.set(id, { items: new Set() });
    const e = wd.get(id);
    e.items.add(b.item.value.split('/').pop());
    for (const k of ['zh_hans', 'zh_cn', 'zh', 'zh_hant', 'zh_tw', 'zh_hk']) {
      if (b[k]) {
        e[k] = e[k] || [];
        if (!e[k].includes(b[k].value)) e[k].push(b[k].value);
      }
    }
  }
}
const neByGn = new Map();
if (neSrc) for (const r of JSON.parse(readFileSync(neSrc, 'utf8'))) if (r.geonames) neByGn.set(r.geonames, r);

const CN_GROUP = new Set(['CN', 'HK', 'MO', 'TW']);
const clean = (s) => {
  if (!s) return s;
  let t = s.trim().replace(/\s*[（(][^）)]*[）)]\s*$/, ''); // 去掉结尾的消歧括注，如「格拉纳达（城市）」
  for (const suf of ['特别市', '市']) if (t.endsWith(suf) && [...t].length - [...suf].length >= 2) t = t.slice(0, -suf.length);
  return t;
};
// GeoNames 原始记录里没有保留语言标签，这里用 zhAlts 中「不含繁体字」的作为 G 级候选
const stats = { total: gn.length, wdLinked: 0, wdMultiItem: 0, rule: {}, zht: 0, zhMissing: 0 };
const outRecs = gn.map((r) => {
  const w = wd.get(r.geonames);
  if (w) stats.wdLinked++;
  if (w && w.items.size > 1) stats.wdMultiItem++;
  const cands = [
    ['W1', w?.zh_hans?.[0]],
    ['W2', w?.zh_cn?.[0]],
    ['N', neSrc ? neByGn.get(r.geonames)?.zh : undefined],
    ['G1', r.zh && !hasTradChar(r.zh) ? r.zh : undefined],
    ['W3', w?.zh?.find((n) => !hasTradChar(n))],
    ['G2', (r.zhAlts || []).find((n) => !hasTradChar(n))],
    ['G3', CN_GROUP.has(r.cc) ? (r.altCjk || []).find((n) => !hasTradChar(n)) : undefined],
  ];
  let zh = null;
  let rule = 'none';
  for (const [k, v] of cands) {
    if (v && isZhName(v)) {
      zh = v;
      rule = k;
      break;
    }
  }
  stats.rule[rule] = (stats.rule[rule] || 0) + 1;
  if (!zh) stats.zhMissing++;
  const tradCands = [w?.zh_hant?.[0], w?.zh_tw?.[0], w?.zh_hk?.[0], r.zht, hasTradChar(r.zh) ? r.zh : undefined].filter((v) => v && isZhName(v));
  let zht = tradCands[0] ? clean(tradCands[0]) : null;
  zh = clean(zh);
  if (zht && zht === zh) zht = null;
  if (zht) stats.zht++;
  return { ...r, zh, zht, zhRule: rule, wikidata: w ? [...w.items][0] : r.wikidata };
});
writeFileSync(out, JSON.stringify(outRecs));
console.log(JSON.stringify(stats));
