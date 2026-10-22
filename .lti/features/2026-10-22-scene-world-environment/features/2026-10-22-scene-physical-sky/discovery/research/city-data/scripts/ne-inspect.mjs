// 检查 Natural Earth populated places：字段、填充率、时区有效性、中文名繁简
// 用法：node ne-inspect.mjs <ne_10m_populated_places.geojson>
import { readFileSync } from 'node:fs';

const path = process.argv[2];
if (!path) throw new Error('usage: node ne-inspect.mjs <geojson>');
const fc = JSON.parse(readFileSync(path, 'utf8'));
const feats = fc.features;
console.log('features:', feats.length);

const keys = Object.keys(feats[0].properties);
console.log('property keys (' + keys.length + '):', keys.join(','));

const filled = (v) => v !== null && v !== undefined && String(v).trim() !== '';
const interesting = keys.filter((k) => /^(NAME|NAMEASCII|NAME_EN|NAME_ZH|NAME_ZHT|NAME_JA|TIMEZONE|UN_FIPS|POP_MAX|POP_MIN|ADM0NAME|ISO_A2|ADM0_A3|ADM1NAME|WIKIDATAID|GEONAMEID|LATITUDE|LONGITUDE|FEATURECLA|SCALERANK|LABELRANK|MIN_ZOOM)$/.test(k));
for (const k of interesting) {
  const n = feats.filter((f) => filled(f.properties[k])).length;
  console.log(`${k}: ${n}/${feats.length} (${((n / feats.length) * 100).toFixed(1)}%)`);
}

// 时区有效性
const tzCount = new Map();
for (const f of feats) {
  const tz = f.properties.TIMEZONE;
  if (!filled(tz)) continue;
  tzCount.set(tz, (tzCount.get(tz) || 0) + 1);
}
const isValidTz = (tz) => {
  try {
    new Intl.DateTimeFormat(undefined, { timeZone: tz });
    return true;
  } catch {
    return false;
  }
};
const invalid = [...tzCount].filter(([tz]) => !isValidTz(tz));
console.log('distinct TIMEZONE:', tzCount.size, 'invalid distinct:', invalid.length, 'invalid rows:', invalid.reduce((s, [, n]) => s + n, 0));
console.log('invalid samples:', JSON.stringify(invalid.slice(0, 40)));
// 非 Area/Location 形式（如 UTC、EST）
const odd = [...tzCount].filter(([tz]) => !tz.includes('/'));
console.log('tz without slash:', JSON.stringify(odd));
// 过时别名（Intl 能加载但规范名不同）
const canonDiff = [];
for (const [tz, n] of tzCount) {
  if (!isValidTz(tz)) continue;
  const c = new Intl.DateTimeFormat(undefined, { timeZone: tz }).resolvedOptions().timeZone;
  if (c !== tz) canonDiff.push([tz, c, n]);
}
console.log('alias (resolved differs):', JSON.stringify(canonDiff));

// 时区缺失的样例
const noTz = feats.filter((f) => !filled(f.properties.TIMEZONE));
console.log('no TIMEZONE samples:', noTz.slice(0, 15).map((f) => `${f.properties.NAME}/${f.properties.ADM0NAME}/${f.properties.POP_MAX}`).join('; '));
console.log('no TIMEZONE by country top:', JSON.stringify(Object.entries(noTz.reduce((m, f) => ((m[f.properties.ADM0NAME] = (m[f.properties.ADM0NAME] || 0) + 1), m), {})).sort((a, b) => b[1] - a[1]).slice(0, 15)));

// 中文名样例
const zhSample = ['Beijing', 'Shanghai', 'Guangzhou', 'Taipei', 'Hong Kong', 'Tokyo', 'Kyoto', 'Seoul', 'Paris', 'London', 'Lhasa', 'Urumqi', 'Kashgar', 'Harbin', 'Xiamen', 'Reykjavík', 'Auckland', 'Cape Town', 'Los Angeles', 'New York'];
for (const name of zhSample) {
  const hits = feats.filter((f) => f.properties.NAME === name || f.properties.NAMEASCII === name);
  for (const f of hits.slice(0, 2)) {
    const p = f.properties;
    console.log(`${p.NAME} | ZH=${p.NAME_ZH} | ZHT=${p.NAME_ZHT} | TZ=${p.TIMEZONE} | POP=${p.POP_MAX} | ${p.ADM0NAME}`);
  }
}

// 中国境内条目
const cn = feats.filter((f) => f.properties.ISO_A2 === 'CN' || f.properties.ADM0NAME === 'China');
console.log('China rows:', cn.length, 'with NAME_ZH:', cn.filter((f) => filled(f.properties.NAME_ZH)).length);
const tw = feats.filter((f) => f.properties.ADM0NAME === 'Taiwan');
console.log('Taiwan rows:', tw.length);
