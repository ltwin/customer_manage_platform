// 用前端已装的 rolldown（Vite 8 的打包器，minify 用 Oxc）测 astronomy-engine 按需引入后的体积。
// 运行：ASTRONOMY_ENGINE=<astronomy-engine/esm/astronomy.js 的绝对路径> node bundle-size.mjs
// 不写临时文件：入口用虚拟模块，产物留在内存。输出 bundle-size.json。
import { existsSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { gzipSync, brotliCompressSync } from 'node:zlib';

const here = dirname(fileURLToPath(import.meta.url));
const engine = process.env.ASTRONOMY_ENGINE;
if (!engine) throw new Error('set ASTRONOMY_ENGINE to astronomy-engine/esm/astronomy.js');
let dir = here;
while (!existsSync(join(dir, 'frontend/node_modules/rolldown'))) {
  const up = dirname(dir);
  if (up === dir) throw new Error('frontend/node_modules/rolldown not found');
  dir = up;
}
const rolldownPkg = join(dir, 'frontend/node_modules/rolldown');
const { build } = await import(pathToFileURL(join(rolldownPkg, 'dist/index.mjs')).href);
const { version: rolldownVersion } = (await import(pathToFileURL(join(rolldownPkg, 'package.json')).href, { with: { type: 'json' } })).default;

const W2 = ['MakeTime', 'Observer', 'Equator', 'Horizon', 'SearchRiseSet', 'Body'];
const W3 = ['MoonPhase', 'Illumination', 'SiderealTime'];
const ENTRIES = {
  'w2-sun': `import { ${W2.join(', ')} } from 'astronomy-engine'; globalThis.__a = { ${W2.join(', ')} };`,
  'w2+w3': `import { ${[...W2, ...W3].join(', ')} } from 'astronomy-engine'; globalThis.__a = { ${[...W2, ...W3].join(', ')} };`,
  'w2+w3+Rotation_EQJ_HOR': `import { ${[...W2, ...W3, 'Rotation_EQJ_HOR', 'RotateVector', 'HorizonFromVector'].join(', ')} } from 'astronomy-engine'; globalThis.__a = { ${[...W2, ...W3, 'Rotation_EQJ_HOR', 'RotateVector', 'HorizonFromVector'].join(', ')} };`,
  'whole-library': `import * as Astronomy from 'astronomy-engine'; globalThis.__a = Astronomy;`,
};

const results = [];
for (const [name, code] of Object.entries(ENTRIES)) {
  for (const minify of [false, true]) {
    const bundle = await build({
      input: 'entry',
      logLevel: 'silent',
      write: false,
      plugins: [
        {
          name: 'virtual-entry',
          resolveId(id) {
            if (id === 'entry') return '\0entry';
            if (id === 'astronomy-engine') return engine;
            return null;
          },
          load(id) {
            return id === '\0entry' ? code : null;
          },
        },
      ],
      output: { format: 'esm', minify },
    });
    const buf = Buffer.from(bundle.output[0].code);
    results.push({ entry: name, minify, bytes: buf.length, gzip: gzipSync(buf, { level: 9 }).length, brotli: brotliCompressSync(buf).length });
  }
}
const out = { generated: '2026-10-22', astronomyEngineVersion: '2.1.19', bundler: `rolldown ${rolldownVersion}`, results };
writeFileSync(join(here, 'bundle-size.json'), `${JSON.stringify(out, null, 2)}\n`);
console.table(results);
