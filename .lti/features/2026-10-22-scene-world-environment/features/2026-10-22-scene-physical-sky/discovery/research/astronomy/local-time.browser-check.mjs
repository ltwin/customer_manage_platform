// 在 Chromium 和 WebKit（Playwright 自带）里跑同一组时区用例，确认浏览器 Intl 与 Node 一致。
// 运行：node local-time.browser-check.mjs   （从仓库 frontend/node_modules 找 playwright）
import { readFileSync, writeFileSync, existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { CASES } from './local-time.cases.mjs';

const here = dirname(fileURLToPath(import.meta.url));
let dir = here;
while (!existsSync(join(dir, 'frontend/node_modules/playwright'))) {
  const up = dirname(dir);
  if (up === dir) throw new Error('frontend/node_modules/playwright not found');
  dir = up;
}
const { chromium, webkit } = await import(pathToFileURL(join(dir, 'frontend/node_modules/playwright/index.mjs')).href);

// 把模块源码去掉 export 关键字后注入页面
const src = readFileSync(join(here, 'local-time.mjs'), 'utf8').replace(/^export /gm, '');

const report = {};
for (const [name, type] of [['chromium', chromium], ['webkit', webkit]]) {
  const browser = await type.launch();
  const page = await browser.newPage();
  const out = await page.evaluate(
    ({ src, cases }) => {
      // eslint-disable-next-line no-new-func
      const lib = new Function(`${src}; return { localToUtcMs };`)();
      const rows = cases.map(([tz, date, time, expected]) => {
        let actual;
        try {
          actual = new Date(lib.localToUtcMs(date, time, tz)).toISOString();
        } catch (e) {
          actual = `${e.name}: ${e.message}`;
        }
        return { tz, date, time, expected, actual, pass: actual === expected };
      });
      const invalid = ['Mars/Phobos', 'Local', ''].map((tz) => {
        try {
          new Intl.DateTimeFormat('en-US', { timeZone: tz });
          return { tz, threw: null };
        } catch (e) {
          return { tz, threw: e.name };
        }
      });
      const aliases = ['asia/shanghai', 'Asia/Calcutta', 'Europe/Kiev', 'Europe/Kyiv', 'America/Ciudad_Juarez', '+08:00', 'UTC', 'Etc/GMT-8'].map((tz) => {
        try {
          const f = new Intl.DateTimeFormat('en-US', { timeZone: tz });
          return { tz, ok: true, resolved: f.resolvedOptions().timeZone };
        } catch (e) {
          return { tz, ok: false, error: e.name };
        }
      });
      return { userAgent: navigator.userAgent, rows, invalid, aliases };
    },
    { src, cases: CASES },
  );
  report[name] = { version: browser.version(), ...out };
  await browser.close();
}
report.node = {
  version: process.version,
  icu: process.versions.icu,
  tz: process.versions.tz,
  aliases: ['asia/shanghai', 'Asia/Calcutta', 'Europe/Kiev', 'Europe/Kyiv', 'America/Ciudad_Juarez', '+08:00', 'UTC', 'Etc/GMT-8'].map((tz) => {
    try {
      const f = new Intl.DateTimeFormat('en-US', { timeZone: tz });
      return { tz, ok: true, resolved: f.resolvedOptions().timeZone };
    } catch (e) {
      return { tz, ok: false, error: e.name };
    }
  }),
};
writeFileSync(join(here, 'local-time.browser-results.json'), `${JSON.stringify(report, null, 2)}\n`);
for (const [name, r] of Object.entries(report)) {
  if (!r.rows) continue;
  const failed = r.rows.filter((x) => !x.pass);
  console.log(name, r.version, `pass ${r.rows.length - failed.length}/${r.rows.length}`, failed);
  console.log('  invalid:', JSON.stringify(r.invalid));
  console.log('  aliases:', JSON.stringify(r.aliases));
}
console.log('node aliases:', JSON.stringify(report.node.aliases));
