import assert from 'node:assert/strict';
import {createRequire} from 'node:module';
import {mkdirSync,writeFileSync} from 'node:fs';
const require=createRequire(new URL('../../../../frontend/package.json',import.meta.url));
const {chromium}=require('playwright');
const base=process.argv[2]||'http://127.0.0.1:8770/docs/prototypes/creative-workspace/v4/';
const out='/tmp/creative-v4-collections';mkdirSync(out,{recursive:true});
const browser=await chromium.launch();const report={checks:[],errors:[]};
const pass=message=>{report.checks.push(message);console.log('PASS',message);};
try{
  const page=await browser.newPage({viewport:{width:1440,height:1050}});
  page.on('pageerror',error=>report.errors.push(error.message));
  const go=async(group='')=>{await page.goto(`${base}#view=collections${group?`&group=${group}`:''}`);await page.waitForFunction(()=>document.querySelector('#library-count').textContent.includes('16 份'));if(group)await page.waitForSelector('#gallery:not([hidden])');else await page.waitForSelector('#group-grid:not([hidden])');};
  const count=async n=>{await page.waitForFunction(n=>document.querySelectorAll('.asset-card').length===n,n);};
  await go();
  assert.equal(await page.locator('.rail nav a').count(),3);
  assert.deepEqual(await page.locator('[data-group]').evaluateAll(els=>els.slice(0,2).map(el=>el.dataset.group)),['system-favorites','system-unfiled']);
  await page.waitForTimeout(400);await page.screenshot({path:`${out}/collections-desktop.png`});
  await go('system-favorites');await count(3);assert(await page.locator('#collection-actions').isHidden());
  await page.locator('[data-favorite="asset-1"]').click();await count(2);
  await page.goto(`${base}#view=saved`);await page.waitForFunction(()=>document.querySelector('#page-title').textContent==='我的最爱');await count(2);
  assert.equal(await page.locator('[data-nav="collections"]').getAttribute('aria-current'),'page');
  pass('three navigation sections, fixed system entries, legacy heart URL and persisted favorites');

  await go('system-unfiled');await count(1);assert(await page.locator('[data-open="note-2"]').isVisible());assert(await page.locator('#collection-actions').isHidden());
  await page.locator('[data-favorite="note-2"]').click();await page.waitForFunction(()=>document.querySelector('[data-favorite="note-2"]').getAttribute('aria-pressed')==='true');await count(1);
  await page.locator('[data-open="note-2"]').click();await page.locator('#detail-add').click();await page.locator('#picker-options input[value="p-mono"]').check();await page.locator('#picker-submit').click();await page.waitForFunction(()=>!document.querySelector('#picker').open);await page.locator('[data-close="detail"]').click();await count(1);
  await page.reload();await page.waitForSelector('[data-open="note-2"]');await count(1);
  pass('favoriting and project references leave unfiled status unchanged');

  await page.locator('[data-open="note-2"]').click();await page.locator('#detail-collection').click();
  assert.equal(await page.locator('#picker-options input[value="system-unfiled"]').count(),0);
  assert(await page.locator('#picker-options input[value="system-favorites"]').isDisabled());
  await page.locator('#picker-options input[value="c-light"]').check();await page.locator('#picker-submit').click();await page.waitForFunction(()=>!document.querySelector('#picker').open);await page.locator('[data-close="detail"]').click();await count(0);assert(await page.locator('#empty').isVisible());
  await go('c-light');await count(7);await page.locator('[data-open="note-2"]').click();await page.locator('#detail-remove').click();await page.waitForFunction(()=>!document.querySelector('#detail').open);await count(6);
  await go('system-unfiled');await count(1);
  pass('normal collection classifies; remove restores unfiled without altering favorites/projects');

  await page.goto(`${base}#view=library`);await page.waitForSelector('[data-open="asset-2"]');await page.locator('[data-open="asset-2"]').click();await page.locator('#detail-collection').click();await page.locator('#picker-options input[value="c-light"]').check();await page.locator('#picker-submit').click();await page.waitForFunction(()=>!document.querySelector('#picker').open);await page.locator('[data-close="detail"]').click();
  await go('c-light');await count(7);await page.locator('#rename-collection').click();await page.locator('#group-name').fill('晨光与轮廓');await page.locator('#create-submit').click();await page.waitForFunction(()=>document.querySelector('#page-title').textContent==='晨光与轮廓');
  await page.reload();await page.waitForFunction(()=>document.querySelector('#page-title').textContent==='晨光与轮廓');
  await page.locator('#delete-collection').click();assert((await page.locator('#delete-description').textContent()).includes('7 份素材仍会保留'));assert.equal(await page.evaluate(()=>document.activeElement.textContent),'保留集合');
  await page.keyboard.press('Escape');await count(7);
  await page.locator('#delete-collection').click();await page.locator('#confirm-delete-collection').click();await page.waitForSelector('#group-grid:not([hidden])');assert.equal(await page.locator('[data-group="c-light"]').count(),0);
  await page.reload();await page.waitForSelector('#group-grid:not([hidden])');assert.equal(await page.locator('[data-group]').count(),4);
  await go('system-unfiled');await count(7);assert.equal(await page.locator('[data-open="asset-2"]').count(),0);
  await go('system-favorites');await count(3);assert.equal(await page.locator('[data-open="note-2"]').count(),1);
  await page.goto(`${base}#view=projects&group=p-mono&pane=references`);await page.waitForSelector('#gallery:not([hidden])');await count(5);
  await page.goto(`${base}#view=library`);await page.waitForSelector('#gallery:not([hidden])');await count(16);
  pass('rename, cancel/delete, persistence; assets and project/favorite/other-collection references survive');

  await go('c-light');assert.equal(await page.locator('#page-title').textContent(),'这个集合或项目已不存在');assert(await page.locator('#collection-actions').isHidden());
  await go();await page.setViewportSize({width:375,height:812});await page.waitForTimeout(400);await page.screenshot({path:`${out}/collections-mobile.png`});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
  await page.locator('[data-group="c-sea"]').click();await page.waitForSelector('#collection-actions:not([hidden])');await page.locator('#delete-collection').click();await page.screenshot({path:`${out}/delete-mobile.png`});await page.locator('[data-close="delete-dialog"]').click();
  pass('missing collection recovery and mobile management actions');
  assert.deepEqual(report.errors,[]);writeFileSync(`${out}/report.json`,JSON.stringify(report,null,2));
}finally{await browser.close();}
