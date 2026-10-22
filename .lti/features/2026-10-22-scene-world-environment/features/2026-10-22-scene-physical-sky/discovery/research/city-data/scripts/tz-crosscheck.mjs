// 交叉核对 Natural Earth 的 TIMEZONE：与 25 km 内最近的 GeoNames 条目比较 2026 年 1 月和 7 月的 UTC 偏移
// 用法：node tz-crosscheck.mjs <ne.json> <gn.json> <out.json>
import { readFileSync, writeFileSync } from 'node:fs';

const [neSrc, gnSrc, out] = process.argv.slice(2);
const ne = JSON.parse(readFileSync(neSrc, 'utf8'));
const gn = JSON.parse(readFileSync(gnSrc, 'utf8'));
const RADIUS = 25;

const grid = new Map();
const key = (lat, lon) => `${Math.floor(lat)}:${Math.floor(lon)}`;
for (const g of gn) {
  const k = key(g.lat, g.lon);
  if (!grid.has(k)) grid.set(k, []);
  grid.get(k).push(g);
}
const km = (a, b, c, d) => {
  const t = Math.PI / 180;
  const x = Math.sin(((c - a) * t) / 2) ** 2 + Math.cos(a * t) * Math.cos(c * t) * Math.sin(((d - b) * t) / 2) ** 2;
  return 2 * 6371 * Math.asin(Math.sqrt(x));
};
const nearest = (lat, lon) => {
  let best = null;
  for (let i = -1; i <= 1; i++)
    for (let j = -1; j <= 1; j++)
      for (const g of grid.get(key(lat + i, lon + j)) || []) {
        const d = km(lat, lon, g.lat, g.lon);
        if (d <= RADIUS && (!best || d < best.d)) best = { g, d };
      }
  return best;
};
const fmtCache = new Map();
const offset = (tz, iso) => {
  if (!fmtCache.has(tz)) fmtCache.set(tz, new Intl.DateTimeFormat('en-US', { timeZone: tz, timeZoneName: 'longOffset' }));
  return fmtCache.get(tz).formatToParts(new Date(iso)).find((p) => p.type === 'timeZoneName').value;
};
const sig = (tz) => `${offset(tz, '2026-01-15T12:00:00Z')}|${offset(tz, '2026-07-15T12:00:00Z')}`;

let compared = 0;
let same = 0;
const diffs = [];
let missing = 0;
let missingFillable = 0;
let noNeighbor = 0;
for (const r of ne) {
  const n = nearest(r.lat, r.lon);
  if (!r.tz) {
    missing++;
    if (n) missingFillable++;
    continue;
  }
  if (!n) {
    noNeighbor++;
    continue;
  }
  compared++;
  if (sig(r.tz) === sig(n.g.tz)) same++;
  else diffs.push({ ne: `${r.en}/${r.cc}`, neTz: r.tz, neSig: sig(r.tz), gn: n.g.en, gnTz: n.g.tz, gnSig: sig(n.g.tz), km: Math.round(n.d), pop: r.pop });
}
diffs.sort((a, b) => b.pop - a.pop);
const result = { neRecords: ne.length, neWithTz: ne.length - missing, compared, sameOffset: same, differentOffset: diffs.length, noNeighborWithin25km: noNeighbor, neMissingTz: missing, neMissingTzFillableFromGn: missingFillable, diffs };
writeFileSync(out, JSON.stringify(result, null, 1));
const { diffs: d, ...head } = result;
console.log(JSON.stringify(head));
for (const x of d.slice(0, 40)) console.log(`${x.ne}\tNE ${x.neTz} ${x.neSig}\tGN ${x.gn} ${x.gnTz} ${x.gnSig}\t${x.km}km\tpop ${x.pop}`);
