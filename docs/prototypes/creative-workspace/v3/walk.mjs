// 原型主链自动走查（截图 + 关键断言）。走查前用来确认页面能跑通，不替代真人走查。
// 用法：NODE_PATH=<含 playwright 的 node_modules> node walk.mjs http://127.0.0.1:8765/docs/prototypes/creative-workspace/v3 /tmp/out
import { createRequire } from 'node:module';
import { mkdirSync } from 'node:fs';
const require = createRequire(process.env.NODE_PATH + '/');
const { chromium } = require('playwright');

const base = process.argv[2];
const out = process.argv[3] || '/tmp/proto-v3';
mkdirSync(out, { recursive: true });

const errors = [];
async function run(name, viewport) {
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport, deviceScaleFactor: 2, hasTouch: viewport.width < 500 });
  const page = await ctx.newPage();
  page.on('pageerror', (e) => errors.push(`${name}: ${e.message}`));
  page.on('console', (m) => { if (m.type() === 'error') errors.push(`${name} console: ${m.text()}`); });
  const shot = (n) => page.screenshot({ path: `${out}/${name}-${n}.png`, fullPage: false });

  await page.goto(`${base}/workspaces.html`);
  await page.evaluate(() => { localStorage.clear(); location.reload(); });
  await page.waitForSelector('.ws-row');
  await shot('01-list');
  const rows = await page.locator('.ws-row').count();
  if (rows < 10) errors.push(`${name}: expected >=10 spaces, got ${rows}`);
  if (!(await page.locator('.history-entry').isVisible())) errors.push(`${name}: history entry missing`);
  // 行级操作：归档 → 撤销 → 归档 → 已归档区恢复
  await page.locator('[data-more="ws_11"]').click({ force: true });
  await page.waitForSelector('#rowMenu.open');
  await shot('01b-row-menu');
  await page.click('#rowMenu [data-act="archive"]');
  await page.waitForSelector('.toast.show .undo');
  if (await page.locator('[data-row="ws_11"]').count() !== 1) errors.push(`${name}: archived row should move to archived list`);
  await page.click('.toast .undo');
  await page.waitForTimeout(200);
  if (await page.locator('#list [data-row="ws_11"]').count() !== 1) errors.push(`${name}: undo archive failed`);
  await page.locator('[data-more="ws_11"]').click({ force: true });
  await page.click('#rowMenu [data-act="archive"]');
  await page.waitForTimeout(200);
  await page.click('#archivedToggle');
  await page.waitForSelector('#archivedList [data-restore="ws_11"]');
  await shot('01c-archived');
  await page.click('#archivedList [data-restore="ws_11"]');
  await page.waitForTimeout(200);
  if (await page.locator('#list [data-row="ws_11"]').count() !== 1) errors.push(`${name}: restore failed`);

  // 任务 1：一步创建
  await page.click('#newBtn');
  await page.waitForURL(/workspace\.html\?id=ws_/);
  await page.waitForSelector('#dropzone');
  await shot('02-new-space');
  // 批量粘贴按行成卡
  await page.click('#importBtn');
  await page.fill('#pasteInput', '阿宁：这次想拍暗一点的\n阿宁：像上次那组走廊的感觉但更冷\nhttps://www.xiaohongshu.com/explore/abc\n我：背光加一点烟？');
  await page.click('#pasteGo');
  await page.waitForSelector('.tile');
  let tiles = await page.locator('.tile').count();
  if (tiles !== 4) errors.push(`${name}: paste should yield 4 tiles, got ${tiles}`);
  if ((await page.locator('.tile.is-link').count()) !== 1) errors.push(`${name}: link tile missing`);
  // 拖入图片（用 file chooser 模拟）
  const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==', 'base64');
  await page.click('#importBtn');
  await page.setInputFiles('#fileInput', [
    { name: 'a.png', mimeType: 'image/png', buffer: png },
    { name: 'b.png', mimeType: 'image/png', buffer: png },
    { name: 'c.mp4', mimeType: 'video/mp4', buffer: png },
  ]);
  await page.waitForFunction(() => document.querySelectorAll('.tile').length >= 6, null, { timeout: 5000 });
  tiles = await page.locator('.tile').count();
  if (tiles !== 6) errors.push(`${name}: 2 images should yield 6 tiles, got ${tiles}`);
  await shot('03-wall');
  // 选择两张 → 成组
  await page.click('#selectBtn');
  const t = page.locator('.tile');
  await t.nth(0).click(); await t.nth(1).click();
  await page.click('#selGroup');
  await page.waitForSelector('.group-name');
  await page.fill('.group-name', '第一套');
  await page.locator('.group-name').press('Enter');
  await shot('04-group');
  // 选择两张 → 要拍的画面
  await page.click('#selectBtn');
  await page.locator('.tile').nth(0).click(); await page.locator('.tile').nth(4).click();
  await page.click('#selShoot');
  await page.waitForSelector('#shootSheet.open');
  await page.click('#shEach');
  await page.waitForSelector('.shoot-item');
  const items = await page.locator('.shoot-item').count();
  if (items !== 2) errors.push(`${name}: expected 2 shoot items, got ${items}`);
  await shot('05-shootlist');
  // 备忘
  await page.click('[data-tab="memo"]');
  await page.fill('#memoInput', '备用电池\n反光板\n租的伞记得还');
  await page.click('#memoAdd');
  if ((await page.locator('.memo-item').count()) !== 3) errors.push(`${name}: memo batch paste failed`);
  await shot('06-memo');
  // 现场模式
  await page.click('#liveBtn');
  await page.waitForURL(/live\.html/);
  await page.waitForSelector('#doneBtn');
  await shot('07-live');
  await page.click('#doneBtn');
  await page.waitForTimeout(500);
  await page.click('#skipBtn');
  await page.click('[data-r="光线不对"]');
  await page.click('#skipGo');
  await page.waitForSelector('#endSheet.open');
  await shot('08-live-end');
  await page.click('#endSheet [data-close]');
  await page.click('#memoBtn');
  await page.waitForSelector('#memoSheet.open');
  await shot('09-live-memo');
  await page.click('#memoSheet [data-close]');
  // 断网只读（先展开原型控制条）
  await page.click('.protobar-toggle');
  await page.click('[data-proto="offline"]');
  await page.click('#undoBtn');
  await page.waitForSelector('.toast.warn.show');
  await shot('10-live-offline');

  // 任务 2：未归类
  await page.goto(`${base}/workspace.html?id=ws_inbox`);
  await page.waitForSelector('.tile');
  await shot('11-inbox');
  await page.locator('[data-more]').first().click({ force: true });
  await page.waitForSelector('#tileMenu.open');
  await shot('12-inbox-menu');
  await page.click('#tileMenu [data-act="move"]');
  await page.waitForSelector('#moveSheet.open');
  await page.click('#mvList [data-to="ws_2"]');
  await page.waitForTimeout(300);

  // 任务 3：关联与跳转
  await page.goto(`${base}/workspace.html?id=ws_1`);
  await page.waitForSelector('.link-state');
  await shot('13-linked-space');
  await page.goto(`${base}/workspace.html?id=ws_4`);
  await page.waitForSelector('.link-state.broken');
  await shot('14-broken-link');
  await page.goto(`${base}/workspace.html?id=ws_2`);
  await page.waitForSelector('.group');
  await shot('15-ws2-groups');
  await page.click('#spaceMore');
  await page.waitForSelector('#spaceMenu.open');
  await shot('16-space-menu');

  await browser.close();
}

await run('desktop', { width: 1280, height: 800 });
await run('mobile', { width: 375, height: 812 });
if (errors.length) { console.error(errors.join('\n')); process.exit(1); }
console.log('walkthrough OK →', out);
