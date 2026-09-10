import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { mkdirSync, writeFileSync } from 'node:fs';
const require=createRequire(new URL('../../../../frontend/package.json',import.meta.url));
const { chromium }=require('playwright');
const base=process.argv[2] || 'http://127.0.0.1:8770/docs/prototypes/creative-workspace/v4/';
const out=process.argv[3] || '/tmp/creative-v4-qa';
mkdirSync(out,{recursive:true});
const browser=await chromium.launch();
const report={checks:[],pageErrors:[],screenshots:[]};
const pass=name=>{report.checks.push(name);console.log('PASS',name);};
const screenshot=async(page,name)=>{
  // 视觉截图等待图片的 350ms 淡入结束；加载态截图故意保留未完成状态。
  if(!name.includes('loading'))await page.waitForTimeout(400);
  await page.screenshot({path:`${out}/${name}.png`,fullPage:false});report.screenshots.push(name);
};
const boxes=page=>page.locator('.asset-card').evaluateAll(els=>els.map(el=>{const r=el.getBoundingClientRect();return [r.x,r.y,r.width,r.height].map(n=>Math.round(n*100)/100);}));
try {
  const context=await browser.newContext({viewport:{width:1440,height:1050},deviceScaleFactor:1});
  const page=await context.newPage();page.on('pageerror',error=>report.pageErrors.push(error.message));
  let release;
  const gate=new Promise(resolve=>{release=resolve;});
  await page.route('**/assets/*.jpg',async route=>{await gate;await route.continue();});
  await page.goto(base,{waitUntil:'domcontentloaded'});await page.waitForSelector('.asset-card');
  await page.waitForTimeout(200);
  assert.equal(await page.locator('.asset-card').count(),16);
  const lazy=await page.locator('.asset-media img:not([src])').count();assert(lazy>0,'offscreen images must not all have src');
  assert(await page.locator('.asset-media.loading').count()>0);
  const before=await boxes(page);await screenshot(page,'01-loading-placeholders');
  release();await page.waitForFunction(()=>document.querySelectorAll('.asset-media.loaded').length>=8);
  assert.deepEqual(await boxes(page),before,'image loading must not shift any cards');
  const ratios=await page.locator('.asset-media img').evaluateAll(images=>images.map(img=>{const r=img.parentElement.parentElement.getBoundingClientRect();return Math.abs(r.width/r.height-Number(img.getAttribute('width'))/Number(img.getAttribute('height')));}));
  assert(ratios.every(delta=>delta<.002));pass('lazy requests + reserved geometry + original aspect ratio');
  await page.unroute('**/assets/*.jpg');await screenshot(page,'02-desktop-library');
  await page.locator('#about-open').click();await page.locator('#slow-mode').check();await page.locator('[data-close="about"]').click();
  assert.equal(await page.locator('.asset-media.loaded').count(),0);
  await page.waitForFunction(()=>document.querySelectorAll('.asset-media.loaded').length>=8);
  await page.locator('#about-open').click();await page.locator('#slow-mode').uncheck();await page.locator('[data-close="about"]').click();
  await page.evaluate(()=>window.scrollTo(0,document.body.scrollHeight));await page.waitForFunction(()=>document.querySelectorAll('.asset-media.loaded').length===12);await page.evaluate(()=>window.scrollTo(0,0));pass('scroll loads all remaining images');

  await page.locator('#search').fill('布光');await page.waitForFunction(()=>document.querySelectorAll('.asset-card').length===2);
  await page.locator('#search').fill('不存在的灵感789');assert(await page.locator('#empty').isVisible());
  await page.locator('#clear-search').click();assert.equal(await page.locator('.asset-card').count(),16);
  await page.locator('#search').dispatchEvent('compositionstart');
  await page.locator('#search').evaluate(input=>{input.value='布光';input.dispatchEvent(new Event('input',{bubbles:true}));});
  assert.equal(await page.locator('.asset-card').count(),16);
  await page.locator('#search').dispatchEvent('compositionend');assert.equal(await page.locator('.asset-card').count(),2);
  await page.reload();await page.waitForSelector('.asset-card');assert.equal(await page.locator('#search').inputValue(),'布光');assert.equal(await page.locator('.asset-card').count(),2);
  await page.locator('#clear-search').click();pass('search, empty, IME composition and query restoration');

  await page.locator('#filter-toggle').click();await page.locator('#orientation').selectOption('landscape');
  assert.equal(await page.locator('.asset-card').count(),4);
  await page.locator('#reset-filters').click();await page.locator('#filter-toggle').click();
  await page.locator('[data-open="asset-1"]').click();await page.waitForSelector('#detail[open]');
  for(let i=0;i<12;i++){await page.keyboard.press('Tab');assert(await page.evaluate(()=>document.querySelector('#detail').contains(document.activeElement)));}
  await page.waitForFunction(()=>document.querySelector('#detail-image img')?.naturalWidth>0);await screenshot(page,'03-detail');assert(await page.locator('#detail-image').evaluate(el=>{const box=el.getBoundingClientRect(),img=el.querySelector('img').getBoundingClientRect();return img.top>=box.top&&img.bottom<=box.bottom&&img.left>=box.left&&img.right<=box.right;}));
  await page.locator('#detail-note').fill('复用测试笔记：关注光线方向');
  await page.keyboard.press('Escape');assert(await page.locator('#discard-dialog').isVisible());
  await page.locator('#keep-editing').click();assert.equal(await page.locator('#detail-note').inputValue(),'复用测试笔记：关注光线方向');
  await page.locator('#save-note').click();await page.waitForFunction(()=>document.querySelector('#note-status').textContent.includes('已保存在'));
  await page.keyboard.press('Escape');assert.equal(await page.evaluate(()=>document.activeElement.dataset.open),'asset-1');
  await page.reload();await page.waitForSelector('.asset-card');await page.locator('[data-open="asset-1"]').click();assert.equal(await page.locator('#detail-note').inputValue(),'复用测试笔记：关注光线方向');
  await page.locator('#detail-next').click();assert.equal(await page.locator('#detail-title').textContent(),'把风景框进画面');await page.locator('[data-close="detail"]').click();pass('detail, note persistence, discard guard, keyboard and focus return');

  await page.locator('#selection-toggle').click();await page.locator('[data-select="asset-1"]').check();await page.locator('[data-select="asset-2"]').check();
  await page.locator('#bulk-project').click();assert((await page.locator('#picker-description').textContent()).includes('2 份'));
  await page.locator('#picker-options input[value="p-sea"]').check();await page.locator('#picker-submit').click();await page.waitForFunction(()=>!document.querySelector('#picker').open);
  await page.locator('#bulk-project').click();assert(await page.locator('#picker-options input[value="p-sea"]').isDisabled());await page.locator('[data-close="picker"]').click();
  await page.locator('[data-nav="projects"]').click();await page.locator('[data-group="p-sea"]').click();await page.locator('#project-tabs [data-project-tab="references"]').click();await page.waitForSelector('#gallery:not([hidden])');await page.waitForFunction(()=>document.querySelector('#page-title').textContent==='九月，向海而行');assert.equal(await page.locator('.asset-card').count(),6);
  await page.reload();await page.waitForSelector('.asset-card');assert.equal(await page.locator('.asset-card').count(),6);pass('bulk multi-project references are durable and deduplicated');

  await page.locator('[data-nav="collections"]').click();await page.waitForSelector('#group-grid:not([hidden])');await screenshot(page,'04-collections');await page.locator('#create-group').click();
  await page.locator('#create-form button[type="submit"]').click();assert.equal(await page.locator('#group-name').getAttribute('aria-invalid'),'true');
  await page.locator('#group-name').fill('薄雾与柔光');await page.locator('#create-form button[type="submit"]').click();await page.waitForFunction(()=>!document.querySelector('#create-dialog').open);await page.waitForSelector('#empty:not([hidden])');
  await page.locator('[data-nav="collections"]').click();await page.locator('[data-group="system-favorites"]').click();await page.waitForFunction(()=>document.querySelector('#page-title').textContent==='我的最爱');assert.equal(await page.locator('.asset-card').count(),3);
  await page.locator('[data-favorite="asset-1"]').click();await page.waitForFunction(()=>document.querySelectorAll('.asset-card').length===2);
  await page.locator('[data-nav="library"]').click();await page.locator('[data-favorite="asset-1"]').click();await page.waitForFunction(()=>document.querySelector('[data-favorite="asset-1"]').getAttribute('aria-pressed')==='true');pass('create collection, validation, empty collection, heart add/remove');

  await page.locator('#import-open').click();
  await page.locator('#files').setInputFiles({name:'不支持.txt',mimeType:'text/plain',buffer:Buffer.from('test')});assert((await page.locator('#import-error').textContent()).includes('请选择'));
  const png=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==','base64');
  await page.locator('#files').setInputFiles({name:'方形参考.png',mimeType:'image/png',buffer:png});
  await page.locator('[data-file-field="description"]').fill('这是图片自身的说明');
  await page.locator('#import-submit').click();await page.waitForFunction(()=>!document.querySelector('#import-dialog').open);
  await page.locator('#import-open').click();await page.locator('[data-import-kind="text"]').click();await page.locator('#import-text-title').fill('新的拍摄想法');await page.locator('#import-text').fill('一段新的拍摄想法\n保持完整段落');
  await page.locator('#import-submit').click();await page.waitForFunction(()=>!document.querySelector('#import-dialog').open);
  await page.locator('#import-open').click();await page.locator('[data-import-kind="link"]').click();await page.locator('#import-link-url').fill('https://example.com/reference');
  await page.locator('#import-submit').click();await page.waitForFunction(()=>!document.querySelector('#import-dialog').open);await page.waitForFunction(()=>document.querySelectorAll('.asset-card').length===19);assert.equal(await page.locator('.asset-card').count(),19);
  await page.reload();await page.waitForSelector('.asset-card');assert.equal(await page.locator('.asset-card').count(),19);pass('image/text/link import, invalid file recovery, refresh persistence');

  const second=await context.newPage();await second.goto(base);await second.waitForSelector('.asset-card');
  await page.locator('[data-favorite="asset-2"]').click();await page.waitForFunction(()=>document.querySelector('[data-favorite="asset-2"]').getAttribute('aria-pressed')==='true');
  await second.locator('[data-favorite="asset-3"]').click();await second.waitForSelector('#storage-warning:not([hidden])');assert((await second.locator('#storage-warning').textContent()).includes('另一个页面'));await second.close();pass('multi-tab stale writes cannot overwrite saved state');
  await context.setOffline(true);await page.locator('[data-favorite="asset-2"]').click();await page.waitForFunction(()=>document.querySelector('[data-favorite="asset-2"]').getAttribute('aria-pressed')==='false');await context.setOffline(false);pass('local favorites still save offline');

  const failureContext=await browser.newContext({viewport:{width:1280,height:900}});const failure=await failureContext.newPage();failure.on('pageerror',e=>report.pageErrors.push(e.message));
  await failure.route('**/assets/photo-01.jpg',route=>route.abort('failed'));
  await failure.goto(base);await failure.waitForSelector('.image-error');const errorBoxes=await boxes(failure);await screenshot(failure,'05-image-error');
  await failure.unroute('**/assets/photo-01.jpg');await failure.locator('[data-id="asset-1"] .image-error button').click();await failure.waitForSelector('[data-id="asset-1"] .asset-media.loaded');assert.deepEqual(await boxes(failure),errorBoxes);pass('image failure retries in original reserved position');
  await failureContext.close();

  const mobileContext=await browser.newContext({viewport:{width:375,height:812},deviceScaleFactor:1,isMobile:true,hasTouch:true,reducedMotion:'reduce'});
  const mobile=await mobileContext.newPage();mobile.on('pageerror',e=>report.pageErrors.push(e.message));await mobile.goto(base);await mobile.waitForSelector('.asset-media.loaded');
  await screenshot(mobile,'06-mobile-library');
  assert(await mobile.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
  assert.equal(await mobile.locator('#gallery').getAttribute('data-columns'),'2');
  assert.equal(await mobile.locator('.asset-media.loading').first().evaluate(el=>getComputedStyle(el,'::before').animationName),'none');
  await mobile.locator('[data-open="asset-1"]').click();await mobile.waitForFunction(()=>document.querySelector('#detail-image img')?.naturalWidth>0);await screenshot(mobile,'07-mobile-detail');
  await mobile.locator('#detail-note').fill('移动端笔记');await mobile.locator('#save-note').click();await mobile.waitForFunction(()=>document.querySelector('#note-status').textContent.includes('已保存在'));await mobile.locator('[data-close="detail"]').click();
  await mobile.locator('[data-nav="collections"]').click();await mobile.waitForSelector('#group-grid:not([hidden])');await screenshot(mobile,'08-mobile-collections');
  await mobile.locator('[data-nav="library"]').click();await mobile.setViewportSize({width:320,height:640});await mobile.waitForTimeout(200);assert(await mobile.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));pass('375px / 320px responsive, touch, modal actions, reduced motion');
  await mobileContext.close();
  report.glass=await page.locator('.toolbar').evaluate(el=>({backdrop:getComputedStyle(el).backdropFilter,background:getComputedStyle(el).backgroundColor}));
  const session=await context.newCDPSession(page);
  const bindingResult=await session.send('Runtime.evaluate',{expression:`[...document.querySelectorAll('button')].map(b=>{let e=b,bound=false;while(e){if(getEventListeners(e).click?.length){bound=true;break;}e=e.parentElement;}return {id:b.id||b.getAttribute('aria-label')||b.textContent.trim(),bound,submit:!!b.form&&b.type==='submit'};})`,includeCommandLineAPI:true,returnByValue:true});
  report.buttonBindings={count:bindingResult.result.value.length,unbound:bindingResult.result.value.filter(b=>!b.bound&&!b.submit)};
  assert.deepEqual(report.buttonBindings.unbound,[]);
  report.textareaResize=await page.locator('textarea').evaluateAll(els=>els.map(e=>getComputedStyle(e).resize));assert(report.textareaResize.every(v=>v==='none'));
  assert(report.glass.backdrop.includes('blur'));assert.deepEqual(report.pageErrors,[]);
  pass('glass computed style and zero runtime exceptions');
  writeFileSync(`${out}/report.json`,JSON.stringify(report,null,2));console.log(JSON.stringify(report,null,2));
} finally {await browser.close();}
