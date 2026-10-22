// 查看统一记录里指定英文名的条目
// 用法：node peek.mjs <records.json> <name1,name2,...> [field=value]
import { readFileSync } from 'node:fs';

const [src, names, filter] = process.argv.slice(2);
const records = JSON.parse(readFileSync(src, 'utf8'));
const want = new Set(names.split(','));
let [fk, fv] = (filter || '').split('=');
for (const r of records) {
  if (!(want.has(r.en) || want.has(r.native) || want.has(r.ascii))) continue;
  if (fk && String(r[fk]) !== fv) continue;
  const { altCjk, ...rest } = r;
  console.log(JSON.stringify({ ...rest, altCjk: (altCjk || []).slice(0, 8) }));
}
