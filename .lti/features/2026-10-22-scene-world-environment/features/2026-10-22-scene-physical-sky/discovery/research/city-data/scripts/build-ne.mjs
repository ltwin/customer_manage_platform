// Natural Earth populated places -> 统一记录格式
// 用法：node build-ne.mjs <ne_10m_populated_places.geojson> <out.json>
import { readFileSync, writeFileSync } from 'node:fs';

const [src, out] = process.argv.slice(2);
if (!src || !out) throw new Error('usage: node build-ne.mjs <geojson> <out.json>');
const fc = JSON.parse(readFileSync(src, 'utf8'));
const s = (v) => (v === null || v === undefined ? '' : String(v).trim());

const records = fc.features.map((f) => {
  const p = f.properties;
  return {
    id: `ne:${p.NE_ID}`,
    wikidata: s(p.WIKIDATAID) || null,
    geonames: p.GEONAMESID && p.GEONAMESID > 0 ? p.GEONAMESID : null,
    zh: s(p.NAME_ZH) || null, // 简体（来源为 Wikidata 标签）
    zht: s(p.NAME_ZHT) || null, // 繁体
    en: s(p.NAME_EN) || s(p.NAME),
    native: s(p.NAME),
    lat: p.LATITUDE,
    lon: p.LONGITUDE,
    tz: s(p.TIMEZONE) || null,
    pop: p.POP_MAX ?? 0,
    cc: s(p.ISO_A2) === '-99' ? null : s(p.ISO_A2) || null,
    country: s(p.ADM0NAME),
    featurecla: s(p.FEATURECLA),
  };
});
writeFileSync(out, JSON.stringify(records));
console.log('records:', records.length, '->', out);
