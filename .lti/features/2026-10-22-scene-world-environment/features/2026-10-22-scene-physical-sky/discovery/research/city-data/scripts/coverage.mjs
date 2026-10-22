// 覆盖率与质量检查：清单命中、中文名填充与繁简、时区有效性
// 用法：node coverage.mjs <records.json> <label> <out.json> [topN]
// topN：档位，见 select.mjs（如 5000、cn+3000、all）
import { readFileSync, writeFileSync } from 'node:fs';
// CHECKLIST_MODULE=./checklist-extra.mjs 可换成补充清单
const { CHECKLIST } = await import(process.env.CHECKLIST_MODULE || './checklist.mjs');
import { hasTradChar, isZhName } from './zh-util.mjs';
import { selectTier } from './select.mjs';

const [src, label, out, topArg] = process.argv.slice(2);
if (!src || !label || !out) throw new Error('usage: node coverage.mjs <records.json> <label> <out.json> [topN]');
const TOLERANCE_KM = 60;

const records = selectTier(JSON.parse(readFileSync(src, 'utf8')), topArg);

const normEn = (s) =>
  String(s || '')
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]/g, '');
const km = (a, b, c, d) => {
  const r = Math.PI / 180;
  const x = Math.sin(((c - a) * r) / 2) ** 2 + Math.cos(a * r) * Math.cos(c * r) * Math.sin(((d - b) * r) / 2) ** 2;
  return 2 * 6371 * Math.asin(Math.sqrt(x));
};
const shippedZh = (r) => [r.zh, r.zht].filter(Boolean);

const items = CHECKLIST.map((item) => {
  const lab = item.label || item.zh[0];
  const zhHits = records.filter((r) => shippedZh(r).some((n) => item.zh.some((q) => n.includes(q))));
  const zhAltHits = records.filter((r) => !zhHits.includes(r) && [...(r.zhAlts || []), ...(r.altCjk || [])].some((n) => item.zh.some((q) => n.includes(q))));
  const aliases = item.en.map(normEn);
  const enHits = records.filter((r) => [r.en, r.native, r.ascii].some((n) => n && aliases.some((a) => normEn(n) === a || normEn(n).startsWith(a))));
  const withDist = (r) => ({ r, d: km(item.lat, item.lon, r.lat, r.lon) });
  const ok = (arr) => arr.map(withDist).filter((x) => x.d <= TOLERANCE_KM).sort((a, b) => a.d - b.d);
  const zhOk = ok(zhHits);
  const enOk = ok(enHits);
  const altOk = ok(zhAltHits);
  let status = 'miss';
  let best = null;
  // 中文命中的条目离期望坐标不能比最近的英文命中远 10 km 以上，避免「纽约州东草地」（38 km）这类附近地名冒充
  const nearestAny = [...zhOk, ...enOk].sort((a, b) => a.d - b.d)[0];
  const zhClose = zhOk.filter((x) => x.d <= nearestAny.d + 10);
  if (zhClose.length) {
    status = 'zh';
    best = zhClose[0];
  } else if (enOk.length) {
    status = altOk.length ? 'en+zhAlt' : 'en';
    best = enOk[0];
  } else if (altOk.length) {
    status = 'zhAltOnly';
    best = altOk[0];
  }
  // 中文查询下的歧义：前缀命中（含完全相同）排在中间命中之前，同类按人口降序；看正确条目排第几
  const isPrefix = (r) => shippedZh(r).some((n) => item.zh.some((q) => n.startsWith(q)));
  const zhSorted = [...zhHits].sort((a, b) => Number(isPrefix(b)) - Number(isPrefix(a)) || b.pop - a.pop);
  const rank = best && status === 'zh' ? zhSorted.indexOf(best.r) + 1 : null;
  const wrongZh = zhHits.filter((r) => km(item.lat, item.lon, r.lat, r.lon) > TOLERANCE_KM);
  return {
    item: lab,
    group: item.group,
    status,
    matched: best
      ? { zh: best.r.zh, zht: best.r.zht, en: best.r.en, tz: best.r.tz, pop: best.r.pop, cc: best.r.cc, km: Math.round(best.d), zhAlts: (best.r.zhAlts || []).slice(0, 4) }
      : null,
    zhRank: rank,
    zhHitCount: zhHits.length,
    wrongZhSamples: wrongZh.slice(0, 4).map((r) => `${r.zh || r.zht}/${r.en}/${r.cc}/${r.pop}`),
  };
});

