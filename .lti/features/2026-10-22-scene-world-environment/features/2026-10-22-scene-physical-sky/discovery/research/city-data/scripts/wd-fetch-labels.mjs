// 按 GeoNames ID 反查 Wikidata（P1566）的中文标签，结果按批写入 <outdir>/batch-NNN.json
// 用法：node wd-fetch-labels.mjs <gn-records.json> <outdir> [batchSize=400]
// Wikidata 结构化数据为 CC0。查询端点：https://query.wikidata.org/sparql
import { existsSync, readFileSync, writeFileSync } from 'node:fs';

const [src, outdir, bs = '400'] = process.argv.slice(2);
if (!src || !outdir) throw new Error('usage: node wd-fetch-labels.mjs <gn-records.json> <outdir> [batchSize]');
const ids = JSON.parse(readFileSync(src, 'utf8')).map((r) => String(r.geonames));
const size = Number(bs);
const LANGS = ['zh-hans', 'zh-cn', 'zh', 'zh-hant', 'zh-tw', 'zh-hk'];
const vars = LANGS.map((l) => '?' + l.replace('-', '_'));
const opt = LANGS.map((l) => `OPTIONAL { ?item rdfs:label ?${l.replace('-', '_')} FILTER(LANG(?${l.replace('-', '_')}) = "${l}") }`).join(' ');

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let done = 0;
for (let i = 0, b = 0; i < ids.length; i += size, b++) {
  const file = `${outdir}/batch-${String(b).padStart(3, '0')}.json`;
  if (existsSync(file)) {
    done++;
    continue;
  }
  const values = ids
    .slice(i, i + size)
    .map((id) => `"${id}"`)
    .join(' ');
  const query = `SELECT ?gn ?item ${vars.join(' ')} WHERE { VALUES ?gn { ${values} } ?item wdt:P1566 ?gn . ${opt} }`;
  for (let attempt = 0; attempt < 4; attempt++) {
    const res = await fetch('https://query.wikidata.org/sparql', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/x-www-form-urlencoded',
        Accept: 'application/sparql-results+json',
        'User-Agent': 'city-data-research/0.1 (one-off offline gazetteer feasibility probe)',
      },
      body: new URLSearchParams({ query }),
    });
    if (res.ok) {
      writeFileSync(file, await res.text());
      done++;
      break;
    }
    console.error(`batch ${b} attempt ${attempt} status ${res.status}`);
    await sleep(5000 * (attempt + 1));
  }
  await sleep(1000);
}
console.log('batches done:', done, 'of', Math.ceil(ids.length / size));
