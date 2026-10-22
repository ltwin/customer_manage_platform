// 裁剪成前端字段并测体积：按人口截断的多个档位，两种 JSON 形状
// 用法：node size.mjs <records.json> <label> <out.json> [tiers=2000,5000,10000,all] [outDir]
// 档位写法见 select.mjs（如 5000、cn+3000、all）
// 形状 A（对象数组，可读）：[{"zh","zht"?,"en","lat","lon","tz","cc","pop"}]
// 形状 B（紧凑）：{"tz":[时区表],"c":[[zh,en,lat,lon,tzIndex,cc,zht?]]}，按人口降序排列，人口不入包（顺序即排序依据）
// 坐标保留 3 位小数（约 100 m；太阳位置对 0.001° 不敏感）。
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { brotliCompressSync, constants, gzipSync } from 'node:zlib';
import { selectTier } from './select.mjs';

const [src, label, out, tiersArg = '2000,5000,10000,all', outDir] = process.argv.slice(2);
if (!src || !label || !out) throw new Error('usage: node size.mjs <records.json> <label> <out.json> [tiers] [outDir]');
const all = JSON.parse(readFileSync(src, 'utf8')).sort((a, b) => b.pop - a.pop);
const r3 = (x) => Math.round(x * 1000) / 1000;

const shapeA = (rs, withZht) =>
  JSON.stringify(
    rs.map((r) => {
      const o = { zh: r.zh || '', en: r.en, lat: r3(r.lat), lon: r3(r.lon), tz: r.tz || '', cc: r.cc || '', pop: r.pop };
      if (withZht && r.zht) o.zht = r.zht;
      return o;
    }),
  );
const shapeB = (rs, withZht) => {
  const tzs = [...new Set(rs.map((r) => r.tz || ''))].sort();
  const idx = new Map(tzs.map((t, i) => [t, i]));
  return JSON.stringify({
    tz: tzs,
    c: rs.map((r) => {
      const row = [r.zh || '', r.en, r3(r.lat), r3(r.lon), idx.get(r.tz || ''), r.cc || ''];
      if (withZht && r.zht) row.push(r.zht);
      return row;
    }),
  });
};
// 形状 C：同 B，但坐标存成 0.01° 的整数（约 1.1 km；太阳角误差 < 0.01°，时角误差约 2.4 s），时区表按出现频率排序
const shapeC = (rs, withZht) => {
  const freq = new Map();
  for (const r of rs) freq.set(r.tz || '', (freq.get(r.tz || '') || 0) + 1);
  const tzs = [...freq.keys()].sort((a, b) => freq.get(b) - freq.get(a));
  const idx = new Map(tzs.map((t, i) => [t, i]));
  return JSON.stringify({
    tz: tzs,
    c: rs.map((r) => {
      const row = [r.zh || '', r.en, Math.round(r.lat * 100), Math.round(r.lon * 100), idx.get(r.tz || ''), r.cc || ''];
      if (withZht && r.zht) row.push(r.zht);
      return row;
    }),
  });
};
// 形状 D：按列存（各字段一列），坐标同 C；名称列用 \u001f 连接。只用于看列存能省多少
const shapeD = (rs, withZht) => {
  const freq = new Map();
  for (const r of rs) freq.set(r.tz || '', (freq.get(r.tz || '') || 0) + 1);
  const tzs = [...freq.keys()].sort((a, b) => freq.get(b) - freq.get(a));
  const idx = new Map(tzs.map((t, i) => [t, i]));
  const o = {
    tz: tzs,
    zh: rs.map((r) => r.zh || '').join('\u001f'),
    en: rs.map((r) => r.en).join('\u001f'),
    lat: rs.map((r) => Math.round(r.lat * 100)),
    lon: rs.map((r) => Math.round(r.lon * 100)),
    t: rs.map((r) => idx.get(r.tz || '')),
    cc: rs.map((r) => r.cc || '--').join(''),
  };
  if (withZht) o.zht = rs.map((r) => r.zht || '').join('\u001f');
  return JSON.stringify(o);
};
const measure = (s) => {
  const buf = Buffer.from(s, 'utf8');
  return {
    raw: buf.length,
    gzip: gzipSync(buf, { level: 9 }).length,
    brotli: brotliCompressSync(buf, { params: { [constants.BROTLI_PARAM_QUALITY]: 11 } }).length,
  };
};

const rows = [];
for (const t of tiersArg.split(',')) {
  const rs = selectTier(all, t);
  const n = rs.length;
  const zhFilled = rs.filter((r) => r.zh).length;
  for (const [shape, fn] of [
    ['A', shapeA],
    ['B', shapeB],
    ['C', shapeC],
    ['D', shapeD],
  ])
    for (const withZht of [false, true]) {
      const s = fn(rs, withZht);
      rows.push({ tier: t, n, zhFilled, shape, zht: withZht, ...measure(s) });
      if (outDir && shape === 'C' && withZht) {
        mkdirSync(outDir, { recursive: true });
        writeFileSync(`${outDir}/${label}-${t}.json`, s);
      }
    }
}
writeFileSync(out, JSON.stringify({ label, source: src.split('/').pop(), rows }, null, 1));
console.log(`== ${label}`);
console.log('tier\tn\tzh\tshape\tzht\traw\tgzip\tbrotli');
for (const r of rows) console.log(`${r.tier}\t${r.n}\t${r.zhFilled}\t${r.shape}\t${r.zht ? 'y' : 'n'}\t${r.raw}\t${r.gzip}\t${r.brotli}`);