// 中文名统计
const withZh = records.filter((r) => r.zh && isZhName(r.zh));
const zhTrad = withZh.filter((r) => hasTradChar(r.zh));
const zhEqualsEn = records.filter((r) => r.zh && !isZhName(r.zh));
// 时区统计
const isValidTz = (tz) => {
  try {
    new Intl.DateTimeFormat(undefined, { timeZone: tz });
    return true;
  } catch {
    return false;
  }
};
const tzMap = new Map();
for (const r of records) if (r.tz) tzMap.set(r.tz, (tzMap.get(r.tz) || 0) + 1);
const invalidTz = [...tzMap].filter(([tz]) => !isValidTz(tz));
const aliasTz = [...tzMap].filter(([tz]) => isValidTz(tz) && new Intl.DateTimeFormat(undefined, { timeZone: tz }).resolvedOptions().timeZone !== tz);

const summary = {
  label,
  source: src.split('/').pop(),
  top: topArg || 'all',
  records: records.length,
  zhFilled: withZh.length,
  zhFilledPct: +((withZh.length / records.length) * 100).toFixed(1),
  zhWithTradCharHeuristic: zhTrad.length,
  zhNotChinese: zhEqualsEn.length,
  tzFilled: records.filter((r) => r.tz).length,
  tzDistinct: tzMap.size,
  tzInvalidIntl: invalidTz,
  tzAliasCount: aliasTz.length,
  checklist: {
    total: items.length,
    zh: items.filter((i) => i.status === 'zh').length,
    enOnly: items.filter((i) => i.status.startsWith('en')).length,
    zhAltOnly: items.filter((i) => i.status === 'zhAltOnly').length,
    miss: items.filter((i) => i.status === 'miss').length,
    found: items.filter((i) => i.status !== 'miss').length,
    zhTop1: items.filter((i) => i.zhRank === 1).length,
    cnFound: items.filter((i) => i.group === 'cn' && i.status !== 'miss').length,
    cnTotal: items.filter((i) => i.group === 'cn').length,
    intlFound: items.filter((i) => i.group === 'intl' && i.status !== 'miss').length,
    intlTotal: items.filter((i) => i.group === 'intl').length,
  },
  items,
};
writeFileSync(out, JSON.stringify(summary, null, 1));
writeFileSync(out.replace(/\.json$/, '.tz.txt'), [...tzMap.keys()].sort().join('\n') + '\n');
const c = summary.checklist;
console.log(`${label} top=${summary.top} records=${summary.records} zh=${summary.zhFilled}(${summary.zhFilledPct}%) zhTrad~${summary.zhWithTradCharHeuristic} tz=${summary.tzFilled} tzInvalid=${invalidTz.length} | checklist found ${c.found}/${c.total} (zh ${c.zh}, zhTop1 ${c.zhTop1}, en-only ${c.enOnly}, zhAltOnly ${c.zhAltOnly}, miss ${c.miss}; cn ${c.cnFound}/${c.cnTotal}, intl ${c.intlFound}/${c.intlTotal})`);
for (const i of items) {
  const m = i.matched;
  console.log(`  ${i.item}\t${i.status}\t${m ? `${m.zh || '-'}|${m.zht || '-'}|${m.en}|${m.tz}|pop ${m.pop}|${m.km}km` : ''}\t${i.zhRank ? 'rank ' + i.zhRank + '/' + i.zhHitCount : ''}\t${i.wrongZhSamples.join('; ')}`);
}
