// 列出指定坐标附近的条目
// 用法：node near.mjs <records.json> <lat> <lon> [km=40]
import { readFileSync } from 'node:fs';

const [src, lat, lon, r = '40'] = process.argv.slice(2);
const records = JSON.parse(readFileSync(src, 'utf8'));
const km = (a, b, c, d) => {
  const t = Math.PI / 180;
  const x = Math.sin(((c - a) * t) / 2) ** 2 + Math.cos(a * t) * Math.cos(c * t) * Math.sin(((d - b) * t) / 2) ** 2;
  return 2 * 6371 * Math.asin(Math.sqrt(x));
};
records
  .map((x) => ({ x, d: km(+lat, +lon, x.lat, x.lon) }))
  .filter((o) => o.d <= +r)
  .sort((a, b) => a.d - b.d)
  .slice(0, 12)
  .forEach(({ x, d }) => console.log(`${Math.round(d)}km\t${x.zh || '-'}\t${x.zht || '-'}\t${x.en}\t${x.tz}\t${x.pop}\t${(x.altCjk || []).slice(0, 4).join(',')}`));
